// Package auth validates per-user GoCD Personal Access Tokens presented as bearer
// tokens and exposes the resulting principal to request handlers.
//
// The MCP server acts as an OAuth-style resource server, but the identity provider
// is GoCD itself: a token is valid iff GoCD's current_user endpoint accepts it. See
// status/architecture.md §2 for the deliberate deviation from the OAuth 2.1 section
// of the MCP spec.
package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"sync"
	"time"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"

	"github.com/ivinco/gocd-mcp/internal/gocd"
)

// patExtraKey is the TokenInfo.Extra key under which we stash the raw PAT so tool
// handlers can build a per-request GoCD client.
const patExtraKey = "gocd_pat"

// Principal is the authenticated GoCD user for a request.
type Principal struct {
	Login string
	PAT   string
}

// Verifier validates GoCD PATs, caching successful validations to spare GoCD a
// current_user call on every MCP request.
type Verifier struct {
	baseURL string
	timeout time.Duration
	ttl     time.Duration

	mu    sync.Mutex
	cache map[string]cacheEntry
	now   func() time.Time
}

type cacheEntry struct {
	login   string
	expires time.Time
}

// NewVerifier creates a Verifier for the given GoCD instance.
func NewVerifier(baseURL string, timeout, ttl time.Duration) *Verifier {
	return &Verifier{
		baseURL: baseURL,
		timeout: timeout,
		ttl:     ttl,
		cache:   make(map[string]cacheEntry),
		now:     time.Now,
	}
}

// Verify implements mcpauth.TokenVerifier. On success it returns a TokenInfo whose
// UserID is the GoCD login and whose Extra carries the PAT. On any failure it returns
// ErrInvalidToken so the middleware responds 401.
func (v *Verifier) Verify(ctx context.Context, token string, _ *http.Request) (*mcpauth.TokenInfo, error) {
	login, err := v.resolve(ctx, token)
	if err != nil {
		// GoCD outages also surface here as 401; readiness is covered by /readyz.
		return nil, errors.Join(mcpauth.ErrInvalidToken, err)
	}
	return &mcpauth.TokenInfo{
		UserID: login,
		// The SDK middleware requires a non-zero expiration. The verifier runs on
		// every request, so we bound validity to the cache window: after it elapses,
		// the next request re-validates against GoCD.
		Expiration: v.now().Add(v.ttl),
		Extra:      map[string]any{patExtraKey: token},
	}, nil
}

func (v *Verifier) resolve(ctx context.Context, token string) (string, error) {
	key := hashToken(token)

	v.mu.Lock()
	if e, ok := v.cache[key]; ok && v.now().Before(e.expires) {
		login := e.login
		v.mu.Unlock()
		return login, nil
	}
	v.mu.Unlock()

	u, err := gocd.NewClient(v.baseURL, token, v.timeout).CurrentUser(ctx)
	if err != nil {
		return "", err
	}

	v.mu.Lock()
	v.cache[key] = cacheEntry{login: u.LoginName, expires: v.now().Add(v.ttl)}
	v.mu.Unlock()
	return u.LoginName, nil
}

// PrincipalFromContext extracts the authenticated principal placed by the bearer-token
// middleware. ok is false if the request is unauthenticated or malformed.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	ti := mcpauth.TokenInfoFromContext(ctx)
	if ti == nil {
		return Principal{}, false
	}
	pat, _ := ti.Extra[patExtraKey].(string)
	if ti.UserID == "" || pat == "" {
		return Principal{}, false
	}
	return Principal{Login: ti.UserID, PAT: pat}, true
}

// hashToken returns a stable cache key for a token without retaining the token itself.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
