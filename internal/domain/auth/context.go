package auth

import "context"

type sessionKey struct{}

// WithSession 把已登录用户的会话放进请求上下文（鉴权中间件写入）。
// 服务层拿不到 gin.Context，风控接入等需要「这次请求是谁、哪台设备、什么 UA」时从这里取。
func WithSession(ctx context.Context, s *Session) context.Context {
	if s == nil {
		return ctx
	}
	return context.WithValue(ctx, sessionKey{}, s)
}

// SessionFrom 请求上下文里的会话，未登录时为 nil
func SessionFrom(ctx context.Context) *Session {
	if ctx == nil {
		return nil
	}
	s, _ := ctx.Value(sessionKey{}).(*Session)
	return s
}
