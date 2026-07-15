// Package gocd is a thin, typed client for the GoCD REST API.
//
// A Client carries a single user's Personal Access Token (PAT) and applies it to
// every request, so GoCD enforces that user's RBAC. Construct one per request from
// the authenticated principal's token.
package gocd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is a per-user GoCD API client.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewClient builds a client bound to one user's bearer token.
func NewClient(baseURL, token string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: timeout},
	}
}

// request describes one GoCD API call.
type request struct {
	method  string
	path    string
	accept  string
	body    []byte            // optional JSON body
	headers map[string]string // optional extra headers (e.g. If-Match, X-GoCD-Confirm)
}

// doJSON executes req and, if out is non-nil, decodes the JSON response body into it.
// It returns the response ETag (if any) and maps HTTP status codes to sentinel errors.
func (c *Client) doJSON(ctx context.Context, r request, out any) (etag string, err error) {
	var bodyReader io.Reader
	if r.body != nil {
		bodyReader = bytes.NewReader(r.body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, r.method, c.baseURL+r.path, bodyReader)
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.token)
	httpReq.Header.Set("Accept", r.accept)
	if r.body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	for k, v := range r.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if err := statusError(resp); err != nil {
		return "", err
	}

	etag = resp.Header.Get("ETag")
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return etag, err
		}
	}
	return etag, nil
}

func statusError(resp *http.Response) error {
	if resp.StatusCode < 400 {
		return nil
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusForbidden:
		return ErrForbidden
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusConflict, http.StatusPreconditionFailed:
		return ErrConflict
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return &APIError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}
}

// User is the subset of GoCD's current_user response we rely on.
type User struct {
	LoginName string `json:"login_name"`
	Enabled   bool   `json:"enabled"`
}

// CurrentUser returns the user identified by the client's token. Used to validate a
// PAT and resolve the caller's GoCD login.
func (c *Client) CurrentUser(ctx context.Context) (*User, error) {
	var u User
	if _, err := c.doJSON(ctx, request{
		method: http.MethodGet,
		path:   "/go/api/current_user",
		accept: acceptCurrentUser,
	}, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// Reachable reports whether the GoCD server responds at all (any HTTP status counts
// as reachable). Used by the readiness probe without requiring a user token.
func Reachable(ctx context.Context, baseURL string, timeout time.Duration) error {
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/go/api/current_user", nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
