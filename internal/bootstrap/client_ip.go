package bootstrap

import (
	"strings"

	"aegis/pkg/banner"
	"aegis/pkg/clientip"

	"go.uber.org/zap"
)

// logClientIPResolution 把生效的客户端 IP 判定方式打进启动日志。
//
// 判定规则由「环境变量 + 平台探测 + 预设展开」三者叠出来，任何一处理解错了，
// 表现都是同一种：限流与封禁按错误的地址执行，而没有任何一条错误日志。
// 因此结论必须在启动时自报一次 —— 排障时第一件事就是看这一行。
func logClientIPResolution(log *zap.Logger, resolver *clientip.Resolver) {
	if log == nil || resolver == nil {
		return
	}
	desc := resolver.Describe()
	fields := []zap.Field{
		zap.String("strategy", string(desc.Strategy)),
		zap.String("list_header", desc.ListHeader),
		zap.Int("trusted_prefixes", len(desc.Prefixes)),
	}
	if desc.Platform != "" {
		fields = append(fields, zap.String("platform", desc.Platform))
	}
	if len(desc.Headers) > 0 {
		fields = append(fields, zap.Strings("headers", desc.Headers))
	}
	if desc.Hops > 0 {
		fields = append(fields, zap.Int("hops", desc.Hops))
	}
	// 「对端受不受信」是判定链上最容易出错的一环，而它可能来自配置、也可能来自
	// 平台探测。只报 true/false 会让人分不清「我配的」还是「它自己认出来的」。
	if desc.TrustsPeer {
		fields = append(fields,
			zap.Bool("trust_peer", true),
			zap.String("trust_peer_reason", desc.PeerTrustReason),
		)
	}
	log.Info("client ip resolution ready", fields...)

	for _, warning := range desc.Warnings {
		log.Warn("client ip resolution warning", zap.String("detail", warning))
	}
}

// clientIPField 横幅「安全」分区里的「客户端 IP」一行。
//
// 报的是生效结果而不是配置原文：`TRUSTED_PROXIES=infra` 展开成 8 个网段、
// 探测到 Zeabur 又补上 Cloudflare 段 —— 照抄配置项的话，这两步都看不见。
func clientIPField(rt BannerRuntime) banner.Field {
	if rt.ClientIP == nil {
		return banner.Field{
			Key:   "客户端 IP",
			Value: "取直连地址",
			State: banner.StateNeutral,
			Note:  "判定器未装配，反代后面会取到反代地址",
		}
	}
	desc := rt.ClientIP.Describe()

	parts := []string{string(desc.Strategy)}
	if desc.PlatformCN != "" {
		parts = append(parts, "探测到 "+desc.PlatformCN)
	}
	switch {
	case len(desc.Headers) > 0:
		parts = append(parts, "读 "+strings.Join(desc.Headers, " / "))
	case desc.Hops > 0:
		parts = append(parts, banner.Countf("%s 右起第 %d 跳", desc.ListHeader, desc.Hops))
	case desc.Strategy != clientip.StrategyPeer:
		parts = append(parts, desc.ListHeader)
	}

	note := banner.Countf("受信 %d 个网段", len(desc.Prefixes))
	if desc.TrustsPeer {
		note = banner.Join(note, "含直连对端（"+desc.PeerTrustReason+"）")
	}
	field := banner.Field{
		Key:   "客户端 IP",
		Value: banner.Join(parts...),
		State: banner.StateOK,
		Note:  note,
	}
	if len(desc.Prefixes) == 0 && !desc.TrustsPeer {
		// 一个网段都不信 = 转发头全部不看。直连公网时这是对的，
		// 挂在反代后面时全站 IP 会收敛成反代地址，所以这里必须显眼。
		field.State = banner.StateNeutral
		field.Note = "不信任任何转发头；反代部署需配置 TRUSTED_PROXIES"
	}
	if desc.TrustsAll {
		field.State = banner.StateWarn
		field.Note = "受信网段覆盖全部地址，客户端 IP 可被伪造"
	}
	return field
}
