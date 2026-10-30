package room

import "errors"

// Error 可以原样告诉客户端的业务错误：Code 是稳定的机器可读标识，客户端据此选文案。
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func fail(code, message string) *Error { return &Error{Code: code, Message: message} }

// 业务错误。客户端只依赖 Code。
var (
	ErrRoomNotFound    = fail("room_not_found", "房间不存在或已解散")
	ErrNotInRoom       = fail("not_in_room", "你不在这个房间里")
	ErrRoomFull        = fail("room_full", "房间人数已满")
	ErrRoomLocked      = fail("room_locked", "房间已锁定，暂不接受新成员")
	ErrBanned          = fail("banned", "你已被移出这个房间")
	ErrNotHost         = fail("not_host", "只有房主可以这样做")
	ErrNoControl       = fail("no_control", "你没有控制播放的权限")
	ErrRequestsOff     = fail("requests_disabled", "房主关闭了点歌")
	ErrTooManyRequests = fail("too_many_requests", "你的点歌还在等房主处理")
	ErrRequestNotFound = fail("request_not_found", "这条点歌已处理")
	ErrUnknownItem     = fail("unknown_item", "队列里没有这首歌")
	ErrQueueTooLong    = fail("queue_too_long", "队列超过房间上限")
	ErrMemberNotFound  = fail("member_not_found", "房间里没有这个人")
	ErrHostOffline     = fail("host_offline", "房主暂时不在线")
	ErrInvalid         = fail("invalid", "请求内容无效")
	ErrRateLimited     = fail("rate_limited", "操作太频繁，请稍后再试")
	ErrCodeExhausted   = fail("unavailable", "暂时无法创建房间，请稍后再试")
)

// Invalid 带说明的参数错误。
func Invalid(message string) *Error { return fail(ErrInvalid.Code, message) }

// AsError 取出业务错误；其它错误（存储故障等）返回 nil，由调用方按内部错误处理。
func AsError(err error) *Error {
	var target *Error
	if errors.As(err, &target) {
		return target
	}
	return nil
}

// ErrNotFound 存储层「没有这条记录」。
var ErrNotFound = errors.New("not found")

// ErrConflict 存储层乐观并发冲突（重试耗尽）。
var ErrConflict = errors.New("concurrent update conflict")

// ErrCodeTaken 房间号已被占用。
var ErrCodeTaken = errors.New("room code taken")
