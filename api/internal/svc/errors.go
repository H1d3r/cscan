package svc

import (
	"fmt"
	"net/http"

	"cscan/pkg/xerr"

	"github.com/zeromicro/go-zero/rest/httpx"
)

// APIError represents a structured API error
type APIError struct {
	Code    string      `json:"code"`
	Message string      `json:"message"`
	Details interface{} `json:"details,omitempty"`
	Status  int         `json:"-"` // HTTP status code
}

func (e *APIError) Error() string {
	return e.Message
}

// NewAPIError creates a new API error
func NewAPIError(code, message string, status int) *APIError {
	return &APIError{
		Code:    code,
		Message: message,
		Status:  status,
	}
}

// WithDetails adds details to the API error
func (e *APIError) WithDetails(details interface{}) *APIError {
	e.Details = details
	return e
}

// ErrorResponse represents the unified error response format
type ErrorResponse struct {
	Code    string      `json:"code"`
	Message string      `json:"message"`
	Details interface{} `json:"details,omitempty"`
	TraceID string      `json:"traceId,omitempty"`
}

// Standard error types - eliminates special case handling
var (
	ErrValidation   = NewErrorType("VALIDATION_ERROR", 400)
	ErrNotFound     = NewErrorType("NOT_FOUND", 404)
	ErrUnauthorized = NewErrorType("UNAUTHORIZED", 401)
	ErrForbidden    = NewErrorType("FORBIDDEN", 403)
	ErrRateLimit    = NewErrorType("RATE_LIMIT", 429)
	ErrInternal     = NewErrorType("INTERNAL_ERROR", 500)
	ErrConflict     = NewErrorType("CONFLICT", 409)
	ErrBadRequest   = NewErrorType("BAD_REQUEST", 400)
)

// ErrorType represents a category of errors
type ErrorType struct {
	Code   string
	Status int
}

// NewErrorType creates a new error type
func NewErrorType(code string, status int) *ErrorType {
	return &ErrorType{
		Code:   code,
		Status: status,
	}
}

// New creates a new API error of this type
func (et *ErrorType) New(message string) *APIError {
	return &APIError{
		Code:    et.Code,
		Message: message,
		Status:  et.Status,
	}
}

// Newf creates a new API error of this type with formatted message
func (et *ErrorType) Newf(format string, args ...interface{}) *APIError {
	return &APIError{
		Code:    et.Code,
		Message: fmt.Sprintf(format, args...),
		Status:  et.Status,
	}
}

// BaseHandler provides unified error handling for all handlers
type BaseHandler struct{}

// WriteError writes an error response in a consistent format
func (h *BaseHandler) WriteError(w http.ResponseWriter, err error) {
	var apiErr *APIError

	// Convert different error types to APIError
	switch e := err.(type) {
	case *APIError:
		apiErr = e
	case *xerr.CodeError:
		// Convert existing xerr.CodeError to new format
		apiErr = &APIError{
			Code:    fmt.Sprintf("ERR_%d", e.Code),
			Message: e.Msg,
			Status:  getHTTPStatusFromCode(e.Code),
		}
	default:
		// Default to internal server error
		apiErr = ErrInternal.New(err.Error())
	}

	response := ErrorResponse{
		Code:    apiErr.Code,
		Message: apiErr.Message,
		Details: apiErr.Details,
		TraceID: getTraceID(w),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(apiErr.Status)
	httpx.WriteJson(w, apiErr.Status, response)
}

// WriteJSON writes a successful JSON response
func (h *BaseHandler) WriteJSON(w http.ResponseWriter, data interface{}) {
	httpx.OkJson(w, data)
}

// WriteSuccess writes a success response with message
func (h *BaseHandler) WriteSuccess(w http.ResponseWriter, message string) {
	response := map[string]interface{}{
		"code": xerr.OK,
		"msg":  message,
	}
	httpx.OkJson(w, response)
}

// WriteSuccessWithData writes a success response with data
func (h *BaseHandler) WriteSuccessWithData(w http.ResponseWriter, data interface{}) {
	response := map[string]interface{}{
		"code": xerr.OK,
		"msg":  "success",
		"data": data,
	}
	httpx.OkJson(w, response)
}

// ValidateRequest validates request data using the validator
func (h *BaseHandler) ValidateRequest(validator Validator, req interface{}) error {
	if err := validator.ValidateStruct(req); err != nil {
		return ErrValidation.New(err.Error())
	}
	return nil
}

// Helper functions

// getHTTPStatusFromCode converts xerr codes to HTTP status codes
func getHTTPStatusFromCode(code int) int {
	switch code {
	case xerr.OK:
		return http.StatusOK
	case xerr.ParamError:
		return http.StatusBadRequest
	case xerr.Unauthorized:
		return http.StatusUnauthorized
	case xerr.Forbidden:
		return http.StatusForbidden
	case xerr.NotFound:
		return http.StatusNotFound
	case xerr.ServerError:
		return http.StatusInternalServerError
	default:
		if code >= 10000 {
			// Business errors map to 400 Bad Request
			return http.StatusBadRequest
		}
		return http.StatusInternalServerError
	}
}

// getTraceID extracts trace ID from response writer
func getTraceID(w http.ResponseWriter) string {
	// Implementation would extract trace ID from headers or context
	// For now, return empty string
	return ""
}

// Common error constructors for business logic

// NotFoundError creates a not found error
func NotFoundError(resource string) *APIError {
	return ErrNotFound.Newf("%s not found", resource)
}

// ValidationError creates a validation error
func ValidationError(message string) *APIError {
	return ErrValidation.New(message)
}

// UnauthorizedError creates an unauthorized error
func UnauthorizedError(message string) *APIError {
	if message == "" {
		message = "Unauthorized access"
	}
	return ErrUnauthorized.New(message)
}

// ForbiddenError creates a forbidden error
func ForbiddenError(message string) *APIError {
	if message == "" {
		message = "Access forbidden"
	}
	return ErrForbidden.New(message)
}

// InternalError creates an internal server error
func InternalError(message string) *APIError {
	if message == "" {
		message = "Internal server error"
	}
	return ErrInternal.New(message)
}

// ConflictError creates a conflict error
func ConflictError(message string) *APIError {
	return ErrConflict.New(message)
}

// BadRequestError creates a bad request error
func BadRequestError(message string) *APIError {
	return ErrBadRequest.New(message)
}
