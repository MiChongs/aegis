package clientip

import (
	"context"
	"strings"
)

type ctxKey struct{}

// WithIP 把判定出的客户端 IP 放进请求上下文。中间件在判定后写入，
// 业务层（拿不到 gin.Context 的服务）据此取「这一次请求」的 IP。
func WithIP(ctx context.Context, ip string) context.Context {
	if ip == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, ip)
}

// FromContext 这一次请求的客户端 IP；没有时返回 fallback。
//
// 会话里存的 IP 是签发令牌（登录）那一刻的地址：令牌靠刷新能活很久，
// 拿它当「当前请求的 IP」会把很早以前的地址一直记下去——
// 2026-11 之前签发的会话里存的还是 CDN 节点、Traefik 的地址。
func FromContext(ctx context.Context, fallback string) string {
	if ip, ok := ctx.Value(ctxKey{}).(string); ok && ip != "" {
		return ip
	}
	return strings.TrimSpace(fallback)
}
