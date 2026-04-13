package model

import (
	"fmt"
)

// ==================== Error Types ====================

// APIError represents a structured API error
type APIError struct {
	Code    string      `json:"code"`
	Message string      `json:"message"`
	Details interface{} `json:"details,omitempty"`
	Status  int         `json:"-"`
}

// Error implements the error interface
func (e *APIError) Error() string {
	if e.Details != nil {
		return fmt.Sprintf("%s: %s (%v)", e.Code, e.Message, e.Details)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// WithDetails adds details to an error
func (e *APIError) WithDetails(details interface{}) *APIError {
	return &APIError{
		Code:    e.Code,
		Message: e.Message,
		Details: details,
		Status:  e.Status,
	}
}

// NewErrorType creates a new error type
func NewErrorType(code string, status int) *APIError {
	return &APIError{
		Code:   code,
		Status: status,
	}
}

// ==================== Standard Error Types ====================

var (
	// ErrValidationFailed represents validation errors
	ErrValidationFailed = &APIError{
		Code:    "VALIDATION_ERROR",
		Message: "Validation failed",
		Status:  400,
	}

	// ErrNotFound represents not found errors
	ErrNotFound = &APIError{
		Code:    "NOT_FOUND",
		Message: "Resource not found",
		Status:  404,
	}

	// ErrUnauthorized represents unauthorized errors
	ErrUnauthorized = &APIError{
		Code:    "UNAUTHORIZED",
		Message: "Unauthorized access",
		Status:  401,
	}

	// ErrForbidden represents forbidden errors
	ErrForbidden = &APIError{
		Code:    "FORBIDDEN",
		Message: "Access forbidden",
		Status:  403,
	}

	// ErrRateLimit represents rate limit errors
	ErrRateLimit = &APIError{
		Code:    "RATE_LIMIT",
		Message: "Rate limit exceeded",
		Status:  429,
	}

	// ErrInternal represents internal server errors
	ErrInternal = &APIError{
		Code:    "INTERNAL_ERROR",
		Message: "Internal server error",
		Status:  500,
	}

	// ErrConflict represents conflict errors
	ErrConflict = &APIError{
		Code:    "CONFLICT",
		Message: "Resource conflict",
		Status:  409,
	}

	// ErrBadRequest represents bad request errors
	ErrBadRequest = &APIError{
		Code:    "BAD_REQUEST",
		Message: "Bad request",
		Status:  400,
	}
)
