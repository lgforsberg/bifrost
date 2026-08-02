package mail

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
)

func TestMatchSpecialFolder(t *testing.T) {
	tests := map[string]struct {
		folders []Folder
		attr    string
		want    string
		wantOK  bool
	}{
		// The whole point of the attribute: on a localized account the English
		// name is either absent or, worse, some unrelated folder.
		"advertised attribute beats the conventional name": {
			folders: []Folder{
				{Name: "Archive"},
				{Name: "Arkiv", Attributes: []string{"\\Archive"}},
			},
			attr:   "\\Archive",
			want:   "Arkiv",
			wantOK: true,
		},
		"attribute match is case insensitive": {
			folders: []Folder{{Name: "Arkiv", Attributes: []string{"\\archive"}}},
			attr:    "\\Archive",
			want:    "Arkiv",
			wantOK:  true,
		},
		"falls back to the conventional name": {
			folders: []Folder{{Name: "INBOX"}, {Name: "Archive"}},
			attr:    "\\Archive",
			want:    "Archive",
			wantOK:  true,
		},
		"falls back through the preference order": {
			folders: []Folder{{Name: "INBOX"}, {Name: "Archives"}},
			attr:    "\\Archive",
			want:    "Archives",
			wantOK:  true,
		},
		"nothing to match": {
			folders: []Folder{{Name: "INBOX"}, {Name: "[Gmail]/All Mail", Attributes: []string{"\\All"}}},
			attr:    "\\Archive",
			wantOK:  false,
		},
		"sent still resolves": {
			folders: []Folder{{Name: "Skickat", Attributes: []string{"\\Sent"}}},
			attr:    "\\Sent",
			want:    "Skickat",
			wantOK:  true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, ok := matchSpecialFolder(tt.folders, tt.attr)
			if ok != tt.wantOK {
				t.Fatalf("matched = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("folder = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClassifyFolderError(t *testing.T) {
	tests := []struct {
		name         string
		rawErr       error
		wantSentinel error
		wantContains string
	}{
		{
			name:         "already exists",
			rawErr:       fmt.Errorf("NO [ALREADYEXISTS] Mailbox already exists (0.001 + 0.000 secs)."),
			wantSentinel: ErrAlreadyExists,
			wantContains: "already exists",
		},
		{
			name:         "nonexistent",
			rawErr:       fmt.Errorf("NO [NONEXISTENT] Mailbox doesn't exist: TestFolder (0.001 + 0.000 secs)."),
			wantSentinel: ErrNotFound,
			wantContains: "does not exist",
		},
		{
			name:         "unrecognized error passes through",
			rawErr:       fmt.Errorf("NO Some unexpected IMAP error"),
			wantSentinel: nil,
			wantContains: "Some unexpected IMAP error",
		},
		// The response code is read off the typed error rather than matched in
		// the rendered string, so the classification no longer depends on how
		// the library happens to format one.
		{
			name:         "already exists response code",
			rawErr:       &imap.Error{Type: imap.StatusResponseTypeNo, Code: imap.ResponseCodeAlreadyExists, Text: "Cannot create"},
			wantSentinel: ErrAlreadyExists,
			wantContains: "already exists",
		},
		{
			name:         "nonexistent response code",
			rawErr:       &imap.Error{Type: imap.StatusResponseTypeNo, Code: imap.ResponseCodeNonExistent, Text: "Cannot select"},
			wantSentinel: ErrNotFound,
			wantContains: "does not exist",
		},
		// Response codes are optional, and a bare NO still has to be understood.
		{
			name:         "bare NO with only wording",
			rawErr:       &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Mailbox doesn't exist"},
			wantSentinel: ErrNotFound,
			wantContains: "does not exist",
		},
		{
			name:         "transport failure is not a folder verdict",
			rawErr:       errors.New("write tcp 10.0.0.1:993: broken pipe"),
			wantSentinel: nil,
			wantContains: "broken pipe",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyFolderError("create", "TestFolder", tt.rawErr)
			if got == nil {
				t.Fatal("expected non-nil error")
			}
			if tt.wantSentinel != nil && !errors.Is(got, tt.wantSentinel) {
				t.Errorf("error should wrap %v, got: %v", tt.wantSentinel, got)
			}
			if tt.wantSentinel == nil && (errors.Is(got, ErrAlreadyExists) || errors.Is(got, ErrNotFound)) {
				t.Errorf("unexpected sentinel in: %v", got)
			}
			if !strings.Contains(got.Error(), tt.wantContains) {
				t.Errorf("error %q should contain %q", got.Error(), tt.wantContains)
			}
		})
	}
}

// The protocol hands sizes over as int64 and counts as int. Wrapping a value
// that does not fit would turn an implausibly large number into a small and
// entirely plausible one.
func TestClampToUint32(t *testing.T) {
	for name, tc := range map[string]struct {
		in   int64
		want uint32
	}{
		"an ordinary size":       {in: 4096, want: 4096},
		"zero":                   {in: 0, want: 0},
		"the largest that fits":  {in: math.MaxUint32, want: math.MaxUint32},
		"one too many saturates": {in: math.MaxUint32 + 1, want: math.MaxUint32},
		"far too many":           {in: math.MaxInt64, want: math.MaxUint32},
		"a negative reads as none rather than as four billion": {in: -1, want: 0},
	} {
		t.Run(name, func(t *testing.T) {
			if got := clampToUint32(tc.in); got != tc.want {
				t.Errorf("clampToUint32(%d) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}
