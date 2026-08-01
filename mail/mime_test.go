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
