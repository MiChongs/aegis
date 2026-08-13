package service

import (
	"context"
	"crypto/rand"
	"errors"
	"math"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	cardkeydomain "aegis/internal/domain/cardkey"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
)

// 卡密相关的业务错误码。
//
// 每一个判据对应一个码，客户端据此分支。400xx 段在本项目里已经用满，
// 参数校验类沿用通用的 40000 / 40082，卡密自身的判定落在 403 / 404 / 409 的空段。
const (
	errCodeCardKeyDisabled     = 40340 // 403
	errCodeCardKeyUsed         = 40341 // 403
	errCodeCardKeyExpired      = 40342 // 403
	errCodeCardKeyDeviceLimit  = 40343 // 403
	errCodeCardKeyBoundOther   = 40344 // 403
	errCodeCardKeyKindMismatch = 40345 // 403
	errCodeCardKeyNoLoginCard  = 40346 // 403
	errCodeCardKeyNotFound     = 40441 // 404
	errCodeCardKeyBatchMissing = 40442 // 404
	errCodeCardKeyConflict     = 40910 // 409
)

// codeCharset 卡面字符集。
//
// 剔掉 I / O / 0 / 1：卡密是**要被人抄写的**（贴在卡片上、发在群里、手打进客户端），
// 而这四个字符在绝大多数字体下两两难辨。剩 32 个字符正好整除 256，
// 于是可以直接对随机字节取模而不引入偏置。
const codeCharset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// 生成规模的上界。
const (
	maxGenerateCount = 10000
	maxBatchNameLen  = 64
)

// CardKeyService 应用级卡密。
//
// 一张卡有两种形态：授权卡（卡即登录凭证）与兑换卡（给已登录用户发权益），
// 共用同一套生成、作废、核销与权益目录。
type CardKeyService struct {
	log *zap.Logger
	pg  *pgrepo.Repository
}

func NewCardKeyService(log *zap.Logger, pg *pgrepo.Repository) *CardKeyService {
	return &CardKeyService{log: log, pg: pg}
}

// ── 生成 ──

// GenerateBatch 生成一批卡密。
func (s *CardKeyService) GenerateBatch(ctx context.Context, input cardkeydomain.GenerateInput) (*cardkeydomain.Batch, error) {
	if err := s.normalizeGenerateInput(&input); err != nil {
		return nil, err
	}

	codes, err := s.mintCodes(ctx, input)
	if err != nil {
		return nil, err
	}

	batch, err := s.pg.CreateCardKeyBatch(ctx, input, codes)
	if err != nil {
		return nil, s.translate(err)
	}
	s.log.Info("card key batch generated",
		zap.Int64("appid", input.AppID), zap.Int64("batchId", batch.ID),
		zap.String("kind", input.Kind), zap.Int("count", len(codes)))
	return batch, nil
}

func (s *CardKeyService) normalizeGenerateInput(input *cardkeydomain.GenerateInput) error {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return apperrors.New(40005, http.StatusBadRequest, "批次名称不能为空")
	}
	if len([]rune(input.Name)) > maxBatchNameLen {
		return apperrors.New(40000, http.StatusBadRequest, "批次名称过长")
	}
	if !cardkeydomain.ValidKind(input.Kind) {
		return apperrors.New(40000, http.StatusBadRequest, "卡密类型只能是 login（授权卡）或 redeem（兑换卡）")
	}
	if input.Count <= 0 || input.Count > maxGenerateCount {
		return apperrors.New(40000, http.StatusBadRequest,
			"单次生成数量需要在 1–10000 之间")
	}

	// 前缀允许全部 A–Z 与 0–9，**不受随机段那条防混淆约束**：
	// 防混淆是为了让人抄得对随机字符，而前缀是运营自己起的可读标签（VIP / GIFT / 2026），
	// 有上下文消歧。照随机段的字符集校验会把 "VIP" 判成非法（I 不在集合里）。
	input.CodePrefix = strings.ToUpper(strings.TrimSpace(input.CodePrefix))
	for _, char := range input.CodePrefix {
		if !isCardCodeRune(char) {
			return apperrors.New(40000, http.StatusBadRequest, "前缀只能使用大写字母与数字")
		}
	}
	if input.Segments <= 0 {
		input.Segments = 4
	}
	if input.SegmentLength <= 0 {
		input.SegmentLength = 4
	}
	if input.Segments > 8 || input.SegmentLength < 3 || input.SegmentLength > 12 {
		return apperrors.New(40000, http.StatusBadRequest, "卡面格式需要在 1–8 段、每段 3–12 位之间")
	}

	// 格式空间不足时提前报错。
	//
	// 不提前算的话，撞码会以「生成到一半失败」的形式出现，而运营看到的是
	// 一个语焉不详的数据库错误。取四分之一是生日悖论下仍能几乎不撞的经验值。
	capacity := math.Pow(float64(len(codeCharset)), float64(input.Segments*input.SegmentLength))
	if float64(input.Count) > capacity/4 {
		return apperrors.New(40000, http.StatusBadRequest,
			"当前卡面格式能表达的组合太少，装不下这个数量；请增加段数或每段位数")
	}

	if !cardkeydomain.ValidValidityMode(input.ValidityMode) {
		input.ValidityMode = cardkeydomain.ValidityPermanent
	}
	switch input.ValidityMode {
	case cardkeydomain.ValidityFromFirstUse:
		if input.ValidityDays <= 0 {
			return apperrors.New(40000, http.StatusBadRequest, "「激活即计时」需要填写有效天数")
		}
		input.ValidUntil = nil
	case cardkeydomain.ValidityFixedUntil:
		if input.ValidUntil == nil || input.ValidUntil.IsZero() {
			return apperrors.New(40000, http.StatusBadRequest, "「统一到期」需要填写到期时间")
		}
		if !input.ValidUntil.After(time.Now()) {
			return apperrors.New(40000, http.StatusBadRequest, "到期时间必须晚于当前时间")
		}
		input.ValidityDays = 0
	default:
		input.ValidityDays = 0
		input.ValidUntil = nil
	}

	if input.Kind == cardkeydomain.KindLogin {
		if input.MaxDevices <= 0 {
			input.MaxDevices = 1
		}
		if input.MaxDevices > 64 {
			return apperrors.New(40000, http.StatusBadRequest, "单卡可绑定设备数最多 64 台")
		}
	} else {
		// 兑换卡不绑设备，把它归一成 1，免得列表上显示一个没有意义的数字。
		input.MaxDevices = 1
	}

	input.Rewards = cardkeydomain.NormalizeRewards(input.Rewards)
	if err := cardkeydomain.ValidateRewards(input.Rewards); err != nil {
		// 授权卡可以不带权益（卡本身就是权益：能登录），兑换卡不带权益就是一张废卡。
		if input.Kind == cardkeydomain.KindLogin && len(input.Rewards) == 0 {
			input.Rewards = []cardkeydomain.Reward{}
		} else {
			return apperrors.New(40000, http.StatusBadRequest, err.Error())
		}
	}
	return nil
}

// mintCodes 生成不重复的卡面。
//
// 两层去重：批内用集合（小格式下生日碰撞是真实的，1 段 3 位只有 32768 种组合），
// 批外查库（同一应用里可能已经存在同样格式的老批次）。
func (s *CardKeyService) mintCodes(ctx context.Context, input cardkeydomain.GenerateInput) ([]string, error) {
	seen := make(map[string]struct{}, input.Count)
	codes := make([]string, 0, input.Count)

	for attempt := 0; attempt < 8 && len(codes) < input.Count; attempt++ {
		batch := make([]string, 0, input.Count-len(codes))
		for len(batch) < cap(batch) {
			code, err := mintCode(input.CodePrefix, input.Segments, input.SegmentLength)
			if err != nil {
				return nil, err
			}
			if _, exists := seen[code]; exists {
				continue
			}
			seen[code] = struct{}{}
			batch = append(batch, code)
		}
		taken, err := s.pg.FilterExistingCardKeyCodes(ctx, input.AppID, batch)
		if err != nil {
			return nil, err
		}
		for _, code := range batch {
			if _, clash := taken[code]; clash {
				delete(seen, code)
				continue
			}
			codes = append(codes, code)
		}
	}
	if len(codes) < input.Count {
		return nil, apperrors.New(50000, http.StatusInternalServerError,
			"卡面生成反复撞码，请增加段数或每段位数后重试")
	}
	return codes, nil
}

// mintCode 生成一张卡面：`PREFIX-XXXX-XXXX-XXXX-XXXX`。
func mintCode(prefix string, segments int, segmentLength int) (string, error) {
	buf := make([]byte, segments*segmentLength)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	var sb strings.Builder
	if prefix != "" {
		sb.WriteString(prefix)
		sb.WriteByte('-')
	}
	for index := range segments {
		if index > 0 {
			sb.WriteByte('-')
		}
		for offset := range segmentLength {
			// len(codeCharset) == 32 整除 256，取模不引入偏置。
			sb.WriteByte(codeCharset[int(buf[index*segmentLength+offset])%len(codeCharset)])
		}
	}
	return sb.String(), nil
}

// ── 管理端 ──

func (s *CardKeyService) ListBatches(ctx context.Context, appID int64) ([]cardkeydomain.Batch, error) {
	items, err := s.pg.ListCardKeyBatches(ctx, appID)
	return items, s.translate(err)
}

func (s *CardKeyService) GetBatch(ctx context.Context, appID int64, batchID int64) (*cardkeydomain.Batch, error) {
	batch, err := s.pg.GetCardKeyBatch(ctx, appID, batchID)
	return batch, s.translate(err)
}

func (s *CardKeyService) SetBatchStatus(ctx context.Context, appID int64, batchID int64, enabled bool) error {
	status := cardkeydomain.BatchDisabled
	if enabled {
		status = cardkeydomain.BatchActive
	}
	return s.translate(s.pg.SetCardKeyBatchStatus(ctx, appID, batchID, status))
}

// DeleteBatch 删除批次。级联带走其下全部卡与核销记录。
func (s *CardKeyService) DeleteBatch(ctx context.Context, appID int64, batchID int64) error {
	return s.translate(s.pg.DeleteCardKeyBatch(ctx, appID, batchID))
}

func (s *CardKeyService) ListCards(ctx context.Context, query cardkeydomain.CardQuery) (*cardkeydomain.CardPage, error) {
	page, err := s.pg.ListCardKeys(ctx, query)
	return page, s.translate(err)
}

func (s *CardKeyService) ExportBatch(ctx context.Context, appID int64, batchID int64) (*cardkeydomain.Batch, []cardkeydomain.Card, error) {
	batch, err := s.pg.GetCardKeyBatch(ctx, appID, batchID)
	if err != nil {
		return nil, nil, s.translate(err)
	}
	cards, err := s.pg.ListCardKeyCodesForExport(ctx, appID, batchID)
	if err != nil {
		return nil, nil, s.translate(err)
	}
	return batch, cards, nil
}

// DisableCards 批量作废。已核销的卡不受影响。
func (s *CardKeyService) DisableCards(ctx context.Context, appID int64, ids []int64, reason string) (int64, error) {
	if len(ids) == 0 {
		return 0, apperrors.New(40005, http.StatusBadRequest, "请先选择要作废的卡密")
	}
	affected, err := s.pg.DisableCardKeys(ctx, appID, ids, strings.TrimSpace(reason))
	return affected, s.translate(err)
}

// RestoreCards 撤销作废。
func (s *CardKeyService) RestoreCards(ctx context.Context, appID int64, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, apperrors.New(40005, http.StatusBadRequest, "请先选择要恢复的卡密")
	}
	affected, err := s.pg.RestoreCardKeys(ctx, appID, ids)
	return affected, s.translate(err)
}

func (s *CardKeyService) ListRedemptions(ctx context.Context, query cardkeydomain.RedemptionQuery) (*cardkeydomain.RedemptionPage, error) {
	page, err := s.pg.ListCardKeyRedemptions(ctx, query)
	return page, s.translate(err)
}

func (s *CardKeyService) ListDevices(ctx context.Context, appID int64, cardID int64) ([]cardkeydomain.Device, error) {
	card, err := s.pg.FindCardKeyByID(ctx, appID, cardID)
	if err != nil {
		return nil, s.translate(err)
	}
	items, err := s.pg.ListCardKeyDevices(ctx, card.ID)
	return items, s.translate(err)
}

// UnbindDevice 解绑一台设备，把名额还回去。
//
// 这是「用户换电脑了」唯一的出口 —— 设备标识会随重装系统变化，
// 没有它，一张一机卡在用户重装之后就永久报废了。
func (s *CardKeyService) UnbindDevice(ctx context.Context, appID int64, cardID int64, deviceID string) error {
	if strings.TrimSpace(deviceID) == "" {
		return apperrors.New(40005, http.StatusBadRequest, "设备标识不能为空")
	}
	return s.translate(s.pg.UnbindCardKeyDevice(ctx, appID, cardID, deviceID))
}

// ── 用户端 ──

// Redeem 兑换一张卡。
func (s *CardKeyService) Redeem(ctx context.Context, input cardkeydomain.RedeemInput) (*cardkeydomain.RedeemResult, error) {
	input.Code = NormalizeCardKeyCode(input.Code)
	if input.Code == "" {
		return nil, apperrors.New(40005, http.StatusBadRequest, "请输入卡密")
	}
	if input.Source == "" {
		input.Source = cardkeydomain.SourceRedeem
	}
	result, err := s.pg.RedeemCardKey(ctx, input)
	if err != nil {
		return nil, s.translate(err)
	}
	return result, nil
}

// MyCards 当前用户名下的授权卡。
func (s *CardKeyService) MyCards(ctx context.Context, appID int64, userID int64) ([]cardkeydomain.Card, error) {
	page, err := s.pg.ListCardKeys(ctx, cardkeydomain.CardQuery{
		AppID:  appID,
		UserID: userID,
		Kind:   cardkeydomain.KindLogin,
		Page:   1,
		Limit:  50,
	})
	if err != nil {
		return nil, s.translate(err)
	}
	return page.Items, nil
}

// ── 登录链路 ──

// PrepareLogin 校验卡面，回答「这张卡能不能登、属于谁」。
//
// 只读：建号发生在认证服务里（那里才有资料、邀请码、搜索同步这些东西），
// 绑定与设备校验发生在 Activate 的事务里。
func (s *CardKeyService) PrepareLogin(ctx context.Context, appID int64, code string) (*cardkeydomain.Card, error) {
	normalized := NormalizeCardKeyCode(code)
	if normalized == "" {
		return nil, apperrors.New(40005, http.StatusBadRequest, "请输入卡密")
	}
	card, err := s.pg.FindCardKeyByCode(ctx, appID, normalized)
	if err != nil {
		return nil, s.translate(err)
	}
	if card.Kind != cardkeydomain.KindLogin {
		return nil, apperrors.New(errCodeCardKeyKindMismatch, http.StatusForbidden,
			"这是一张兑换卡，请登录后在「卡密兑换」里使用")
	}
	if card.Status == cardkeydomain.StatusDisabled {
		return nil, apperrors.New(errCodeCardKeyDisabled, http.StatusForbidden, "该卡密已被作废")
	}
	if card.Expired(time.Now().UTC()) {
		return nil, apperrors.New(errCodeCardKeyExpired, http.StatusForbidden, "该卡密的授权期已结束")
	}
	return card, nil
}

// Activate 绑定设备并（首次激活时）发放随卡权益。
func (s *CardKeyService) Activate(ctx context.Context, input cardkeydomain.ActivateLoginInput) (*cardkeydomain.LoginAuthorization, error) {
	auth, err := s.pg.ActivateLoginCard(ctx, input)
	if err != nil {
		return nil, s.translate(err)
	}
	return auth, nil
}

// NormalizeCardKeyCode 把用户输入的卡面收敛成库里存的形态。
//
// 抄卡密的人会带上空格、用小写、把分隔符打成下划线或空格。
// 不归一化的表现是「明明抄对了却提示卡密不存在」，而这是最没法自查的一类错误。
//
// 保留的是 A–Z 与 0–9 全集，**不是** codeCharset：前缀里可以有 I / O / 0 / 1
// （防混淆只约束随机段），按 codeCharset 过滤会把 `VIP-…` 悄悄削成 `VP-…`。
func NormalizeCardKeyCode(code string) string {
	upper := strings.ToUpper(strings.TrimSpace(code))
	var sb strings.Builder
	for _, char := range upper {
		switch {
		case isCardCodeRune(char):
			sb.WriteRune(char)
		case char == '-' || char == '_' || char == ' ':
			sb.WriteRune('-')
		}
	}
	return sb.String()
}

func isCardCodeRune(char rune) bool {
	return (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')
}

// CardKeyAccount 由卡面派生登录账号。
//
// 账号名就是卡面本身，这不是偷懒：它让「首次使用自动建号」变成一次**确定性**操作。
// 用随机账号名的话，同一张卡的两次并发首登会造出两个账号，其中一个成为
// 谁也够不着的孤儿；用卡面则第二次会撞上 uq_users_appid_account，
// 调用方据此回读同一个用户即可。
func CardKeyAccount(code string) string {
	return code
}

// translate 把仓储层的判定翻成带错误码的业务错误。
func (s *CardKeyService) translate(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, pgrepo.ErrCardKeyNotFound):
		return apperrors.New(errCodeCardKeyNotFound, http.StatusNotFound, "卡密不存在，请核对后重试")
	case errors.Is(err, pgrepo.ErrCardKeyBatchMissing):
		return apperrors.New(errCodeCardKeyBatchMissing, http.StatusNotFound, "卡密批次不存在")
	case errors.Is(err, pgrepo.ErrCardKeyDisabled):
		return apperrors.New(errCodeCardKeyDisabled, http.StatusForbidden, "该卡密已被作废")
	case errors.Is(err, pgrepo.ErrCardKeyUsed):
		return apperrors.New(errCodeCardKeyUsed, http.StatusForbidden, "该卡密已被使用")
	case errors.Is(err, pgrepo.ErrCardKeyExpired):
		return apperrors.New(errCodeCardKeyExpired, http.StatusForbidden, "该卡密已过期")
	case errors.Is(err, pgrepo.ErrCardKeyDeviceLimit):
		return apperrors.New(errCodeCardKeyDeviceLimit, http.StatusForbidden,
			"这张卡可绑定的设备数已满，请在其它设备上退出后重试")
	case errors.Is(err, pgrepo.ErrCardKeyBoundOther):
		return apperrors.New(errCodeCardKeyBoundOther, http.StatusForbidden, "该卡密已绑定其它账号")
	case errors.Is(err, pgrepo.ErrCardKeyKindMismatch):
		return apperrors.New(errCodeCardKeyKindMismatch, http.StatusForbidden, "卡密类型不符")
	case errors.Is(err, pgrepo.ErrCardKeyNoLoginCard):
		return apperrors.New(errCodeCardKeyNoLoginCard, http.StatusForbidden,
			"这张卡要给授权卡加设备位，但你名下没有仍在授权期内的授权卡")
	case pgrepo.IsUniqueViolation(err):
		// card_key_redemptions 上的唯一约束。走到这里说明抢占逻辑被绕过了，
		// 结果是对的（没有重复发放），但值得记一条日志。
		s.log.Warn("card key redemption hit unique constraint", zap.Error(err))
		return apperrors.New(errCodeCardKeyConflict, http.StatusConflict, "该卡密正在被核销，请稍后重试")
	case errors.Is(err, pgrepo.ErrUserNotFound):
		return apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	return err
}
