package response

import (
	"errors"
	"net/http"
)

const (
	CodeSuccess               = 0
	CodeBadRequest            = 10001
	CodeUnsupportedMediaType  = 10002
	CodeNotAcceptable         = 10003
	CodePayloadTooLarge       = 10004
	CodeNotFound              = 10005
	CodeMethodNotAllowed      = 10006
	CodeValidationFailed      = 10007
	CodeUnauthorized          = 20001
	CodeForbidden             = 20002
	CodeConflict              = 30001
	CodeRateLimited           = 30002
	CodeInternal              = 50000
	CodeDependencyUnavailable = 50001
)

type ValidationDetail struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

type AppError struct {
	Code       int
	HTTPStatus int
	Message    string
	Details    []ValidationDetail
	Err        error
}

func (e *AppError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Message
}

func (e *AppError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func NewError(code, status int, message string, err error) *AppError {
	return &AppError{
		Code:       code,
		HTTPStatus: status,
		Message:    message,
		Err:        err,
	}
}

func BadRequest(message string, err error) *AppError {
	return NewError(CodeBadRequest, http.StatusBadRequest, message, err)
}

func UnsupportedMediaType() *AppError {
	return NewError(CodeUnsupportedMediaType, http.StatusUnsupportedMediaType, "Content-Type 必须是 application/json", nil)
}

func NotAcceptable() *AppError {
	return NewError(CodeNotAcceptable, http.StatusNotAcceptable, "客户端必须接受 JSON 响应", nil)
}

func PayloadTooLarge(err error) *AppError {
	return NewError(CodePayloadTooLarge, http.StatusRequestEntityTooLarge, "请求体过大", err)
}

func NotFound() *AppError {
	return NewError(CodeNotFound, http.StatusNotFound, "资源不存在", nil)
}

func MethodNotAllowed() *AppError {
	return NewError(CodeMethodNotAllowed, http.StatusMethodNotAllowed, "请求方法不允许", nil)
}

func ValidationFailed(details []ValidationDetail) *AppError {
	return &AppError{
		Code:       CodeValidationFailed,
		HTTPStatus: http.StatusUnprocessableEntity,
		Message:    "请求参数不合法",
		Details:    details,
	}
}

func Unauthorized() *AppError {
	return NewError(CodeUnauthorized, http.StatusUnauthorized, "认证失败", nil)
}

func Forbidden() *AppError {
	return NewError(CodeForbidden, http.StatusForbidden, "没有操作权限", nil)
}

func Conflict(message string, err error) *AppError {
	return NewError(CodeConflict, http.StatusConflict, message, err)
}

func RateLimited() *AppError {
	return NewError(CodeRateLimited, http.StatusTooManyRequests, "尝试次数过多，请稍后再试", nil)
}

func Internal(err error) *AppError {
	return NewError(CodeInternal, http.StatusInternalServerError, "内部服务器错误", err)
}

func DependencyUnavailable(err error) *AppError {
	return NewError(CodeDependencyUnavailable, http.StatusServiceUnavailable, "服务暂不可用", err)
}

func AsAppError(err error) *AppError {
	if err == nil {
		return nil
	}
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr
	}
	return Internal(err)
}
