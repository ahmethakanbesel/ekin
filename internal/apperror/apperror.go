// Package apperror defines the application-wide error taxonomy and its
// mapping to HTTP status codes (CONVENTIONS.md §4). Handlers return
// errors; the HTTP layer asks StatusFor for the status to write.
package apperror

import (
	"errors"
	"net/http"
)

// Code identifies a distinct application error condition.
type Code string

const (
	BadRequest          Code = "BadRequest"
	ValidationFailed    Code = "ValidationFailed"
	Unauthenticated     Code = "Unauthenticated"
	Forbidden           Code = "Forbidden"
	NotFound            Code = "NotFound"
	Conflict            Code = "Conflict"
	InternalError       Code = "InternalError"
	ProviderUnavailable Code = "ProviderUnavailable"
	// TooManyAttempts is a rate limit on guessing (logins, invitation
	// passwords).
	TooManyAttempts Code = "TooManyAttempts"

	WorkspaceNotFound  Code = "WorkspaceNotFound"
	PipelineNotFound   Code = "PipelineNotFound"
	JobNotFound        Code = "JobNotFound"
	UserNotFound       Code = "UserNotFound"
	MemberNotFound     Code = "MemberNotFound"
	InvitationNotFound Code = "InvitationNotFound"
	DocumentNotFound   Code = "DocumentNotFound"
	StoryNotFound      Code = "StoryNotFound"
	AssetNotFound      Code = "AssetNotFound"

	// EmailTaken is a second account for one email address.
	EmailTaken Code = "EmailTaken"
	// AlreadyMember is inviting or adding someone already in the workspace.
	AlreadyMember Code = "AlreadyMember"
	// LastOwner is demoting or removing the workspace's only owner.
	LastOwner Code = "LastOwner"

	// InvalidTransition is a state-machine move the aggregate's current
	// state does not allow (pausing a paused pipeline, completing a job
	// someone else holds).
	InvalidTransition Code = "InvalidTransition"
	// StaleVersion is an optimistic-concurrency miss: the row changed
	// since the caller read it.
	StaleVersion Code = "StaleVersion"
)

var statusByCode = map[Code]int{
	BadRequest:          http.StatusBadRequest,
	ValidationFailed:    http.StatusBadRequest,
	Unauthenticated:     http.StatusUnauthorized,
	Forbidden:           http.StatusForbidden,
	NotFound:            http.StatusNotFound,
	Conflict:            http.StatusConflict,
	InternalError:       http.StatusInternalServerError,
	ProviderUnavailable: http.StatusBadGateway,
	TooManyAttempts:     http.StatusTooManyRequests,

	WorkspaceNotFound:  http.StatusNotFound,
	PipelineNotFound:   http.StatusNotFound,
	JobNotFound:        http.StatusNotFound,
	UserNotFound:       http.StatusNotFound,
	MemberNotFound:     http.StatusNotFound,
	InvitationNotFound: http.StatusNotFound,
	DocumentNotFound:   http.StatusNotFound,
	StoryNotFound:      http.StatusNotFound,
	AssetNotFound:      http.StatusNotFound,

	EmailTaken:    http.StatusConflict,
	AlreadyMember: http.StatusConflict,
	LastOwner:     http.StatusConflict,

	InvalidTransition: http.StatusConflict,
	StaleVersion:      http.StatusConflict,
}

// StatusFor returns the HTTP status for code. Unknown codes map to 500.
func StatusFor(code Code) int {
	if status, ok := statusByCode[code]; ok {
		return status
	}
	return http.StatusInternalServerError
}

// AppError is an error carrying a taxonomy Code, a caller-facing message
// and an optional wrapped cause.
type AppError struct {
	Code    Code
	Message string
	cause   error
}

func (e *AppError) Error() string {
	if e.cause != nil {
		return e.Message + ": " + e.cause.Error()
	}
	return e.Message
}

func (e *AppError) Unwrap() error { return e.cause }

// New creates an AppError without a cause.
func New(code Code, message string) *AppError {
	return &AppError{Code: code, Message: message}
}

// Wrap creates an AppError wrapping err. It returns nil for a nil err.
func Wrap(code Code, err error, message string) *AppError {
	if err == nil {
		return nil
	}
	return &AppError{Code: code, Message: message, cause: err}
}

// Is reports whether err is (or wraps) an *AppError with code.
func Is(err error, code Code) bool {
	c, ok := GetCode(err)
	return ok && c == code
}

// GetCode extracts the Code from err if err is (or wraps) an *AppError.
func GetCode(err error) (Code, bool) {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Code, true
	}
	return "", false
}

// Message returns the caller-facing message of an *AppError, without the
// wrapped cause. For any other error it returns err.Error().
func Message(err error) string {
	if err == nil {
		return ""
	}
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Message
	}
	return err.Error()
}
