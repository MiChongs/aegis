package errors

type AppError struct {
	Code       int
	HTTPStatus int
	Message    string
	// Data 随错误一起返回给调用方的结构化信息（响应信封的 data），例如封禁详情与申诉凭证；可为空
	Data any
	// PublicMessage 文案是写给终端用户看的、不含内部细节，响应层不必再脱敏。
	//
	// 响应层会把 429 / 5xx 的文案一律换成通用句（防止上游报错、SQL 报错漏到客户端）。
	// 这对「请 37 秒后再试」「邮件暂时发不出去」这类本来就是写给用户的话是误伤：
	// 换成「请求过于频繁」之后，用户第一次点就看到「过于频繁」，既不知道要等多久，
	// 也分不清是自己点多了还是服务端出了问题。只有构造处确认文案可公开时才打这个标记。
	PublicMessage bool
}

func (e *AppError) Error() string {
	return e.Message
}

func New(code int, httpStatus int, message string) *AppError {
	return &AppError{Code: code, HTTPStatus: httpStatus, Message: message}
}

// WithData 带上结构化信息（响应信封的 data）
func (e *AppError) WithData(data any) *AppError {
	e.Data = data
	return e
}

// Public 标记文案可原样给终端用户看，见 PublicMessage。
func (e *AppError) Public() *AppError {
	e.PublicMessage = true
	return e
}
