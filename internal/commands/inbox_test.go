package commands

import (
	"testing"
	"unicode/utf8"
)

// truncate used to slice bytes, which cut multi-byte characters in half and
// shortened any non-Latin subject to a fraction of the column it was given.
func TestTruncate(t *testing.T) {
	tests := map[string]struct {
		in   string
		max  int
		want string
	}{
		"short enough to pass through": {
			in: "Alice", max: 25, want: "Alice",
		},
		"exactly the limit": {
			in: "12345", max: 5, want: "12345",
		},
		"one over": {
			in: "123456", max: 5, want: "12...",
		},
		"accents are one character each": {
			in: "Zoë Ångström", max: 12, want: "Zoë Ångström",
		},
		"a cut never splits a character": {
			in: "Zoë Ångström Söderberg", max: 12, want: "Zoë Ångst...",
		},
		"characters outside the basic plane": {
			in: "日本語のメールの件名です", max: 8, want: "日本語のメ...",
		},
		"no room for the ellipsis": {
			in: "abcdef", max: 3, want: "abc",
		},
		"barely any room": {
			in: "abcdef", max: 1, want: "a",
		},
		"no room at all": {
			in: "abcdef", max: 0, want: "",
		},
		// The old implementation panicked here, slicing to a negative index.
		"a nonsense limit": {
			in: "abcdef", max: -1, want: "",
		},
		"empty stays empty": {
			in: "", max: 10, want: "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := truncate(tt.in, tt.max)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
			}
			if n := utf8.RuneCountInString(got); tt.max > 0 && n > tt.max {
				t.Errorf("truncate(%q, %d) returned %d characters", tt.in, tt.max, n)
			}
			if !utf8.ValidString(got) {
				t.Errorf("truncate(%q, %d) = %q, which is not valid UTF-8", tt.in, tt.max, got)
			}
		})
	}
}
