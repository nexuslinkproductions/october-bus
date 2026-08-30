package bus

import (
	"errors"
	"net/http"
)

type ErrorCode string

const (
	CodeInvalidArgument  ErrorCode = "INVALID_ARGUMENT"
	CodeUnauthenticated  ErrorCode = "UNAUTHENTICATED"
	CodePermissionDenied ErrorCode = "PERMISSION_DENIED"
	CodeNotFound         ErrorCode = "NOT_FOUND"
	CodeConflict         ErrorCode = "CONFLICT"
	CodeBackpressure     ErrorCode = "BACKPRESSURE"
	CodeInternal         ErrorCode = "INTERNAL"
)

type BusError struct {
	Code    ErrorCode
	Message string
}

func (e *BusError) Error() string { return e.Message }

func Errorf(code ErrorCode, message string) error {
	return &BusError{Code: code, Message: message}
}

func AsBusError(err error) *BusError {
	var target *BusError
	if errors.As(err, &target) {
		return target
	}
	return &BusError{Code: CodeInternal, Message: "Internal October Bus error"}
}

func ErrorStatus(err error) int {
	switch AsBusError(err).Code {
	case CodeInvalidArgument:
		return http.StatusBadRequest
	case CodeUnauthenticated:
		return http.StatusUnauthorized
	case CodePermissionDenied:
		return http.StatusForbidden
	case CodeNotFound:
		return http.StatusNotFound
	case CodeConflict:
		return http.StatusConflict
	case CodeBackpressure:
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}
