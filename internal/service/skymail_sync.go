package service

import (
	emaildomain "aegis/internal/domain/email"
	pgrepo "aegis/internal/repository/postgres"
	"context"
	"net/url"
	"strconv"
	"time"

	"go.uber.org/zap"
)

// Cloud Mail 投递状态对账。
//
// Cloud Mail 把站外信交给 Resend / Cloudflare Email 投递，结果（送达、退信、投诉、延迟）
// 回写到它自己的发件箱，却不向外推送。平台因此定时比对：拿出还停在「已发出」的信，
// 到对应邮局的发件箱里找同一封，状态有变化就走 applyDeliveryEvent —— 与 webhook 回执
// 是同一个入口，自动抑制、时间线、单向推进的规则一条不少。

const (
	skymailSyncInterval = 2 * time.Minute
	// skymailSyncWindow 只对账三天内的信。超过这个时间还没结果的，Resend 那边也不会再变了。
	skymailSyncWindow = 72 * time.Hour
	skymailSyncBatch  = 500
	// skymailSyncMaxPages 每个邮局最多翻几页发件箱（每页 50 封）。
	skymailSyncMaxPages = 20
)

// StartSkymailSync 起对账循环。多实例各自起，靠 Redis 锁保证同一时刻只有一个实例在对账。
func (s *EmailService) StartSkymailSync(ctx context.Context) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.log.Error("skymail sync loop panic", zap.Any("panic", r))
			}
		}()
		timer := time.NewTimer(45 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				if s.acquireSkymailSyncLock(ctx) {
					if updated, err := s.SyncSkymailDeliveries(ctx); err != nil {
						s.log.Warn("Cloud Mail 投递状态对账失败", zap.Error(err))
					} else if updated > 0 {
						s.log.Info("Cloud Mail 投递状态已回填", zap.Int("updated", updated))
					}
				}
				timer.Reset(skymailSyncInterval)
			}
		}
	}()
}

func (s *EmailService) acquireSkymailSyncLock(ctx context.Context) bool {
	if s.redis == nil {
		return true
	}
	ok, err := s.redis.SetNX(ctx, s.keyPrefix+"email:skymail:sync", "1", skymailSyncInterval-10*time.Second).Result()
	if err != nil {
		// Redis 不可用时宁可多个实例都对一遍账（结果幂等），也不要停掉回填
		return true
	}
	return ok
}

// SyncSkymailDeliveries 对一轮账，返回状态被推进的信件数。
func (s *EmailService) SyncSkymailDeliveries(ctx context.Context) (int, error) {
	client := s.skymail()
	if client == nil || s.pg == nil {
		return 0, nil
	}
	pending, err := s.pg.ListPendingProviderDeliveries(ctx, emaildomain.ProviderSkymail,
		time.Now().Add(-skymailSyncWindow), skymailSyncBatch)
	if err != nil || len(pending) == 0 {
		return 0, err
	}
	type configKey struct{ appID, configID int64 }
	groups := map[configKey][]pgrepo.PendingProviderDelivery{}
	for _, item := range pending {
		key := configKey{item.ConfigAppID, item.ConfigID}
		groups[key] = append(groups[key], item)
	}
	updated := 0
	for key, items := range groups {
		if ctx.Err() != nil {
			return updated, ctx.Err()
		}
		count, err := s.syncSkymailConfig(ctx, client, key.appID, key.configID, items)
		updated += count
		if err != nil {
			s.log.Warn("Cloud Mail 通道对账失败",
				zap.Int64("appid", key.appID), zap.Int64("config", key.configID), zap.Error(err))
		}
	}
	return updated, nil
}

func (s *EmailService) syncSkymailConfig(ctx context.Context, client *skymailClient, appID int64, configID int64, items []pgrepo.PendingProviderDelivery) (int, error) {
	config, err := s.loadConfig(ctx, appID, configID)
	if err != nil {
		return 0, err
	}
	if normalizeEmailProvider(config.Provider) != emaildomain.ProviderSkymail {
		// 通道换过服务商：旧信再也对不上账，等它们滑出时间窗
		return 0, nil
	}
	cred := skymailCredentialsOf(config)
	if cred.Password == "" {
		return 0, nil
	}
	host := cred.host()
	wanted := make(map[int64]pgrepo.PendingProviderDelivery, len(items))
	var oldest int64
	for _, item := range items {
		id, itemHost, ok := parseSkymailProviderMessageID(item.ProviderMessageID)
		if !ok || itemHost != host {
			continue
		}
		wanted[id] = item
		if oldest == 0 || id < oldest {
			oldest = id
		}
	}
	if len(wanted) == 0 {
		return 0, nil
	}
	fromAddress, _, _ := config.SenderIdentity()
	accountID, err := client.resolveAccountID(ctx, cred, fromAddress)
	if err != nil {
		return 0, err
	}

	updated := 0
	// allReceive=1 一次查这个用户名下全部邮箱的发件：发件地址中途改过，旧信也找得到
	query := url.Values{
		"accountId": {strconv.FormatInt(accountID, 10)}, "type": {"1"}, "size": {"50"},
		"timeSort": {"0"}, "full": {"0"}, "allReceive": {"1"},
	}
	for page := 0; page < skymailSyncMaxPages && len(wanted) > 0; page++ {
		result, err := client.listEmails(ctx, cred, query)
		if err != nil {
			return updated, err
		}
		for _, email := range result.List {
			delivery, ok := wanted[email.EmailID]
			if !ok {
				continue
			}
			delete(wanted, email.EmailID)
			update, ok := skymailDeliveryUpdate(appID, delivery, email)
			if !ok {
				continue
			}
			matched, err := s.applyDeliveryEvent(ctx, update)
			if err != nil {
				return updated, err
			}
			if matched {
				updated++
			}
		}
		if len(result.List) < 50 {
			break
		}
		last := result.List[len(result.List)-1].EmailID
		if last <= oldest {
			break
		}
		query.Set("emailId", strconv.FormatInt(last, 10))
	}
	return updated, nil
}

// skymailDeliveryUpdate 把 Cloud Mail 的邮件状态翻译成一条投递事件。没有新消息时返回 false。
func skymailDeliveryUpdate(appID int64, delivery pgrepo.PendingProviderDelivery, email SkymailEmail) (pgrepo.EmailDeliveryStatusUpdate, bool) {
	update := pgrepo.EmailDeliveryStatusUpdate{
		AppID:             appID,
		ProviderMessageID: delivery.ProviderMessageID,
		OccurredAt:        time.Now(),
		Payload: map[string]any{
			"source":        "skymail-sync",
			"skymailStatus": email.Status,
			"emailId":       email.EmailID,
		},
	}
	switch email.Status {
	case skymailStatusDelivered:
		update.Status, update.Event, update.MarkDelivered = emaildomain.DeliveryStatusDelivered, "delivered", true
	case skymailStatusBounced:
		update.Status, update.Event = emaildomain.DeliveryStatusBounced, "bounced"
		update.ErrorMessage = "Cloud Mail：收件方退信"
	case skymailStatusComplaint:
		update.Status, update.Event = emaildomain.DeliveryStatusComplained, "complained"
		update.ErrorMessage = "Cloud Mail：收件人投诉为垃圾邮件"
	case skymailStatusNoRecip:
		// 站内收件人不存在：与硬退信同性质，下一封也到不了
		update.Status, update.Event = emaildomain.DeliveryStatusBounced, "bounced"
		update.ErrorMessage = "Cloud Mail：邮局内没有这个收件地址"
	case skymailStatusFailed:
		update.Status, update.Event = emaildomain.DeliveryStatusFailed, "failed"
		update.ErrorMessage = "Cloud Mail：投递失败"
	case skymailStatusDelayed:
		// 延迟不改主状态，只记一条时间线；已经记过就不再重复
		if delivery.LastEvent == "delayed" {
			return update, false
		}
		update.Event = "delayed"
	default:
		return update, false
	}
	return update, true
}
