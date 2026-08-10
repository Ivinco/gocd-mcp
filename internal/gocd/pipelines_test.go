package gocd

import "testing"

func TestNextAfterCursor(t *testing.T) {
	for _, tc := range []struct{ href, want string }{
		{"", ""},
		{"http://gocd.example/go/api/pipelines/p/history?after=77", "77"},
		{"/go/api/pipelines/p/history?after=8", "8"},           // relative href
		{"http://gocd.example/go/api/pipelines/p/history", ""}, // link without a cursor
		{"://bad", ""},                          // unparsable href
		{"http://gocd.example/h?after=abc", ""}, // non-numeric cursor
		{"http://gocd.example/h?after=0", ""},   // not a positive integer
	} {
		if got := nextAfterCursor(tc.href); got != tc.want {
			t.Fatalf("nextAfterCursor(%q) = %q, want %q", tc.href, got, tc.want)
		}
	}
}
