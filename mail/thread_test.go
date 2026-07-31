package mail

import (
	"strings"
	"testing"
	"time"
)

func TestBuildReplyHeaders(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		msg := &Message{}
		msg.MessageID = "<abc@example.com>"
		msg.References = nil

		irt, refs := BuildReplyHeaders(msg)
		if irt != "<abc@example.com>" {
			t.Errorf("InReplyTo = %q, want %q", irt, "<abc@example.com>")
		}
		if len(refs) != 1 || refs[0] != "<abc@example.com>" {
			t.Errorf("References = %v, want [<abc@example.com>]", refs)
		}
	})

	t.Run("chain", func(t *testing.T) {
		msg := &Message{}
		msg.MessageID = "<c@example.com>"
		msg.References = []string{"<a@example.com>", "<b@example.com>"}

		irt, refs := BuildReplyHeaders(msg)
		if irt != "<c@example.com>" {
			t.Errorf("InReplyTo = %q", irt)
		}
		if len(refs) != 3 {
			t.Errorf("References length = %d, want 3", len(refs))
		}
		if refs[2] != "<c@example.com>" {
			t.Errorf("last ref = %q", refs[2])
		}
	})

	t.Run("empty message-id", func(t *testing.T) {
		msg := &Message{}
		irt, refs := BuildReplyHeaders(msg)
		if irt != "" {
			t.Errorf("InReplyTo = %q, want empty", irt)
		}
		if len(refs) != 0 {
			t.Errorf("References = %v, want empty", refs)
		}
	})
}

func TestStripSubjectPrefix(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Re: Hello", "Hello"},
		{"Re: Re: Hello", "Hello"},
		{"Fwd: Hello", "Hello"},
		{"FW: Hello", "Hello"},
		{"RE: FW: Re: Hello", "Hello"},
		{"Hello", "Hello"},
		{"re: hello", "hello"},
		{"Re:nospace", "nospace"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := StripSubjectPrefix(tt.input)
			if got != tt.want {
				t.Errorf("StripSubjectPrefix(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestReplySubject(t *testing.T) {
	if got := ReplySubject("Re: Hello"); got != "Re: Hello" {
		t.Errorf("ReplySubject(Re: Hello) = %q", got)
	}
	if got := ReplySubject("Hello"); got != "Re: Hello" {
		t.Errorf("ReplySubject(Hello) = %q", got)
	}
}

func TestForwardSubject(t *testing.T) {
	if got := ForwardSubject("Hello"); got != "Fwd: Hello" {
		t.Errorf("ForwardSubject(Hello) = %q", got)
	}
	if got := ForwardSubject("Fwd: Hello"); got != "Fwd: Hello" {
		t.Errorf("ForwardSubject(Fwd: Hello) = %q", got)
	}
}

func TestQuoteBody(t *testing.T) {
	msg := &Message{}
	msg.From = Address{Name: "Alice", Address: "alice@example.com"}
	msg.Date = time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	msg.Subject = "Hello"
	msg.TextBody = "Hi there.\nHow are you?"

	quoted := QuoteBody(msg)

	if !strings.Contains(quoted, "--- Original Message ---") {
		t.Error("missing separator")
	}
	if !strings.Contains(quoted, "From: Alice <alice@example.com>") {
		t.Error("missing From line")
	}
	if !strings.Contains(quoted, "> Hi there.") {
		t.Error("missing quoted line")
	}
	if !strings.Contains(quoted, "> How are you?") {
		t.Error("missing second quoted line")
	}
}
