package mail

import (
	"bytes"
	"net/netip"
	"strings"
	"time"

	"github.com/emersion/go-message/mail"
	"github.com/google/uuid"
)

// ComposeMessage builds an RFC 822 message from SendOptions and returns
// the raw bytes. Pure composition — no I/O, no context needed.
//
// The Bcc header is never written. Blind-copied recipients belong in the SMTP
// envelope; emitting the header would disclose them to every other recipient.
func ComposeMessage(opts SendOptions) ([]byte, error) {
	return composeMessage(opts, false)
}

// composeMessage optionally retains the Bcc header. Only copies that stay on
// the server (Sent, Drafts) may keep it; never hand such bytes to SMTP.
func composeMessage(opts SendOptions, includeBcc bool) ([]byte, error) {
	var buf bytes.Buffer

	var h mail.Header
	h.SetDate(time.Now().UTC())
	msgID := opts.MessageID
	if msgID == "" {
		msgID = messageIDFor(opts.From.Address)
	}
	h.SetMessageID(msgID)
	h.SetSubject(opts.Subject)

	h.SetAddressList("From", []*mail.Address{
		{Name: opts.From.Name, Address: opts.From.Address},
	})

	toAddrs := make([]*mail.Address, len(opts.To))
	for i, a := range opts.To {
		toAddrs[i] = &mail.Address{Name: a.Name, Address: a.Address}
	}
	h.SetAddressList("To", toAddrs)

	if len(opts.Cc) > 0 {
		ccAddrs := make([]*mail.Address, len(opts.Cc))
		for i, a := range opts.Cc {
			ccAddrs[i] = &mail.Address{Name: a.Name, Address: a.Address}
		}
		h.SetAddressList("Cc", ccAddrs)
	}

	if includeBcc && len(opts.Bcc) > 0 {
		bccAddrs := make([]*mail.Address, len(opts.Bcc))
		for i, a := range opts.Bcc {
			bccAddrs[i] = &mail.Address{Name: a.Name, Address: a.Address}
		}
		h.SetAddressList("Bcc", bccAddrs)
	}

	if opts.InReplyTo != "" {
		h.SetMsgIDList("In-Reply-To", []string{opts.InReplyTo})
	}
	if len(opts.References) > 0 {
		h.SetMsgIDList("References", opts.References)
	}

	hasAttachments := len(opts.Attachments) > 0
	hasHTML := opts.HTMLBody != ""

	// An HTML message still gets a text/plain alternative, so it reads as
	// something in a client that will not render HTML and does not look like
	// an empty message to a spam filter. Derived only when the caller left it
	// empty; a text body that was supplied is always preferred.
	if hasHTML && opts.TextBody == "" {
		opts.TextBody = htmlToText(opts.HTMLBody)
	}

	switch {
	case hasAttachments:
		// multipart/mixed wrapping body parts + attachments
		w, err := mail.CreateWriter(&buf, h)
		if err != nil {
			return nil, err
		}

		if hasHTML {
			// multipart/alternative for text + html
			iw, err := w.CreateInline()
			if err != nil {
				return nil, err
			}
			if err := writeInlinePart(iw, "text/plain", opts.TextBody); err != nil {
				return nil, err
			}
			if err := writeInlinePart(iw, "text/html", opts.HTMLBody); err != nil {
				return nil, err
			}
			if err := iw.Close(); err != nil {
				return nil, err
			}
		} else {
			// Single text part
			var ih mail.InlineHeader
			ih.SetContentType("text/plain", map[string]string{"charset": "utf-8"})
			pw, err := w.CreateSingleInline(ih)
			if err != nil {
				return nil, err
			}
			if _, err := pw.Write([]byte(opts.TextBody)); err != nil {
				return nil, err
			}
			if err := pw.Close(); err != nil {
				return nil, err
			}
		}

		for _, att := range opts.Attachments {
			ct := att.ContentType
			if ct == "" {
				ct = contentTypeFromFilename(att.Filename)
			}
			var ah mail.AttachmentHeader
			ah.SetContentType(ct, nil)
			ah.SetFilename(att.Filename)
			aw, err := w.CreateAttachment(ah)
			if err != nil {
				return nil, err
			}
			if _, err := aw.Write(att.Data); err != nil {
				return nil, err
			}
			if err := aw.Close(); err != nil {
				return nil, err
			}
		}

		if err := w.Close(); err != nil {
			return nil, err
		}

	case hasHTML:
		// multipart/alternative for text + html (no attachments)
		iw, err := mail.CreateInlineWriter(&buf, h)
		if err != nil {
			return nil, err
		}
		if err := writeInlinePart(iw, "text/plain", opts.TextBody); err != nil {
			return nil, err
		}
		if err := writeInlinePart(iw, "text/html", opts.HTMLBody); err != nil {
			return nil, err
		}
		if err := iw.Close(); err != nil {
			return nil, err
		}

	default:
		// Single text/plain message
		h.SetContentType("text/plain", map[string]string{"charset": "utf-8"})
		pw, err := mail.CreateSingleInlineWriter(&buf, h)
		if err != nil {
			return nil, err
		}
		if _, err := pw.Write([]byte(opts.TextBody)); err != nil {
			return nil, err
		}
		if err := pw.Close(); err != nil {
			return nil, err
		}
	}

	return buf.Bytes(), nil
}

func writeInlinePart(iw *mail.InlineWriter, contentType, body string) error {
	var ih mail.InlineHeader
	ih.SetContentType(contentType, map[string]string{"charset": "utf-8"})
	pw, err := iw.CreatePart(ih)
	if err != nil {
		return err
	}
	if _, err := pw.Write([]byte(body)); err != nil {
		return err
	}
	return pw.Close()
}

// fallbackMessageIDDomain is used when the sender's address yields nothing
// usable. It is deliberately not the machine's name.
const fallbackMessageIDDomain = "bifrost.local"

// messageIDFor builds a Message-ID for mail sent from address.
//
// The right hand side is the sender's own domain, which is what RFC 5322 asks
// for: a domain the sender is entitled to speak for. It used to be the
// machine's hostname, which put the name of a laptop or an internal server
// into a header that every recipient keeps forever, and told them nothing
// they could use.
func messageIDFor(address string) string {
	_, _, domain := ParsePlusAddress(address)
	domain = strings.Trim(strings.TrimSpace(domain), "[]")

	// An address literal is a legal domain but a poor identifier: it is not
	// stable, not the sender's to claim, and filters treat it with suspicion.
	if domain == "" || isIPAddr(domain) || strings.ContainsAny(domain, " <>@\"") {
		domain = fallbackMessageIDDomain
	}
	return uuid.New().String() + "@" + domain
}

func isIPAddr(s string) bool {
	_, err := netip.ParseAddr(s)
	return err == nil
}
