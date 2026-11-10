package errors

type AppError struct {
	Code       int
	HTTPStatus int
	Message    string
	// Data 随错误一起返回给调用方的结构化信息（响应信封的 data），例如封禁详情与申诉凭证；可为空
	Data any
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
