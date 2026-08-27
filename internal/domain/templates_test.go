package domain

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestTemplateValidation(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&fakeClient{})
	obj := json.RawMessage(`{"name":"t","stages":[]}`)

	for _, name := range []string{"", "a/b", "has space"} {
		if _, err := svc.TemplateConfig(ctx, name); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("get %q: expected ErrInvalidArgument, got %v", name, err)
		}
		if err := svc.DeleteTemplate(ctx, name); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("delete %q: expected ErrInvalidArgument, got %v", name, err)
		}
	}

	// create: the name lives in the object and must be valid there.
	for _, body := range []string{`[]`, `{}`, `{"stages":[]}`, `{"name":""}`, `{"name":"a b"}`, `{"name":42}`} {
		if err := svc.CreateTemplate(ctx, json.RawMessage(body)); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("create %s: expected ErrInvalidArgument, got %v", body, err)
		}
	}

	// update: etag required, object required, object name must match.
	if _, err := svc.UpdateTemplate(ctx, "t", obj, ""); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("update without etag: expected ErrInvalidArgument, got %v", err)
	}
	if _, err := svc.UpdateTemplate(ctx, "t", json.RawMessage(`[]`), "etag"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("update with non-object: expected ErrInvalidArgument, got %v", err)
	}
	for _, body := range []string{`{"stages":[]}`, `{"name":"other","stages":[]}`} {
		if _, err := svc.UpdateTemplate(ctx, "t", json.RawMessage(body), "etag"); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("update with body %s: expected ErrInvalidArgument (name mismatch), got %v", body, err)
		}
	}
}

func TestTemplateHappyPathReachesClient(t *testing.T) {
	ctx := context.Background()
	f := &fakeClient{}
	svc := NewService(f)
	obj := json.RawMessage(`{"name":"t","stages":[]}`)

	if err := svc.CreateTemplate(ctx, obj); err != nil {
		t.Fatalf("create: %v", err)
	}
	newETag, err := svc.UpdateTemplate(ctx, "t", obj, "v1-etag")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if newETag != "new-etag" {
		t.Fatalf("new etag = %q", newETag)
	}
	if err := svc.DeleteTemplate(ctx, "t"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	want := []string{"create_template:" + string(obj), "update_template:t:v1-etag", "delete_template:t"}
	if len(f.calls) != len(want) {
		t.Fatalf("calls = %v, want %v", f.calls, want)
	}
	for i := range want {
		if f.calls[i] != want[i] {
			t.Fatalf("call %d = %q, want %q", i, f.calls[i], want[i])
		}
	}
}
