package service

import (
	"context"
	"net/http"
	"strings"
	"time"

	authdomain "aegis/internal/domain/auth"
	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"go.uber.org/zap"
)

// 服务端会员校验与功能标识目录。
//
// ── 为什么要有服务端校验这条路 ──
//
// 平台此前只有两条问「他是不是会员」的路：客户端拿自己的令牌问 `/vip/status`，
// 管理员拿管理端令牌问 `/vip/entitlement`。接入方**自己的服务器**两样都不该有 ——
// 它没有用户令牌（那是用户的东西），更不该配管理员账号（那是整个租户的权限）。
// 于是接入方只能让客户端把「我是会员」这句话带上来，而这句话客户端说了不算。
//
// 服务端校验就是补这条路：用应用自己的服务端密钥问，答案直接来自 users 表。
//
// ── 为什么判定要细到功能标识 ──
//
// 「是不是会员」只有一个维度。接入方一旦有两档会员（基础版能导出、高级版还能用 AI），
// 后端就只能拿套餐名做字符串比较 —— 而套餐名是运营随时会改的展示文案。
// 功能标识把「卖的是哪个套餐」和「解锁的是哪个能力」拆开：套餐改名、拆分、合并
// 都不影响判定，接入方代码里那句 `feature == "export"` 永远成立。

const (
	errCodeFeatureUnknown    = 40486 // 404：校验时传了目录里没有的功能标识
	errCodeFeatureDuplicated = 40905 // 409：功能标识重复
)

// ── 功能标识目录（管理端）──

// AdminListFeatures 应用的功能标识目录。
func (s *VipService) AdminListFeatures(ctx context.Context, appID int64) ([]vipdomain.Feature, error) {
	if appID <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "应用ID不能为空")
	}
	return s.pg.ListVipFeatures(ctx, appID, false)
}

// AdminSaveFeature 新建 / 更新一个功能标识。
//
// tag 是定位键，只能新建时定、之后不可改：它已经写进接入方的代码里，
// 也写进了每一条历史开通记录的功能快照里。要换展示文案改 name 就够了。
func (s *VipService) AdminSaveFeature(ctx context.Context, mutation vipdomain.FeatureMutation) (*vipdomain.Feature, error) {
	if mutation.AppID <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "应用ID不能为空")
	}
	tag := strings.ToLower(strings.TrimSpace(mutation.Tag))
	if !vipdomain.FeatureTagPattern.MatchString(tag) {
		return nil, apperrors.New(40000, http.StatusBadRequest,
			"功能标识只能是 2-64 位小写字母开头、含数字与 . _ - 的短标识，例如 export 或 ai.chat")
	}
	mutation.Tag = tag

	existing, err := s.pg.GetVipFeature(ctx, mutation.AppID, tag)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		// 新建时名称必填：一个只有 tag 没有名字的功能，在套餐编辑器里
		// 就是一行看不出是什么的字符串。
		if mutation.Name == nil || strings.TrimSpace(*mutation.Name) == "" {
			return nil, apperrors.New(40000, http.StatusBadRequest, "功能名称不能为空")
		}
	}

	feature, err := s.pg.UpsertVipFeature(ctx, mutation)
	if err != nil {
		if pgrepo.IsUniqueViolation(err) {
			return nil, apperrors.New(errCodeFeatureDuplicated, http.StatusConflict, "该功能标识已存在")
		}
		return nil, err
	}
	return feature, nil
}

// AdminDeleteFeature 删除一个功能标识。
//
// 不级联清理套餐里的引用：删功能是运营动作，不该被"还有套餐在用"卡住；
// 而残留的引用会在校验入口明确报「未登记的功能标识」，比静默改写一批套餐配置好排查。
// 因此这里把「还有几个套餐在用」一并返回，让控制台能在删之前说清后果。
func (s *VipService) AdminDeleteFeature(ctx context.Context, appID int64, tag string) (int64, error) {
	if appID <= 0 {
		return 0, apperrors.New(40000, http.StatusBadRequest, "应用ID不能为空")
	}
	tag = strings.ToLower(strings.TrimSpace(tag))
	affectedPlans, err := s.pg.CountVipPlansUsingFeature(ctx, appID, tag)
	if err != nil {
		return 0, err
	}
	deleted, err := s.pg.DeleteVipFeature(ctx, appID, tag)
	if err != nil {
		return 0, err
	}
	if !deleted {
		return 0, apperrors.New(errCodeFeatureUnknown, http.StatusNotFound, "功能标识不存在")
	}
	s.log.Info("vip feature deleted",
		zap.Int64("appid", appID), zap.String("tag", tag), zap.Int64("affectedPlans", affectedPlans))
	return affectedPlans, nil
}

// ── 服务端校验 ──

// VerifyMembership 校验某个用户当前是不是会员（可选：是否覆盖某个功能）。
//
// **userID 必须来自已验证的访问令牌**（见 `VerifyQuery` 的说明）——
// 这个方法不导出给传输层直接用，对外入口只有 `VerifyMembershipByToken`。
//
// 功能标识没登记过一律报错而不是返回 false：拼错一个字母（`exprot`）
// 与"他没有这项权益"是完全不同的两件事，后者在静默方案里永远查不出来。
func (s *VipService) verifyMembership(ctx context.Context, appID int64, query vipdomain.VerifyQuery) (*vipdomain.Verification, error) {
	if appID <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "应用ID不能为空")
	}
	userID := query.UserID
	if userID <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "缺少用户身份")
	}
	account := ""

	// 功能标识先验目录再判权益：顺序反过来的话，"没登记的标识"与
	// "登记了但他没有"会给出同一个 false，而这两件事一个是接入方的 bug、
	// 一个是正常业务结论。
	var verdict *vipdomain.FeatureVerdict
	featureDisabled := false
	if tag := strings.ToLower(strings.TrimSpace(query.Feature)); tag != "" {
		feature, err := s.pg.GetVipFeature(ctx, appID, tag)
		if err != nil {
			return nil, err
		}
		if feature == nil {
			return nil, apperrors.New(errCodeFeatureUnknown, http.StatusNotFound,
				"功能标识 "+tag+" 未登记，请先在会员功能目录里创建它")
		}
		// 停用的功能一律判不通过。这是运营动作（某个能力整体下线），
		// 与"这个用户没有这项权益"是两回事，但对调用方而言结论一样：不放行。
		featureDisabled = !feature.IsActive
		verdict = &vipdomain.FeatureVerdict{Tag: feature.Tag, Name: feature.Name}
	}

	entitlement, err := s.ResolveEntitlement(ctx, appID, userID, "")
	if err != nil {
		return nil, err
	}
	// 把账号补上：调用方多半要把它写进自己的日志，而令牌里只有 userId
	if user, err := s.pg.GetUserByID(ctx, userID); err == nil && user != nil {
		account = user.Account
	}

	// 未指定功能标识 ⇒ 通用档，结论就是"是不是会员"；
	// 指定了 ⇒ 还要求当前权益覆盖它（HasFeature 自带"不是会员就没有"这条）。
	granted := entitlement.IsVIP
	if verdict != nil {
		verdict.Granted = !featureDisabled && entitlement.HasFeature(verdict.Tag)
		granted = verdict.Granted
	}

	return &vipdomain.Verification{
		Granted:    granted,
		Matched:    true,
		UserID:     userID,
		Account:    account,
		Membership: entitlement.View(),
		Feature:    verdict,
		CheckedAt:  time.Now().UTC(),
	}, nil
}

// VerifyMembershipByToken 服务端校验的**唯一**入口：用用户的访问令牌定位再判权益。
//
// 两样凭据各证明一件事，缺一不可：
//
//	服务端密钥（X-Aegis-Function-Key）—— 证明「谁在问」，只有接入方的后端持有
//	用户访问令牌                      —— 证明「问的是谁」，由平台签发与验证
//
// 刻意不提供「按 userId 查」的变体：那等于让调用方自报被查者的身份，
// 而接入方的后端几乎一定会把这个身份交给它自己的客户端来说（详见 VerifyQuery）。
//
// 令牌本身由传输层验（那里已持有 AuthService），这里只做归属校验：
// 拿 A 应用的令牌来问 B 应用，必须当场拒绝而不是去查一个同号的陌生用户。
func (s *VipService) VerifyMembershipByToken(ctx context.Context, appID int64, session *authdomain.Session, feature string) (*vipdomain.Verification, error) {
	if session == nil {
		return nil, apperrors.New(40100, http.StatusUnauthorized, "用户令牌无效")
	}
	if session.AppID != appID {
		return nil, apperrors.New(40372, http.StatusForbidden, "该令牌不属于当前应用")
	}
	return s.verifyMembership(ctx, appID, vipdomain.VerifyQuery{UserID: session.UserID, Feature: feature})
}

// EnsureFeatureTagsRegistered 保存套餐时校验它引用的功能标识都在目录里。
//
// 不校验的话，套餐上写一个拼错的标识不会有任何提示，直到几周后接入方来问
// "为什么买了高级版还是用不了导出" —— 而那时谁也想不到是套餐里多了一个字母。
func (s *VipService) EnsureFeatureTagsRegistered(ctx context.Context, appID int64, tags []string) error {
	tags = vipdomain.NormalizeFeatureTags(tags)
	if len(tags) == 0 {
		return nil
	}
	catalog, err := s.pg.ListVipFeatures(ctx, appID, false)
	if err != nil {
		return err
	}
	known := make(map[string]struct{}, len(catalog))
	for _, item := range catalog {
		known[item.Tag] = struct{}{}
	}
	missing := make([]string, 0, len(tags))
	for _, tag := range tags {
		if _, ok := known[tag]; !ok {
			missing = append(missing, tag)
		}
	}
	if len(missing) > 0 {
		return apperrors.New(errCodeFeatureUnknown, http.StatusNotFound,
			"功能标识未登记："+strings.Join(missing, "、")+"，请先在会员功能目录里创建")
	}
	return nil
}
