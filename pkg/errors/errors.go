package errors

type AppError struct {
	Code       int
	HTTPStatus int
	Message    string
}

func (e *AppError) Error() string {
	return e.Message
}

func New(code int, httpStatus int, message string) *AppError {
	return &AppError{Code: code, HTTPStatus: httpStatus, Message: message}
}
