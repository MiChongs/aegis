package service

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"aegis/internal/domain/accountdeletion"
	"aegis/internal/domain/suspension"
	userdomain "aegis/internal/domain/user"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"

	"go.uber.org/zap"
)

// 全站封禁：封的是「这个人」。主体是这个账号绑定的邮箱、手机号、第三方身份与用过的设备（只存摘要与打码值）；
// 所有应用里与这些主体相连的现有账号一并封禁（各自一条 level=platform 的账号封禁），之后用这些主体注册、登录也被拦。

// subjectDigests 一个账号的主体摘要
func (s *SuspensionService) subjectDigests(v *pgrepo.SubjectValues) []pgrepo.SubjectDigest {
	if v == nil {
		return nil
	}
	var out []pgrepo.SubjectDigest
	for _, e := range v.Emails {
		out = append(out, pgrepo.SubjectDigest{Kind: "email", Digest: s.digest("email", strings.ToLower(strings.TrimSpace(e))), Masked: maskEmailAddress(e)})
	}
	for _, p := range v.Phones {
		out = append(out, pgrepo.SubjectDigest{Kind: "phone", Digest: s.digest("phone", strings.TrimSpace(p)), Masked: maskPhoneNumber(p)})
	}
	for _, id := range v.Identities {
		out = append(out, pgrepo.SubjectDigest{Kind: "identity", Digest: s.digest("identity", strings.ToLower(id)), Masked: maskIdentity(id)})
	}
	for _, d := range v.Devices {
		out = append(out, pgrepo.SubjectDigest{Kind: "device", Digest: s.digest("device", d), Masked: maskDevice(d)})
	}
	return out
}

// BanPlatform 全站封禁一个账号背后的人；返回这个账号上的封禁
func (s *SuspensionService) BanPlatform(ctx context.Context, appID, userID int64, input userdomain.AccountBanCreateInput) (*userdomain.AccountBan, error) {
	s.mu.RLock()
	bans := s.bans
	s.mu.RUnlock()
	if bans == nil {
		return nil, errors.New("封禁服务未接入")
	}
	values, err := s.pg.SubjectValuesOf(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	if values == nil {
		return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	appealable := input.Appealable == nil || *input.Appealable
	app, uid := appID, userID
	pb, err := s.pg.CreatePlatformBan(ctx, suspension.PlatformBan{
		BanType: input.BanType, Reason: input.Reason, PublicReason: input.PublicReason, ReasonCode: input.ReasonCode, Source: input.Source,
		Appealable: appealable, OriginAppID: &app, OriginUserID: &uid, Evidence: input.Evidence,
		CreatedByAdminID: nullableID(input.Operator.AdminID), CreatedByName: input.Operator.AdminName, StartAt: *input.StartAt, EndAt: input.EndAt,
	}, s.subjectDigests(values))
	if err != nil {
		return nil, err
	}
	accounts, err := s.pg.AccountsBySubjects(ctx, *values)
	if err != nil {
		s.log.Warn("全站封禁：查关联账号失败，只封当前账号", zap.Int64("platformBanId", pb.ID), zap.Error(err))
	}
	// 当前账号放第一个
	ordered := []pgrepo.AccountRef{{AppID: appID, UserID: userID}}
	for _, a := range accounts {
		if a.AppID != appID || a.UserID != userID {
			ordered = append(ordered, a)
		}
	}
	notify := input.Notify == nil || *input.Notify
	var origin *userdomain.AccountBan
	var created []*userdomain.AccountBan
	for i, a := range ordered {
		one := input
		pbID := pb.ID
		one.Level, one.PlatformBanID = userdomain.AccountBanLevelPlatform, &pbID
		if i > 0 {
			one.Source = userdomain.AccountBanSourcePlatform
		}
		ban, err := bans.createOne(ctx, a.AppID, a.UserID, one)
		if err != nil {
			if i == 0 {
				return nil, err
			}
			s.log.Warn("全站封禁：连带账号没有封上", zap.Int64("platformBanId", pb.ID), zap.Int64("appid", a.AppID), zap.Int64("userId", a.UserID), zap.Error(err))
			continue
		}
		if i == 0 {
			origin = ban
		}
		created = append(created, ban)
	}
	if notify && len(created) > 0 {
		// 通知在后台发：多个账号共用一个邮箱时只发一封
		go func(ctx context.Context) {
			sent := map[string]bool{}
			for _, b := range created {
				s.notifySuspended(ctx, b, sent)
			}
		}(context.WithoutCancel(ctx))
	}
	s.log.Warn("全站封禁", zap.Int64("platformBanId", pb.ID), zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Int("accounts", len(ordered)))
	return origin, nil
}

// checkPlatformAtSignIn 登录时核对：账号的主体撞上生效中的全站封禁，就当场连带封上（并把它的新主体补进去）
func (s *SuspensionService) checkPlatformAtSignIn(ctx context.Context, user *userdomain.User) (*userdomain.AccountBan, error) {
	s.mu.RLock()
	bans := s.bans
	s.mu.RUnlock()
	if bans == nil {
		return nil, nil
	}
	values, err := s.pg.SubjectValuesOf(ctx, user.AppID, user.ID)
	if err != nil || values == nil {
		return nil, err
	}
	digests := s.subjectDigests(values)
	pb, err := s.pg.MatchPlatformBan(ctx, digests)
	if err != nil || pb == nil {
		return nil, err
	}
	_ = s.pg.AddPlatformBanSubjects(ctx, pb.ID, digests)
	pbID := pb.ID
	appealable := pb.Appealable
	ban, err := bans.createOne(ctx, user.AppID, user.ID, userdomain.AccountBanCreateInput{
		BanType: pb.BanType, BanScope: userdomain.AccountBanScopeAll, Reason: pb.Reason, PublicReason: pb.PublicReason, ReasonCode: pb.ReasonCode,
		Level: userdomain.AccountBanLevelPlatform, PlatformBanID: &pbID, Source: userdomain.AccountBanSourcePlatform, Appealable: &appealable,
		StartAt: &pb.StartAt, EndAt: pb.EndAt, Evidence: map[string]any{"platformBanId": pb.ID, "matched": "sign_in"},
		Operator: userdomain.BanOperator{AdminName: "全站封禁"},
	})
	if err != nil {
		return nil, err
	}
	s.log.Warn("全站封禁：登录时连带封禁", zap.Int64("platformBanId", pb.ID), zap.Int64("appid", user.AppID), zap.Int64("userId", user.ID))
	go s.notifySuspended(context.WithoutCancel(ctx), ban, map[string]bool{})
	return ban, nil
}

// CheckRegistration 注册前核对：邮箱、手机号、第三方身份撞上全站封禁就拒绝（45100）。查询出错时放行。
func (s *SuspensionService) CheckRegistration(ctx context.Context, identifiers ...RegisterIdentifier) error {
	var digests []pgrepo.SubjectDigest
	for _, id := range identifiers {
		v := strings.TrimSpace(id.Value)
		if v == "" {
			continue
		}
		switch id.Kind {
		case accountdeletion.TombstoneEmail:
			digests = append(digests, pgrepo.SubjectDigest{Kind: "email", Digest: s.digest("email", strings.ToLower(v))})
		case accountdeletion.TombstonePhone:
			digests = append(digests, pgrepo.SubjectDigest{Kind: "phone", Digest: s.digest("phone", v)})
		case RegisterIdentifierIdentity:
			digests = append(digests, pgrepo.SubjectDigest{Kind: "identity", Digest: s.digest("identity", strings.ToLower(v))})
		}
	}
	pb, err := s.pg.MatchPlatformBan(ctx, digests)
	if err != nil {
		s.log.Warn("注册前核对全站封禁失败，放行", zap.Error(err))
		return nil
	}
	if pb != nil {
		locale := localeFor(ctx, "")
		return apperrors.New(45100, http.StatusForbidden, suspension.ErrorMessage(pb.BanType == userdomain.AccountBanTypePermanent, locale)).
			WithData(map[string]any{"platformSuspended": true, "reasonCode": pb.ReasonCode, "reasonTitle": suspension.Reason(pb.ReasonCode, locale).Title})
	}
	return nil
}

// RegisterIdentifierIdentity 注册前核对用的第三方身份（provider:provider_user_id）
const RegisterIdentifierIdentity = "identity"

// RevokePlatform 解除全站封禁与它连带的全部账号封禁
func (s *SuspensionService) RevokePlatform(ctx context.Context, platformBanID int64, by userdomain.BanOperator, reason string, notify bool) (int, error) {
	s.mu.RLock()
	bans := s.bans
	s.mu.RUnlock()
	linked, err := s.pg.RevokePlatformBan(ctx, platformBanID, by, strings.TrimSpace(reason))
	if errors.Is(err, pgrepo.ErrAccountBanNotActive) {
		return 0, apperrors.New(40934, http.StatusConflict, "该全站封禁已到期或已解除")
	}
	if err != nil {
		return 0, err
	}
	n := 0
	var revokedAll []*userdomain.AccountBan
	for _, b := range linked {
		if bans == nil {
			break
		}
		revoked, err := bans.revokeOne(ctx, b.AppID, b.UserID, b.ID, userdomain.AccountBanRevokeInput{Reason: reason, Operator: by})
		if err != nil {
			s.log.Warn("解除全站封禁：连带账号没有解开", zap.Int64("platformBanId", platformBanID), zap.Int64("banId", b.ID), zap.Error(err))
			continue
		}
		n++
		revokedAll = append(revokedAll, revoked)
	}
	if notify && len(revokedAll) > 0 {
		go func(ctx context.Context) {
			sent := map[string]bool{}
			for _, b := range revokedAll {
				s.notifyReinstated(ctx, b, sent)
			}
		}(context.WithoutCancel(ctx))
	}
	return n, nil
}

// ── 管理端 ──

// ListPlatformBans 全站封禁列表
func (s *SuspensionService) ListPlatformBans(ctx context.Context, q pgrepo.PlatformBanQuery) ([]suspension.PlatformBan, int64, error) {
	items, total, err := s.pg.ListPlatformBans(ctx, q)
	if items == nil {
		items = []suspension.PlatformBan{}
	}
	return items, total, err
}

// PlatformBanDetail 一条全站封禁（主体打码与连带账号）
func (s *SuspensionService) PlatformBanDetail(ctx context.Context, id int64) (*suspension.PlatformBan, error) {
	pb, err := s.pg.GetPlatformBan(ctx, id)
	if err != nil {
		return nil, err
	}
	if pb == nil {
		return nil, apperrors.New(40424, http.StatusNotFound, "全站封禁不存在")
	}
	if pb.Subjects, err = s.pg.PlatformBanSubjects(ctx, id); err != nil {
		return nil, err
	}
	if pb.Accounts, err = s.pg.PlatformBanAccounts(ctx, id); err != nil {
		return nil, err
	}
	pb.SubjectCount = len(pb.Subjects)
	for _, a := range pb.Accounts {
		if a.Status == userdomain.AccountBanStatusActive {
			pb.AccountCount++
		}
	}
	return pb, nil
}

// ── 打码 ──

func maskEmailAddress(e string) string {
	e = strings.TrimSpace(e)
	at := strings.LastIndexByte(e, '@')
	if at <= 0 {
		return "***"
	}
	name := e[:at]
	if len(name) <= 2 {
		return name[:1] + "***" + e[at:]
	}
	return name[:2] + "***" + e[at:]
}

func maskPhoneNumber(p string) string {
	p = strings.TrimSpace(p)
	if len(p) < 7 {
		return "****"
	}
	return p[:3] + "****" + p[len(p)-4:]
}

func maskIdentity(id string) string {
	provider, subject, ok := strings.Cut(id, ":")
	if !ok || len(subject) < 4 {
		return provider + ":****"
	}
	return provider + ":****" + subject[len(subject)-4:]
}

func maskDevice(d string) string {
	if len(d) <= 8 {
		return "****"
	}
	return d[:4] + "…" + d[len(d)-4:]
}

func nullableID(id int64) *int64 {
	if id <= 0 {
		return nil
	}
	return &id
}
