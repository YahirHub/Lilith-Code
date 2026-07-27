package tui

import "testing"

func TestSubsequenceMatch(t *testing.T) {
	cases := []struct {
		hay, needle string
		wantOK      bool
	}{
		{"gpt-4o", "gpt", true},
		{"gpt-4o", "g4o", true},
		{"gpt-4o", "xyz", false},
		{"claude-3-5-sonnet", "c35s", true},
		{"foo", "", true},
	}
	for _, c := range cases {
		_, ok := subsequenceMatch(c.hay, c.needle)
		if ok != c.wantOK {
			t.Errorf("subsequenceMatch(%q, %q) = %v, want %v", c.hay, c.needle, ok, c.wantOK)
		}
	}
}
