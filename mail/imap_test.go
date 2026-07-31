package mail

import (
	"errors"
	"fmt"
	"strings"
	"testing"
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
