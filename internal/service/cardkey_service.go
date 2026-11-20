package service

import (
	securitydomain "aegis/internal/domain/security"
	"cmp"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	cardkeydomain "aegis/internal/domain/cardkey"
	notificationdomain "aegis/internal/domain/notification"
	"aegis/internal/domain/pointstore"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
)

// 卡密相关的业务错误码。
//
// 每一个判据对应一个码，客户端据此分支。403 段已经用满，新加的判据落在 409 / 404 / 429 的空段。
const (
	errCodeCardKeyDisabled          = 40340 // 403
	errCodeCardKeyUsed              = 40341 // 403
	errCodeCardKeyExpired           = 40342 // 403
	errCodeCardKeyDeviceLimit       = 40343 // 403
	errCodeCardKeyBoundOther        = 40344 // 403
	errCodeCardKeyKindMismatch      = 40345 // 403
	errCodeCardKeyNoLoginCard       = 40346 // 403
	errCodeCardKeyNotFound          = 40441 // 404
	errCodeCardKeyBatchMissing      = 40442 // 404
	errCodeCardKeyDeviceMissing     = 40458 // 404
	errCodeCardKeyConflict          = 40910 // 409
	errCodeCardKeyNotStarted        = 40975 // 409
	errCodeCardKeyAlreadyRedeemed   = 40976 // 409
	errCodeCardKeyUserLimit         = 40977 // 409
	errCodeCardKeyAudience          = 40978 // 409
	errCodeCardKeyUnbindLimit       = 40979 // 409
	errCodeCardKeyLicensePermanent  = 40981 // 409
	errCodeCardKeyBatchInUse        = 40982 // 409
	errCodeCardKeyRewardOwned       = 40983 // 409
	errCodeCardKeyRewardUnavailable = 40984 // 409
	errCodeCardKeyCodeTaken         = 40985 // 409
	errCodeCardKeyUnbindDisabled    = 40995 // 409
	errCodeCardKeyTooManyAttempts   = 42916 // 429
)

// 生成规模的上界。
const (
	maxGenerateCount    = 10000
	maxBatchNameLen     = 64
	maxChannelLen       = 64
	maxCardRemarkLen    = 256
	maxDistributionNote = 128
	maxFaceValue        = 1_000_000
)

// 防爆破：同一个人 / 同一个 IP 在窗口内查不到卡的次数达到阈值即锁一段时间。
//
// 只数「卡密不存在」：作废、已用、过期这些都说明对方手里确实有一张真卡，
// 数它们只会误伤抄错一位又重试的正常用户，而穷举攻击产生的几乎全是「不存在」。
const (
	cardKeyGuardWindow    = 10 * time.Minute
	cardKeyGuardThreshold = 8
	cardKeyGuardLock      = 15 * time.Minute
)

// cardKeyGuardStore 防爆破计数的存储（Redis 上的 LoginGuardRepository 满足它）。
type cardKeyGuardStore interface {
	IncrFailure(ctx context.Context, kind string, appID int64, subject string, window time.Duration) (int64, error)
	ClearFailure(ctx context.Context, kind string, appID int64, subject string) error
	SetLock(ctx context.Context, kind string, appID int64, subject string, ttl time.Duration) error
	LockRemaining(ctx context.Context, kind string, appID int64, subject string) (time.Duration, error)
}

// CardKeyService 应用级卡密。
//
// 一张卡有两种形态：授权卡（卡即登录凭证）与兑换卡（给已登录用户发权益），
// 共用同一套生成、作废、核销与权益目录。
type CardKeyService struct {
	riskHook
	log      *zap.Logger
	pg       *pgrepo.Repository
	guard    cardKeyGuardStore
	notifier notificationDeliverer
}

func NewCardKeyService(log *zap.Logger, pg *pgrepo.Repository) *CardKeyService {
	return &CardKeyService{log: log, pg: pg}
}

// SetGuard 接上防爆破计数。未接时不限次（单元测试与无 Redis 的部署）。
func (s *CardKeyService) SetGuard(guard cardKeyGuardStore) { s.guard = guard }

// SetNotifier 接上站内信：管理员代兑时通知用户。
func (s *CardKeyService) SetNotifier(notifier notificationDeliverer) { s.notifier = notifier }

// ── 目录 ──

// CardKeyCatalog 控制台的批次编辑表单需要的一切：权益目录、枚举与可引用的套餐 / 功能。
type CardKeyCatalog struct {
	Rewards       []cardkeydomain.RewardSpec `json:"rewards"`
	Kinds         []string                   `json:"kinds"`
	ValidityModes []string                   `json:"validityModes"`
	Charsets      []string                   `json:"charsets"`
	Audiences     []string                   `json:"audiences"`
	MaxRewards    int                        `json:"maxRewards"`
	MaxCount      int                        `json:"maxCount"`
	Plans         []pointstore.PlanRef       `json:"plans"`
	Features      []pointstore.FeatureRef    `json:"features"`
}

func (s *CardKeyService) Catalog(ctx context.Context, appID int64) (*CardKeyCatalog, error) {
	plans, features, err := s.pg.PointsStoreCatalogRefs(ctx, appID)
	if err != nil {
		return nil, err
	}
	out := &CardKeyCatalog{
		Rewards:       cardkeydomain.RewardCatalog(),
		Kinds:         cardkeydomain.Kinds(),
		ValidityModes: cardkeydomain.ValidityModes(),
		Charsets:      cardkeydomain.Charsets(),
		Audiences:     cardkeydomain.Audiences(),
		MaxRewards:    cardkeydomain.MaxRewardsPerCard,
		MaxCount:      maxGenerateCount,
		Plans:         make([]pointstore.PlanRef, 0, len(plans)),
		Features:      make([]pointstore.FeatureRef, 0, len(features)),
	}
	for _, plan := range plans {
		out.Plans = append(out.Plans, plan)
	}
	for _, feature := range features {
		out.Features = append(out.Features, feature)
	}
	slices.SortFunc(out.Plans, func(a, b pointstore.PlanRef) int { return cmp.Compare(a.ID, b.ID) })
	slices.SortFunc(out.Features, func(a, b pointstore.FeatureRef) int { return cmp.Compare(a.Tag, b.Tag) })
	return out, nil
}

// ── 生成 ──

// GenerateBatch 生成（或导入）一批卡密。
func (s *CardKeyService) GenerateBatch(ctx context.Context, input cardkeydomain.GenerateInput) (*cardkeydomain.Batch, error) {
	if err := s.normalizeGenerateInput(&input); err != nil {
		return nil, err
	}
	if err := s.checkRewardRefs(ctx, input.AppID, input.Rewards); err != nil {
		return nil, err
	}

	var codes []string
	var err error
	if input.Origin == cardkeydomain.OriginImported {
		codes, err = s.prepareImportedCodes(ctx, input.AppID, input.Codes)
	} else {
		codes, err = s.mintCodes(ctx, input.AppID, input.CodePrefix, input.Segments, input.SegmentLength,
			input.Charset, input.Count)
	}
	if err != nil {
		return nil, err
	}

	batch, err := s.pg.CreateCardKeyBatch(ctx, input, codes)
	if err != nil {
		return nil, s.translate(err)
	}
	s.log.Info("card key batch generated",
		zap.Int64("appid", input.AppID), zap.Int64("batchId", batch.ID),
		zap.String("kind", input.Kind), zap.String("origin", input.Origin), zap.Int("count", len(codes)))
	return s.GetBatch(ctx, input.AppID, batch.ID)
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

	input.Origin = cardkeydomain.OriginGenerated
	if len(input.Codes) > 0 {
		input.Origin = cardkeydomain.OriginImported
		if len(input.Codes) > maxGenerateCount {
			return apperrors.New(40000, http.StatusBadRequest, "单次最多导入 10000 张卡密")
		}
	} else if input.Count <= 0 || input.Count > maxGenerateCount {
		return apperrors.New(40000, http.StatusBadRequest, "单次生成数量需要在 1–10000 之间")
	}

	// 前缀允许全部 A–Z 与 0–9，**不受随机段那条防混淆约束**：
	// 防混淆是为了让人抄得对随机字符，而前缀是运营自己起的可读标签（VIP / GIFT / 2026），
	// 有上下文消歧。照随机段的字符集校验会把 "VIP" 判成非法（I 不在集合里）。
	input.CodePrefix = strings.ToUpper(strings.TrimSpace(input.CodePrefix))
	if len(input.CodePrefix) > 16 {
		return apperrors.New(40000, http.StatusBadRequest, "前缀最多 16 个字符")
	}
	for _, char := range input.CodePrefix {
		if !cardkeydomain.IsCodeRune(char) {
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
	if input.Charset == "" {
		input.Charset = cardkeydomain.CharsetSafe
	}
	if !cardkeydomain.ValidCharset(input.Charset) {
		return apperrors.New(40000, http.StatusBadRequest, "未知的卡面字符集")
	}
	if input.Origin == cardkeydomain.OriginGenerated {
		if err := checkCodeCapacity(input.Charset, input.Segments, input.SegmentLength, input.Count); err != nil {
			return err
		}
	}

	if !cardkeydomain.ValidValidityMode(input.ValidityMode) {
		input.ValidityMode = cardkeydomain.ValidityPermanent
	}
	switch input.ValidityMode {
	case cardkeydomain.ValidityFromFirstUse:
		if input.ValidityDays <= 0 || input.ValidityDays > 36500 {
			return apperrors.New(40000, http.StatusBadRequest, "「激活即计时」需要填写 1–36500 之间的有效天数")
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
		// 授权卡就是一个账号，不存在「多人共用一张授权卡」。
		input.MaxUses = 1
	} else {
		// 兑换卡不绑设备，把它归一成 1，免得列表上显示一个没有意义的数字。
		input.MaxDevices = 1
		input.SelfUnbindLimit = 0
		if input.MaxUses <= 0 {
			input.MaxUses = 1
		}
		if input.MaxUses > 1_000_000 {
			return apperrors.New(40000, http.StatusBadRequest, "单张卡最多可被 1000000 人使用")
		}
	}

	common := cardKeyRules{
		Name: input.Name, Channel: input.Channel, FaceValue: input.FaceValue, PerUserLimit: input.PerUserLimit,
		Audience: input.Audience, SelfUnbindLimit: input.SelfUnbindLimit, StartsAt: input.StartsAt,
	}
	if err := normalizeCardKeyRules(&common); err != nil {
		return err
	}
	input.Channel, input.FaceValue, input.PerUserLimit = common.Channel, common.FaceValue, common.PerUserLimit
	input.Audience, input.SelfUnbindLimit, input.StartsAt = common.Audience, common.SelfUnbindLimit, common.StartsAt
	if input.Kind == cardkeydomain.KindLogin {
		// 授权卡首登时还没有账号，「每人限兑」「仅会员」无从判起；它们只对登录后的绑定生效没有意义。
		input.PerUserLimit = 0
		input.Audience = cardkeydomain.AudienceAll
	}
	input.Remark = strings.TrimSpace(input.Remark)

	rewards, err := normalizeCardKeyRewards(input.Kind, input.Rewards)
	if err != nil {
		return err
	}
	input.Rewards = rewards
	return nil
}

// cardKeyRules 生成与修改共用的那部分规则。
type cardKeyRules struct {
	Name            string
	Channel         string
	FaceValue       decimal.Decimal
	PerUserLimit    int
	Audience        string
	SelfUnbindLimit int
	StartsAt        *time.Time
}

func normalizeCardKeyRules(rules *cardKeyRules) error {
	rules.Channel = strings.TrimSpace(rules.Channel)
	if len([]rune(rules.Channel)) > maxChannelLen {
		return apperrors.New(40000, http.StatusBadRequest, "渠道名称过长")
	}
	if rules.FaceValue.IsNegative() {
		return apperrors.New(40000, http.StatusBadRequest, "面值不能为负数")
	}
	if rules.FaceValue.GreaterThan(decimal.NewFromInt(maxFaceValue)) {
		return apperrors.New(40000, http.StatusBadRequest, "面值过大")
	}
	rules.FaceValue = rules.FaceValue.Round(2)
	if rules.PerUserLimit < 0 || rules.PerUserLimit > 100000 {
		return apperrors.New(40000, http.StatusBadRequest, "每人限兑张数需要在 0–100000 之间（0 为不限）")
	}
	if rules.Audience == "" {
		rules.Audience = cardkeydomain.AudienceAll
	}
	if !cardkeydomain.ValidAudience(rules.Audience) {
		return apperrors.New(40000, http.StatusBadRequest, "领取对象只能是 all / vip / non_vip")
	}
	if rules.SelfUnbindLimit < 0 || rules.SelfUnbindLimit > 100 {
		return apperrors.New(40000, http.StatusBadRequest, "自助解绑次数需要在 0–100 之间")
	}
	if rules.StartsAt != nil && rules.StartsAt.IsZero() {
		rules.StartsAt = nil
	}
	return nil
}

// normalizeCardKeyRewards 归一并校验权益清单的形状。
func normalizeCardKeyRewards(kind string, input []cardkeydomain.Reward) ([]cardkeydomain.Reward, error) {
	rewards := cardkeydomain.NormalizeRewards(input)
	if len(rewards) == 0 && kind == cardkeydomain.KindLogin {
		// 授权卡可以不带权益（卡本身就是权益：能登录），兑换卡不带权益就是一张废卡。
		return []cardkeydomain.Reward{}, nil
	}
	if err := cardkeydomain.ValidateRewards(rewards); err != nil {
		return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	return rewards, nil
}

// checkRewardRefs 核对权益引用的套餐与功能确实存在。
//
// 引用核对放在生成时：一张卡挂着一个不存在的套餐，要等到第一个用户来兑换时
// 才以「兑换失败」的形式暴露，而那时卡已经发出去了。
func (s *CardKeyService) checkRewardRefs(ctx context.Context, appID int64, rewards []cardkeydomain.Reward) error {
	if len(rewards) == 0 {
		return nil
	}
	plans, features, err := s.pg.PointsStoreCatalogRefs(ctx, appID)
	if err != nil {
		return err
	}
	for _, reward := range rewards {
		switch reward.Type {
		case cardkeydomain.RewardVipPlan:
			if _, ok := plans[reward.RefID]; !ok {
				return apperrors.New(40000, http.StatusBadRequest, "选择的会员套餐不存在")
			}
		case cardkeydomain.RewardFeature:
			feature, ok := features[reward.Feature]
			if !ok {
				return apperrors.New(40000, http.StatusBadRequest, "功能「"+reward.Feature+"」不在功能目录里")
			}
			if !feature.IsActive {
				return apperrors.New(40000, http.StatusBadRequest, "功能「"+feature.Name+"」已停用")
			}
		case cardkeydomain.RewardCosmetic:
			if err := checkCosmeticRef(ctx, s.pg, appID, reward.RefID); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkCodeCapacity 格式空间不足时提前报错。
//
// 不提前算的话，撞码会以「生成到一半失败」的形式出现，而运营看到的是
// 一个语焉不详的数据库错误。取四分之一是生日悖论下仍能几乎不撞的经验值。
func checkCodeCapacity(charset string, segments int, segmentLength int, count int) error {
	alphabet := float64(len(cardkeydomain.Alphabet(charset)))
	capacity := math.Pow(alphabet, float64(segments*segmentLength))
	if float64(count) > capacity/4 {
		return apperrors.New(40000, http.StatusBadRequest,
			"当前卡面格式能表达的组合太少，装不下这个数量；请增加段数或每段位数")
	}
	return nil
}

// prepareImportedCodes 归一化、去重并核对导入的卡面。
func (s *CardKeyService) prepareImportedCodes(ctx context.Context, appID int64, raw []string) ([]string, error) {
	seen := make(map[string]struct{}, len(raw))
	codes := make([]string, 0, len(raw))
	duplicated := make([]string, 0)
	for _, item := range raw {
		code := cardkeydomain.NormalizeCode(item)
		if code == "" {
			continue
		}
		if err := cardkeydomain.ValidateImportedCode(code); err != nil {
			return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
		}
		if _, exists := seen[code]; exists {
			if len(duplicated) < 5 {
				duplicated = append(duplicated, code)
			}
			continue
		}
		seen[code] = struct{}{}
		codes = append(codes, code)
	}
	if len(duplicated) > 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "导入的卡面里有重复："+strings.Join(duplicated, "、"))
	}
	if len(codes) == 0 {
		return nil, apperrors.New(40005, http.StatusBadRequest, "没有可导入的卡面")
	}
	taken, err := s.pg.FilterExistingCardKeyCodes(ctx, appID, codes)
	if err != nil {
		return nil, err
	}
	if len(taken) > 0 {
		clash := make([]string, 0, 5)
		for _, code := range codes {
			if _, ok := taken[code]; ok {
				clash = append(clash, code)
				if len(clash) == 5 {
					break
				}
			}
		}
		return nil, s.translate(&pgrepo.CardKeyCodeTakenError{Codes: clash})
	}
	return codes, nil
}

// mintCodes 生成不重复的卡面。
//
// 两层去重：批内用集合（小格式下生日碰撞是真实的，1 段 4 位纯数字只有一万种组合），
// 批外查库（同一应用里可能已经存在同样格式的老批次）。
func (s *CardKeyService) mintCodes(ctx context.Context, appID int64, prefix string, segments int,
	segmentLength int, charset string, count int) ([]string, error) {
	seen := make(map[string]struct{}, count)
	codes := make([]string, 0, count)
	alphabet := cardkeydomain.Alphabet(charset)

	for attempt := 0; attempt < 8 && len(codes) < count; attempt++ {
		batch := make([]string, 0, count-len(codes))
		for len(batch) < cap(batch) {
			code, err := mintCode(prefix, segments, segmentLength, alphabet)
			if err != nil {
				return nil, err
			}
			if _, exists := seen[code]; exists {
				continue
			}
			seen[code] = struct{}{}
			batch = append(batch, code)
		}
		taken, err := s.pg.FilterExistingCardKeyCodes(ctx, appID, batch)
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
	if len(codes) < count {
		return nil, apperrors.New(50000, http.StatusInternalServerError,
			"卡面生成反复撞码，请增加段数或每段位数后重试")
	}
	return codes, nil
}

// mintCode 生成一张卡面：`PREFIX-XXXX-XXXX-XXXX-XXXX`。
//
// 32 个字符的 safe 集整除 256，可以直接取模；10 / 24 个字符的集合不整除，
// 直接取模会让前几个字符出现得更频繁（偏置），这里用拒绝采样：落在最大整倍数之外的字节丢掉重抽。
func mintCode(prefix string, segments int, segmentLength int, alphabet string) (string, error) {
	size := len(alphabet)
	limit := 256 - 256%size
	total := segments * segmentLength
	chars := make([]byte, 0, total)
	buf := make([]byte, total*2)
	for len(chars) < total {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		for _, value := range buf {
			if int(value) >= limit {
				continue
			}
			chars = append(chars, alphabet[int(value)%size])
			if len(chars) == total {
				break
			}
		}
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
		sb.Write(chars[index*segmentLength : (index+1)*segmentLength])
	}
	return sb.String(), nil
}

// AppendBatch 按原批次的卡面格式补发。
func (s *CardKeyService) AppendBatch(ctx context.Context, input cardkeydomain.AppendInput) (*cardkeydomain.Batch, error) {
	if input.Count <= 0 || input.Count > maxGenerateCount {
		return nil, apperrors.New(40000, http.StatusBadRequest, "单次补发数量需要在 1–10000 之间")
	}
	batch, err := s.pg.GetCardKeyBatch(ctx, input.AppID, input.BatchID)
	if err != nil {
		return nil, s.translate(err)
	}
	if batch.Origin == cardkeydomain.OriginImported {
		return nil, apperrors.New(40000, http.StatusBadRequest, "导入的批次没有卡面格式，不能补发；请重新导入一批")
	}
	if err := checkCodeCapacity(batch.Charset, batch.Segments, batch.SegmentLength, batch.Total+input.Count); err != nil {
		return nil, err
	}
	codes, err := s.mintCodes(ctx, input.AppID, batch.CodePrefix, batch.Segments, batch.SegmentLength,
		batch.Charset, input.Count)
	if err != nil {
		return nil, err
	}
	if _, err := s.pg.AppendCardKeys(ctx, input.AppID, input.BatchID, codes); err != nil {
		return nil, s.translate(err)
	}
	s.log.Info("card key batch appended", zap.Int64("appid", input.AppID), zap.Int64("batchId", input.BatchID),
		zap.Int("count", len(codes)), zap.String("operator", input.Operator))
	return s.GetBatch(ctx, input.AppID, input.BatchID)
}

// UpdateBatch 修改批次的名称、渠道、权益与领取规则。
func (s *CardKeyService) UpdateBatch(ctx context.Context, input cardkeydomain.BatchUpdateInput) (*cardkeydomain.Batch, error) {
	current, err := s.pg.GetCardKeyBatch(ctx, input.AppID, input.BatchID)
	if err != nil {
		return nil, s.translate(err)
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return nil, apperrors.New(40005, http.StatusBadRequest, "批次名称不能为空")
	}
	if len([]rune(input.Name)) > maxBatchNameLen {
		return nil, apperrors.New(40000, http.StatusBadRequest, "批次名称过长")
	}
	rules := cardKeyRules{
		Name: input.Name, Channel: input.Channel, FaceValue: input.FaceValue, PerUserLimit: input.PerUserLimit,
		Audience: input.Audience, SelfUnbindLimit: input.SelfUnbindLimit, StartsAt: input.StartsAt,
	}
	if err := normalizeCardKeyRules(&rules); err != nil {
		return nil, err
	}
	input.Channel, input.FaceValue, input.PerUserLimit = rules.Channel, rules.FaceValue, rules.PerUserLimit
	input.Audience, input.SelfUnbindLimit, input.StartsAt = rules.Audience, rules.SelfUnbindLimit, rules.StartsAt
	if current.Kind == cardkeydomain.KindLogin {
		input.PerUserLimit = 0
		input.Audience = cardkeydomain.AudienceAll
	} else {
		input.SelfUnbindLimit = 0
	}
	input.Remark = strings.TrimSpace(input.Remark)

	if current.ValidityMode == cardkeydomain.ValidityFixedUntil && input.ValidUntil != nil {
		if !input.ValidUntil.After(time.Now()) {
			return nil, apperrors.New(40000, http.StatusBadRequest, "到期时间必须晚于当前时间")
		}
	} else {
		input.ValidUntil = nil
	}

	rewards, err := normalizeCardKeyRewards(current.Kind, input.Rewards)
	if err != nil {
		return nil, err
	}
	if err := s.checkRewardRefs(ctx, input.AppID, rewards); err != nil {
		return nil, err
	}
	input.Rewards = rewards

	if _, err := s.pg.UpdateCardKeyBatch(ctx, input); err != nil {
		return nil, s.translate(err)
	}
	return s.GetBatch(ctx, input.AppID, input.BatchID)
}

// ── 管理端 ──

func (s *CardKeyService) ListBatches(ctx context.Context, appID int64, keyword string, kind string) ([]cardkeydomain.Batch, error) {
	if kind != "" && !cardkeydomain.ValidKind(kind) {
		kind = ""
	}
	items, err := s.pg.ListCardKeyBatches(ctx, appID, keyword, kind)
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

// DeleteBatch 删除批次。只有从没被用过的批次能删（有核销记录的只能停用）。
func (s *CardKeyService) DeleteBatch(ctx context.Context, appID int64, batchID int64) error {
	return s.translate(s.pg.DeleteCardKeyBatch(ctx, appID, batchID))
}

// ExtractCards 提卡：取出一些还没交给渠道的未使用卡并打上标记。
func (s *CardKeyService) ExtractCards(ctx context.Context, input cardkeydomain.ExtractInput) (*cardkeydomain.Batch, []cardkeydomain.Card, error) {
	if input.Count <= 0 || input.Count > maxGenerateCount {
		return nil, nil, apperrors.New(40000, http.StatusBadRequest, "单次提取数量需要在 1–10000 之间")
	}
	input.Note = strings.TrimSpace(input.Note)
	if len([]rune(input.Note)) > maxDistributionNote {
		return nil, nil, apperrors.New(40000, http.StatusBadRequest, "提取备注过长")
	}
	batch, err := s.pg.GetCardKeyBatch(ctx, input.AppID, input.BatchID)
	if err != nil {
		return nil, nil, s.translate(err)
	}
	if batch.Status != cardkeydomain.BatchActive {
		return nil, nil, apperrors.New(errCodeCardKeyDisabled, http.StatusForbidden, "批次已停用，不能提卡")
	}
	if batch.Stats == nil || batch.Stats.Available < int64(input.Count) {
		available := int64(0)
		if batch.Stats != nil {
			available = batch.Stats.Available
		}
		return nil, nil, apperrors.New(40000, http.StatusBadRequest,
			fmt.Sprintf("这一批只剩 %d 张可提取的卡，请先补发", available))
	}
	cards, err := s.pg.ExtractCardKeys(ctx, input)
	if err != nil {
		return nil, nil, s.translate(err)
	}
	return batch, cards, nil
}

// SetDistributed 批量标记 / 取消「已提取」。
func (s *CardKeyService) SetDistributed(ctx context.Context, appID int64, ids []int64, distributed bool, note string) (int64, error) {
	if len(ids) == 0 {
		return 0, apperrors.New(40005, http.StatusBadRequest, "请先选择卡密")
	}
	note = strings.TrimSpace(note)
	if len([]rune(note)) > maxDistributionNote {
		return 0, apperrors.New(40000, http.StatusBadRequest, "提取备注过长")
	}
	affected, err := s.pg.SetCardKeysDistributed(ctx, appID, ids, distributed, note)
	return affected, s.translate(err)
}

func (s *CardKeyService) ListCards(ctx context.Context, query cardkeydomain.CardQuery) (*cardkeydomain.CardPage, error) {
	page, err := s.pg.ListCardKeys(ctx, query)
	return page, s.translate(err)
}

func (s *CardKeyService) GetCardDetail(ctx context.Context, appID int64, cardID int64) (*cardkeydomain.CardDetail, error) {
	detail, err := s.pg.GetCardKeyDetail(ctx, appID, cardID)
	return detail, s.translate(err)
}

// UpdateCard 调整单卡：备注、可绑设备数、到期时间。
func (s *CardKeyService) UpdateCard(ctx context.Context, input cardkeydomain.CardUpdateInput) (*cardkeydomain.Card, error) {
	if input.Remark != nil {
		trimmed := strings.TrimSpace(*input.Remark)
		if len([]rune(trimmed)) > maxCardRemarkLen {
			return nil, apperrors.New(40000, http.StatusBadRequest, "备注过长")
		}
		input.Remark = &trimmed
	}
	if input.MaxDevices != nil && (*input.MaxDevices < 1 || *input.MaxDevices > 64) {
		return nil, apperrors.New(40000, http.StatusBadRequest, "可绑定设备数需要在 1–64 之间")
	}
	switch input.ExpiryAction {
	case "":
	case cardkeydomain.ExpiryExtend:
		if input.ExtendDays == 0 || input.ExtendDays < -36500 || input.ExtendDays > 36500 {
			return nil, apperrors.New(40000, http.StatusBadRequest, "延长天数需要在 -36500–36500 之间且不为 0")
		}
	case cardkeydomain.ExpirySet:
		if input.ExpiresAt == nil || input.ExpiresAt.IsZero() {
			return nil, apperrors.New(40000, http.StatusBadRequest, "请填写到期时间")
		}
	case cardkeydomain.ExpiryPermanent:
	default:
		return nil, apperrors.New(40000, http.StatusBadRequest, "未知的到期时间调整方式")
	}
	card, err := s.pg.UpdateCardKey(ctx, input)
	return card, s.translate(err)
}

// DisableCards 批量作废。已核销的卡不受影响。
func (s *CardKeyService) DisableCards(ctx context.Context, appID int64, ids []int64, reason string) (int64, error) {
	if len(ids) == 0 {
		return 0, apperrors.New(40005, http.StatusBadRequest, "请先选择要作废的卡密")
	}
	affected, err := s.pg.DisableCardKeys(ctx, appID, ids, truncateRunes(strings.TrimSpace(reason), 256))
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

// DeleteCards 删除从没被用过的卡。被用过的跳过（返回实际删除数）。
func (s *CardKeyService) DeleteCards(ctx context.Context, appID int64, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, apperrors.New(40005, http.StatusBadRequest, "请先选择要删除的卡密")
	}
	affected, err := s.pg.DeleteCardKeys(ctx, appID, ids)
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

// UnbindDevice 管理员解绑一台设备，把名额还回去。
//
// 这是「用户换电脑了」的兜底出口 —— 设备标识会随重装系统变化，
// 没有它，一张一机卡在用户重装之后就永久报废了。
func (s *CardKeyService) UnbindDevice(ctx context.Context, appID int64, cardID int64, deviceID string, operator string) error {
	if strings.TrimSpace(deviceID) == "" {
		return apperrors.New(40005, http.StatusBadRequest, "设备标识不能为空")
	}
	return s.translate(s.pg.UnbindCardKeyDevice(ctx, cardkeydomain.UnbindInput{
		AppID: appID, CardID: cardID, DeviceID: deviceID, Source: cardkeydomain.UnbindAdmin, Operator: operator,
	}))
}

// ResetDevices 清空一张卡的全部设备。
func (s *CardKeyService) ResetDevices(ctx context.Context, appID int64, cardID int64, operator string) (int64, error) {
	affected, err := s.pg.ResetCardKeyDevices(ctx, appID, cardID, operator)
	return affected, s.translate(err)
}

// AdminRedeem 管理员代兑：替用户核销一张卡（客服补发、线下活动）。
func (s *CardKeyService) AdminRedeem(ctx context.Context, appID int64, code string, userID int64, account string, operator string) (*cardkeydomain.RedeemResult, error) {
	if account = strings.TrimSpace(account); userID <= 0 && account != "" {
		user, err := s.pg.GetUserByAppAndAccount(ctx, appID, account)
		if err != nil {
			return nil, err
		}
		if user == nil {
			return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
		}
		userID = user.ID
	}
	if userID <= 0 {
		return nil, apperrors.New(40005, http.StatusBadRequest, "请选择要代兑的用户")
	}
	user, err := s.pg.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil || user.AppID != appID {
		return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	result, err := s.redeem(ctx, cardkeydomain.RedeemInput{
		AppID: appID, Code: code, UserID: userID, Source: cardkeydomain.SourceAdmin, Operator: operator,
	})
	if err != nil {
		return nil, err
	}
	s.notifyRedeemed(appID, userID, result, "管理员已为你兑换卡密")
	return result, nil
}

// Stats 卡密概览。days 为趋势天数，timezone 为分日时区（控制台传浏览器时区）。
func (s *CardKeyService) Stats(ctx context.Context, appID int64, days int, timezone string) (*cardkeydomain.Stats, error) {
	if days <= 0 || days > 180 {
		days = 30
	}
	loc := cardKeyStatsLocation(timezone)
	now := time.Now().In(loc)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	since := dayStart.AddDate(0, 0, -(days - 1))
	stats, err := s.pg.CardKeyStats(ctx, appID, since.UTC(), dayStart.UTC(), loc.String())
	if err != nil {
		return nil, s.translate(err)
	}
	stats.Trend = fillCardKeyTrend(stats.Trend, since, days)
	return stats, nil
}

// cardKeyStatsLocation 分日用的时区。
//
// 不能回落到 time.Local：它的名字是 "Local"，原样交给 Postgres 的 AT TIME ZONE 会报「不认识的时区」。
// 没传或传错时用 Asia/Shanghai（与 PG 会话的默认时区一致），连它也加载不了才用 UTC。
func cardKeyStatsLocation(timezone string) *time.Location {
	if name := strings.TrimSpace(timezone); name != "" && name != "Local" {
		if loc, err := time.LoadLocation(name); err == nil {
			return loc
		}
	}
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		return loc
	}
	return time.UTC
}

// fillCardKeyTrend 补齐没有核销的日子：趋势图上缺一天和那天是 0 是两回事。
func fillCardKeyTrend(points []cardkeydomain.TrendPoint, since time.Time, days int) []cardkeydomain.TrendPoint {
	byDay := make(map[string]cardkeydomain.TrendPoint, len(points))
	for _, point := range points {
		byDay[point.Day] = point
	}
	out := make([]cardkeydomain.TrendPoint, 0, days)
	for index := range days {
		day := since.AddDate(0, 0, index).Format("2006-01-02")
		if point, ok := byDay[day]; ok {
			out = append(out, point)
		} else {
			out = append(out, cardkeydomain.TrendPoint{Day: day})
		}
	}
	return out
}

func (s *CardKeyService) Export(ctx context.Context, query cardkeydomain.ExportQuery) (*cardkeydomain.Batch, []cardkeydomain.Card, error) {
	batch, err := s.pg.GetCardKeyBatch(ctx, query.AppID, query.BatchID)
	if err != nil {
		return nil, nil, s.translate(err)
	}
	cards, err := s.pg.ListCardKeysForExport(ctx, query)
	if err != nil {
		return nil, nil, s.translate(err)
	}
	return batch, cards, nil
}

// ── 用户端 ──

// CardKeyClientContext 用户端请求的上下文：谁、从哪来。
type CardKeyClientContext struct {
	AppID     int64
	UserID    int64
	DeviceID  string
	ClientIP  string
	UserAgent string
}

// Preview 兑换前的预览：这张卡是什么、会发什么、你现在能不能用。
func (s *CardKeyService) Preview(ctx context.Context, client CardKeyClientContext, code string) (*cardkeydomain.Preview, error) {
	normalized := NormalizeCardKeyCode(code)
	if normalized == "" {
		return nil, apperrors.New(40005, http.StatusBadRequest, "请输入卡密")
	}
	if err := s.checkGuard(ctx, client.AppID, client.UserID, client.ClientIP); err != nil {
		return nil, err
	}
	card, err := s.pg.FindCardKeyByCode(ctx, client.AppID, normalized)
	if err != nil {
		if errors.Is(err, pgrepo.ErrCardKeyNotFound) {
			s.recordGuardFailure(ctx, client.AppID, client.UserID, client.ClientIP)
		}
		return nil, s.translate(err)
	}
	batch, err := s.pg.GetCardKeyBatchByID(ctx, card.BatchID)
	if err != nil {
		return nil, s.translate(err)
	}

	preview := &cardkeydomain.Preview{
		Code:          card.Code,
		Kind:          card.Kind,
		BatchName:     batch.Name,
		Rewards:       s.describeRewards(ctx, client.AppID, batch.Rewards),
		ValidityMode:  batch.ValidityMode,
		ValidityDays:  batch.ValidityDays,
		ExpiresAt:     card.ExpiresAt,
		StartsAt:      batch.StartsAt,
		MaxUses:       card.MaxUses,
		RemainingUses: card.RemainingUses(),
		Audience:      batch.Audience,
		Usable:        true,
	}
	if card.Kind == cardkeydomain.KindLogin {
		preview.MaxDevices = card.MaxDevices
	}

	reason := s.previewBlocker(ctx, client, card, batch)
	if reason != nil {
		var appErr *apperrors.AppError
		if errors.As(reason, &appErr) {
			preview.Usable = false
			preview.ReasonCode = appErr.Code
			preview.Reason = appErr.Message
		} else {
			return nil, reason
		}
	}
	return preview, nil
}

// previewBlocker 预览与兑换走同一套判据，返回第一条拦住这次兑换的原因。
func (s *CardKeyService) previewBlocker(ctx context.Context, client CardKeyClientContext,
	card *cardkeydomain.Card, batch *cardkeydomain.Batch) error {
	if err := pgrepo.CheckCardKeyUsable(card, batch, time.Now().UTC()); err != nil {
		return s.translate(err)
	}
	already, err := s.pg.HasUserRedeemedCardKey(ctx, card.ID, client.UserID)
	if err != nil {
		return err
	}
	if already {
		return s.translate(pgrepo.ErrCardKeyAlreadyRedeemed)
	}
	if batch.PerUserLimit > 0 {
		count, err := s.pg.CountUserBatchRedemptions(ctx, batch.ID, client.UserID)
		if err != nil {
			return err
		}
		if count >= batch.PerUserLimit {
			return s.translate(&pgrepo.CardKeyUserLimitError{Limit: batch.PerUserLimit})
		}
	}
	if err := s.pg.CheckCardKeyAudience(ctx, batch, client.AppID, client.UserID); err != nil {
		return s.translate(err)
	}
	return nil
}

// describeRewards 把权益翻成人话：套餐与功能给出名字，而不是一个 ID。
func (s *CardKeyService) describeRewards(ctx context.Context, appID int64, rewards []cardkeydomain.Reward) []cardkeydomain.ClientReward {
	out := make([]cardkeydomain.ClientReward, 0, len(rewards))
	if len(rewards) == 0 {
		return out
	}
	plans, features, err := s.pg.PointsStoreCatalogRefs(ctx, appID)
	if err != nil {
		s.log.Warn("card key: load catalog refs failed", zap.Error(err))
	}
	for _, reward := range rewards {
		spec, ok := cardkeydomain.FindRewardSpec(reward.Type)
		if !ok {
			continue
		}
		item := cardkeydomain.ClientReward{Type: reward.Type, Label: spec.Label}
		switch spec.Value {
		case cardkeydomain.ValueAmount:
			item.Detail = strconv.FormatInt(reward.Amount, 10) + " " + spec.Unit
		case cardkeydomain.ValueMoney:
			item.Detail = reward.Money.StringFixed(2)
		case cardkeydomain.ValueRef:
			if plan, ok := plans[reward.RefID]; ok {
				item.Detail = plan.Name
				if plan.Lifetime {
					item.Detail += "（永久）"
				} else if plan.DurationDays > 0 {
					item.Detail += "（" + strconv.Itoa(plan.DurationDays) + " 天）"
				}
			}
		case cardkeydomain.ValueFeature:
			name := reward.Feature
			if feature, ok := features[reward.Feature]; ok && feature.Name != "" {
				name = feature.Name
			}
			if reward.Amount == 0 {
				item.Detail = name + " 永久"
			} else {
				item.Detail = name + " " + strconv.FormatInt(reward.Amount, 10) + " 天"
			}
		}
		out = append(out, item)
	}
	return out
}

// Redeem 用户兑换一张卡。
func (s *CardKeyService) Redeem(ctx context.Context, client CardKeyClientContext, code string) (*cardkeydomain.RedeemResult, error) {
	if err := s.checkGuard(ctx, client.AppID, client.UserID, client.ClientIP); err != nil {
		return nil, err
	}
	if err := s.riskGuard(ctx, securitydomain.RiskEvalRequest{
		Scene: securitydomain.SceneCardKey, AppID: &client.AppID, UserID: &client.UserID,
		IP: client.ClientIP, DeviceID: client.DeviceID, UserAgent: client.UserAgent,
	}, RiskGuardOption{Message: "当前环境存在风险，暂时无法兑换"}); err != nil {
		return nil, err
	}
	result, err := s.redeem(ctx, cardkeydomain.RedeemInput{
		AppID: client.AppID, Code: code, UserID: client.UserID, Source: cardkeydomain.SourceRedeem,
		DeviceID: client.DeviceID, ClientIP: client.ClientIP, UserAgent: client.UserAgent,
	})
	if err != nil {
		var appErr *apperrors.AppError
		if errors.As(err, &appErr) && appErr.Code == errCodeCardKeyNotFound {
			s.recordGuardFailure(ctx, client.AppID, client.UserID, client.ClientIP)
		}
		return nil, err
	}
	s.clearGuard(ctx, client.AppID, client.UserID)
	return result, nil
}

func (s *CardKeyService) redeem(ctx context.Context, input cardkeydomain.RedeemInput) (*cardkeydomain.RedeemResult, error) {
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
	s.log.Info("card key redeemed", zap.Int64("appid", input.AppID), zap.Int64("userId", input.UserID),
		zap.String("source", input.Source), zap.String("kind", result.Kind))
	return result, nil
}

// MyLicenses 当前用户名下的授权卡：还剩多久、绑了哪些设备、还能自助解绑几次。
func (s *CardKeyService) MyLicenses(ctx context.Context, appID int64, userID int64, currentDevice string) ([]cardkeydomain.ClientLicense, error) {
	cards, batches, err := s.pg.ListUserLoginCards(ctx, appID, userID)
	if err != nil {
		return nil, s.translate(err)
	}
	now := time.Now().UTC()
	since := now.Add(-cardkeydomain.SelfUnbindWindow)
	out := make([]cardkeydomain.ClientLicense, 0, len(cards))
	for index := range cards {
		card := &cards[index]
		devices, err := s.pg.ListCardKeyDevices(ctx, card.ID)
		if err != nil {
			return nil, s.translate(err)
		}
		license := cardkeydomain.ClientLicense{
			ID: card.ID, Code: card.Code, BatchName: card.BatchName, Status: card.Status,
			Expired: card.Expired(now), ActivatedAt: card.ActivatedAt, ExpiresAt: card.ExpiresAt,
			MaxDevices: card.MaxDevices, Devices: make([]cardkeydomain.ClientDevice, 0, len(devices)),
		}
		for _, device := range devices {
			license.Devices = append(license.Devices, cardkeydomain.ClientDevice{
				DeviceID: device.DeviceID, DeviceName: device.DeviceName, FirstSeenAt: device.FirstSeenAt,
				LastSeenAt: device.LastSeenAt, Current: currentDevice != "" && device.DeviceID == currentDevice,
			})
		}
		if batch, ok := batches[card.BatchID]; ok && batch.SelfUnbindLimit > 0 {
			used, err := s.pg.CountCardKeySelfUnbinds(ctx, card.ID, since)
			if err != nil {
				return nil, s.translate(err)
			}
			license.SelfUnbindLimit = batch.SelfUnbindLimit
			license.SelfUnbindRemaining = max(batch.SelfUnbindLimit-used, 0)
		}
		out = append(out, license)
	}
	return out, nil
}

// MyRedemptions 当前用户的兑换记录。
func (s *CardKeyService) MyRedemptions(ctx context.Context, appID int64, userID int64, page int, limit int) (*cardkeydomain.ClientRedemptionPage, error) {
	result, err := s.pg.ListUserCardKeyRedemptions(ctx, appID, userID, page, limit)
	return result, s.translate(err)
}

// SelfUnbind 用户在客户端自助解绑自己授权卡上的一台设备（受批次的每 30 天次数限制）。
func (s *CardKeyService) SelfUnbind(ctx context.Context, appID int64, userID int64, cardID int64, deviceID string) error {
	if strings.TrimSpace(deviceID) == "" {
		return apperrors.New(40005, http.StatusBadRequest, "设备标识不能为空")
	}
	card, err := s.pg.FindCardKeyByID(ctx, appID, cardID)
	if err != nil {
		return s.translate(err)
	}
	if card.BoundUserID == nil || *card.BoundUserID != userID || card.Kind != cardkeydomain.KindLogin {
		return s.translate(pgrepo.ErrCardKeyNotFound)
	}
	batch, err := s.pg.GetCardKeyBatchByID(ctx, card.BatchID)
	if err != nil {
		return s.translate(err)
	}
	return s.translate(s.pg.UnbindCardKeyDevice(ctx, cardkeydomain.UnbindInput{
		AppID: appID, CardID: cardID, DeviceID: deviceID, UserID: userID,
		Source: cardkeydomain.UnbindSelf, Limit: batch.SelfUnbindLimit,
	}))
}

// ── 登录链路 ──

// PrepareLogin 校验卡面，回答「这张卡能不能登、属于谁」。
//
// 只读：建号发生在认证服务里（那里才有资料、邀请码、搜索同步这些东西），
// 绑定与设备校验发生在 Activate 的事务里。登录前没有用户，防爆破只按 IP 计。
// MigrateUserDevice 用户名下各卡在旧设备码 from 下的设备绑定改记到新码 to（客户端换了设备码生成方式）。
// 返回改记的绑定条数（合并掉的不计）。
func (s *CardKeyService) MigrateUserDevice(ctx context.Context, appID int64, userID int64, from string, to string) (int64, error) {
	if s == nil || s.pg == nil {
		return 0, nil
	}
	return s.pg.MigrateCardKeyDevices(ctx, appID, userID, from, to)
}

func (s *CardKeyService) PrepareLogin(ctx context.Context, appID int64, code string, clientIP string) (*cardkeydomain.Card, error) {
	normalized := NormalizeCardKeyCode(code)
	if normalized == "" {
		return nil, apperrors.New(40005, http.StatusBadRequest, "请输入卡密")
	}
	if err := s.checkGuard(ctx, appID, 0, clientIP); err != nil {
		return nil, err
	}
	card, err := s.pg.FindCardKeyByCode(ctx, appID, normalized)
	if err != nil {
		if errors.Is(err, pgrepo.ErrCardKeyNotFound) {
			s.recordGuardFailure(ctx, appID, 0, clientIP)
		}
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

// ── 防爆破 ──

func (s *CardKeyService) guardSubjects(userID int64, clientIP string) [][2]string {
	subjects := make([][2]string, 0, 2)
	if userID > 0 {
		subjects = append(subjects, [2]string{"ck-user", strconv.FormatInt(userID, 10)})
	}
	if ip := strings.TrimSpace(clientIP); ip != "" {
		subjects = append(subjects, [2]string{"ck-ip", ip})
	}
	return subjects
}

func (s *CardKeyService) checkGuard(ctx context.Context, appID int64, userID int64, clientIP string) error {
	if s.guard == nil {
		return nil
	}
	for _, subject := range s.guardSubjects(userID, clientIP) {
		remaining, err := s.guard.LockRemaining(ctx, subject[0], appID, subject[1])
		if err != nil {
			// Redis 抖动时放行：防爆破是加固，不该让它的故障变成「谁都兑换不了」。
			s.log.Warn("card key guard check failed", zap.Error(err))
			return nil
		}
		if remaining > 0 {
			minutes := int(math.Ceil(remaining.Minutes()))
			return apperrors.New(errCodeCardKeyTooManyAttempts, http.StatusTooManyRequests,
				fmt.Sprintf("输错卡密的次数过多，请 %d 分钟后再试", minutes))
		}
	}
	return nil
}

func (s *CardKeyService) recordGuardFailure(ctx context.Context, appID int64, userID int64, clientIP string) {
	if s.guard == nil {
		return
	}
	for _, subject := range s.guardSubjects(userID, clientIP) {
		threshold := int64(cardKeyGuardThreshold)
		if subject[0] == "ck-ip" {
			// 一个出口 IP 后面可能是一整个校园网，阈值放宽一些。
			threshold *= 3
		}
		count, err := s.guard.IncrFailure(ctx, subject[0], appID, subject[1], cardKeyGuardWindow)
		if err != nil {
			s.log.Warn("card key guard incr failed", zap.Error(err))
			return
		}
		if count >= threshold {
			_ = s.guard.SetLock(ctx, subject[0], appID, subject[1], cardKeyGuardLock)
			_ = s.guard.ClearFailure(ctx, subject[0], appID, subject[1])
			s.log.Warn("card key guard locked", zap.Int64("appid", appID), zap.String("kind", subject[0]),
				zap.String("subject", subject[1]))
		}
	}
}

func (s *CardKeyService) clearGuard(ctx context.Context, appID int64, userID int64) {
	if s.guard == nil || userID <= 0 {
		return
	}
	_ = s.guard.ClearFailure(ctx, "ck-user", appID, strconv.FormatInt(userID, 10))
}

// ── 通知 ──

func (s *CardKeyService) notifyRedeemed(appID int64, userID int64, result *cardkeydomain.RedeemResult, title string) {
	if s.notifier == nil || result == nil {
		return
	}
	lines := make([]string, 0, len(result.Results))
	for _, item := range result.Results {
		lines = append(lines, item.Label+" "+item.Detail)
	}
	content := "卡密 " + result.Code + " 已兑换。"
	if len(lines) > 0 {
		content += "到账：" + strings.Join(lines, "；") + "。"
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := s.notifier.Deliver(ctx, notificationdomain.DeliverInput{
			AppID: appID, UserID: userID, Type: notificationdomain.TypeCardKey,
			Title: title, Content: content, Level: "success",
			Metadata: map[string]any{"code": result.Code, "kind": result.Kind},
			Action:   notificationdomain.RouteAction(notificationdomain.RouteCardKeys, "查看兑换记录"),
		}); err != nil {
			s.log.Warn("card key: notify user failed", zap.Int64("appid", appID), zap.Int64("userId", userID),
				zap.Error(err))
		}
	}()
}

// NormalizeCardKeyCode 把用户输入的卡面收敛成库里存的形态（见 cardkey.NormalizeCode）。
func NormalizeCardKeyCode(code string) string {
	return cardkeydomain.NormalizeCode(code)
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
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) {
		return err
	}
	var audience *pgrepo.CardKeyAudienceError
	var userLimit *pgrepo.CardKeyUserLimitError
	var notStarted *pgrepo.CardKeyNotStartedError
	var taken *pgrepo.CardKeyCodeTakenError
	var owned *pgrepo.CardKeyRewardOwnedError
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
	case errors.As(err, &notStarted):
		return apperrors.New(errCodeCardKeyNotStarted, http.StatusConflict,
			"该卡密 "+notStarted.StartsAt.In(time.Local).Format("2006-01-02 15:04")+" 起才能使用")
	case errors.Is(err, pgrepo.ErrCardKeyDeviceLimit):
		return apperrors.New(errCodeCardKeyDeviceLimit, http.StatusForbidden,
			"这张卡可绑定的设备数已满，请在其它设备上解绑后重试")
	case errors.Is(err, pgrepo.ErrCardKeyBoundOther):
		return apperrors.New(errCodeCardKeyBoundOther, http.StatusForbidden, "该卡密已绑定其它账号")
	case errors.Is(err, pgrepo.ErrCardKeyKindMismatch):
		return apperrors.New(errCodeCardKeyKindMismatch, http.StatusForbidden, "卡密类型不符")
	case errors.Is(err, pgrepo.ErrCardKeyNoLoginCard):
		return apperrors.New(errCodeCardKeyNoLoginCard, http.StatusForbidden,
			"这张卡要加在授权卡上，但你名下没有可用的授权卡")
	case errors.Is(err, pgrepo.ErrCardKeyLicensePermanent):
		return apperrors.New(errCodeCardKeyLicensePermanent, http.StatusConflict,
			"你名下的授权卡已是永久授权，无需续费")
	case errors.Is(err, pgrepo.ErrCardKeyAlreadyRedeemed):
		return apperrors.New(errCodeCardKeyAlreadyRedeemed, http.StatusConflict, "你已经兑换过这个卡密")
	case errors.As(err, &userLimit):
		return apperrors.New(errCodeCardKeyUserLimit, http.StatusConflict,
			fmt.Sprintf("这一批卡每人最多兑换 %d 张，你已达到上限", userLimit.Limit))
	case errors.As(err, &audience):
		message := "该卡密仅限会员兑换"
		if audience.Audience == cardkeydomain.AudienceNonVip {
			message = "该卡密仅限非会员兑换"
		}
		return apperrors.New(errCodeCardKeyAudience, http.StatusConflict, message)
	case errors.Is(err, pgrepo.ErrCardKeyUnbindLimit):
		return apperrors.New(errCodeCardKeyUnbindLimit, http.StatusConflict, "近 30 天的自助解绑次数已用完，请联系客服")
	case errors.Is(err, pgrepo.ErrCardKeyUnbindDisabled):
		return apperrors.New(errCodeCardKeyUnbindDisabled, http.StatusConflict, "这张卡不支持自助解绑设备，请联系客服")
	case errors.Is(err, pgrepo.ErrCardKeyDeviceMissing):
		return apperrors.New(errCodeCardKeyDeviceMissing, http.StatusNotFound, "这台设备没有绑定在该卡上")
	case errors.Is(err, pgrepo.ErrCardKeyBatchInUse):
		return apperrors.New(errCodeCardKeyBatchInUse, http.StatusConflict, "该批次已有卡被使用，不能删除，可以停用")
	case errors.As(err, &owned):
		return apperrors.New(errCodeCardKeyRewardOwned, http.StatusConflict,
			"你已永久拥有「"+owned.Feature+"」，这张卡可以留给别人使用")
	case errors.Is(err, pgrepo.ErrCardKeyRewardMissing):
		return apperrors.New(errCodeCardKeyRewardUnavailable, http.StatusConflict, "这张卡的权益已下架，请联系客服")
	case errors.As(err, &taken):
		return apperrors.New(errCodeCardKeyCodeTaken, http.StatusConflict,
			"这些卡面已经存在："+strings.Join(taken.Codes, "、"))
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
