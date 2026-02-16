package errors

import (
	"errors"
	"fmt"
)

// ErrorCode 错误码类型
type ErrorCode string

const (
	CodeInvalidInput   ErrorCode = "INVALID_INPUT"
	CodeNotFound       ErrorCode = "NOT_FOUND"
	CodeAlreadyExists  ErrorCode = "ALREADY_EXISTS"
	CodeUnauthorized   ErrorCode = "UNAUTHORIZED"
	CodeForbidden      ErrorCode = "FORBIDDEN"
	CodeInternal       ErrorCode = "INTERNAL_ERROR"
	CodeServiceUnavail ErrorCode = "SERVICE_UNAVAILABLE"
)

// AppError 应用错误
type AppError struct {
	Code    ErrorCode
	Message string
	Err     error
}

// Error 实现 error 接口
func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap 实现 errors.Unwrap
func (e *AppError) Unwrap() error {
	return e.Err
}

// NewInvalidInputError 创建无效输入错误
func NewInvalidInputError(message string) *AppError {
	return &AppError{
		Code:    CodeInvalidInput,
		Message: message,
	}
}

// NewNotFoundError 创建未找到错误
func NewNotFoundError(message string) *AppError {
	return &AppError{
		Code:    CodeNotFound,
		Message: message,
	}
}

// NewAlreadyExistsError 创建已存在错误
func NewAlreadyExistsError(message string) *AppError {
	return &AppError{
		Code:    CodeAlreadyExists,
		Message: message,
	}
}

// NewInternalError 创建内部错误
func NewInternalError(message string) *AppError {
	return &AppError{
		Code:    CodeInternal,
		Message: message,
	}
}

// NewInternalErrorWithCause 创建带原因的内部错误
func NewInternalErrorWithCause(message string, cause error) *AppError {
	return &AppError{
		Code:    CodeInternal,
		Message: message,
		Err:     cause,
	}
}

// IsNotFound 判断是否为未找到错误
func IsNotFound(err error) bool {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Code == CodeNotFound
	}
	return false
}

// IsInvalidInput 判断是否为无效输入错误
func IsInvalidInput(err error) bool {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Code == CodeInvalidInput
	}
	return false
}
