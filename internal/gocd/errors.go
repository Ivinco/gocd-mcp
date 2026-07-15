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

// APIError carries an unexpected GoCD response that has no dedicated sentinel.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("gocd: unexpected status %d: %s", e.StatusCode, e.Body)
}
