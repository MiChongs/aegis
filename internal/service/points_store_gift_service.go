package service

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"

	notificationdomain "aegis/internal/domain/notification"
	"aegis/internal/domain/pointstore"
	securitydomain "aegis/internal/domain/security"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
)

// 积分商城「赠好友」：选人、核对、提交，我的赠送记录；管理端的赠送记录、审核、撤销与风控策略。
// 规则见 docs/points-store-gift.md。

// 赠送的错误码。可兑换判定（库存、限兑、积分……）沿用 403xx。
const (
	errCodeGiftRecipientNotFound = 40432
	errCodeGiftNotFound          = 40433
	errCodeGiftState             = 40909
	errCodeRedemptionIsGift      = 40969
	errCodeGiftSelf              = 42251
	errCodeGiftRecipientInactive = 42252
	errCodeGiftRecipientOwned    = 42253
	errCodeGiftItemMismatch      = 42254
	errCodeGiftRisk              = 42255
	errCodeGiftDisabled          = 42256
	errCodeGiftLimit             = 42930
	errCodeGiftFrequency         = 42931
)

// AvatarResolver 把库里的头像原值换成客户端能访问的地址（传输层按请求的域名给）。
type AvatarResolver func(userID int64, raw string) string

// ── 管理端：策略 ──

// GiftPolicy 赠送风控策略（没保存过时是推荐默认值），连同判据目录。
func (s *PointsStoreService) GiftPolicy(ctx context.Context, appID int64) (map[string]any, error) {
	policy, err := s.pg.GetPointsStoreGiftPolicy(ctx, appID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"policy": policy, "catalog": pointstore.GiftRuleCatalog(),
		"defaults": pointstore.DefaultGiftPolicy(appID)}, nil
}

// SaveGiftPolicy 保存赠送风控策略。
func (s *PointsStoreService) SaveGiftPolicy(ctx context.Context, policy pointstore.GiftPolicy, operator string) (*pointstore.GiftPolicy, error) {
	policy = pointstore.NormalizeGiftPolicy(policy)
	if err := pointstore.ValidateGiftPolicy(policy); err != nil {
		return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	return s.pg.SavePointsStoreGiftPolicy(ctx, policy, operator)
}

// ── 管理端：记录、审核、撤销 ──

// ListGifts 赠送记录与各状态的笔数。
func (s *PointsStoreService) ListGifts(ctx context.Context, query pointstore.GiftQuery) (map[string]any, error) {
	if query.Status != "" && !pointstore.ValidGiftStatus(query.Status) {
		return nil, apperrors.New(40000, http.StatusBadRequest, "未知的赠送状态："+query.Status)
	}
	page, err := s.pg.ListPointsStoreGifts(ctx, query)
	if err != nil {
		return nil, err
	}
	summary, err := s.pg.PointsStoreGiftSummary(ctx, query.AppID, s.clock().Windows.Day)
	if err != nil {
		return nil, err
	}
	return map[string]any{"items": page.Items, "total": page.Total, "page": page.Page, "limit": page.Limit,
		"totalPages": page.TotalPages, "summary": summary, "statuses": pointstore.GiftStatuses()}, nil
}

// GetGift 一条赠送的详情（含风控事实与命中的判据）。
func (s *PointsStoreService) GetGift(ctx context.Context, appID, giftID int64) (*pointstore.Gift, error) {
	gift, err := s.pg.GetPointsStoreGift(ctx, appID, giftID)
	return gift, s.translate(err)
}

// ApproveGift 审核通过：给接收人发放，通知双方。
func (s *PointsStoreService) ApproveGift(ctx context.Context, input pointstore.GiftReviewInput) (*pointstore.Gift, error) {
	if err := validateHandleNote(input.Note); err != nil {
		return nil, err
	}
	gift, err := s.pg.ApprovePointsStoreGift(ctx, input)
	if err != nil {
		return nil, s.translate(err)
	}
	s.notifyGiftDelivered(gift)
	return gift, nil
}

// RejectGift 审核驳回：积分退回赠送人，通知赠送人（接收人没收到过任何东西，不打扰）。
func (s *PointsStoreService) RejectGift(ctx context.Context, input pointstore.GiftReviewInput) (*pointstore.Gift, error) {
	if err := validateHandleNote(input.Note); err != nil {
		return nil, err
	}
	gift, err := s.pg.RejectPointsStoreGift(ctx, input)
	if err != nil {
		return nil, s.translate(err)
	}
	content := "你送给 " + giftUserName(gift.Recipient) + " 的「" + gift.ItemTitle + "」未通过审核，" +
		strconv.FormatInt(gift.Cost, 10) + " 积分已退回。"
	if note := strings.TrimSpace(gift.ReviewNote); note != "" {
		content += "\n" + note
	}
	s.notifyGift(gift, gift.Sender.UserID, "赠送未通过审核", content, "warning")
	s.publishUser(gift.AppID, gift.Sender.UserID, map[string]any{"giftNo": gift.GiftNo, "status": gift.Status})
	return gift, nil
}

// RevokeGift 撤销一笔已送达的赠送并追回权益，通知双方。
func (s *PointsStoreService) RevokeGift(ctx context.Context, input pointstore.GiftReviewInput) (*pointstore.Gift, error) {
	if err := validateHandleNote(input.Note); err != nil {
		return nil, err
	}
	gift, err := s.pg.RevokePointsStoreGift(ctx, input)
	if err != nil {
		return nil, s.translate(err)
	}
	note := strings.TrimSpace(gift.RevokeNote)
	recipientContent := giftUserName(gift.Sender) + " 送给你的「" + gift.ItemTitle + "」已被撤销，相应权益已收回。"
	senderContent := "你送给 " + giftUserName(gift.Recipient) + " 的「" + gift.ItemTitle + "」已被撤销。"
	if gift.RefundTransactionNo != "" {
		senderContent += strconv.FormatInt(gift.Cost, 10) + " 积分已退回。"
	} else {
		senderContent += "积分不予退回。"
	}
	if note != "" {
		recipientContent += "\n" + note
		senderContent += "\n" + note
	}
	s.notifyGift(gift, gift.Recipient.UserID, "收到的赠送已撤销", recipientContent, "warning")
	s.notifyGift(gift, gift.Sender.UserID, "赠送已撤销", senderContent, "warning")
	s.publishUser(gift.AppID, gift.Recipient.UserID, map[string]any{"giftNo": gift.GiftNo, "status": gift.Status})
	s.publishUser(gift.AppID, gift.Sender.UserID, map[string]any{"giftNo": gift.GiftNo, "status": gift.Status})
	return gift, nil
}

// ── 用户端 ──

// GiftCandidates 选人：q 为空时给最近互动过的人（送过的、送过我的、邀请关系），
// q 是号码（或对外 ID）时按号码找人。找不到返回空列表，不报错。
func (s *PointsStoreService) GiftCandidates(ctx context.Context, appID, userID int64, q string, avatar AvatarResolver) ([]pointstore.GiftCandidate, error) {
	q = strings.TrimSpace(q)
	var out []pointstore.GiftCandidate
	if q == "" {
		items, err := s.pg.PointsStoreGiftCandidates(ctx, appID, userID, 20)
		if err != nil {
			return nil, err
		}
		out = items
	} else {
		if !isDigits(q) || len(q) > 19 {
			return []pointstore.GiftCandidate{}, nil
		}
		recipientID, err := s.pg.FindPointsStoreGiftRecipient(ctx, appID, q)
		if err != nil {
			return nil, err
		}
		if recipientID == 0 || recipientID == userID {
			return []pointstore.GiftCandidate{}, nil
		}
		account, err := s.pg.PointsStoreGiftAccount(ctx, appID, recipientID)
		if err != nil {
			return nil, s.translate(err)
		}
		out = []pointstore.GiftCandidate{{GiftUser: account.GiftUser, Relation: "search"}}
	}
	for i := range out {
		out[i].GiftUser = publicGiftUser(out[i].GiftUser, avatar)
	}
	return out, nil
}

// GiftRequest 用户端的一次赠送 / 核对。
type GiftRequest struct {
	AppID    int64
	SenderID int64
	ItemID   int64
	// Recipient 接收人的号码，或从候选列表里选的人的对外 ID
	Recipient string
	Message   string
	RequestID string
	DeviceID  string
	ClientIP  string
	UserAgent string
}

// PreviewGift 提交前的核对：对方是谁、会收到什么、能不能送（不能送时给出原因）。不扣积分、不加锁。
// 需要审核的情形在这里照样回 allowed=true：审核是事后的，不提前告诉用户哪些条件会触发审核。
func (s *PointsStoreService) PreviewGift(ctx context.Context, req GiftRequest, avatar AvatarResolver) (*pointstore.GiftPreview, error) {
	input, recipient, err := s.prepareGift(ctx, req)
	if err != nil {
		return nil, err
	}
	preview := &pointstore.GiftPreview{
		Recipient: pointstore.GiftCandidate{GiftUser: publicGiftUser(recipient.GiftUser, avatar), Relation: "search"},
		Allowed:   true,
		Rewards:   []pointstore.ClientReward{},
	}
	result, err := s.pg.GiftPointsStoreItem(ctx, input, true)
	if err != nil {
		translated := s.translate(err)
		var appErr *apperrors.AppError
		if !errors.As(translated, &appErr) || appErr.HTTPStatus >= http.StatusInternalServerError {
			return nil, translated
		}
		preview.Allowed = false
		preview.Reason, preview.Message = giftErrorReason(err), appErr.Message
		return preview, nil
	}
	plans, features, err := s.pg.PointsStoreCatalogRefs(ctx, req.AppID)
	if err != nil {
		return nil, err
	}
	for _, reward := range result.Gift.Rewards {
		preview.Rewards = append(preview.Rewards, pointstore.DescribeReward(reward, plans, features))
	}
	return preview, nil
}

// Gift 赠送：放行的当场送达，需要审核的积分先扣、记为审核中，拒绝的不扣积分并给出原因。
func (s *PointsStoreService) Gift(ctx context.Context, req GiftRequest, avatar AvatarResolver) (*pointstore.GiftResult, error) {
	input, _, err := s.prepareGift(ctx, req)
	if err != nil {
		return nil, err
	}
	input.Platform = s.giftPlatformVerdict(ctx, input)
	result, err := s.pg.GiftPointsStoreItem(ctx, input, false)
	if err != nil {
		var refused *pgrepo.PointsStoreGiftError
		if errors.As(err, &refused) && refused.Verdict != nil && refused.Facts != nil {
			// 风控拒绝留一条痕：没扣积分，只让管理员看得见谁在试
			if recordErr := s.pg.RecordPointsStoreGiftBlock(ctx, input, refused.Item, *refused.Verdict, *refused.Facts); recordErr != nil {
				s.log.Warn("points store gift: record block failed", zap.Int64("appid", input.AppID), zap.Error(recordErr))
			}
		}
		return nil, s.translate(err)
	}
	if !result.Replayed {
		switch result.Gift.Status {
		case pointstore.GiftDelivered:
			s.notifyGiftDelivered(&result.Gift)
		case pointstore.GiftReviewing:
			s.notifyGift(&result.Gift, result.Gift.Sender.UserID, "赠送已提交审核",
				"你送给 "+giftUserName(result.Gift.Recipient)+" 的「"+result.Gift.ItemTitle+"」正在审核，通过后对方即可收到；"+
					"未通过时积分原路退回。", "info")
		}
		s.publishUser(input.AppID, input.SenderID, map[string]any{"balance": result.Balance,
			"giftNo": result.Gift.GiftNo, "status": result.Gift.Status})
	}
	result.Gift = userGiftView(result.Gift, input.SenderID, avatar)
	return result, nil
}

// MyGifts 我的赠送记录：box=sent 我送出的（不含被拦下的尝试）、received 我收到的（只有已送达与已撤销），空为两者。
func (s *PointsStoreService) MyGifts(ctx context.Context, appID, userID int64, box string, page, limit int, avatar AvatarResolver) (*pointstore.Page[pointstore.Gift], error) {
	query := pointstore.GiftQuery{AppID: appID, Page: page, Limit: limit}
	switch box {
	case "sent":
		query.SenderID = userID
	case "received":
		query.RecipientID = userID
	case "":
		query.UserID = userID
	default:
		return nil, apperrors.New(40000, http.StatusBadRequest, "box 只能是 sent 或 received")
	}
	result, err := s.pg.ListPointsStoreGifts(ctx, query)
	if err != nil {
		return nil, err
	}
	items := make([]pointstore.Gift, 0, len(result.Items))
	for _, gift := range result.Items {
		if gift.Status == pointstore.GiftBlocked {
			continue
		}
		// 接收人只看到真正到过手的：审核中、被驳回的对他来说从没发生过
		if gift.Recipient.UserID == userID && gift.Sender.UserID != userID &&
			gift.Status != pointstore.GiftDelivered && gift.Status != pointstore.GiftRevoked {
			continue
		}
		items = append(items, userGiftView(gift, userID, avatar))
	}
	result.Items = items
	return result, nil
}

// prepareGift 校验请求、找到接收人，带上此刻的周期边界与策略。
func (s *PointsStoreService) prepareGift(ctx context.Context, req GiftRequest) (pointstore.GiftInput, *pgrepo.PointsStoreGiftAccount, error) {
	input := pointstore.GiftInput{AppID: req.AppID, SenderID: req.SenderID, ItemID: req.ItemID,
		RequestID: strings.TrimSpace(req.RequestID), DeviceID: req.DeviceID, ClientIP: req.ClientIP, UserAgent: req.UserAgent}
	if len(input.RequestID) > pointsStoreRequestIDMaxLength {
		return input, nil, apperrors.New(40000, http.StatusBadRequest, "requestId 不能超过 64 位")
	}
	if input.ItemID <= 0 {
		return input, nil, apperrors.New(40000, http.StatusBadRequest, "itemId 无效")
	}
	message, err := pointstore.NormalizeGiftMessage(req.Message)
	if err != nil {
		return input, nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	input.Message = message
	key := strings.TrimSpace(req.Recipient)
	if key == "" {
		return input, nil, apperrors.New(40000, http.StatusBadRequest, "请选择赠送对象")
	}
	settings, err := s.pg.GetPointsStoreSettings(ctx, req.AppID)
	if err != nil {
		return input, nil, err
	}
	if !settings.Enabled {
		return input, nil, apperrors.New(errCodePointsStoreDisabled, http.StatusForbidden, "积分商城暂未开放")
	}
	policy, err := s.pg.GetPointsStoreGiftPolicy(ctx, req.AppID)
	if err != nil {
		return input, nil, err
	}
	if !policy.Enabled {
		return input, nil, s.translate(&pgrepo.PointsStoreGiftError{Reason: pointstore.GiftReasonDisabled,
			Message: pointstore.GiftReasonMessage(pointstore.GiftReasonDisabled)})
	}
	input.Policy = *policy
	recipientID := int64(0)
	if isDigits(key) && len(key) <= 19 {
		if recipientID, err = s.pg.FindPointsStoreGiftRecipient(ctx, req.AppID, key); err != nil {
			return input, nil, err
		}
	}
	if recipientID == 0 {
		return input, nil, s.translate(&pgrepo.PointsStoreGiftError{Reason: pointstore.GiftReasonRecipientNotFound,
			Message: pointstore.GiftReasonMessage(pointstore.GiftReasonRecipientNotFound)})
	}
	if recipientID == req.SenderID {
		return input, nil, s.translate(&pgrepo.PointsStoreGiftError{Reason: pointstore.GiftReasonSelf,
			Message: pointstore.GiftReasonMessage(pointstore.GiftReasonSelf)})
	}
	recipient, err := s.pg.PointsStoreGiftAccount(ctx, req.AppID, recipientID)
	if err != nil {
		return input, nil, s.translate(err)
	}
	input.RecipientID = recipientID
	clock := s.clock()
	input.Windows, input.Location = clock.Windows, clock.Location
	return input, recipient, nil
}

// giftPlatformVerdict 风险决策平台对这次赠送的评估（points_gift 场景）。没接入、没启用这条判据、
// 平台出错时为空：平台抖一下不该让赠送整个不可用，余下的判据照常生效。
func (s *PointsStoreService) giftPlatformVerdict(ctx context.Context, input pointstore.GiftInput) *pointstore.GiftPlatformVerdict {
	if s.risk == nil || !s.risk.Enabled() || !input.Policy.Rule(pointstore.GiftRuleRiskPlatform).Enabled {
		return nil
	}
	appID, senderID := input.AppID, input.SenderID
	result, err := s.risk.Evaluate(ctx, securitydomain.RiskEvalRequest{
		Scene: securitydomain.ScenePointsGift, AppID: &appID, UserID: &senderID,
		IP: input.ClientIP, DeviceID: input.DeviceID, UserAgent: input.UserAgent,
		Extra: map[string]any{"itemId": input.ItemID, "recipientId": input.RecipientID},
	})
	if err != nil || result == nil {
		if err != nil {
			s.log.Warn("points store gift: risk platform evaluate failed, skipped", zap.Error(err))
		}
		return nil
	}
	return &pointstore.GiftPlatformVerdict{DecisionID: result.DecisionID, Action: result.Action, Score: result.Score,
		Reasons: result.Reasons}
}

// ── 视图与通知 ──

// publicGiftUser 用户端看到的另一方：只给对外 ID、号码、昵称与头像。
func publicGiftUser(user pointstore.GiftUser, avatar AvatarResolver) pointstore.GiftUser {
	out := pointstore.GiftUser{PublicID: user.PublicID, Number: user.Number, Nickname: user.Nickname}
	if avatar != nil && user.UserID > 0 {
		out.Avatar = avatar(user.UserID, user.RawAvatar)
	}
	return out
}

// userGiftView 用户端的赠送记录：去掉风控与排障字段，补一句状态说明。
func userGiftView(gift pointstore.Gift, viewerID int64, avatar AvatarResolver) pointstore.Gift {
	out := pointstore.Gift{
		GiftNo: gift.GiftNo, AppID: gift.AppID, RedemptionNo: gift.RedemptionNo, ItemID: gift.ItemID, ItemTitle: gift.ItemTitle,
		Cost: gift.Cost, Results: gift.Results, Message: gift.Message, Status: gift.Status,
		ReviewedAt: gift.ReviewedAt, RevokedAt: gift.RevokedAt, CreatedAt: gift.CreatedAt, UpdatedAt: gift.UpdatedAt,
		Sender: publicGiftUser(gift.Sender, avatar), Recipient: publicGiftUser(gift.Recipient, avatar),
	}
	if out.Results == nil {
		out.Results = []pointstore.RewardResult{}
	}
	out.Direction = "sent"
	if gift.Recipient.UserID == viewerID && gift.Sender.UserID != viewerID {
		out.Direction = "received"
		// 接收人不必知道对方花了多少积分
		out.Cost = 0
		out.RedemptionNo = ""
	}
	switch gift.Status {
	case pointstore.GiftReviewing:
		out.StatusMessage = "正在审核，通过后对方即可收到"
	case pointstore.GiftRejected:
		out.StatusMessage = firstNonEmpty(strings.TrimSpace(gift.ReviewNote), "未通过审核，积分已退回")
	case pointstore.GiftRevoked:
		out.StatusMessage = firstNonEmpty(strings.TrimSpace(gift.RevokeNote), "已撤销，权益已收回")
		if out.Direction == "sent" && gift.RefundTransactionNo != "" {
			out.StatusMessage += "（积分已退回）"
		}
	}
	return out
}

func giftUserName(user pointstore.GiftUser) string {
	switch {
	case strings.TrimSpace(user.Nickname) != "":
		return user.Nickname
	case user.Number > 0:
		return strconv.FormatInt(user.Number, 10)
	}
	return "好友"
}

// notifyGiftDelivered 送达：接收人收到「谁送了什么」与赠言，赠送人收到「已送达」。
func (s *PointsStoreService) notifyGiftDelivered(gift *pointstore.Gift) {
	details := make([]string, 0, len(gift.Results))
	for _, result := range gift.Results {
		details = append(details, result.Detail)
	}
	content := giftUserName(gift.Sender) + " 送给你「" + gift.ItemTitle + "」，已到账：" + strings.Join(details, "、") + "。"
	if message := strings.TrimSpace(gift.Message); message != "" {
		content += "\n留言：" + message
	}
	s.notifyGift(gift, gift.Recipient.UserID, "你收到一份赠送", content, "success")
	s.notifyGift(gift, gift.Sender.UserID, "赠送已送达",
		"你送给 "+giftUserName(gift.Recipient)+" 的「"+gift.ItemTitle+"」已送达。", "success")
	s.publishUser(gift.AppID, gift.Recipient.UserID, map[string]any{"giftNo": gift.GiftNo, "status": gift.Status, "received": true})
}

func (s *PointsStoreService) notifyGift(gift *pointstore.Gift, userID int64, title, content, level string) {
	if s.notifier == nil || userID <= 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := s.notifier.Deliver(ctx, notificationdomain.DeliverInput{
			AppID: gift.AppID, UserID: userID, Type: "points_store",
			Title: title, Content: content, Level: level,
			Metadata: map[string]any{"giftNo": gift.GiftNo, "status": gift.Status, "redemptionNo": gift.RedemptionNo},
			Action:   notificationdomain.RouteAction(notificationdomain.RoutePointsStore, "查看赠送记录"),
		}); err != nil {
			s.log.Warn("points store gift: notify failed", zap.Int64("appid", gift.AppID), zap.Int64("userId", userID), zap.Error(err))
		}
	}()
}

// giftErrorReason 赠送被拒的原因键（核对结果里给客户端分支用）。
func giftErrorReason(err error) string {
	var refused *pgrepo.PointsStoreGiftError
	if errors.As(err, &refused) {
		return refused.Reason
	}
	var unavailable *pgrepo.PointsStoreUnavailableError
	if errors.As(err, &unavailable) {
		return unavailable.Availability.Reason
	}
	if errors.Is(err, pgrepo.ErrInsufficientIntegral) {
		return pointstore.ReasonInsufficientPoints
	}
	return ""
}

// giftReasonCode 赠送被拒的原因 → 错误码。
func giftReasonCode(reason string) (int, int) {
	switch reason {
	case pointstore.GiftReasonDisabled:
		return errCodeGiftDisabled, http.StatusForbidden
	case pointstore.GiftReasonNotGiftItem, pointstore.GiftReasonGiftItem:
		return errCodeGiftItemMismatch, http.StatusUnprocessableEntity
	case pointstore.GiftReasonRecipientNotFound:
		return errCodeGiftRecipientNotFound, http.StatusNotFound
	case pointstore.GiftReasonSelf:
		return errCodeGiftSelf, http.StatusUnprocessableEntity
	case pointstore.GiftReasonRecipientInactive:
		return errCodeGiftRecipientInactive, http.StatusUnprocessableEntity
	case pointstore.GiftReasonRecipientOwned:
		return errCodeGiftRecipientOwned, http.StatusUnprocessableEntity
	case pointstore.GiftReasonLimit:
		return errCodeGiftLimit, http.StatusTooManyRequests
	case pointstore.GiftReasonFrequency:
		return errCodeGiftFrequency, http.StatusTooManyRequests
	}
	return errCodeGiftRisk, http.StatusForbidden
}

// translateGift 赠送相关的仓储错误 → 业务错误；不是赠送的错误返回 nil。
func translateGift(err error) error {
	var refused *pgrepo.PointsStoreGiftError
	if errors.As(err, &refused) {
		code, status := giftReasonCode(refused.Reason)
		message := refused.Message
		if message == "" {
			message = pointstore.GiftReasonMessage(refused.Reason)
		}
		return apperrors.New(code, status, message)
	}
	switch {
	case errors.Is(err, pgrepo.ErrPointsStoreGiftNotFound):
		return apperrors.New(errCodeGiftNotFound, http.StatusNotFound, "赠送记录不存在")
	case errors.Is(err, pgrepo.ErrPointsStoreGiftState):
		return apperrors.New(errCodeGiftState, http.StatusConflict, "这笔赠送当前的状态不允许这样处理")
	case errors.Is(err, pgrepo.ErrPointsStoreRedemptionIsGift):
		return apperrors.New(errCodeRedemptionIsGift, http.StatusConflict, "这笔兑换是赠送，请在赠送记录里审核")
	}
	return nil
}

func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return utf8.ValidString(value)
}
