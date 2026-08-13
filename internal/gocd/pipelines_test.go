package gocd

import "testing"

func TestNextAfterCursor(t *testing.T) {
	// An absent link means the last page; a present link must yield a usable cursor,
	// anything else is an error (silent truncation would lie to the caller).
	for _, tc := range []struct {
		href, want string
		wantErr    bool
	}{
		{href: "", want: ""},
		{href: "http://gocd.example/go/api/pipelines/p/history?after=77", want: "77"},
		{href: "/go/api/pipelines/p/history?after=8", want: "8"},                // relative href
		{href: "http://gocd.example/go/api/pipelines/p/history", wantErr: true}, // link without a cursor
		{href: "://bad", wantErr: true},                                         // unparsable href
		{href: "http://gocd.example/h?after=abc", wantErr: true},                // non-numeric cursor
		{href: "http://gocd.example/h?after=0", wantErr: true},                  // not a positive integer
	} {
		got, err := nextAfterCursor(tc.href)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("nextAfterCursor(%q) = %q, want error", tc.href, got)
			}
			continue
		}
		if err != nil {
			t.Fatalf("nextAfterCursor(%q): %v", tc.href, err)
		}
		if got != tc.want {
			t.Fatalf("nextAfterCursor(%q) = %q, want %q", tc.href, got, tc.want)
		}
	}
}
