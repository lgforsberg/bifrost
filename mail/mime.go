package mail

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"strings"

	message "github.com/emersion/go-message"

	// Registers decoders for non-UTF-8 charsets. Without it go-message handles
	// only utf-8 and us-ascii, and iso-8859-x or windows-125x mail fails to parse.
	_ "github.com/emersion/go-message/charset"
	"github.com/emersion/go-message/mail"
)

// maxConsecutiveBadParts bounds how many failing parts in a row the walk will
// skip. A truncated or malformed body makes NextPart report the same failure
// on every call without advancing, so an unbounded skip would never terminate.
const maxConsecutiveBadParts = 10

// ParseMessage reads an RFC 2822 message from r and extracts headers,
// text/html bodies, and attachments. Pure parsing — no I/O beyond the reader.
//
// Parsing is best-effort. A message whose MIME structure breaks part way
// through yields the headers and whatever was read before it broke, and one
// labelled with a charset or transfer encoding we cannot decode is read as
// raw bytes rather than refused. Anything lost or left undecoded is recorded
// in Message.Warnings, so best-effort never means silent.
func ParseMessage(r io.Reader) (*Message, error) {
	// message.Read rather than mail.CreateReader: both return a usable entity
	// when the charset or the transfer encoding is one they cannot decode, but
	// CreateReader throws that entity away for an unknown encoding and reports
	// only the error. Taking the error at face value there loses the whole
	// message, headers included, over one bad label.
	e, err := message.Read(r)
	if e == nil {
		return nil, err
	}

	msg := &Message{}
	if err != nil {
		msg.Warnings = append(msg.Warnings, fmt.Sprintf("message read without decoding: %v", err))
	}

	mr := mail.NewReader(e)
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
	// Only present on drafts and Sent copies, which are composed with it so the
	// blind recipients survive a save-and-send round trip.
	if bcc, err := h.AddressList("Bcc"); err == nil {
		for _, a := range bcc {
			msg.Bcc = append(msg.Bcc, Address{Name: a.Name, Address: a.Address})
		}
	}
	if cc, err := h.AddressList("Cc"); err == nil {
		for _, a := range cc {
			msg.Cc = append(msg.Cc, Address{Name: a.Name, Address: a.Address})
		}
	}
	// Where the sender wants answers, which is not always where the message
	// came from: mailing lists and send-as setups both rely on this.
	if replyTo, err := h.AddressList("Reply-To"); err == nil {
		for _, a := range replyTo {
			msg.ReplyTo = append(msg.ReplyTo, Address{Name: a.Name, Address: a.Address})
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

	badParts := 0
	for {
		p, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if p == nil {
			// Nothing to salvage. Skip it, but stop once the failures stop
			// being occasional: a broken body repeats one error indefinitely
			// without ever advancing.
			badParts++
			msg.Warnings = append(msg.Warnings, fmt.Sprintf("skipped an unreadable part: %v", err))
			if badParts >= maxConsecutiveBadParts {
				msg.Warnings = append(msg.Warnings,
					fmt.Sprintf("stopped reading after %d unreadable parts in a row", badParts))
				break
			}
			continue
		}
		badParts = 0
		if err != nil {
			// A part the reader could not decode but handed over regardless.
			// The bytes are undecoded rather than absent, so they are worth
			// keeping; the warning is what says they may not read as text.
			msg.Warnings = append(msg.Warnings, fmt.Sprintf("part read without decoding: %v", err))
		}

		switch h := p.Header.(type) {
		case *mail.InlineHeader:
			ct, _, _ := h.ContentType()
			body, readErr := io.ReadAll(p.Body)
			if readErr != nil {
				// io.ReadAll returns what it managed to read alongside the
				// error. Keeping that prefix is the whole difference between
				// a truncated message and an apparently empty one.
				msg.Warnings = append(msg.Warnings, fmt.Sprintf(
					"%s part truncated after %d bytes: %v", ct, len(body), readErr))
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
				// Surfaced rather than withheld. A partial file is sometimes
				// still usable, and one that disappears tells the reader
				// nothing at all; the warning is what says not to trust it.
				msg.Warnings = append(msg.Warnings, fmt.Sprintf(
					"attachment %q truncated after %d bytes: %v", filename, len(data), readErr))
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
