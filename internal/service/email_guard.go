package service

import (
	emaildomain "aegis/internal/domain/email"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"time"

	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

/*
发信的护栏：验证码与重置链接的限频、错误次数、存储方式，以及抑制名单。

此前公开的 /auth/email/code 没有任何频次限制（只有按 IP 的全局限流，而网关路径根本不在那条规则里），
一个脚本就能把任意地址轰炸到发信域名被服务商封禁；验证码明文存在 Redis 里，
校验接口可以无限次试错，6 位数字在 10 分钟里并不算难猜。现在：

  - 重发冷却：同一地址同一用途 N 秒内只发一次（失败的那次不算，立刻可以重试）
  - 频次上限：单个地址每小时 / 每天、单个 IP 每小时，按作用域计
  - 错误次数：同一个码错 N 次即作废，必须重新获取
  - 只存摘要：Redis 里是 HMAC 摘要，拿到 Redis 读权限也拿不到能用的码
  - 抑制名单：硬退信与投诉的地址不再投递（通道自检除外）

全部规则来自邮件策略（Policy.Code），管理员可在控制台调整。
*/

// CodeRequest 一次验证码发送。
type CodeRequest struct {
	AppID   int64
	Email   string
	Purpose string
	// ExpireMinutes 调用方希望的有效期；只能比策略里的短，不能更长
	ExpireMinutes int
	ConfigName    string
	// IP 发起请求的客户端地址，用于按 IP 计频次；内部调用留空即不计
	IP string
}

var emailPurposePattern = regexp.MustCompile(`^[a-z0-9_-]{0,48}$`)

// SendCode 发一封验证码邮件并保存摘要。
func (s *EmailService) SendCode(ctx context.Context, request CodeRequest) (*emaildomain.VerificationResult, error) {
	email := strings.TrimSpace(request.Email)
	if _, err := mail.ParseAddress(email); err != nil {
		return nil, apperrors.New(40062, http.StatusBadRequest, "邮箱地址格式错误")
	}
	purpose := normalizeEmailPurpose(request.Purpose)
	if purpose == "test" || !emailPurposePattern.MatchString(purpose) {
		return nil, apperrors.New(40000, http.StatusBadRequest, "验证码用途不合法")
	}
	config, err := s.resolveConfig(ctx, request.AppID, request.ConfigName)
	if err != nil {
		return nil, err
	}
	policy := s.policyFor(ctx, request.AppID)
	ttl := policy.Code.TTLMinutes
	if request.ExpireMinutes > 0 && request.ExpireMinutes < ttl {
		ttl = request.ExpireMinutes
	}
	// 抑制名单先查：被拦下的请求不该消耗冷却与频次
	if err := s.ensureNotSuppressed(ctx, request.AppID, email); err != nil {
		return nil, err
	}
	release, err := s.reserveSend(ctx, request.AppID, purpose, email, request.IP, policy.Code)
	if err != nil {
		return nil, err
	}

	code := generateEmailCodeLength(policy.Code.Length)
	expireAt, messageID, err := s.sendCodeMail(ctx, request.AppID, config, email, code, purpose, ttl)
	if err != nil {
		release()
		return nil, err
	}
	key := s.emailCodeKey(request.AppID, purpose, email)
	if err := s.storeSecret(ctx, key, emailCodeDigestInput(request.AppID, purpose, email, code), timeutil.Minutes(ttl)); err != nil {
		return nil, err
	}
	return &emaildomain.VerificationResult{Success: true, Email: email, Purpose: purpose, ExpireAt: expireAt, MessageID: messageID}, nil
}

// reserveSend 占冷却与频次额度。返回的 release 在发送失败时调用，把冷却退回去。
//
// Redis 出错时放行并记 warn：为一次缓存抖动让所有人收不到验证码，代价比偶尔多发一封大得多。
func (s *EmailService) reserveSend(ctx context.Context, appID int64, purpose string, email string, ip string, rule emaildomain.CodePolicy) (func(), error) {
	noop := func() {}
	if s.redis == nil {
		return noop, nil
	}
	address := strings.ToLower(strings.TrimSpace(email))
	coolKey := fmt.Sprintf("%s:email:cool:%d:%s:%s", s.keyPrefix, appID, purpose, address)
	if rule.ResendSeconds > 0 {
		ok, err := s.redis.SetNX(ctx, coolKey, "1", time.Duration(rule.ResendSeconds)*time.Second).Result()
		if err != nil {
			s.log.Warn("email cooldown check failed", zap.Int64("appid", appID), zap.Error(err))
		} else if !ok {
			wait := rule.ResendSeconds
			if ttl, err := s.redis.TTL(ctx, coolKey).Result(); err == nil && ttl > 0 {
				wait = int((ttl + time.Second - 1) / time.Second)
			}
			return nil, apperrors.New(42961, http.StatusTooManyRequests, fmt.Sprintf("发送太频繁，请 %d 秒后再试", wait))
		}
	}
	release := func() {
		if rule.ResendSeconds > 0 {
			_ = s.redis.Del(context.WithoutCancel(ctx), coolKey).Err()
		}
	}

	now := timeutil.Now()
	hour := now.Unix() / 3600
	day := now.In(timeutil.DefaultLocation()).Format("20060102")
	quotas := []struct {
		key     string
		limit   int
		ttl     time.Duration
		code    int
		message string
	}{
		{fmt.Sprintf("%s:email:quota:h:%d:%s:%d", s.keyPrefix, appID, address, hour), rule.AddressHourly, 2 * time.Hour,
			42962, "该邮箱一小时内获取验证码的次数已达上限，请稍后再试"},
		{fmt.Sprintf("%s:email:quota:d:%d:%s:%s", s.keyPrefix, appID, address, day), rule.AddressDaily, 26 * time.Hour,
			42962, "该邮箱今天获取验证码的次数已达上限，请明天再试"},
	}
	if ip = strings.TrimSpace(ip); ip != "" {
		quotas = append(quotas, struct {
			key     string
			limit   int
			ttl     time.Duration
			code    int
			message string
		}{fmt.Sprintf("%s:email:quota:ip:%d:%s:%d", s.keyPrefix, appID, ip, hour), rule.IPHourly, 2 * time.Hour,
			42963, "当前网络获取验证码的次数过多，请稍后再试"})
	}
	taken := make([]string, 0, len(quotas))
	for _, quota := range quotas {
		if quota.limit <= 0 {
			continue
		}
		count, err := s.redis.Incr(ctx, quota.key).Result()
		if err != nil {
			s.log.Warn("email quota check failed", zap.Int64("appid", appID), zap.Error(err))
			continue
		}
		if count == 1 {
			_ = s.redis.Expire(ctx, quota.key, quota.ttl).Err()
		}
		if count > int64(quota.limit) {
			// 被拒的这次不占额度，已经占下的也退回去
			_ = s.redis.Decr(ctx, quota.key).Err()
			for _, key := range taken {
				_ = s.redis.Decr(ctx, key).Err()
			}
			release()
			return nil, apperrors.New(quota.code, http.StatusTooManyRequests, quota.message)
		}
		taken = append(taken, quota.key)
	}
	return release, nil
}

// ── 摘要存储与校验 ──

// secretDigestPrefix 摘要格式的版本前缀。没有前缀的值是升级前写入的明文，校验时照常兼容。
const secretDigestPrefix = "h1:"

func emailCodeDigestInput(appID int64, purpose string, email string, code string) secretInput {
	return secretInput{
		raw:    strings.TrimSpace(code),
		binder: fmt.Sprintf("code\x00%d\x00%s\x00%s", appID, normalizeEmailPurpose(purpose), strings.ToLower(strings.TrimSpace(email))),
	}
}

func resetTokenDigestInput(appID int64, email string, token string) secretInput {
	return secretInput{
		raw:    strings.TrimSpace(token),
		binder: fmt.Sprintf("reset\x00%d\x00%s", appID, strings.ToLower(strings.TrimSpace(email))),
	}
}

// secretInput 一次性凭据与它绑定的上下文。摘要覆盖两者：同一个码换个地址或用途就不成立。
type secretInput struct {
	raw    string
	binder string
}

func (s *EmailService) digestSecret(input secretInput) string {
	mac := hmac.New(sha256.New, s.masterKey)
	mac.Write([]byte(input.binder))
	mac.Write([]byte{0})
	mac.Write([]byte(input.raw))
	return secretDigestPrefix + hex.EncodeToString(mac.Sum(nil))
}

func (s *EmailService) storeSecret(ctx context.Context, key string, input secretInput, ttl time.Duration) error {
	pipe := s.redis.TxPipeline()
	pipe.Set(ctx, key, s.digestSecret(input), ttl)
	pipe.Del(ctx, key+":tries")
	_, err := pipe.Exec(ctx)
	return err
}

// verifyStoredSecret 校验一次性凭据。通过即作废；错满策略里的次数也作废。
func (s *EmailService) verifyStoredSecret(ctx context.Context, appID int64, key string, input secretInput) (bool, error) {
	if input.raw == "" {
		return false, nil
	}
	stored, err := s.redis.Get(ctx, key).Result()
	if err != nil {
		if err == redislib.Nil {
			return false, nil
		}
		return false, err
	}
	stored = strings.TrimSpace(stored)
	var match bool
	if strings.HasPrefix(stored, secretDigestPrefix) {
		match = subtle.ConstantTimeCompare([]byte(stored), []byte(s.digestSecret(input))) == 1
	} else {
		match = subtle.ConstantTimeCompare([]byte(stored), []byte(input.raw)) == 1
	}
	triesKey := key + ":tries"
	if match {
		_ = s.redis.Del(ctx, key, triesKey).Err()
		return true, nil
	}

	maxAttempts := s.policyFor(ctx, appID).Code.MaxAttempts
	tries, err := s.redis.Incr(ctx, triesKey).Result()
	if err != nil {
		return false, nil
	}
	if tries == 1 {
		if ttl, err := s.redis.PTTL(ctx, key).Result(); err == nil && ttl > 0 {
			_ = s.redis.PExpire(ctx, triesKey, ttl).Err()
		} else {
			_ = s.redis.Expire(ctx, triesKey, time.Hour).Err()
		}
	}
	if tries >= int64(maxAttempts) {
		_ = s.redis.Del(ctx, key, triesKey).Err()
		return false, apperrors.New(42964, http.StatusTooManyRequests, "错误次数过多，验证码已失效，请重新获取")
	}
	return false, nil
}

// ── 密码重置链接（旧版接口） ──

// ResetLinkRequest 旧版「发重置链接」接口的入参。
type ResetLinkRequest struct {
	AppID      int64
	Email      string
	ResetURL   string
	ConfigName string
	IP         string
}

// SendPasswordResetLink 给账号邮箱发重置链接。
//
// 两处收紧：
//   - 链接地址必须落在策略里登记的前缀内。此前接口原样采用调用方给的 resetUrl，
//     于是任何人都能用应用自己的发件人身份，把带着真实令牌的链接指向自己的网站 —— 一个现成的钓鱼中继。
//   - 只给真实存在的账号发信。不存在的地址同样返回成功、但不发信也不生成令牌，响应上看不出差别，
//     既不让接口被拿来轰炸任意地址，也不泄露「这个邮箱注册过没有」。
func (s *EmailService) SendPasswordResetLink(ctx context.Context, request ResetLinkRequest) (*emaildomain.ResetResult, error) {
	email := strings.TrimSpace(request.Email)
	if _, err := mail.ParseAddress(email); err != nil {
		return nil, apperrors.New(40062, http.StatusBadRequest, "邮箱地址格式错误")
	}
	config, err := s.resolveConfig(ctx, request.AppID, request.ConfigName)
	if err != nil {
		return nil, err
	}
	policy, brand, err := s.mailContext(ctx, request.AppID)
	if err != nil {
		return nil, err
	}
	base, ok := emaildomain.MatchResetLink(policy.ResetLinks, request.ResetURL)
	if !ok {
		if len(policy.ResetLinks) == 0 {
			return nil, apperrors.New(42291, http.StatusUnprocessableEntity,
				"尚未登记密码重置链接，请在控制台「邮件 → 发信策略」中添加允许的地址")
		}
		return nil, apperrors.New(42291, http.StatusUnprocessableEntity, "重置链接不在登记的地址范围内")
	}
	expireAt := timeutil.Now().Add(timeutil.Minutes(passwordResetTTLMinutes))
	result := &emaildomain.ResetResult{Success: true, Email: email, ExpireAt: expireAt}

	exists, err := s.accountEmailExists(ctx, request.AppID, email)
	if err != nil {
		return nil, err
	}
	if !exists {
		return result, nil
	}
	if err := s.ensureNotSuppressed(ctx, request.AppID, email); err != nil {
		return nil, err
	}
	release, err := s.reserveSend(ctx, request.AppID, "password_reset_link", email, request.IP, policy.Code)
	if err != nil {
		return nil, err
	}
	token, err := generateResetToken()
	if err != nil {
		release()
		return nil, err
	}
	resetURL := base + "?token=" + token + "&email=" + email
	subject, html, text := composeScene(policy, brand, sceneMail{
		Scene:     emaildomain.ScenePasswordResetLink,
		Vars:      map[string]string{"minutes": fmt.Sprint(passwordResetTTLMinutes), "email": email},
		Preheader: fmt.Sprintf("重置密码的链接 %d 分钟内有效", passwordResetTTLMinutes),
		Blocks:    passwordResetBlocks(resetURL, passwordResetTTLMinutes),
	})
	messageID, err := s.sendRenderedMail(ctx, request.AppID, config, email, subject, mailBody{HTML: html, Text: text}, "password_reset_link")
	if err != nil {
		release()
		return nil, err
	}
	if err := s.storeSecret(ctx, s.resetTokenKey(request.AppID, email), resetTokenDigestInput(request.AppID, email, token),
		timeutil.Minutes(passwordResetTTLMinutes)); err != nil {
		return nil, err
	}
	result.Token, result.ResetURL, result.MessageID = token, resetURL, messageID
	return result, nil
}

// accountEmailExists 这个地址是不是应用里某个账号的邮箱（资料邮箱，或账号名本身就是这个地址）。
func (s *EmailService) accountEmailExists(ctx context.Context, appID int64, email string) (bool, error) {
	userID, err := s.pg.FindUserIDByProfileEmail(ctx, appID, email)
	if err != nil {
		return false, err
	}
	if userID > 0 {
		return true, nil
	}
	user, err := s.pg.GetUserByAppAndAccount(ctx, appID, strings.ToLower(email))
	if err != nil {
		return false, err
	}
	return user != nil, nil
}

// ── 抑制名单 ──

// ensureNotSuppressed 地址在抑制名单里就拒绝投递，并说清原因。
// 查询失败时放行：抑制名单是保护发信信誉的，不该因为它自己出错让验证码发不出去。
func (s *EmailService) ensureNotSuppressed(ctx context.Context, appID int64, address string) error {
	if s.pg == nil {
		return nil
	}
	item, err := s.pg.GetActiveEmailSuppression(ctx, appID, address)
	if err != nil {
		s.log.Warn("check email suppression failed", zap.Int64("appid", appID), zap.Error(err))
		return nil
	}
	if item == nil {
		return nil
	}
	reason := "已被暂停投递"
	switch item.Reason {
	case emaildomain.SuppressionBounce:
		reason = "此前的邮件被退回，已暂停投递"
	case emaildomain.SuppressionComplaint:
		reason = "收件人曾将邮件标记为垃圾邮件，已暂停投递"
	}
	return apperrors.New(42290, http.StatusUnprocessableEntity, "该邮箱"+reason+"，请更换邮箱或联系客服")
}

// ListSuppressions 分页查询抑制名单。
func (s *EmailService) ListSuppressions(ctx context.Context, query emaildomain.SuppressionQuery) (*emaildomain.SuppressionPage, error) {
	if err := s.ensureScope(ctx, query.AppID); err != nil {
		return nil, err
	}
	return s.pg.ListEmailSuppressions(ctx, query)
}

// AddSuppression 管理员手动加入抑制名单。
func (s *EmailService) AddSuppression(ctx context.Context, input emaildomain.SuppressionInput) (*emaildomain.Suppression, error) {
	if err := s.ensureScope(ctx, input.AppID); err != nil {
		return nil, err
	}
	if err := emaildomain.ValidateSuppressionInput(&input); err != nil {
		return nil, apperrors.New(42292, http.StatusUnprocessableEntity, err.Error())
	}
	return s.pg.UpsertEmailSuppression(ctx, emaildomain.Suppression{
		AppID:     input.AppID,
		Address:   input.Address,
		Reason:    input.Reason,
		Source:    emaildomain.SuppressionSourceAdmin,
		Note:      input.Note,
		CreatedBy: strings.TrimSpace(input.Operator),
		ExpiresAt: input.ExpiresAt,
	})
}

// RemoveSuppression 移出抑制名单。
func (s *EmailService) RemoveSuppression(ctx context.Context, appID int64, id int64) error {
	if err := s.ensureScope(ctx, appID); err != nil {
		return err
	}
	deleted, err := s.pg.DeleteEmailSuppression(ctx, appID, id)
	if err != nil {
		return err
	}
	if !deleted {
		return apperrors.New(40470, http.StatusNotFound, "抑制记录不存在")
	}
	return nil
}

// applyDeliveryEvent 回执落地的唯一入口：推进状态、记事件，再按策略自动抑制。
//
// 自动抑制只看**这次事件推进到的终态**：软退信不改主状态，自然也不会触发；
// 抑制记在信所属的作用域（发信的应用）上，而不是回执地址的作用域 —— 借道平台通道的信，
// 拦的应该是这个应用的下一封。
func (s *EmailService) applyDeliveryEvent(ctx context.Context, update pgrepo.EmailDeliveryStatusUpdate) (bool, error) {
	outcome, err := s.pg.ApplyEmailDeliveryEventOutcome(ctx, update)
	if err != nil || !outcome.Matched {
		return outcome.Matched, err
	}
	var reason string
	switch {
	case update.Status == emaildomain.DeliveryStatusBounced && outcome.Status == emaildomain.DeliveryStatusBounced:
		reason = emaildomain.SuppressionBounce
	case update.Status == emaildomain.DeliveryStatusComplained && outcome.Status == emaildomain.DeliveryStatusComplained:
		reason = emaildomain.SuppressionComplaint
	default:
		return true, nil
	}
	policy := s.policyFor(ctx, outcome.AppID)
	if (reason == emaildomain.SuppressionBounce && !policy.Suppression.HardBounce) ||
		(reason == emaildomain.SuppressionComplaint && !policy.Suppression.Complaint) {
		return true, nil
	}
	note := strings.TrimSpace(update.ErrorMessage)
	if len([]rune(note)) > 200 {
		note = string([]rune(note)[:200])
	}
	if _, err := s.pg.UpsertEmailSuppression(ctx, emaildomain.Suppression{
		AppID:      outcome.AppID,
		Address:    outcome.ToAddress,
		Reason:     reason,
		Source:     emaildomain.SuppressionSourceWebhook,
		Note:       note,
		DeliveryID: outcome.DeliveryID,
	}); err != nil {
		// 回执本身已经落地，抑制失败只记日志：返回错误会让服务商重推，状态却已推进过了
		s.log.Warn("auto suppress email address failed",
			zap.Int64("appid", outcome.AppID), zap.Int64("delivery", outcome.DeliveryID), zap.Error(err))
	}
	return true, nil
}

// ── 投递详情与概览 ──

// DeliveryDetail 一封信的留痕、回执时间线，以及收件地址当前是否被抑制。
func (s *EmailService) DeliveryDetail(ctx context.Context, appID int64, id int64) (*emaildomain.DeliveryDetail, error) {
	if err := s.ensureScope(ctx, appID); err != nil {
		return nil, err
	}
	delivery, err := s.pg.GetEmailDelivery(ctx, appID, id)
	if err != nil {
		return nil, err
	}
	if delivery == nil {
		return nil, apperrors.New(40469, http.StatusNotFound, "投递记录不存在")
	}
	events, err := s.pg.ListEmailDeliveryEvents(ctx, delivery.ID)
	if err != nil {
		return nil, err
	}
	suppression, err := s.pg.GetActiveEmailSuppression(ctx, appID, delivery.ToAddress)
	if err != nil {
		return nil, err
	}
	return &emaildomain.DeliveryDetail{Delivery: *delivery, Events: events, Suppression: suppression}, nil
}

// DeliveryOverview 概览：累计数字、按天趋势、按用途与状态的分布。tz 为浏览器时区，非法时按服务器时区。
func (s *EmailService) DeliveryOverview(ctx context.Context, appID int64, days int, tz string) (*emaildomain.DeliveryOverview, error) {
	if err := s.ensureScope(ctx, appID); err != nil {
		return nil, err
	}
	switch {
	case days <= 0:
		days = 30
	case days > 180:
		days = 180
	}
	tz = strings.TrimSpace(tz)
	if _, err := time.LoadLocation(tz); tz == "" || err != nil {
		tz = timeutil.DefaultLocation().String()
	}
	return s.pg.EmailDeliveryOverview(ctx, appID, days, tz)
}
