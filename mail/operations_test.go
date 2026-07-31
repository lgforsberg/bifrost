package mail

import (
	"strings"
	"testing"
	"time"
)

func TestBuildReply_Basic(t *testing.T) {
	acct := AccountConfig{
		Address:     "me@example.com",
		DisplayName: "Me",
	}
	original := &Message{}
	original.From = Address{Name: "Alice", Address: "alice@example.com"}
	original.To = []Address{{Address: "me@example.com"}}
	original.Subject = "Hello"
	original.MessageID = "<123@example.com>"
	original.TextBody = "Original body."

	opts := BuildReply(acct, original, "Thanks!", false, false)

	if opts.Subject != "Re: Hello" {
		t.Errorf("Subject = %q", opts.Subject)
	}
	if len(opts.To) != 1 || opts.To[0].Address != "alice@example.com" {
		t.Errorf("To = %v", opts.To)
	}
	if opts.InReplyTo != "<123@example.com>" {
		t.Errorf("InReplyTo = %q", opts.InReplyTo)
	}
	if opts.TextBody != "Thanks!" {
		t.Errorf("TextBody = %q", opts.TextBody)
	}
}

func TestBuildReply_AllWithSelfExclusion(t *testing.T) {
	acct := AccountConfig{
		Address:     "me@example.com",
		DisplayName: "Me",
	}
	original := &Message{}
	original.From = Address{Address: "alice@example.com"}
	original.To = []Address{
		{Address: "me@example.com"},
		{Address: "bob@example.com"},
	}
	original.Cc = []Address{
		{Address: "me@example.com"},
		{Address: "carol@example.com"},
	}
	original.Subject = "Discussion"
	original.MessageID = "<d@example.com>"

	opts := BuildReply(acct, original, "Agreed.", true, false)

	// To should be [alice, bob] (me excluded)
	toAddrs := make([]string, len(opts.To))
	for i, a := range opts.To {
		toAddrs[i] = a.Address
	}
	if !containsStr(toAddrs, "alice@example.com") {
		t.Error("To missing alice")
	}
	if !containsStr(toAddrs, "bob@example.com") {
		t.Error("To missing bob")
	}
	if containsStr(toAddrs, "me@example.com") {
		t.Error("To should not contain self")
	}

	// Cc should be [carol] (me excluded)
	ccAddrs := make([]string, len(opts.Cc))
	for i, a := range opts.Cc {
		ccAddrs[i] = a.Address
	}
	if !containsStr(ccAddrs, "carol@example.com") {
		t.Error("Cc missing carol")
	}
	if containsStr(ccAddrs, "me@example.com") {
		t.Error("Cc should not contain self")
	}
}

func TestBuildReply_WithQuoting(t *testing.T) {
	acct := AccountConfig{Address: "me@example.com"}
	original := &Message{}
	original.From = Address{Name: "Alice", Address: "alice@example.com"}
	original.Date = time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	original.Subject = "Hello"
	original.MessageID = "<q@example.com>"
	original.TextBody = "How are you?"

	opts := BuildReply(acct, original, "Good!", false, true)

	if !strings.Contains(opts.TextBody, "Good!") {
		t.Error("missing reply body")
	}
	if !strings.Contains(opts.TextBody, "> How are you?") {
		t.Error("missing quoted text")
	}
	if !strings.Contains(opts.TextBody, "--- Original Message ---") {
		t.Error("missing quote separator")
	}
}

func TestBuildReply_SubjectDedup(t *testing.T) {
	acct := AccountConfig{Address: "me@example.com"}
	original := &Message{}
	original.From = Address{Address: "alice@example.com"}
	original.Subject = "Re: Re: FW: Hello"
	original.MessageID = "<s@example.com>"

	opts := BuildReply(acct, original, "Body", false, false)

	if opts.Subject != "Re: Hello" {
		t.Errorf("Subject = %q, want %q", opts.Subject, "Re: Hello")
	}
}

func TestBuildForward(t *testing.T) {
	acct := AccountConfig{Address: "me@example.com", DisplayName: "Me"}
	original := &Message{}
	original.From = Address{Name: "Alice", Address: "alice@example.com"}
	original.Date = time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	original.Subject = "Hello"
	original.TextBody = "Original content."
	original.Attachments = []Attachment{
		{Filename: "doc.pdf", Data: []byte("pdf"), ContentType: "application/pdf"},
	}

	to := []Address{{Address: "bob@example.com"}}
	opts := BuildForward(acct, original, to, "FYI")

	if opts.Subject != "Fwd: Hello" {
		t.Errorf("Subject = %q", opts.Subject)
	}
	if len(opts.To) != 1 || opts.To[0].Address != "bob@example.com" {
		t.Errorf("To = %v", opts.To)
	}
	if !strings.Contains(opts.TextBody, "FYI") {
		t.Error("missing forward body")
	}
	if !strings.Contains(opts.TextBody, "> Original content.") {
		t.Error("missing quoted original")
	}
	if len(opts.Attachments) != 1 {
		t.Error("original attachments not carried")
	}
}

func TestCollectRecipients_Empty(t *testing.T) {
	opts := SendOptions{
		From: Address{Address: "me@example.com"},
	}
	recipients := collectRecipients(opts)
	if len(recipients) != 0 {
		t.Errorf("expected empty recipients, got %v", recipients)
	}
}

func TestQuoteBody_HTMLOnly(t *testing.T) {
	original := &Message{}
	original.From = Address{Name: "Alice", Address: "alice@example.com"}
	original.Date = time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	original.Subject = "HTML only"
	original.HTMLBody = "<p>This is <b>bold</b> text.</p>"

	quoted := QuoteBody(original)
	if !strings.Contains(quoted, "This is bold text.") {
		t.Errorf("QuoteBody should strip HTML tags, got: %q", quoted)
	}
	if strings.Contains(quoted, "<p>") {
		t.Error("QuoteBody should not contain HTML tags")
	}
}

func containsStr(ss []string, s string) bool {
	for _, item := range ss {
		if item == s {
			return true
		}
	}
	return false
}
