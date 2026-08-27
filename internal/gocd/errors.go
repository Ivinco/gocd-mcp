package gocd

import (
	"errors"
	"fmt"
)

// Sentinel errors mapped from GoCD HTTP status codes. Callers use errors.Is to
// translate these into MCP tool errors (see internal/domain).
var (
	ErrUnauthorized = errors.New("gocd: unauthorized")                 // 401
	ErrForbidden    = errors.New("gocd: forbidden")                    // 403
	ErrNotFound     = errors.New("gocd: not found")                    // 404
	ErrConflict     = errors.New("gocd: conflict (etag/precondition)") // 409/412
)

// ConflictError is a 409/412 from GoCD together with its message, so callers can tell
// a scheduling refusal ("Cannot schedule: ... is still in progress") from an ETag
// mismatch. errors.Is(err, ErrConflict) holds for it.
type ConflictError struct {
	StatusCode int
	Message    string
}

func (e *ConflictError) Error() string {
	if e.Message == "" {
		return ErrConflict.Error()
	}
	return "gocd: conflict: " + e.Message
}

// Is makes ConflictError match the ErrConflict sentinel.
func (e *ConflictError) Is(target error) bool { return target == ErrConflict }

// APIError carries an unexpected GoCD response that has no dedicated sentinel.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("gocd: unexpected status %d: %s", e.StatusCode, e.Body)
}
