package mail

import (
	"io"
	"mime"
	"strings"

	"github.com/emersion/go-message/mail"
)

// ParseMessage reads an RFC 2822 message from r and extracts headers,
// text/html bodies, and attachments. Pure parsing — no I/O beyond the reader.
func ParseMessage(r io.Reader) (*Message, error) {
	mr, err := mail.CreateReader(r)
	if err != nil {
		return nil, err
	}

	msg := &Message{}

	h := mr.Header
	if date, err := h.Date(); err == nil {
		msg.Date = date
	}
	if subject, err := h.Subject(); err == nil {
		msg.Subject = subject
	}
	if from, err := h.AddressList("From"); err == nil && len(from) > 0 {
		msg.From = Address{Name: from[0].Name, Address: from[0].Address}
	}
	if to, err := h.AddressList("To"); err == nil {
		for _, a := range to {
			msg.To = append(msg.To, Address{Name: a.Name, Address: a.Address})
		}
	}
	if cc, err := h.AddressList("Cc"); err == nil {
		for _, a := range cc {
			msg.Cc = append(msg.Cc, Address{Name: a.Name, Address: a.Address})
		}
	}
	if mid, err := h.MessageID(); err == nil {
		msg.MessageID = mid
	}
	if irts, err := h.MsgIDList("In-Reply-To"); err == nil && len(irts) > 0 {
		msg.InReplyTo = irts[0]
	}
	if refs, err := h.MsgIDList("References"); err == nil {
		msg.References = refs
	}

	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			// Gracefully skip malformed parts
			continue
		}

		switch h := p.Header.(type) {
		case *mail.InlineHeader:
			ct, _, _ := h.ContentType()
			body, readErr := io.ReadAll(p.Body)
			if readErr != nil {
				continue
			}
			switch {
			case strings.HasPrefix(ct, "text/plain"):
				if msg.TextBody == "" {
					msg.TextBody = string(body)
				}
			case strings.HasPrefix(ct, "text/html"):
				if msg.HTMLBody == "" {
					msg.HTMLBody = string(body)
				}
			}
		case *mail.AttachmentHeader:
			filename, _ := h.Filename()
			ct, _, _ := h.ContentType()
			data, readErr := io.ReadAll(p.Body)
			if readErr != nil {
				continue
			}
			msg.Attachments = append(msg.Attachments, Attachment{
				Filename:    filename,
				ContentType: ct,
				Size:        int64(len(data)),
				Data:        data,
			})
		default:
			io.Copy(io.Discard, p.Body)
		}
	}

	return msg, nil
}

// contentTypeFromFilename returns a MIME type based on file extension.
func contentTypeFromFilename(filename string) string {
	ct := mime.TypeByExtension("." + fileExtension(filename))
	if ct == "" {
		return "application/octet-stream"
	}
	return ct
}

func fileExtension(filename string) string {
	for i := len(filename) - 1; i >= 0; i-- {
		if filename[i] == '.' {
			return filename[i+1:]
		}
	}
	return ""
}
