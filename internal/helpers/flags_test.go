package helpers

import (
	"fmt"
	"testing"
)

func TestParseUIDs(t *testing.T) {
	tests := []struct {
		args    []string
		want    []uint32
		wantErr bool
	}{
		{[]string{"42"}, []uint32{42}, false},
		{[]string{"1", "2", "3"}, []uint32{1, 2, 3}, false},
		{[]string{"4294967295"}, []uint32{4294967295}, false},
		{[]string{"abc"}, nil, true},
		{[]string{"-1"}, nil, true},
		{[]string{"42", "bad"}, nil, true},
		{[]string{}, nil, true},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%v", tt.args), func(t *testing.T) {
			got, err := ParseUIDs(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("index %d: got %d, want %d", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestReorderArgs(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		boolFlags map[string]bool
		want      []string
	}{
		{
			name: "flags already first",
			args: []string{"--body", "hello", "42"},
			want: []string{"--body", "hello", "42"},
		},
		{
			name: "positional before flag",
			args: []string{"42", "--body", "hello"},
			want: []string{"--body", "hello", "42"},
		},
		{
			name:      "bool flag without value",
			args:      []string{"42", "--all"},
			boolFlags: map[string]bool{"all": true},
			want:      []string{"--all", "42"},
		},
		{
			name:      "mixed positional and flags",
			args:      []string{"42", "--folder", "Sent", "--peek"},
			boolFlags: map[string]bool{"peek": true},
			want:      []string{"--folder", "Sent", "--peek", "42"},
		},
		{
			name: "flag=value syntax",
			args: []string{"42", "--folder=Sent"},
			want: []string{"--folder=Sent", "42"},
		},
		{
			name: "multiple positional",
			args: []string{"42", "43", "--folder", "INBOX"},
			want: []string{"--folder", "INBOX", "42", "43"},
		},
		{
			name: "no flags",
			args: []string{"42", "43"},
			want: []string{"42", "43"},
		},
		{
			name: "empty",
			args: []string{},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReorderArgs(tt.args, tt.boolFlags)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("index %d: got %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestStringSliceFlag(t *testing.T) {
	var f StringSliceFlag
	f.Set("a@example.com")
	f.Set("b@example.com")

	if len(f) != 2 {
		t.Fatalf("got %d items", len(f))
	}
	if f[0] != "a@example.com" || f[1] != "b@example.com" {
		t.Errorf("got %v", f)
	}
	if s := f.String(); s != "a@example.com, b@example.com" {
		t.Errorf("String() = %q", s)
	}
}
