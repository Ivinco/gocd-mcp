package gocd

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFlexInt_UnmarshalJSON(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int
		err  bool
	}{
		{`1`, 1, false},    // bare
		{`"7"`, 7, false},  // quoted, as GoCD serializes stage counters
		{`null`, 0, false}, // never scheduled
		{`""`, 0, false},   // empty string
		{`"abc"`, 0, true}, // not a number
		{`1.5`, 0, true},   // not an integer
	} {
		var n flexInt
		err := json.Unmarshal([]byte(c.in), &n)
		if (err != nil) != c.err || int(n) != c.want {
			t.Fatalf("%s: got %d, err=%v; want %d, err=%v", c.in, n, err, c.want, c.err)
		}
	}
}

func TestTruncate_KeepsRunesWhole(t *testing.T) {
	s := strings.Repeat("п", 10) // 2 bytes per rune, 20 bytes
	if got := truncate(s, 20); got != s {
		t.Fatalf("no cut expected, got %q", got)
	}
	got := truncate(s, 5) // would split the third rune
	if got != "пп" || len(got) != 4 {
		t.Fatalf("truncate(5) = %q (%d bytes), want %q", got, len(got), "пп")
	}
	if truncate("abc", 0) != "" {
		t.Fatalf("truncate to 0 must be empty")
	}
}
