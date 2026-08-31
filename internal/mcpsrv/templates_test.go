package mcpsrv_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// resultText concatenates the text content of a tool result (error messages).
func resultText(res *mcp.CallToolResult) string {
	var text string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text += tc.Text
		}
	}
	return text
}

func TestTemplates_EndToEnd(t *testing.T) {
	gocd := fakeGoCD(t)
	defer gocd.Close()

	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ts := stackCfg(t, gocd.URL, "full", log)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sess, err := connect(ctx, ts.URL+"/mcp", goodToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer sess.Close()

	call := func(name string, args map[string]any) *mcp.CallToolResult {
		t.Helper()
		res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: transport error (should be tool result): %v", name, err)
		}
		return res
	}
	decode := func(res *mcp.CallToolResult, out any) {
		t.Helper()
		raw, _ := json.Marshal(res.StructuredContent)
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("decode output %q: %v", raw, err)
		}
	}

	// list: pipelines using each template; an unused one reports [] not null.
	res := call("list_templates", nil)
	if res.IsError {
		t.Fatalf("list_templates: %s", resultText(res))
	}
	var list struct {
		Templates []struct {
			Name      string   `json:"name"`
			CanEdit   bool     `json:"can_edit"`
			Pipelines []string `json:"pipelines"`
		} `json:"templates"`
	}
	decode(res, &list)
	if len(list.Templates) != 2 || list.Templates[0].Name != "t1" || !list.Templates[0].CanEdit ||
		strings.Join(list.Templates[0].Pipelines, ",") != "p1" {
		t.Fatalf("templates = %+v", list.Templates)
	}
	if t2 := list.Templates[1]; t2.Name != "t2" || t2.Pipelines == nil || len(t2.Pipelines) != 0 {
		t.Fatalf("unused template must serialize pipelines as []: %+v", t2)
	}

	// get of an unknown template is a tool error (the fake answers 404), never a
	// handler failure: the zero output must still satisfy the output schema.
	res = call("get_template", map[string]any{"name": "nope"})
	if !res.IsError || !strings.Contains(resultText(res), "not found") {
		t.Fatalf("get_template unknown: IsError=%v text=%q", res.IsError, resultText(res))
	}

	// get: config plus the ETag needed for the update.
	res = call("get_template", map[string]any{"name": "t1"})
	if res.IsError {
		t.Fatalf("get_template: %s", resultText(res))
	}
	var got struct {
		ETag     string         `json:"etag"`
		Template map[string]any `json:"template"`
	}
	decode(res, &got)
	if got.ETag != `"tpl-etag-1"` || got.Template["name"] != "t1" {
		t.Fatalf("get_template = %+v", got)
	}

	// update with the current ETag succeeds and returns the new one.
	tpl := map[string]any{"name": "t1", "stages": []any{map[string]any{"name": "build"}}}
	res = call("update_template", map[string]any{"name": "t1", "etag": got.ETag, "template": tpl})
	if res.IsError {
		t.Fatalf("update_template: %s", resultText(res))
	}
	var upd struct {
		OK   bool   `json:"ok"`
		ETag string `json:"etag"`
	}
	decode(res, &upd)
	if !upd.OK || upd.ETag != `"tpl-etag-2"` {
		t.Fatalf("update_template = %+v", upd)
	}

	// stale ETag → conflict tool error, not a transport error.
	res = call("update_template", map[string]any{"name": "t1", "etag": "stale", "template": tpl})
	if !res.IsError || !strings.Contains(resultText(res), "conflict") {
		t.Fatalf("update_template with stale etag: IsError=%v text=%q", res.IsError, resultText(res))
	}

	// name mismatch is caught before the round-trip.
	res = call("update_template", map[string]any{"name": "t1", "etag": got.ETag, "template": map[string]any{"name": "other"}})
	if !res.IsError || !strings.Contains(resultText(res), "renamed") {
		t.Fatalf("update_template with other name: IsError=%v text=%q", res.IsError, resultText(res))
	}

	// create: needs a name inside the object.
	res = call("create_template", map[string]any{"template": map[string]any{"name": "t3", "stages": []any{}}})
	if res.IsError {
		t.Fatalf("create_template: %s", resultText(res))
	}
	res = call("create_template", map[string]any{"template": map[string]any{"stages": []any{}}})
	if !res.IsError || !strings.Contains(resultText(res), "name") {
		t.Fatalf("create_template without name: IsError=%v text=%q", res.IsError, resultText(res))
	}

	// delete: GoCD's refusal for an in-use template reaches the caller verbatim.
	res = call("delete_template", map[string]any{"name": "t1"})
	if text := resultText(res); !res.IsError || !strings.HasPrefix(text, "GoCD rejected the request (HTTP 422): ") ||
		!strings.Contains(text, "referenced by pipeline(s): [p1]") || strings.Contains(text, "{") {
		t.Fatalf("delete_template in use: IsError=%v text=%q, want GoCD's message without raw JSON", res.IsError, text)
	}
	res = call("delete_template", map[string]any{"name": "t2"})
	if res.IsError {
		t.Fatalf("delete_template t2: %s", resultText(res))
	}

	// Every mutation leaves an audit line; none leaks the token.
	logs := buf.String()
	for _, action := range []string{"create_template", "update_template", "delete_template"} {
		if !strings.Contains(logs, `"action":"`+action+`"`) {
			t.Fatalf("audit log missing %s: %s", action, logs)
		}
	}
	if !strings.Contains(logs, `"target":"t3"`) || !strings.Contains(logs, `"login":"alice"`) {
		t.Fatalf("audit log missing target/login: %s", logs)
	}
	if strings.Contains(logs, goodToken) {
		t.Fatalf("audit log leaked the token")
	}
}
