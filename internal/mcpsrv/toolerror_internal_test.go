package mcpsrv

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ivinco/gocd-mcp/internal/gocd"
)

func TestToolError_ConflictMapping(t *testing.T) {
	text := func(err error) string {
		res := toolError(err)
		if !res.IsError {
			t.Fatalf("toolError must flag IsError")
		}
		return res.Content[0].(*mcp.TextContent).Text
	}
	cases := []struct {
		err  error
		want string
	}{
		{&gocd.ConflictError{StatusCode: 409, Message: "Cannot schedule: still in progress"}, "GoCD refused: Cannot schedule: still in progress"},
		{&gocd.ConflictError{StatusCode: 409}, "GoCD refused the request (conflict)"}, // no reason parsed: generic text, no raw body
		{gocd.ErrConflict, "GoCD refused the request (conflict)"},
		{&gocd.ConflictError{StatusCode: 412}, "version conflict (ETag mismatch)"},
		{&gocd.ConflictError{StatusCode: 412, Message: "stale"}, "version conflict (ETag mismatch)"}, // ETag hint wins for 412
		{gocd.ErrPreconditionFailed, "version conflict (ETag mismatch)"},
	}
	for _, c := range cases {
		if got := text(c.err); !strings.HasPrefix(got, c.want) {
			t.Fatalf("%v → %q, want prefix %q", c.err, got, c.want)
		}
	}
}
