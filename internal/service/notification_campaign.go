package service

// 站内信群发任务（docs/notification-center.md「群发任务」）。
//
// 发起：校验内容（与 Deliver 同一套规则）与人群 → 落任务 → 叫醒执行器 → 立刻返回。
// 执行：任意实例 SKIP LOCKED 认领 → 定格人群（人数、最大用户 ID）→ 每批 1000 人一条语句写消息与回执、
// 同一事务推进游标 → 提交后失效这批人的缓存（一次管道）→ 交给事件协程推 notification.created
// （只推在线的人，计数一条语句按批查）。事件协程的队列有界，推不过来就让写入等着，不在内存里堆积。
// 中断：心跳过期后别的实例从游标接着发；(campaign_id, user_id) 唯一索引保证不重复。
// 撤回：同一个执行器按批删除这个任务写进去的消息，逐批失效缓存、推 notification.deleted。

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"aegis/internal/cluster"
	notificationdomain "aegis/internal/domain/notification"
	plugindomain "aegis/internal/domain/plugin"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	errCodeNotificationCampaignNotFound = 40427
	errCodeNotificationCampaignState    = 40958
)

const (
	// notificationCampaignBatch 每批投递的人数（一条语句、一个事务）。实测 1000 → 2500 每秒多发约 25%，
	// 再大收益变小、任务行锁（取消 / 撤回要等它）持有得更久
	notificationCampaignBatch = 2000
	// notificationRecallBatch 撤回每批删除的条数
	notificationRecallBatch = 2000
	// notificationCampaignWorkers 本实例同时执行的任务数
	notificationCampaignWorkers = 2
	// notificationCampaignFanoutQueue 每个任务待推送事件的批数上限：满了写入就等着
	notificationCampaignFanoutQueue = 4
	notificationCampaignPoll        = 5 * time.Second
	notificationCampaignStaleAfter  = 2 * time.Minute
	notificationCampaignMaxAttempts = 5
	notificationFanoutTimeout       = 30 * time.Second
	// notificationBulkInlineLimit 旧群发接口人数不超过它就在请求里发完（保留同步返回收件人的老行为）
	notificationBulkInlineLimit = 2000
	// notificationAudienceSamples 人群预览抽几个人
	notificationAudienceSamples = 10
)

// onlineUserFilter 实时发布器若能判断在线，就只给在线的人推（RealtimeService 实现）。
type onlineUserFilter interface {
	FilterOnlineUsers(ctx context.Context, appID int64, userIDs []int64) ([]int64, error)
}

var _ onlineUserFilter = (*RealtimeService)(nil)

func errNotificationCampaignNotFound() error {
	return apperrors.New(errCodeNotificationCampaignNotFound, http.StatusNotFound, "群发任务不存在")
}

func errNotificationCampaignState(message string) error {
	return apperrors.New(errCodeNotificationCampaignState, http.StatusConflict, message)
}

// notificationCampaignRunner 本实例上的群发执行器。
type notificationCampaignRunner struct {
	link clusterLink
	// suffix 进程级随机后缀：没接集群时各实例的节点标识相同，持有者必须仍能区分进程
	suffix  string
	kick    chan struct{}
	running sync.Map // campaignID → context.CancelFunc
	active  atomic.Int32
	wg      sync.WaitGroup
	started atomic.Bool
	cancel  context.CancelFunc
	mu      sync.Mutex
}

func newNotificationCampaignRunner() *notificationCampaignRunner {
	return &notificationCampaignRunner{suffix: uuid.NewString()[:8], kick: make(chan struct{}, 1)}
}

func (r *notificationCampaignRunner) wake() {
	select {
	case r.kick <- struct{}{}:
	default:
	}
}

// SetCluster 接上集群协调：任务持有者标识带上集群节点标识（bootstrap 在 StartCampaigns 之前调用）。
func (s *NotificationService) SetCluster(c *cluster.Cluster) { s.campaigns.link.set(c) }

func (s *NotificationService) campaignOwner() string {
	return s.campaigns.link.get().ID() + "/" + s.campaigns.suffix
}

// ── 管理端：发起、查看、取消、撤回、预览 ──

// CampaignCatalog 表单目录：预设人群、平台、会员身份、账号状态、分类、状态、上限。
func (s *NotificationService) CampaignCatalog() notificationdomain.CampaignCatalog {
	return notificationdomain.BuildCampaignCatalog()
}

// PreviewAudience 人群预览：人数与最近注册的几个人。
func (s *NotificationService) PreviewAudience(ctx context.Context, appID int64, audience notificationdomain.Audience) (*notificationdomain.AudiencePreview, error) {
	normalized, msg := notificationdomain.NormalizeAudience(audience)
	if msg != "" {
		return nil, apperrors.New(40010, http.StatusBadRequest, msg)
	}
	total, err := s.pg.CountNotificationAudience(ctx, appID, normalized)
	if err != nil {
		return nil, err
	}
	samples, err := s.pg.SampleNotificationAudience(ctx, appID, normalized, notificationAudienceSamples)
	if err != nil {
		return nil, err
	}
	return &notificationdomain.AudiencePreview{Audience: normalized, Label: notificationdomain.AudienceLabel(normalized),
		Total: total, Samples: samples}, nil
}

// normalizeCampaignInput 内容按 Deliver 的规则校验（分类默认公告活动、类型默认 announcement），人群、定时、幂等键另校验。
func (s *NotificationService) normalizeCampaignInput(in notificationdomain.CampaignInput) (notificationdomain.CampaignInput, error) {
	out := in
	if strings.TrimSpace(out.Type) == "" {
		out.Type = notificationdomain.TypeAnnouncement
	}
	if strings.TrimSpace(out.Category) == "" {
		out.Category = notificationdomain.CategoryAnnouncement
	}
	content, err := s.normalizeDeliverInput(notificationdomain.DeliverInput{
		AppID: in.AppID, Type: out.Type, Category: out.Category, Title: in.Title, Content: in.Content, Level: in.Level,
		Metadata: in.Metadata, Action: in.Action, Image: in.Image, ExpiresAt: in.ExpiresAt,
	})
	if err != nil {
		return out, err
	}
	out.Type, out.Category, out.Title, out.Content, out.Level = content.Type, content.Category, content.Title, content.Content, content.Level
	out.Action, out.Image, out.ExpiresAt = content.Action, content.Image, content.ExpiresAt
	audience, msg := notificationdomain.NormalizeAudience(in.Audience)
	if msg != "" {
		return out, apperrors.New(40010, http.StatusBadRequest, msg)
	}
	out.Audience = audience
	out.RequestID = strings.TrimSpace(in.RequestID)
	if len(out.RequestID) > notificationdomain.MaxCampaignRequestID {
		return out, apperrors.New(40010, http.StatusBadRequest, "requestId 不能超过 64 个字符")
	}
	if in.ScheduledAt != nil {
		now := s.now()
		at := in.ScheduledAt.UTC()
		switch {
		case at.Before(now.Add(-time.Minute)):
			return out, apperrors.New(40010, http.StatusBadRequest, "定时发送的时间已经过去")
		case at.After(now.Add(notificationdomain.MaxCampaignSchedule)):
			return out, apperrors.New(40010, http.StatusBadRequest, "定时发送最远只能在 90 天之后")
		case !at.After(now):
			// 已经到点（或差不到一分钟）：直接排队
			out.ScheduledAt = nil
		default:
			out.ScheduledAt = &at
		}
	}
	if out.ScheduledAt != nil && out.ExpiresAt != nil && !out.ExpiresAt.After(*out.ScheduledAt) {
		return out, apperrors.New(40010, http.StatusBadRequest, "过期时间必须晚于定时发送的时间")
	}
	return out, nil
}

// CreateCampaign 发起群发（一键全站、指定用户、按条件筛选都走这里），立刻返回任务。
// 同一 requestId 重复提交返回第一次建的任务，created=false。
func (s *NotificationService) CreateCampaign(ctx context.Context, in notificationdomain.CampaignInput) (*notificationdomain.Campaign, bool, error) {
	normalized, err := s.normalizeCampaignInput(in)
	if err != nil {
		return nil, false, err
	}
	if err := s.ensureNotificationAllowed(normalized.AppID); err != nil {
		return nil, false, err
	}
	status := notificationdomain.CampaignQueued
	if normalized.ScheduledAt != nil {
		status = notificationdomain.CampaignScheduled
	} else {
		total, err := s.pg.CountNotificationAudience(ctx, normalized.AppID, normalized.Audience)
		if err != nil {
			return nil, false, err
		}
		if total == 0 {
			return nil, false, apperrors.New(40401, http.StatusNotFound, "未找到可发送的目标用户")
		}
	}
	campaign, created, err := s.pg.CreateNotificationCampaign(ctx, normalized, status, "")
	if err != nil {
		return nil, false, err
	}
	if created {
		s.campaigns.wake()
	}
	return campaign, created, nil
}

// ListCampaigns 任务列表。
func (s *NotificationService) ListCampaigns(ctx context.Context, query notificationdomain.CampaignQuery) (*notificationdomain.CampaignPage, error) {
	query.Status = strings.TrimSpace(query.Status)
	if query.Status == "all" {
		query.Status = ""
	}
	if query.Status != "" && !notificationdomain.ValidCampaignStatus(query.Status) {
		return nil, apperrors.New(40010, http.StatusBadRequest, "未知的任务状态："+query.Status)
	}
	if query.Page < 1 {
		query.Page = 1
	}
	if query.Limit <= 0 {
		query.Limit = 20
	}
	query.Limit = min(query.Limit, 100)
	items, total, err := s.pg.ListNotificationCampaigns(ctx, query)
	if err != nil {
		return nil, err
	}
	return &notificationdomain.CampaignPage{Items: items, Page: query.Page, Limit: query.Limit, Total: total,
		TotalPages: totalPages(total, query.Limit)}, nil
}

// GetCampaign 任务详情（现算已读人数）。
func (s *NotificationService) GetCampaign(ctx context.Context, appID, campaignID int64) (*notificationdomain.Campaign, error) {
	campaign, err := s.pg.GetNotificationCampaign(ctx, appID, campaignID)
	if err != nil {
		return nil, translateCampaignError(err)
	}
	if campaign.Delivered > campaign.Recalled {
		read, err := s.pg.NotificationCampaignReadCount(ctx, campaign.ID)
		if err != nil {
			return nil, err
		}
		campaign.ReadCount = &read
	} else {
		zero := int64(0)
		campaign.ReadCount = &zero
	}
	return campaign, nil
}

// CancelCampaign 取消定时、排队或发送中的任务：发送中的写完当前这一批就停，已经发出的保留（要收回用撤回）。
func (s *NotificationService) CancelCampaign(ctx context.Context, appID, campaignID int64, operator string) (*notificationdomain.Campaign, error) {
	cancelled, err := s.pg.CancelNotificationCampaign(ctx, appID, campaignID, operatorName(operator))
	if err != nil {
		return nil, err
	}
	if !cancelled {
		campaign, err := s.pg.GetNotificationCampaign(ctx, appID, campaignID)
		if err != nil {
			return nil, translateCampaignError(err)
		}
		return nil, errNotificationCampaignState("任务" + campaign.StatusLabel + "，不能取消")
	}
	return s.GetCampaign(ctx, appID, campaignID)
}

// RecallCampaign 撤回：把这个任务发出去的消息从所有人的消息中心删掉（后台按批进行）。
// 还没开始发的任务直接取消。
func (s *NotificationService) RecallCampaign(ctx context.Context, appID, campaignID int64, operator string) (*notificationdomain.Campaign, error) {
	campaign, err := s.pg.GetNotificationCampaign(ctx, appID, campaignID)
	if err != nil {
		return nil, translateCampaignError(err)
	}
	switch campaign.Status {
	case notificationdomain.CampaignScheduled, notificationdomain.CampaignQueued:
		return s.CancelCampaign(ctx, appID, campaignID, operator)
	case notificationdomain.CampaignRecalling, notificationdomain.CampaignRecalled:
		return nil, errNotificationCampaignState("任务" + campaign.StatusLabel)
	}
	recalled, err := s.pg.RecallNotificationCampaign(ctx, appID, campaignID, operatorName(operator))
	if err != nil {
		return nil, err
	}
	if !recalled {
		return nil, errNotificationCampaignState("任务状态已变化，请刷新后重试")
	}
	// 本实例若正在发它：不必等它写完下一批才发现，直接叫停（另一实例上的会在下一批的行锁检查里停下）
	if stop, ok := s.campaigns.running.Load(campaignID); ok {
		stop.(context.CancelFunc)()
	}
	s.campaigns.wake()
	return s.GetCampaign(ctx, appID, campaignID)
}

func operatorName(operator string) string {
	if operator = strings.TrimSpace(operator); operator == "" {
		return "管理员"
	}
	return operator
}

func translateCampaignError(err error) error {
	if errors.Is(err, pgrepo.ErrNotificationCampaignNotFound) {
		return errNotificationCampaignNotFound()
	}
	return err
}

// ── 旧群发接口 ──

// AdminBulkSend POST /notifications/bulk（旧接口，保留请求与响应形状）：按 userIds 或 keyword / enabled 选人。
// 人数不超过 2000 时在请求里发完并返回收件人；更多（只会出现在 userIds 很长时）转后台任务，async=true。
// 两条路都是同一个群发任务，进度与撤回一致。
func (s *NotificationService) AdminBulkSend(ctx context.Context, appID int64, cmd notificationdomain.AdminBulkSendCommand) (*notificationdomain.AdminBulkSendResult, error) {
	if strings.TrimSpace(cmd.Type) == "" {
		return nil, apperrors.New(40010, http.StatusBadRequest, "通知类型不能为空")
	}
	if strings.TrimSpace(cmd.Content) == "" {
		return nil, apperrors.New(40010, http.StatusBadRequest, "通知内容不能为空")
	}
	audience := notificationdomain.Audience{Scope: notificationdomain.AudienceUsers, UserIDs: cmd.UserIDs, Account: notificationdomain.AccountAll}
	if len(cmd.UserIDs) == 0 {
		// 旧口径：不给 enabled 即不论账号状态；人数上限默认 200、最多 2000
		limit := cmd.Limit
		if limit <= 0 {
			limit = 200
		}
		audience = notificationdomain.Audience{Scope: notificationdomain.AudienceFilter, Keyword: cmd.Keyword,
			Account: notificationdomain.AccountAll, Limit: int64(min(limit, notificationBulkInlineLimit))}
		if cmd.Enabled != nil {
			audience.Account = notificationdomain.AccountDisabled
			if *cmd.Enabled {
				audience.Account = notificationdomain.AccountAvailable
			}
		}
	}
	input, err := s.normalizeCampaignInput(notificationdomain.CampaignInput{
		AppID: appID, Type: cmd.Type, Category: cmd.Category, Title: cmd.Title, Content: cmd.Content, Level: cmd.Level,
		Metadata: cmd.Metadata, Action: cmd.Action, Image: cmd.Image, ExpiresAt: cmd.ExpiresAt, Audience: audience,
	})
	if err != nil {
		return nil, err
	}
	if err := s.ensureNotificationAllowed(appID); err != nil {
		return nil, err
	}
	total, err := s.pg.CountNotificationAudience(ctx, appID, input.Audience)
	if err != nil {
		return nil, err
	}
	if total == 0 {
		return nil, apperrors.New(40401, http.StatusNotFound, "未找到可发送的目标用户")
	}
	if total > notificationBulkInlineLimit {
		campaign, _, err := s.pg.CreateNotificationCampaign(ctx, input, notificationdomain.CampaignQueued, "")
		if err != nil {
			return nil, err
		}
		s.campaigns.wake()
		return &notificationdomain.AdminBulkSendResult{AppID: appID, Requested: int(total), RecipientIDs: []int64{},
			Async: true, Campaign: campaign}, nil
	}

	// 人少：以本请求为持有者建一个已在执行的任务，就地发完。请求中途断了，任务心跳过期后由执行器接着发完
	owner := s.campaignOwner() + "/inline-" + uuid.NewString()[:8]
	campaign, _, err := s.pg.CreateNotificationCampaign(ctx, input, notificationdomain.CampaignRunning, owner)
	if err != nil {
		return nil, err
	}
	recipients := make([]int64, 0, total)
	finished, err := s.sendCampaign(ctx, *campaign, owner, func(batch []notificationdomain.Item) {
		batchUsers := make([]int64, len(batch))
		for i, item := range batch {
			batchUsers[i] = *item.UserID
		}
		recipients = append(recipients, batchUsers...)
		// 就地发送：返回之前缓存必须已经失效（调用方紧接着就可能去读）
		s.invalidateCachesBatch(ctx, appID, batchUsers)
		s.runEvent(func(eventCtx context.Context) { s.publishCreatedBatch(eventCtx, appID, batch) })
	})
	if err != nil {
		return nil, err
	}
	if finished != nil {
		campaign = finished
	}
	return &notificationdomain.AdminBulkSendResult{AppID: appID, Requested: int(campaign.Processed), Delivered: len(recipients),
		RecipientIDs: recipients, Campaign: campaign}, nil
}

// ── 执行器 ──

// StartCampaigns 启动群发执行器（API 进程，bootstrap 调用）。
func (s *NotificationService) StartCampaigns(ctx context.Context) {
	r := s.campaigns
	if r.started.Swap(true) {
		return
	}
	loopCtx, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	r.cancel = cancel
	r.mu.Unlock()
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("notification campaign loop panic", zap.Any("panic", rec))
			}
		}()
		ticker := time.NewTicker(notificationCampaignPoll)
		defer ticker.Stop()
		r.wake()
		for {
			select {
			case <-loopCtx.Done():
				return
			case <-ticker.C:
			case <-r.kick:
			}
			s.claimCampaigns(loopCtx)
		}
	}()
}

// StopCampaigns 停下执行器：手上的任务写完当前一批后放回去（别的实例或重启后的自己接着发）。
func (s *NotificationService) StopCampaigns() {
	r := s.campaigns
	r.mu.Lock()
	cancel := r.cancel
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	r.wg.Wait()
}

func (s *NotificationService) claimCampaigns(ctx context.Context) {
	r := s.campaigns
	for ctx.Err() == nil {
		free := notificationCampaignWorkers - int(r.active.Load())
		if free <= 0 {
			return
		}
		stale := s.now().Add(-notificationCampaignStaleAfter)
		if failed, err := s.pg.FailExhaustedNotificationCampaigns(ctx, stale, notificationCampaignMaxAttempts); err != nil {
			if ctx.Err() == nil {
				s.log.Warn("notification campaign: fail exhausted", zap.Error(err))
			}
		} else if failed > 0 {
			s.log.Warn("notification campaign: campaigns failed after repeated interruptions", zap.Int64("campaigns", failed))
		}
		claimed, err := s.pg.ClaimNotificationCampaigns(ctx, s.campaignOwner(), stale, notificationCampaignMaxAttempts, free)
		if err != nil {
			if ctx.Err() == nil {
				s.log.Warn("notification campaign: claim failed", zap.Error(err))
			}
			return
		}
		if len(claimed) == 0 {
			return
		}
		for _, campaign := range claimed {
			r.active.Add(1)
			r.wg.Add(1)
			go func() {
				defer func() {
					r.active.Add(-1)
					r.wg.Done()
					r.wake()
				}()
				s.runCampaign(ctx, campaign)
			}()
		}
	}
}

// runCampaign 执行一个已认领的任务（发送或撤回）。实例关闭时把任务放回去。
func (s *NotificationService) runCampaign(ctx context.Context, campaign notificationdomain.Campaign) {
	owner := s.campaignOwner()
	log := s.log.With(zap.Int64("campaign", campaign.ID), zap.Int64("appid", campaign.AppID), zap.String("status", campaign.Status))
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	s.campaigns.running.Store(campaign.ID, stop)
	defer s.campaigns.running.Delete(campaign.ID)
	defer func() {
		if rec := recover(); rec != nil {
			log.Error("notification campaign panic", zap.Any("panic", rec))
			fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			_ = s.pg.FailNotificationCampaign(fctx, campaign.ID, owner, fmt.Sprint("执行中断：", rec))
		}
	}()

	var err error
	if campaign.Status == notificationdomain.CampaignRecalling {
		err = s.recallCampaign(runCtx, campaign, owner)
	} else {
		fanout := newCampaignFanout(s, campaign.AppID)
		_, err = s.sendCampaign(runCtx, campaign, owner, fanout.push)
		fanout.close()
	}
	if ctx.Err() != nil {
		// 实例在关闭：放回去，不必等心跳过期
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if rerr := s.pg.ReleaseNotificationCampaign(fctx, campaign.ID, owner); rerr != nil {
			log.Warn("notification campaign: release failed", zap.Error(rerr))
		}
		return
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		// 数据库出错：不判失败，留给心跳过期后重新认领（认领次数用完才判失败）
		log.Warn("notification campaign: interrupted", zap.Error(err))
	}
}

// sendCampaign 按批发送直到发完、被取消 / 撤回、或不再归 owner。onBatch 拿到每批写进去的完整消息，
// 由它负责失效这批人的缓存并推事件。发完时返回结束后的任务。
func (s *NotificationService) sendCampaign(ctx context.Context, campaign notificationdomain.Campaign, owner string,
	onBatch func([]notificationdomain.Item)) (*notificationdomain.Campaign, error) {
	if err := s.ensureNotificationAllowed(campaign.AppID); err != nil {
		return nil, s.failCampaign(ctx, campaign.ID, owner, "应用已被限制发送消息")
	}
	if campaign.ExpiresAt != nil && !campaign.ExpiresAt.After(s.now()) {
		return nil, s.failCampaign(ctx, campaign.ID, owner, "消息已过期，停止发送")
	}
	started, err := s.pg.BeginNotificationCampaign(ctx, campaign, owner)
	if err != nil {
		if errors.Is(err, pgrepo.ErrNotificationCampaignNotFound) {
			return nil, nil
		}
		return nil, err
	}
	for ctx.Err() == nil {
		batch, err := s.pg.DeliverNotificationCampaignBatch(ctx, *started, owner, notificationCampaignBatch)
		if err != nil {
			return nil, err
		}
		if len(batch.Inserted) > 0 {
			onBatch(campaignItems(*started, batch))
		}
		if batch.Status != notificationdomain.CampaignRunning {
			if batch.Status == notificationdomain.CampaignSucceeded {
				return s.afterCampaignSent(ctx, *started)
			}
			return nil, nil
		}
	}
	return nil, ctx.Err()
}

func (s *NotificationService) failCampaign(ctx context.Context, campaignID int64, owner, message string) error {
	return s.pg.FailNotificationCampaign(ctx, campaignID, owner, message)
}

// afterCampaignSent 发完：整个任务发一次 notification.onSent 插件钩子。
func (s *NotificationService) afterCampaignSent(ctx context.Context, campaign notificationdomain.Campaign) (*notificationdomain.Campaign, error) {
	finished, err := s.pg.GetNotificationCampaign(ctx, campaign.AppID, campaign.ID)
	if err != nil {
		return nil, err
	}
	if s.plugin != nil {
		appID := finished.AppID
		go s.plugin.ExecuteHook(context.Background(), HookNotificationSent, map[string]any{
			"appId": appID, "campaignId": finished.ID, "delivered": finished.Delivered, "type": finished.Type,
			"category": finished.Category, "title": finished.Title,
		}, plugindomain.HookMetadata{AppID: &appID})
	}
	s.log.Info("notification campaign finished", zap.Int64("campaign", finished.ID), zap.Int64("delivered", finished.Delivered),
		zap.Int64("skipped", finished.Skipped))
	return finished, nil
}

// campaignItems 把一批写入还原成完整的 Item（内容全来自任务，ID 与收件人来自写入结果）。
func campaignItems(c notificationdomain.Campaign, batch pgrepo.NotificationCampaignBatch) []notificationdomain.Item {
	items := make([]notificationdomain.Item, len(batch.Inserted))
	for i, delivery := range batch.Inserted {
		userID := delivery.UserID
		items[i] = notificationdomain.Item{
			ID: delivery.ID, AppID: c.AppID, UserID: &userID, Type: c.Type, Category: c.Category, CategoryLabel: c.CategoryLabel,
			Title: c.Title, Content: c.Content, Level: c.Level, Status: "unread", Action: c.Action, Image: c.Image,
			ExpiresAt: c.ExpiresAt, Metadata: c.Metadata, CreatedAt: batch.CreatedAt, UpdatedAt: batch.CreatedAt,
		}
	}
	return items
}

// recallCampaign 按批删除这个任务写进去的消息，直到删完（任务变 recalled）或不再归 owner。
func (s *NotificationService) recallCampaign(ctx context.Context, campaign notificationdomain.Campaign, owner string) error {
	for ctx.Err() == nil {
		status, removed, err := s.pg.RecallNotificationCampaignBatch(ctx, campaign.ID, owner, notificationRecallBatch)
		if err != nil {
			return err
		}
		if len(removed) > 0 {
			owners := map[int64][]int64{}
			userIDs := make([]int64, 0, len(removed))
			for _, row := range removed {
				if _, ok := owners[row.UserID]; !ok {
					userIDs = append(userIDs, row.UserID)
				}
				owners[row.UserID] = append(owners[row.UserID], row.ID)
			}
			s.invalidateCachesBatch(ctx, campaign.AppID, userIDs)
			fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), notificationFanoutTimeout)
			s.publishDeletedBatch(fctx, campaign.AppID, owners, "admin_recall")
			cancel()
		}
		if status != notificationdomain.CampaignRecalling {
			if status == notificationdomain.CampaignRecalled {
				s.log.Info("notification campaign recalled", zap.Int64("campaign", campaign.ID))
			}
			return nil
		}
	}
	return ctx.Err()
}

// campaignFanout 一个任务的提交后处理协程：先失效这批人的缓存，再推事件。和下一批的写入并行，
// 队列有界，满了 push 就阻塞（让写入等它，而不是在内存里堆积）。实例在这之间崩溃，这批人的缓存
// 最多旧 NotificationCacheMaxTTL（60 秒），事件本来就是尽力而为（客户端重连会拉 summary）。
type campaignFanout struct {
	s     *NotificationService
	appID int64
	queue chan []notificationdomain.Item
	done  chan struct{}
}

func newCampaignFanout(s *NotificationService, appID int64) *campaignFanout {
	f := &campaignFanout{s: s, appID: appID, queue: make(chan []notificationdomain.Item, notificationCampaignFanoutQueue),
		done: make(chan struct{})}
	go func() {
		defer close(f.done)
		for batch := range f.queue {
			func() {
				defer func() {
					if rec := recover(); rec != nil {
						s.log.Error("notification fanout panic", zap.Any("panic", rec))
					}
				}()
				ctx, cancel := context.WithTimeout(context.Background(), notificationFanoutTimeout)
				defer cancel()
				userIDs := make([]int64, len(batch))
				for i, item := range batch {
					userIDs[i] = *item.UserID
				}
				s.invalidateCachesBatch(ctx, appID, userIDs)
				s.publishCreatedBatch(ctx, appID, batch)
			}()
		}
	}()
	return f
}

func (f *campaignFanout) push(batch []notificationdomain.Item) {
	f.queue <- batch
}

// close 等已经排队的批次推完。
func (f *campaignFanout) close() {
	close(f.queue)
	<-f.done
}

// ── 批量实时事件 ──

// onlineOnly 只留在线的人；发布器判断不了在线（Worker 侧、测试）就原样返回。
func (s *NotificationService) onlineOnly(ctx context.Context, appID int64, userIDs []int64) []int64 {
	filter, ok := s.realtime.(onlineUserFilter)
	if !ok || len(userIDs) == 0 {
		return userIDs
	}
	online, err := filter.FilterOnlineUsers(ctx, appID, userIDs)
	if err != nil {
		s.log.Debug("filter online users failed", zap.Error(err), zap.Int64("appid", appID))
		return userIDs
	}
	return online
}

// batchCounts 一批人的未读计数；查失败时每人都是 Unread = -1（emit 不带计数字段）。
func (s *NotificationService) batchCounts(ctx context.Context, appID int64, userIDs []int64) func(int64) notificationdomain.Counts {
	counts, err := s.pg.NotificationUnreadCountsBatch(ctx, appID, userIDs)
	if err != nil {
		s.log.Debug("batch count notifications for realtime failed", zap.Error(err), zap.Int64("appid", appID))
	}
	return func(userID int64) notificationdomain.Counts {
		if err != nil {
			return notificationdomain.Counts{Unread: -1, CategoryUnread: map[string]int64{}}
		}
		if c, ok := counts[userID]; ok {
			return c
		}
		return notificationdomain.Counts{CategoryUnread: map[string]int64{}}
	}
}

// publishCreatedBatch 一批新消息（同一内容、不同收件人）：只推在线的人，计数与偏好各一条语句。
func (s *NotificationService) publishCreatedBatch(ctx context.Context, appID int64, items []notificationdomain.Item) {
	if s.realtime == nil || len(items) == 0 {
		return
	}
	byUser := make(map[int64]notificationdomain.Item, len(items))
	userIDs := make([]int64, 0, len(items))
	for _, item := range items {
		if item.UserID == nil {
			continue
		}
		byUser[*item.UserID] = item
		userIDs = append(userIDs, *item.UserID)
	}
	userIDs = s.onlineOnly(ctx, appID, userIDs)
	if len(userIDs) == 0 {
		return
	}
	countsOf := s.batchCounts(ctx, appID, userIDs)
	category := items[0].Category
	muted := map[int64]bool{}
	if notificationdomain.PushMutable(category) {
		var err error
		if muted, err = s.pg.NotificationPushMutedUsers(ctx, appID, category, userIDs); err != nil {
			s.log.Debug("batch load notification preferences failed", zap.Error(err))
			muted = map[int64]bool{}
		}
	}
	for _, userID := range userIDs {
		counts := countsOf(userID)
		s.emit(ctx, appID, userID, notificationdomain.EventCreated, map[string]any{
			"notification": byUser[userID], "silent": muted[userID],
		}, &counts, "created")
	}
}

// publishDeletedBatch 管理员删除 / 撤回后给受影响的在线用户推 notification.deleted（每人一条，带这个人被删的 ID）。
func (s *NotificationService) publishDeletedBatch(ctx context.Context, appID int64, owners map[int64][]int64, reason string) {
	if s.realtime == nil || len(owners) == 0 {
		return
	}
	userIDs := make([]int64, 0, len(owners))
	for userID := range owners {
		userIDs = append(userIDs, userID)
	}
	userIDs = s.onlineOnly(ctx, appID, userIDs)
	if len(userIDs) == 0 {
		return
	}
	countsOf := s.batchCounts(ctx, appID, userIDs)
	for _, userID := range userIDs {
		counts := countsOf(userID)
		s.emit(ctx, appID, userID, notificationdomain.EventDeleted, map[string]any{
			"ids": owners[userID], "cleared": false,
		}, &counts, reason)
	}
}
