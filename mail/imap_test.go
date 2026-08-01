package mail

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
)

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
