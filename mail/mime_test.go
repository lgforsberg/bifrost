package mail

import (
	"strings"
	"testing"
	"time"
)

func TestParseMessage_DecodesLegacyCharsets(t *testing.T) {
	tests := map[string]struct {
		charset  string
		body     string // encoded in that charset, not UTF-8
		wantBody string
	}{
		"iso-8859-1": {
			charset:  "iso-8859-1",
			body:     "Caf\xe9",
			wantBody: "Café",
		},
		// 0x93/0x94 are smart quotes in windows-1252 and undefined in latin-1,
		// so decoding them proves the right table was picked.
		"windows-1252": {
			charset:  "windows-1252",
			body:     "\x93quoted\x94",
			wantBody: "“quoted”",
		},
		"utf-8": {
			charset:  "utf-8",
			body:     "Café",
			wantBody: "Café",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			raw := "From: alice@example.com\r\n" +
				"Subject: Legacy\r\n" +
				"Content-Type: text/plain; charset=" + tt.charset + "\r\n" +
				"\r\n" + tt.body

			msg, err := ParseMessage(strings.NewReader(raw))
			if err != nil {
				t.Fatalf("ParseMessage error: %v", err)
			}
			if !strings.Contains(msg.TextBody, tt.wantBody) {
				t.Errorf("TextBody = %q, want it to contain %q", msg.TextBody, tt.wantBody)
			}
		})
	}
}

func TestParseMessage_LegacyCharsetInMultipart(t *testing.T) {
	raw := "From: alice@example.com\r\n" +
		"Subject: Legacy\r\n" +
		"Content-Type: multipart/mixed; boundary=xyz\r\n" +
		"\r\n" +
		"--xyz\r\n" +
		"Content-Type: text/plain; charset=iso-8859-1\r\n" +
		"\r\n" +
		"Caf\xe9\r\n" +
		"--xyz--\r\n"

	msg, err := ParseMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseMessage error: %v", err)
	}
	if !strings.Contains(msg.TextBody, "Café") {
		t.Errorf("TextBody = %q, want the latin-1 part decoded rather than dropped", msg.TextBody)
	}
}

func TestParseMessage_ReadsReplyTo(t *testing.T) {
	raw := "From: Alice <alice@example.com>\r\n" +
		"Reply-To: The List <list@example.com>, moderator@example.com\r\n" +
		"To: me@example.com\r\n" +
		"Subject: Topic\r\n" +
		"\r\n" +
		"Body\r\n"

	msg, err := ParseMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseMessage error: %v", err)
	}
	if len(msg.ReplyTo) != 2 {
		t.Fatalf("ReplyTo has %d addresses, want 2: %v", len(msg.ReplyTo), msg.ReplyTo)
	}
	if msg.ReplyTo[0].Address != "list@example.com" || msg.ReplyTo[0].Name != "The List" {
		t.Errorf("first Reply-To = %+v, want the named list address", msg.ReplyTo[0])
	}
	if msg.ReplyTo[1].Address != "moderator@example.com" {
		t.Errorf("second Reply-To = %+v", msg.ReplyTo[1])
	}
}

func TestParseMessage_EncodedWordHeader(t *testing.T) {
	raw := "From: =?iso-8859-1?Q?Caf=E9_Owner?= <alice@example.com>\r\n" +
		"Subject: =?iso-8859-1?Q?Caf=E9_menu?=\r\n" +
		"Content-Type: text/plain; charset=us-ascii\r\n" +
		"\r\n" +
		"body"

	msg, err := ParseMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseMessage error: %v", err)
	}
	if msg.Subject != "Café menu" {
		t.Errorf("Subject = %q, want %q", msg.Subject, "Café menu")
	}
	if msg.From.Name != "Café Owner" {
		t.Errorf("From.Name = %q, want %q", msg.From.Name, "Café Owner")
	}
}

// Every input here made the part walk spin on a repeated error before the
// retry bound was added. Parts whose body cannot be read are skipped, so only
// cleanly readable parts show up in the body.
func TestParseMessage_MalformedMultipartDoesNotHang(t *testing.T) {
	const headers = "From: alice@example.com\r\n" +
		"Subject: Broken\r\n" +
		"Content-Type: multipart/mixed; boundary=xyz\r\n" +
		"\r\n"

	tests := map[string]struct {
		raw      string
		wantBody string
	}{
		"truncated mid part": {
			raw: headers + "--xyz\r\nContent-Type: text/plain\r\n\r\nhello\r\n",
		},
		"malformed trailing part header": {
			raw: headers + "--xyz\r\nContent-Type: text/plain\r\n\r\nhello\r\n" +
				"--xyz\r\nContent-Type: \x00garbage\r\n",
			wantBody: "hello",
		},
		"no parts at all": {
			raw: headers + "not a mime part at all\r\n",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			type result struct {
				msg *Message
				err error
			}
			done := make(chan result, 1)
			go func() {
				msg, err := ParseMessage(strings.NewReader(tt.raw))
				done <- result{msg, err}
			}()

			select {
			case got := <-done:
				if got.err != nil {
					t.Fatalf("ParseMessage error: %v", got.err)
				}
				if got.msg.Subject != "Broken" {
					t.Errorf("Subject = %q, want headers preserved", got.msg.Subject)
				}
				if !strings.Contains(got.msg.TextBody, tt.wantBody) {
					t.Errorf("TextBody = %q, want it to contain %q", got.msg.TextBody, tt.wantBody)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("ParseMessage did not return: the part walk is spinning on a repeated error")
			}
		})
	}
}

func TestParseMessage_SimplePlainText(t *testing.T) {
	raw := "From: alice@example.com\r\n" +
		"To: bob@example.com\r\n" +
		"Subject: Hello\r\n" +
		"Message-ID: <msg1@example.com>\r\n" +
		"Date: Mon, 02 Jan 2006 15:04:05 -0700\r\n" +
		"Content-Type: text/plain\r\n" +
		"\r\n" +
		"Hello, Bob!"

	msg, err := ParseMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseMessage error: %v", err)
	}

	if msg.Subject != "Hello" {
		t.Errorf("Subject = %q, want %q", msg.Subject, "Hello")
	}
	if msg.From.Address != "alice@example.com" {
		t.Errorf("From = %q", msg.From.Address)
	}
	if len(msg.To) != 1 || msg.To[0].Address != "bob@example.com" {
		t.Errorf("To = %v", msg.To)
	}
	if msg.MessageID != "msg1@example.com" {
		t.Errorf("MessageID = %q", msg.MessageID)
	}
	if !strings.Contains(msg.TextBody, "Hello, Bob!") {
		t.Errorf("TextBody = %q", msg.TextBody)
	}
	if len(msg.Attachments) != 0 {
		t.Errorf("Attachments = %d", len(msg.Attachments))
	}
}

func TestParseMessage_MultipartAlternative(t *testing.T) {
	raw := "From: alice@example.com\r\n" +
		"To: bob@example.com\r\n" +
		"Subject: HTML test\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/alternative; boundary=boundary1\r\n" +
		"\r\n" +
		"--boundary1\r\n" +
		"Content-Type: text/plain\r\n" +
		"\r\n" +
		"Plain text body\r\n" +
		"--boundary1\r\n" +
		"Content-Type: text/html\r\n" +
		"\r\n" +
		"<p>HTML body</p>\r\n" +
		"--boundary1--\r\n"

	msg, err := ParseMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseMessage error: %v", err)
	}

	if !strings.Contains(msg.TextBody, "Plain text body") {
		t.Errorf("TextBody = %q", msg.TextBody)
	}
	if !strings.Contains(msg.HTMLBody, "<p>HTML body</p>") {
		t.Errorf("HTMLBody = %q", msg.HTMLBody)
	}
}

func TestParseMessage_WithAttachment(t *testing.T) {
	raw := "From: alice@example.com\r\n" +
		"To: bob@example.com\r\n" +
		"Subject: With attachment\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=boundary2\r\n" +
		"\r\n" +
		"--boundary2\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"Content-Disposition: inline\r\n" +
		"\r\n" +
		"See attached.\r\n" +
		"--boundary2\r\n" +
		"Content-Type: application/pdf; name=\"doc.pdf\"\r\n" +
		"Content-Disposition: attachment; filename=\"doc.pdf\"\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"\r\n" +
		"SlZCRVI=\r\n" +
		"--boundary2--\r\n"

	msg, err := ParseMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseMessage error: %v", err)
	}

	if !strings.Contains(msg.TextBody, "See attached") {
		t.Errorf("TextBody = %q", msg.TextBody)
	}
	if len(msg.Attachments) != 1 {
		t.Fatalf("Attachments count = %d, want 1", len(msg.Attachments))
	}
	if msg.Attachments[0].Filename != "doc.pdf" {
		t.Errorf("Attachment filename = %q", msg.Attachments[0].Filename)
	}
}

func TestParseMessage_EmptyBody(t *testing.T) {
	raw := "From: alice@example.com\r\n" +
		"Subject: Empty\r\n" +
		"\r\n"

	msg, err := ParseMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseMessage error: %v", err)
	}

	if msg.Subject != "Empty" {
		t.Errorf("Subject = %q", msg.Subject)
	}
}

func TestParseMessage_MissingContentType(t *testing.T) {
	raw := "From: alice@example.com\r\n" +
		"Subject: No content type\r\n" +
		"\r\n" +
		"Just plain text without content-type header."

	msg, err := ParseMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseMessage error: %v", err)
	}

	if msg.Subject != "No content type" {
		t.Errorf("Subject = %q", msg.Subject)
	}
}

func TestParseMessage_References(t *testing.T) {
	raw := "From: alice@example.com\r\n" +
		"Subject: Thread\r\n" +
		"In-Reply-To: <parent@example.com>\r\n" +
		"References: <root@example.com> <parent@example.com>\r\n" +
		"Content-Type: text/plain\r\n" +
		"\r\n" +
		"Reply text."

	msg, err := ParseMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseMessage error: %v", err)
	}

	if msg.InReplyTo != "parent@example.com" {
		t.Errorf("InReplyTo = %q", msg.InReplyTo)
	}
	if len(msg.References) != 2 {
		t.Errorf("References = %v", msg.References)
	}
}

func TestParseMessage_MultipleInReplyTo(t *testing.T) {
	raw := "From: alice@example.com\r\n" +
		"Subject: Multi IRT\r\n" +
		"In-Reply-To: <first@example.com> <second@example.com>\r\n" +
		"Content-Type: text/plain\r\n" +
		"\r\n" +
		"Body."

	msg, err := ParseMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseMessage error: %v", err)
	}

	if msg.InReplyTo != "first@example.com" {
		t.Errorf("InReplyTo = %q, want %q", msg.InReplyTo, "first@example.com")
	}
}

func TestParseMessage_MultipartRelatedDrained(t *testing.T) {
	raw := "From: alice@example.com\r\n" +
		"Subject: Related wrapper\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=outer\r\n" +
		"\r\n" +
		"--outer\r\n" +
		"Content-Type: multipart/related; boundary=inner\r\n" +
		"\r\n" +
		"--inner\r\n" +
		"Content-Type: text/html\r\n" +
		"\r\n" +
		"<p>Hello</p>\r\n" +
		"--inner\r\n" +
		"Content-Type: image/png\r\n" +
		"Content-ID: <img1>\r\n" +
		"\r\n" +
		"PNGDATA\r\n" +
		"--inner--\r\n" +
		"--outer\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"Content-Disposition: inline\r\n" +
		"\r\n" +
		"Plain text after related.\r\n" +
		"--outer--\r\n"

	msg, err := ParseMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseMessage error: %v", err)
	}

	if !strings.Contains(msg.TextBody, "Plain text after related") {
		t.Errorf("TextBody = %q, expected text after multipart/related to be parsed", msg.TextBody)
	}
}

// The four ways a damaged message used to come back wrong. Each of these
// returned nothing at all, or nothing useful, before the parser learned to
// keep what it could read.

// io.ReadAll hands back the bytes it managed to read alongside the error, and
// the old code dropped them, so a message cut off in transit read as empty.
func TestParseMessage_KeepsThePrefixOfATruncatedBody(t *testing.T) {
	// "Hello, world!" encodes to SGVsbG8sIHdvcmxkIQ==, cut here to an
	// incomplete final group so the decoder fails after the readable part.
	raw := "From: sender@example.com\r\n" +
		"To: rcpt@example.com\r\n" +
		"Subject: Cut short\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"\r\n" +
		"SGVsbG8sIHdvcmxkIQ"

	msg, err := ParseMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	if !strings.HasPrefix(msg.TextBody, "Hello, world") {
		t.Errorf("TextBody = %q, want the part that was readable", msg.TextBody)
	}
	if len(msg.Warnings) == 0 {
		t.Error("a truncated body should be reported, not quietly patched over")
	}
	if msg.Subject != "Cut short" {
		t.Errorf("Subject = %q, want the headers intact", msg.Subject)
	}
}

// An attachment that stops part way used to vanish, which reads exactly like a
// message that never carried one.
func TestParseMessage_KeepsATruncatedAttachment(t *testing.T) {
	raw := "From: sender@example.com\r\n" +
		"Subject: Report\r\n" +
		"Content-Type: multipart/mixed; boundary=BOUND\r\n" +
		"\r\n" +
		"--BOUND\r\n" +
		"Content-Type: text/plain\r\n" +
		"\r\n" +
		"See attached.\r\n" +
		"--BOUND\r\n" +
		"Content-Type: application/pdf\r\n" +
		"Content-Disposition: attachment; filename=\"report.pdf\"\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"\r\n" +
		"SGVsbG8sIHdvcmxkIQ\r\n" +
		"--BOUND--\r\n"

	msg, err := ParseMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	if len(msg.Attachments) != 1 {
		t.Fatalf("got %d attachments, want the damaged one surfaced rather than dropped", len(msg.Attachments))
	}
	att := msg.Attachments[0]
	if att.Filename != "report.pdf" {
		t.Errorf("Filename = %q, want report.pdf", att.Filename)
	}
	if att.Size != int64(len(att.Data)) {
		t.Errorf("Size %d does not match the %d bytes actually held", att.Size, len(att.Data))
	}
	if len(msg.Warnings) == 0 {
		t.Error("a partial attachment must carry a warning, or it looks like a whole file")
	}
	if !strings.Contains(msg.TextBody, "See attached") {
		t.Errorf("TextBody = %q, want the good part unaffected", msg.TextBody)
	}
}

// A charset nothing can decode used to fail the entire message, headers and
// all, over a single mislabelled parameter.
func TestParseMessage_ReadsThroughAnUnknownCharset(t *testing.T) {
	raw := "From: sender@example.com\r\n" +
		"Subject: Odd label\r\n" +
		"Content-Type: text/plain; charset=x-nonesuch\r\n" +
		"\r\n" +
		"perfectly readable ascii\r\n"

	msg, err := ParseMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	if !strings.Contains(msg.TextBody, "perfectly readable ascii") {
		t.Errorf("TextBody = %q, want the undecoded bytes", msg.TextBody)
	}
	if msg.Subject != "Odd label" {
		t.Errorf("Subject = %q, want the headers intact", msg.Subject)
	}
	if len(msg.Warnings) == 0 {
		t.Error("reading a part without decoding it should be reported")
	}
}

// Same for a transfer encoding the reader has no decoder for. This one is why
// ParseMessage calls message.Read directly: mail.CreateReader discards the
// entity here and reports only the error.
func TestParseMessage_ReadsThroughAnUnknownTransferEncoding(t *testing.T) {
	raw := "From: sender@example.com\r\n" +
		"Subject: Ancient client\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: x-uuencode\r\n" +
		"\r\n" +
		"body text here\r\n"

	msg, err := ParseMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	if !strings.Contains(msg.TextBody, "body text here") {
		t.Errorf("TextBody = %q, want the raw bytes", msg.TextBody)
	}
	if msg.Subject != "Ancient client" {
		t.Errorf("Subject = %q, want the headers intact", msg.Subject)
	}
	if len(msg.Warnings) == 0 {
		t.Error("an undecodable encoding should be reported")
	}
}

// The common case must stay silent, or warnings become noise nobody reads.
func TestParseMessage_CleanMessageWarnsAboutNothing(t *testing.T) {
	raw := "From: sender@example.com\r\n" +
		"To: rcpt@example.com\r\n" +
		"Subject: Ordinary\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"Nothing wrong here.\r\n"

	msg, err := ParseMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	if len(msg.Warnings) != 0 {
		t.Errorf("Warnings = %v, want none for a well-formed message", msg.Warnings)
	}
}

// One bad part inside a multipart must not cost the others. The reader will
// not hand this part over at all (see T-041), so its content is genuinely
// lost, but the walk carries on and the loss is named rather than silent.
func TestParseMessage_OneBadPartDoesNotCostTheRest(t *testing.T) {
	raw := "From: sender@example.com\r\n" +
		"Subject: Mixed\r\n" +
		"Content-Type: multipart/mixed; boundary=BOUND\r\n" +
		"\r\n" +
		"--BOUND\r\n" +
		"Content-Type: text/plain\r\n" +
		"\r\n" +
		"good part\r\n" +
		"--BOUND\r\n" +
		"Content-Type: text/plain\r\n" +
		"Content-Transfer-Encoding: x-uuencode\r\n" +
		"\r\n" +
		"odd part\r\n" +
		"--BOUND\r\n" +
		"Content-Type: application/pdf\r\n" +
		"Content-Disposition: attachment; filename=\"a.pdf\"\r\n" +
		"\r\n" +
		"PDFBYTES\r\n" +
		"--BOUND--\r\n"

	msg, err := ParseMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	if !strings.Contains(msg.TextBody, "good part") {
		t.Errorf("TextBody = %q, want the part before the bad one", msg.TextBody)
	}
	// The attachment comes after the failure, so this is the assertion that
	// the walk resumed instead of stopping at the first thing it could not do.
	if len(msg.Attachments) != 1 {
		t.Errorf("got %d attachments, want the one that follows the bad part", len(msg.Attachments))
	}
	if len(msg.Warnings) == 0 {
		t.Error("the skipped part must be named, or it is silent data loss")
	}
}
