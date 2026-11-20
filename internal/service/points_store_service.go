package service

import (
	securitydomain "aegis/internal/domain/security"
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
	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"
)

// 积分商城与功能解锁的错误码。
//
// 403 段按判据各给一个码（客户端据此分支，不匹配文案）：
// 「已兑完」「本周兑完了」「积分不够」对用户是三件不同的事，前两件等、最后一件去攒。
const (
	errCodePointsStoreDisabled      = 40303
	errCodePointsStoreUnavailable   = 40304
	errCodePointsStoreSoldOut       = 40305
	errCodePointsStoreLimitReached  = 40306
	errCodePointsInsufficient       = 40307
	errCodePointsStoreRequirement   = 40308
	errCodePointsStoreOwned         = 40309
	errCodePointsStoreItemNotFound  = 40443
	errCodePointsStoreRedemptionNF  = 40444
	errCodeFeatureUnlockNotFound    = 40445
	errCodePointsStoreRedeeming     = 40911
	errCodePointsStoreHandled       = 40912
	errCodeFeatureAlreadyUnlocked   = 40913
	pointsStoreStatsDefaultDays     = 14
	pointsStoreStatsMaxDays         = 90
	pointsStoreRequestIDMaxLength   = 64
	featureUnlockRevokeReasonLength = 256
)

// PointsStoreService 积分商城：商品、兑换、人工发放的处理，以及功能解锁的管理。
//
// 可兑换判定只有一处（pointstore.Evaluate）：列表上每件商品的 availability 与兑换时的拒绝原因
// 出自同一个函数，用户看到的「为什么灰着」与点了之后的报错永远是同一句话。
type PointsStoreService struct {
	riskHook
	log        *zap.Logger
	pg         *pgrepo.Repository
	notifier   notificationDeliverer
	userEvents UserEventPublisher
}

func NewPointsStoreService(log *zap.Logger, pg *pgrepo.Repository) *PointsStoreService {
	if log == nil {
		log = zap.NewNop()
	}
	return &PointsStoreService{log: log, pg: pg}
}

// SetNotifier 接上站内信。为空时人工发放的处理结果只是不通知。
func (s *PointsStoreService) SetNotifier(notifier notificationDeliverer) { s.notifier = notifier }

// SetEventPublisher 接上实时推送：兑换成功后通知这个用户的其它在线设备刷新积分与权益。
func (s *PointsStoreService) SetEventPublisher(user UserEventPublisher) { s.userEvents = user }

// ── 管理端：目录与开关 ──

// Catalog 控制台的商品表单由它驱动。
func (s *PointsStoreService) Catalog(ctx context.Context, appID int64) (map[string]any, error) {
	categories, err := s.pg.PointsStoreCategories(ctx, appID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"rewards":      pointstore.RewardCatalog(),
		"limitPeriods": pointstore.LimitPeriods(),
		// 补货周期；全站名额的周期与限兑周期同一组
		"restockPeriods": pointstore.RestockPeriods(),
		"timezone":       timeutil.DefaultLocation().String(),
		"audiences":      pointstore.Audiences(),
		"maxRewards":     pointstore.MaxRewardsPerItem,
		"categories":     categories,
	}, nil
}

// Settings 开关。
func (s *PointsStoreService) Settings(ctx context.Context, appID int64) (*pointstore.Settings, error) {
	return s.pg.GetPointsStoreSettings(ctx, appID)
}

// SaveSettings 保存开关。
func (s *PointsStoreService) SaveSettings(ctx context.Context, appID int64, enabled bool, operator string) (*pointstore.Settings, error) {
	return s.pg.SavePointsStoreSettings(ctx, appID, enabled, operator)
}

// ── 管理端：商品 ──

// ListItems 管理端商品列表（含下架的，带兑换汇总），以及每件商品此刻的库存、名额与开放状态。
func (s *PointsStoreService) ListItems(ctx context.Context, appID int64) ([]pointstore.AdminItem, error) {
	items, err := s.pg.ListPointsStoreAdminItems(ctx, appID)
	if err != nil {
		return nil, err
	}
	clock := s.clock()
	global, err := s.pg.PointsStoreGlobalUsage(ctx, appID, clock.Windows)
	if err != nil {
		return nil, err
	}
	for index := range items {
		item := &items[index]
		item.CurrentStock, _ = item.Item.CurrentStock(clock.Windows)
		item.NextRestockAt = item.Item.NextRestockAt(clock.Windows)
		item.OpenNow = item.Item.OpenAt(clock)
		if item.GlobalLimited() {
			item.GlobalUsed = global[item.ID].In(item.GlobalLimitPeriod)
			item.GlobalResetsAt = clock.Windows.Next(item.GlobalLimitPeriod)
		}
	}
	return items, nil
}

// CreateItem 新建商品。
func (s *PointsStoreService) CreateItem(ctx context.Context, input pointstore.ItemInput) (*pointstore.Item, error) {
	input, err := s.prepareItem(ctx, input)
	if err != nil {
		return nil, err
	}
	return s.pg.CreatePointsStoreItem(ctx, input)
}

// UpdateItem 整体更新商品。
func (s *PointsStoreService) UpdateItem(ctx context.Context, itemID int64, input pointstore.ItemInput) (*pointstore.Item, error) {
	input, err := s.prepareItem(ctx, input)
	if err != nil {
		return nil, err
	}
	item, err := s.pg.UpdatePointsStoreItem(ctx, itemID, input)
	return item, s.translate(err)
}

// SetItemStatus 上架 / 下架。
func (s *PointsStoreService) SetItemStatus(ctx context.Context, appID, itemID int64, active bool) (*pointstore.Item, error) {
	status := pointstore.StatusInactive
	if active {
		status = pointstore.StatusActive
	}
	item, err := s.pg.SetPointsStoreItemStatus(ctx, appID, itemID, status)
	return item, s.translate(err)
}

// ReorderItems 按给定顺序重排商品（控制台拖拽排序）。编号去重、最多 500 个。
func (s *PointsStoreService) ReorderItems(ctx context.Context, appID int64, itemIDs []int64) (int64, error) {
	if len(itemIDs) == 0 || len(itemIDs) > 500 {
		return 0, apperrors.New(40000, http.StatusBadRequest, "排序的商品数量须在 1 到 500 之间")
	}
	seen := make(map[int64]struct{}, len(itemIDs))
	ids := make([]int64, 0, len(itemIDs))
	for _, id := range itemIDs {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return 0, apperrors.New(40000, http.StatusBadRequest, "缺少商品编号")
	}
	return s.pg.ReorderPointsStoreItems(ctx, appID, ids)
}

// DeleteItem 删除商品（兑换记录保留快照）。
func (s *PointsStoreService) DeleteItem(ctx context.Context, appID, itemID int64) error {
	return s.translate(s.pg.DeletePointsStoreItem(ctx, appID, itemID))
}

// prepareItem 清洗、校验商品输入，并核对引用的套餐与功能都存在。
//
// 引用在保存时就核对，而不是等到兑换时才发现发不出去：那时用户的积分已经在路上了，
// 能做的只有整笔回滚、报一句「权益已失效」，而运营根本不知道自己配错了。
func (s *PointsStoreService) prepareItem(ctx context.Context, input pointstore.ItemInput) (pointstore.ItemInput, error) {
	input = pointstore.NormalizeItemInput(input)
	if err := pointstore.ValidateItemInput(input); err != nil {
		return input, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	// 保存时填的剩余库存属于「当前这个补货周期」：下个周期开始时回到补货数量
	if input.RestockPeriod != pointstore.RestockNone {
		start := s.clock().Windows.Start(input.RestockPeriod)
		input.StockWindowStart = &start
	}
	plans, features, err := s.pg.PointsStoreCatalogRefs(ctx, input.AppID)
	if err != nil {
		return input, err
	}
	for _, reward := range input.Rewards {
		switch reward.Type {
		case pointstore.RewardVipPlan:
			plan, ok := plans[reward.RefID]
			if !ok {
				return input, apperrors.New(40000, http.StatusBadRequest, "所选会员套餐不存在")
			}
			if plan.Trial {
				return input, apperrors.New(40000, http.StatusBadRequest, "试用套餐不能用于兑换")
			}
		case pointstore.RewardFeature:
			if _, ok := features[reward.Tag]; !ok {
				return input, apperrors.New(40000, http.StatusBadRequest,
					"功能标识 "+reward.Tag+" 未登记，请先在会员功能目录里创建它")
			}
		case pointstore.RewardCosmetic:
			if err := checkCosmeticRef(ctx, s.pg, input.AppID, reward.RefID); err != nil {
				return input, err
			}
		}
	}
	return input, nil
}

// ── 管理端：兑换记录与人工发放 ──

// ListRedemptions 兑换记录。
func (s *PointsStoreService) ListRedemptions(ctx context.Context, query pointstore.RedemptionQuery) (*pointstore.Page[pointstore.Redemption], error) {
	return s.pg.ListPointsStoreRedemptions(ctx, query)
}

// Fulfill 标记一笔待发放的兑换为已发放，并通知用户。
func (s *PointsStoreService) Fulfill(ctx context.Context, input pointstore.HandleInput) (*pointstore.Redemption, error) {
	if err := validateHandleNote(input.Note); err != nil {
		return nil, err
	}
	redemption, err := s.pg.FulfillPointsStoreRedemption(ctx, input)
	if err != nil {
		return nil, s.translate(err)
	}
	content := "你兑换的「" + redemption.ItemTitle + "」已发放。"
	if note := strings.TrimSpace(redemption.Note); note != "" {
		content += "\n" + note
	}
	s.notify(redemption, "兑换已发放", content, "success")
	return redemption, nil
}

// Cancel 取消一笔待发放的兑换：退回积分、库存加回，并通知用户。
func (s *PointsStoreService) Cancel(ctx context.Context, input pointstore.HandleInput) (*pointstore.Redemption, error) {
	if err := validateHandleNote(input.Note); err != nil {
		return nil, err
	}
	redemption, err := s.pg.CancelPointsStoreRedemption(ctx, input)
	if err != nil {
		return nil, s.translate(err)
	}
	content := "你兑换的「" + redemption.ItemTitle + "」已取消，" + strconv.FormatInt(redemption.Cost, 10) + " 积分已退回。"
	if note := strings.TrimSpace(redemption.Note); note != "" {
		content += "\n" + note
	}
	s.notify(redemption, "兑换已取消", content, "warning")
	s.publishUser(redemption.AppID, redemption.UserID, map[string]any{"redemptionNo": redemption.RedemptionNo, "status": redemption.Status})
	return redemption, nil
}

func validateHandleNote(note string) error {
	if utf8.RuneCountInString(strings.TrimSpace(note)) > pointstore.MaxNoteRunes {
		return apperrors.New(40000, http.StatusBadRequest, "备注不能超过 "+strconv.Itoa(pointstore.MaxNoteRunes)+" 字")
	}
	return nil
}

// Stats 概览：窗口内的兑换汇总、按天趋势（缺的日期补零）与兑换最多的商品。
// timezone 为 IANA 时区名（控制台传浏览器时区），决定「今天」与按天分桶；无效或为空用服务器默认时区。
func (s *PointsStoreService) Stats(ctx context.Context, appID int64, days int, timezone string) (*pointstore.Stats, error) {
	if days <= 0 {
		days = pointsStoreStatsDefaultDays
	}
	if days > pointsStoreStatsMaxDays {
		days = pointsStoreStatsMaxDays
	}
	loc := timeutil.DefaultLocation()
	if tz := strings.TrimSpace(timezone); tz != "" {
		if parsed, err := time.LoadLocation(tz); err == nil {
			loc = parsed
		}
	}
	windows := pointstore.WindowsAt(time.Now(), loc)
	since := windows.Day.AddDate(0, 0, -(days - 1))
	stats, err := s.pg.PointsStoreStats(ctx, appID, since, windows.Day, loc.String())
	if err != nil {
		return nil, err
	}
	byDate := make(map[string]pointstore.StatsDay, len(stats.Trend))
	for _, day := range stats.Trend {
		byDate[day.Date] = day
	}
	trend := make([]pointstore.StatsDay, 0, days)
	for i := 0; i < days; i++ {
		date := since.AddDate(0, 0, i).Format("2006-01-02")
		day, ok := byDate[date]
		if !ok {
			day = pointstore.StatsDay{Date: date}
		}
		trend = append(trend, day)
	}
	stats.Trend = trend
	return stats, nil
}

// ── 管理端：功能解锁 ──

// UserUnlocks 一个用户的功能解锁：全部记录 + 当前生效的合并视图。
func (s *PointsStoreService) UserUnlocks(ctx context.Context, appID, userID int64) (map[string]any, error) {
	items, err := s.pg.ListUserFeatureUnlocks(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	facts, err := s.pg.GetVipEntitlementFacts(ctx, appID, userID)
	if err != nil {
		return nil, s.translate(err)
	}
	entitlement := vipdomain.Evaluate(*facts, time.Now())
	return map[string]any{"items": items, "active": entitlement.Unlocks}, nil
}

// GrantUnlock 管理员授予功能解锁（天数为 0 即永久）。
func (s *PointsStoreService) GrantUnlock(ctx context.Context, input pointstore.UnlockGrantInput) (*vipdomain.FeatureUnlock, error) {
	tags := vipdomain.NormalizeFeatureTags([]string{input.Tag})
	if len(tags) == 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "请选择要解锁的功能")
	}
	if input.Days < 0 || input.Days > pointstore.MaxFeatureDays {
		return nil, apperrors.New(40000, http.StatusBadRequest,
			"天数需要在 0–"+strconv.Itoa(pointstore.MaxFeatureDays)+" 之间（0 为永久）")
	}
	feature, err := s.pg.GetVipFeature(ctx, input.AppID, tags[0])
	if err != nil {
		return nil, err
	}
	if feature == nil {
		return nil, apperrors.New(errCodeFeatureUnknown, http.StatusNotFound,
			"功能标识 "+tags[0]+" 未登记，请先在会员功能目录里创建它")
	}
	unlock, err := s.pg.GrantFeatureUnlock(ctx, pgrepo.FeatureUnlockGrant{
		AppID:    input.AppID,
		UserID:   input.UserID,
		Tag:      feature.Tag,
		Days:     input.Days,
		Source:   vipdomain.UnlockSourceAdminGrant,
		Operator: input.Operator,
	})
	if err != nil {
		return nil, s.translate(err)
	}
	s.publishUser(input.AppID, input.UserID, map[string]any{"unlock": feature.Tag})
	return unlock, nil
}

// RevokeUnlock 收回一条功能解锁。
func (s *PointsStoreService) RevokeUnlock(ctx context.Context, appID, userID, unlockID int64, operator, reason string) (*vipdomain.FeatureUnlock, error) {
	if utf8.RuneCountInString(strings.TrimSpace(reason)) > featureUnlockRevokeReasonLength {
		return nil, apperrors.New(40000, http.StatusBadRequest, "原因不能超过 256 字")
	}
	unlock, err := s.pg.RevokeFeatureUnlock(ctx, appID, userID, unlockID, operator, reason)
	if err != nil {
		return nil, s.translate(err)
	}
	s.publishUser(appID, userID, map[string]any{"unlockRevoked": unlock.Tag})
	return unlock, nil
}

// ── 用户端 ──

// Store 积分商城首页：余额、等级、商品与每件商品对这个人的可兑换判定。
//
// 没开启时不报错，回 enabled=false 与空列表：客户端据此隐藏入口，
// 报错的话「没开」和「接口坏了」在客户端上长得一样。
func (s *PointsStoreService) Store(ctx context.Context, appID, userID int64) (*pointstore.ClientStore, error) {
	clock := s.clock()
	now := clock.Now
	store := &pointstore.ClientStore{Categories: []string{}, Items: []pointstore.ClientItem{}, ServerTime: now.UTC(),
		Timezone: clock.Location.String()}
	settings, err := s.pg.GetPointsStoreSettings(ctx, appID)
	if err != nil {
		return nil, err
	}
	facts, err := s.pg.PointsStoreUserFacts(ctx, appID, userID)
	if err != nil {
		return nil, s.translate(err)
	}
	store.Balance, store.Level, store.IsVip = facts.Integral, facts.Level, facts.IsVip
	if !settings.Enabled {
		return store, nil
	}
	store.Enabled = true

	items, err := s.pg.ListPointsStoreItems(ctx, appID, true)
	if err != nil {
		return nil, err
	}
	usage, err := s.pg.PointsStoreUsage(ctx, appID, userID, clock.Windows)
	if err != nil {
		return nil, err
	}
	global, err := s.pg.PointsStoreGlobalUsage(ctx, appID, clock.Windows)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, item := range items {
		// 已结束的不再列出；还没开始的照列（「即将开始」本身就是一条有用的信息）
		if item.EndsAt != nil && !now.Before(*item.EndsAt) {
			continue
		}
		itemUsage := usage[item.ID]
		itemUsage.Global = global[item.ID]
		view := clientItem(item, *facts, itemUsage, clock)
		store.Items = append(store.Items, view)
		if view.Category != "" && !seen[view.Category] {
			seen[view.Category] = true
			store.Categories = append(store.Categories, view.Category)
		}
	}
	return store, nil
}

func clientItem(item pointstore.Item, facts pointstore.UserFacts, usage pointstore.Usage, clock pointstore.Clock) pointstore.ClientItem {
	rewards := pointstore.NormalizeRewards(item.Rewards)
	stock, _ := item.CurrentStock(clock.Windows)
	view := pointstore.ClientItem{
		ID:                item.ID,
		Title:             item.Title,
		Subtitle:          item.Subtitle,
		Description:       item.Description,
		Category:          item.Category,
		Badge:             item.Badge,
		Cost:              item.Cost,
		OriginalCost:      item.OriginalCost,
		Rewards:           make([]pointstore.ClientReward, 0, len(rewards)),
		Stock:             stock,
		SoldCount:         item.SoldCount,
		RestockPeriod:     item.RestockPeriod,
		RestockQuantity:   item.RestockQuantity,
		NextRestockAt:     item.NextRestockAt(clock.Windows),
		GlobalLimitPeriod: item.GlobalLimitPeriod,
		GlobalLimitCount:  item.GlobalLimitCount,
		LimitPeriod:       item.LimitPeriod,
		LimitCount:        item.LimitCount,
		Used:              usage.In(item.LimitPeriod),
		UserTotalLimit:    item.UserTotalLimit,
		UsedTotal:         usage.Total,
		OpenHours:         item.OpenHoursLabel(),
		OpenNow:           item.OpenAt(clock),
		NextOpenAt:        item.NextOpenAt(clock),
		Audience:          item.Audience,
		MinLevel:          item.MinLevel,
		StartsAt:          item.StartsAt,
		EndsAt:            item.EndsAt,
		Availability:      pointstore.Evaluate(item, facts, usage, clock),
	}
	if view.RestockPeriod == "" {
		view.RestockPeriod = pointstore.RestockNone
	}
	if item.LimitCount > 0 {
		view.LimitResetsAt = clock.Windows.Next(item.LimitPeriod)
	}
	if item.GlobalLimited() {
		view.GlobalUsed = usage.Global.In(item.GlobalLimitPeriod)
		view.GlobalResetsAt = clock.Windows.Next(item.GlobalLimitPeriod)
	}
	for _, reward := range rewards {
		view.Rewards = append(view.Rewards, pointstore.DescribeReward(reward, facts.Plans, facts.Features))
	}
	if len(rewards) > 0 {
		view.Kind = rewards[0].Type
	}
	return view
}

// Redeem 兑换一件商品。
func (s *PointsStoreService) Redeem(ctx context.Context, input pointstore.RedeemInput) (*pointstore.RedeemResult, error) {
	input.RequestID = strings.TrimSpace(input.RequestID)
	if len(input.RequestID) > pointsStoreRequestIDMaxLength {
		return nil, apperrors.New(40000, http.StatusBadRequest, "requestId 不能超过 64 位")
	}
	if input.ItemID <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "itemId 无效")
	}
	settings, err := s.pg.GetPointsStoreSettings(ctx, input.AppID)
	if err != nil {
		return nil, err
	}
	if !settings.Enabled {
		return nil, apperrors.New(errCodePointsStoreDisabled, http.StatusForbidden, "积分商城暂未开放")
	}
	if err := s.riskGuard(ctx, securitydomain.RiskEvalRequest{
		Scene: securitydomain.ScenePointsRedeem, AppID: &input.AppID, UserID: &input.UserID,
		IP: input.ClientIP, DeviceID: input.DeviceID, UserAgent: input.UserAgent,
		Extra: map[string]any{"itemId": input.ItemID},
	}, RiskGuardOption{Message: "当前环境存在风险，暂时无法兑换"}); err != nil {
		return nil, err
	}
	clock := s.clock()
	input.Windows, input.Location = clock.Windows, clock.Location
	result, err := s.pg.RedeemPointsStoreItem(ctx, input)
	if err != nil {
		return nil, s.translate(err)
	}
	result.Redemption = userRedemption(result.Redemption)
	if !result.Replayed {
		s.publishUser(input.AppID, input.UserID, map[string]any{
			"balance":      result.Balance,
			"redemptionNo": result.Redemption.RedemptionNo,
			"status":       result.Redemption.Status,
		})
	}
	return result, nil
}

// MyRedemptions 我的兑换记录。
func (s *PointsStoreService) MyRedemptions(ctx context.Context, appID, userID int64, page, limit int) (*pointstore.Page[pointstore.Redemption], error) {
	result, err := s.pg.ListPointsStoreRedemptions(ctx, pointstore.RedemptionQuery{
		AppID: appID, UserID: userID, Page: page, Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	for index := range result.Items {
		result.Items[index] = userRedemption(result.Items[index])
	}
	return result, nil
}

// userRedemption 用户端看到的兑换记录：去掉只对排障有用的字段。
func userRedemption(item pointstore.Redemption) pointstore.Redemption {
	item.Account, item.Nickname = "", ""
	item.DeviceID, item.ClientIP, item.HandledBy = "", "", ""
	item.Rewards = nil
	if item.Results == nil {
		item.Results = []pointstore.RewardResult{}
	}
	return item
}

// ── 内部 ──

// clock 判定所用的此刻：周期边界与开放时段一律按应用默认时区算。
func (s *PointsStoreService) clock() pointstore.Clock {
	return pointstore.ClockAt(time.Now(), timeutil.DefaultLocation())
}

func (s *PointsStoreService) notify(redemption *pointstore.Redemption, title, content, level string) {
	if s.notifier == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := s.notifier.Deliver(ctx, notificationdomain.DeliverInput{
			AppID: redemption.AppID, UserID: redemption.UserID, Type: "points_store",
			Title: title, Content: content, Level: level,
			Metadata: map[string]any{"redemptionNo": redemption.RedemptionNo, "status": redemption.Status},
			Action:   notificationdomain.RouteAction(notificationdomain.RoutePointsStore, "查看兑换记录"),
		}); err != nil {
			s.log.Warn("points store: notify user failed", zap.Int64("appid", redemption.AppID),
				zap.Int64("userId", redemption.UserID), zap.Error(err))
		}
	}()
}

func (s *PointsStoreService) publishUser(appID, userID int64, data map[string]any) {
	if s.userEvents == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.userEvents.PublishUserEvent(ctx, appID, userID, "points.changed", data); err != nil {
			s.log.Debug("publish points store event failed", zap.Error(err))
		}
	}()
}

// translate 把仓储层的判定翻成带错误码的业务错误。
func (s *PointsStoreService) translate(err error) error {
	if err == nil {
		return nil
	}
	var unavailable *pgrepo.PointsStoreUnavailableError
	if errors.As(err, &unavailable) {
		message := unavailable.Availability.Message
		if message == "" {
			message = pointstore.ReasonMessage(unavailable.Availability.Reason)
		}
		return apperrors.New(pointsStoreReasonCode(unavailable.Availability.Reason), http.StatusForbidden, message)
	}
	switch {
	case errors.Is(err, pgrepo.ErrPointsStoreItemNotFound):
		return apperrors.New(errCodePointsStoreItemNotFound, http.StatusNotFound, "商品不存在或已删除")
	case errors.Is(err, pgrepo.ErrPointsStoreRedemptionNotFound):
		return apperrors.New(errCodePointsStoreRedemptionNF, http.StatusNotFound, "兑换记录不存在")
	case errors.Is(err, pgrepo.ErrPointsStoreRedemptionState):
		return apperrors.New(errCodePointsStoreHandled, http.StatusConflict, "这笔兑换已经处理过了")
	case errors.Is(err, pgrepo.ErrFeatureUnlockNotFound):
		return apperrors.New(errCodeFeatureUnlockNotFound, http.StatusNotFound, "解锁记录不存在或已收回")
	case errors.Is(err, pgrepo.ErrFeatureUnlockOwned):
		return apperrors.New(errCodeFeatureAlreadyUnlocked, http.StatusConflict, "该用户已永久解锁这个功能")
	case errors.Is(err, pgrepo.ErrInsufficientIntegral):
		return apperrors.New(errCodePointsInsufficient, http.StatusForbidden, pointstore.ReasonMessage(pointstore.ReasonInsufficientPoints))
	case errors.Is(err, pgrepo.ErrUserNotFound):
		return apperrors.New(40401, http.StatusNotFound, "用户不存在")
	case pgrepo.IsUniqueViolation(err):
		// 同一个 requestId 的并发提交撞上了唯一索引：结果是对的（没有重复扣），让客户端稍后重查
		s.log.Warn("points store redemption hit unique constraint", zap.Error(err))
		return apperrors.New(errCodePointsStoreRedeeming, http.StatusConflict, "兑换正在处理，请稍后查看兑换记录")
	}
	return err
}

// pointsStoreReasonCode 判据 → 错误码。
func pointsStoreReasonCode(reason string) int {
	switch reason {
	case pointstore.ReasonSoldOut, pointstore.ReasonQuotaReached:
		// 名额已满与已兑完对用户是同一件事：现在没有了，等下个周期
		return errCodePointsStoreSoldOut
	case pointstore.ReasonLimitReached:
		return errCodePointsStoreLimitReached
	case pointstore.ReasonInsufficientPoints:
		return errCodePointsInsufficient
	case pointstore.ReasonLevelRequired, pointstore.ReasonVipOnly, pointstore.ReasonNonVipOnly, pointstore.ReasonLifetimeOnly:
		return errCodePointsStoreRequirement
	case pointstore.ReasonOwned:
		return errCodePointsStoreOwned
	default:
		return errCodePointsStoreUnavailable
	}
}
