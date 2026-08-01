package mail

import (
	"strings"
	"testing"
)

func TestComposeMessage_TextOnly(t *testing.T) {
	opts := SendOptions{
		From:     Address{Name: "Alice", Address: "alice@example.com"},
		To:       []Address{{Address: "bob@example.com"}},
		Subject:  "Test",
		TextBody: "Hello, World!",
	}

	data, err := ComposeMessage(opts)
	if err != nil {
		t.Fatalf("ComposeMessage error: %v", err)
	}

	raw := string(data)
	if !strings.Contains(raw, "Subject: Test") {
		t.Error("missing Subject header")
	}
	if !strings.Contains(raw, "alice@example.com") {
		t.Error("missing From address")
	}
	if !strings.Contains(raw, "bob@example.com") {
		t.Error("missing To address")
	}
	if !strings.Contains(raw, "Hello, World!") {
		t.Error("missing body text")
	}
}

func TestComposeMessage_TextAndHTML(t *testing.T) {
	opts := SendOptions{
		From:     Address{Address: "alice@example.com"},
		To:       []Address{{Address: "bob@example.com"}},
		Subject:  "HTML test",
		TextBody: "Plain text",
		HTMLBody: "<p>HTML text</p>",
	}

	data, err := ComposeMessage(opts)
	if err != nil {
		t.Fatalf("ComposeMessage error: %v", err)
	}

	raw := string(data)
	if !strings.Contains(raw, "multipart/alternative") {
		t.Error("expected multipart/alternative for text+html")
	}
}

func TestComposeMessage_WithAttachments(t *testing.T) {
	opts := SendOptions{
		From:     Address{Address: "alice@example.com"},
		To:       []Address{{Address: "bob@example.com"}},
		Subject:  "Attachment test",
		TextBody: "See attached.",
		Attachments: []Attachment{
			{Filename: "test.txt", Data: []byte("file content"), ContentType: "text/plain"},
		},
	}

	data, err := ComposeMessage(opts)
	if err != nil {
		t.Fatalf("ComposeMessage error: %v", err)
	}

	raw := string(data)
	if !strings.Contains(raw, "multipart/mixed") {
		t.Error("expected multipart/mixed for attachments")
	}
	if !strings.Contains(raw, "test.txt") {
		t.Error("missing attachment filename")
	}
}

func TestComposeMessage_WithThreadingHeaders(t *testing.T) {
	opts := SendOptions{
		From:       Address{Address: "alice@example.com"},
		To:         []Address{{Address: "bob@example.com"}},
		Subject:    "Re: Hello",
		TextBody:   "Reply body",
		InReplyTo:  "<parent@example.com>",
		References: []string{"<root@example.com>", "<parent@example.com>"},
	}

	data, err := ComposeMessage(opts)
	if err != nil {
		t.Fatalf("ComposeMessage error: %v", err)
	}

	raw := string(data)
	if !strings.Contains(raw, "In-Reply-To") {
		t.Error("missing In-Reply-To header")
	}
	if !strings.Contains(raw, "References") {
		t.Error("missing References header")
	}
}

func TestComposeMessage_OmitsBccHeader(t *testing.T) {
	base := SendOptions{
		From:     Address{Address: "alice@example.com"},
		To:       []Address{{Address: "bob@example.com"}},
		Cc:       []Address{{Address: "carol@example.com"}},
		Bcc:      []Address{{Address: "dave@example.com"}},
		Subject:  "Bcc test",
		TextBody: "Body",
	}

	withHTML := base
	withHTML.HTMLBody = "<p>Body</p>"

	withAttachment := base
	withAttachment.Attachments = []Attachment{
		{Filename: "test.txt", Data: []byte("file content"), ContentType: "text/plain"},
	}

	variants := map[string]SendOptions{
		"text only":        base,
		"text and html":    withHTML,
		"with attachments": withAttachment,
	}

	for name, opts := range variants {
		t.Run(name, func(t *testing.T) {
			data, err := ComposeMessage(opts)
			if err != nil {
				t.Fatalf("ComposeMessage error: %v", err)
			}

			raw := string(data)
			if strings.Contains(raw, "dave@example.com") {
				t.Errorf("Bcc recipient disclosed in delivered message:\n%s", raw)
			}
			if strings.Contains(raw, "Bcc:") {
				t.Error("Bcc header written to delivered message")
			}
			if !strings.Contains(raw, "bob@example.com") {
				t.Error("missing To address")
			}
			if !strings.Contains(raw, "carol@example.com") {
				t.Error("missing Cc address")
			}
		})
	}
}

func TestComposeMessage_BccReachesEnvelope(t *testing.T) {
	// With the header omitted, delivery to blind recipients depends entirely on
	// the SMTP envelope.
	opts := SendOptions{
		From:     Address{Address: "alice@example.com"},
		To:       []Address{{Address: "bob@example.com"}},
		Bcc:      []Address{{Address: "dave@example.com"}},
		Subject:  "Bcc test",
		TextBody: "Body",
	}

	recipients := collectRecipients(opts)
	if !containsStr(recipients, "dave@example.com") {
		t.Errorf("Bcc recipient missing from SMTP envelope: %v", recipients)
	}
	if !containsStr(recipients, "bob@example.com") {
		t.Errorf("To recipient missing from SMTP envelope: %v", recipients)
	}
}

func TestComposeMessage_ServerCopyKeepsBcc(t *testing.T) {
	// Copies that stay on the server (Sent, Drafts) record who was blind-copied.
	opts := SendOptions{
		From:     Address{Address: "alice@example.com"},
		To:       []Address{{Address: "bob@example.com"}},
		Bcc:      []Address{{Address: "dave@example.com"}},
		Subject:  "Bcc test",
		TextBody: "Body",
	}

	data, err := composeMessage(opts, true)
	if err != nil {
		t.Fatalf("composeMessage error: %v", err)
	}

	raw := string(data)
	if !strings.Contains(raw, "Bcc:") {
		t.Error("missing Bcc header on server-side copy")
	}
	if !strings.Contains(raw, "dave@example.com") {
		t.Error("missing Bcc address on server-side copy")
	}
}

func TestComposeMessage_RoundTrip(t *testing.T) {
	opts := SendOptions{
		From:     Address{Name: "Alice", Address: "alice@example.com"},
		To:       []Address{{Name: "Bob", Address: "bob@example.com"}},
		Subject:  "Round trip",
		TextBody: "Round trip body.",
	}

	data, err := ComposeMessage(opts)
	if err != nil {
		t.Fatalf("ComposeMessage error: %v", err)
	}

	parsed, err := ParseMessage(strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("ParseMessage error: %v", err)
	}

	if parsed.Subject != "Round trip" {
		t.Errorf("Subject = %q", parsed.Subject)
	}
	if parsed.From.Address != "alice@example.com" {
		t.Errorf("From = %q", parsed.From.Address)
	}
	if !strings.Contains(parsed.TextBody, "Round trip body.") {
		t.Errorf("TextBody = %q", parsed.TextBody)
	}
}
