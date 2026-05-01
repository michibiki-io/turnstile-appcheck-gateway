package apperrors

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/model"
)

// APIError is a structured internal error type carrying status and stable client-facing code.
type APIError struct {
	Status  int
	Code    string
	Message string
	Cause   error
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	return e.Code + ": " + e.Message
}

func (e *APIError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func New(status int, code, message string, cause error) *APIError {
	return &APIError{
		Status:  status,
		Code:    code,
		Message: message,
		Cause:   cause,
	}
}

func BadRequest(code, message string, cause error) *APIError {
	return New(http.StatusBadRequest, code, message, cause)
}

func Unauthorized(code, message string, cause error) *APIError {
	return New(http.StatusUnauthorized, code, message, cause)
}

func Forbidden(code, message string, cause error) *APIError {
	return New(http.StatusForbidden, code, message, cause)
}

func Internal(code, message string, cause error) *APIError {
	return New(http.StatusInternalServerError, code, message, cause)
}

// WriteError writes an APIError as JSON. Nil errors become INTERNAL_ERROR.
func WriteError(c *gin.Context, err *APIError) {
	if err == nil {
		err = Internal("INTERNAL_ERROR", "Unexpected server error", nil)
	}
	c.JSON(err.Status, model.ErrorResponse{
		Error: model.ErrorDetail{
			Code:    err.Code,
			Message: err.Message,
		},
	})
}
