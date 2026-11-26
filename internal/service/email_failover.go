package service

import (
	"context"
	"net/http"
	"strings"

	emaildomain "aegis/internal/domain/email"
	apperrors "aegis/pkg/errors"

	"go.uber.org/zap"
)

/*
终端用户发起的邮件（验证码、找回密码、重置链接、注销验证码）的通道故障切换。

2026-11 的事故：应用只配了一条 Zeabur 通道，月配额用尽之后每一封验证码都发不出去，
服务商的 429 原样冒到客户端，又被响应层脱敏成「请求过于频繁」—— 用户第一次点「发送验证码」
看到的就是「过于频繁」，以为是自己点多了，实际上一封信都没发过。

两件事一起改：
  - 主通道是自己坏了（服务商拒绝、配额用尽、熔断、超时）就换下一条：应用自己启用的其他配置
    （默认的在前，按 id），最后是平台共享通道。与通道无关的失败（地址格式、抑制名单、平台治理）
    换哪条都一样，原样返回。做法参考 Symfony Mailer 的 FailoverTransport（MIT）：
    只有传输类错误才切下一条，其余错误直接抛出，全部失败再汇总。坏通道一段时间内跳过
    这件事由各服务商适配层的熔断器负责，这里不再重复记一份。
  - 全部通道都发不出去时，报一句用户能懂、也不会和限频混淆的话（50316，HTTP 503）。
    服务商原文（含配额细节）留在投递记录与日志里给管理员看，不往客户端送 ——
    其中有的码与网关业务码撞号（如 Zeabur 的 40071 是「API Key 无效」，网关里 40071 是时间戳过期，
    客户端会据此去校时），原样透出去只会把客户端带偏。

指名了配置（configName）的调用不切换：那是「这个业务必须用这个发件人」，
静默换通道会让这类要求悄悄失效（见 resolveConfig）。
*/

// errCodeEmailDeliveryUnavailable 所有可用邮件通道都发不出去。
const errCodeEmailDeliveryUnavailable = 50316

func errEmailDeliveryUnavailable() error {
	return apperrors.New(errCodeEmailDeliveryUnavailable, http.StatusServiceUnavailable,
		"邮件暂时发不出去，请稍后再试；多次失败请联系客服").Public()
}

// mailAttempt 用某条通道发一次。channelFailed 见 sendRenderedMailVia。
type mailAttempt func(config *emaildomain.Config) (messageID string, channelFailed bool, err error)

// deliverUserMail 先用 primary 发，通道自己出问题时依次换 fallbackConfigs 给出的通道。
func (s *EmailService) deliverUserMail(ctx context.Context, appID int64, primary *emaildomain.Config, configName string, purpose string, attempt mailAttempt) (string, error) {
	var fallbacks func() []*emaildomain.Config
	if strings.TrimSpace(configName) == "" {
		fallbacks = func() []*emaildomain.Config { return s.fallbackConfigs(ctx, appID, primary) }
	}
	messageID, err := deliverAcross(primary, fallbacks, attempt, func(config *emaildomain.Config, err error, last bool) {
		s.log.Warn("email channel failed",
			zap.Int64("appid", appID), zap.Int64("config", config.ID), zap.String("provider", config.Provider),
			zap.String("purpose", purpose), zap.Bool("lastChannel", last), zap.Error(err))
	})
	return messageID, err
}

// deliverAcross 故障切换的骨架，不碰数据库，便于单测。
//
// fallbacks 只在主通道失败后才调用：绝大多数请求一次就发出去了，不该为它们多查一次配置表。
// onChannelFailed 每条通道失败时回调一次，last 表示后面已经没有可换的了。
func deliverAcross(primary *emaildomain.Config, fallbacks func() []*emaildomain.Config, attempt mailAttempt,
	onChannelFailed func(config *emaildomain.Config, err error, last bool)) (string, error) {
	messageID, channelFailed, err := attempt(primary)
	if err == nil {
		return messageID, nil
	}
	if !channelFailed {
		return "", err
	}
	var rest []*emaildomain.Config
	if fallbacks != nil {
		rest = fallbacks()
	}
	if onChannelFailed != nil {
		onChannelFailed(primary, err, len(rest) == 0)
	}
	for i, config := range rest {
		messageID, channelFailed, err = attempt(config)
		if err == nil {
			return messageID, nil
		}
		if !channelFailed {
			return "", err
		}
		if onChannelFailed != nil {
			onChannelFailed(config, err, i == len(rest)-1)
		}
	}
	return "", errEmailDeliveryUnavailable()
}

// fallbackConfigs 主通道之外还能用的通道：本作用域里其他启用的配置，最后是平台共享通道。
//
// 查询出错只记日志并当作没有备用通道 —— 主通道已经失败了，这里再报一个数据库错误
// 只会盖住真正的原因。
func (s *EmailService) fallbackConfigs(ctx context.Context, appID int64, primary *emaildomain.Config) []*emaildomain.Config {
	if s.pg == nil || primary == nil {
		return nil
	}
	seen := map[int64]bool{primary.ID: true}
	out := make([]*emaildomain.Config, 0, 2)
	items, err := s.pg.ListEmailConfigs(ctx, appID)
	if err != nil {
		s.log.Warn("list fallback email configs failed", zap.Int64("appid", appID), zap.Error(err))
	}
	for i := range items {
		item := items[i]
		if !item.Enabled || seen[item.ID] {
			continue
		}
		seen[item.ID] = true
		s.decryptSecrets(&item)
		out = append(out, &item)
	}
	if appID != emaildomain.PlatformAppID {
		shared, err := s.pg.GetSharedPlatformEmailConfig(ctx)
		if err != nil {
			s.log.Warn("load shared platform email config failed", zap.Int64("appid", appID), zap.Error(err))
		} else if shared != nil && !seen[shared.ID] {
			s.decryptSecrets(shared)
			out = append(out, shared)
		}
	}
	return out
}
