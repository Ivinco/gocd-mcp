package gocd

import (
	"errors"
	"fmt"
	"net/http"
)

// Sentinel errors mapped from GoCD HTTP status codes. Callers use errors.Is to
// translate these into MCP tool errors (see internal/domain).
var (
	ErrUnauthorized       = errors.New("gocd: unauthorized")                     // 401
	ErrForbidden          = errors.New("gocd: forbidden")                        // 403
	ErrNotFound           = errors.New("gocd: not found")                        // 404
	ErrConflict           = errors.New("gocd: conflict")                         // 409: refused because of the current state
	ErrPreconditionFailed = errors.New("gocd: precondition failed (stale etag)") // 412: If-Match did not match
)

// ConflictError is a 409 or 412 from GoCD together with its message. It matches
// ErrConflict for a 409 (GoCD refused the request because of the current state, e.g.
// "Cannot schedule: ... is still in progress") and ErrPreconditionFailed for a 412 (a
// stale ETag), so callers tell the two apart by sentinel, never by status code.
type ConflictError struct {
	StatusCode int
	Message    string
}

func (e *ConflictError) Error() string {
	if e.Message == "" {
		return e.sentinel().Error()
	}
	return e.sentinel().Error() + ": " + e.Message
}

// Is makes ConflictError match the sentinel for its status.
func (e *ConflictError) Is(target error) bool { return target == e.sentinel() }

func (e *ConflictError) sentinel() error {
	if e.StatusCode == http.StatusPreconditionFailed {
		return ErrPreconditionFailed
	}
	return ErrConflict
}

// APIError carries an unexpected GoCD response that has no dedicated sentinel.
// Message is GoCD's own explanation: the top-level "message" plus any field-level
// validation errors nested under "data" (a 422 on a pipeline create says only
// "Validation failed." up top and names the offending field deeper down, e.g.
// "materials[0].auto_update: ..."). Body is the raw, bounded response, kept for
// responses that are not in that shape.
type APIError struct {
	StatusCode int
	Message    string
	Body       string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("gocd: %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("gocd: unexpected status %d: %s", e.StatusCode, e.Body)
}
