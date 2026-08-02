package mail

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"path/filepath"
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

	parts := newPartReader(e)
	badParts := 0
	for {
		p, err := parts.next()
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
		if p.err != nil {
			// A part that could not be decoded but was handed over regardless.
			// The bytes are undecoded rather than absent, so they are worth
			// keeping; the warning is what says they may not read as text.
			msg.Warnings = append(msg.Warnings, fmt.Sprintf("part read without decoding: %v", p.err))
		}

		contentType, _, _ := p.header.ContentType()
		body, readErr := io.ReadAll(p.body)

		if isInlinePart(p.header) {
			if readErr != nil {
				// io.ReadAll returns what it managed to read alongside the
				// error. Keeping that prefix is the whole difference between
				// a truncated message and an apparently empty one.
				msg.Warnings = append(msg.Warnings, fmt.Sprintf(
					"%s part truncated after %d bytes: %v", contentType, len(body), readErr))
			}
			switch {
			case strings.HasPrefix(contentType, "text/plain"):
				if msg.TextBody == "" {
					msg.TextBody = string(body)
				}
			case strings.HasPrefix(contentType, "text/html"):
				if msg.HTMLBody == "" {
					msg.HTMLBody = string(body)
				}
			}
			continue
		}

		filename := attachmentFilename(p.header)
		if readErr != nil {
			// Surfaced rather than withheld. A partial file is sometimes
			// still usable, and one that disappears tells the reader nothing
			// at all; the warning is what says not to trust it.
			msg.Warnings = append(msg.Warnings, fmt.Sprintf(
				"attachment %q truncated after %d bytes: %v", filename, len(body), readErr))
		}
		msg.Attachments = append(msg.Attachments, Attachment{
			Filename:    filename,
			ContentType: contentType,
			Size:        int64(len(body)),
			Data:        body,
		})
	}

	return msg, nil
}

// messagePart is one leaf of a message: its header, its body, and whatever
// went wrong producing it. A non-nil err means the body is readable but was
// not decoded, which is worth keeping and worth saying.
type messagePart struct {
	header message.Header
	body   io.Reader
	err    error
}

// partReader yields a message's leaf parts.
//
// It exists because mail.Reader.NextPart drops a part whose transfer encoding
// has no decoder: the layer beneath hands over an entity carrying the raw
// bytes, and the mail reader returns nil and the error instead, so the part is
// lost. ParseMessage already goes to some trouble to keep an undecodable whole
// message; a part nested in a multipart deserves the same.
//
// Everything else here is the walk the mail reader does, including its rule
// for telling an inline part from an attachment, which is a few lines and not
// worth losing bytes over.
type partReader struct {
	// single is a message that is not a multipart at all, and so is its own
	// only part. The mail reader wraps one in a synthetic multipart; handing
	// it over directly is the same thing with less ceremony.
	single *message.Entity

	stack []message.MultipartReader
}

// maxPartDepth bounds nesting. Real mail goes three or four deep at most
// (mixed wrapping related wrapping alternative), while a boundary costs a
// dozen bytes, so without a limit a small hostile message could ask for a
// great many levels.
const maxPartDepth = 20

func newPartReader(e *message.Entity) *partReader {
	if mr := e.MultipartReader(); mr != nil {
		return &partReader{stack: []message.MultipartReader{mr}}
	}
	return &partReader{single: e}
}

// next returns the next leaf part, io.EOF when there are none left, or a nil
// part with an error for one that could not be recovered at all.
func (r *partReader) next() (*messagePart, error) {
	if r.single != nil {
		e := r.single
		r.single = nil
		return &messagePart{header: e.Header, body: e.Body}, nil
	}

	for len(r.stack) > 0 {
		mr := r.stack[len(r.stack)-1]

		e, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			r.stack = r.stack[:len(r.stack)-1]
			continue
		}
		if e == nil {
			// No entity to salvage: a boundary the reader could not make
			// sense of, rather than a body it could not decode.
			return nil, err
		}

		if nested := e.MultipartReader(); nested != nil {
			if len(r.stack) >= maxPartDepth {
				// Read past it rather than descending. Skipping the whole
				// subtree loses less than refusing the message.
				io.Copy(io.Discard, e.Body)
				return nil, fmt.Errorf("multipart nested deeper than %d levels", maxPartDepth)
			}
			r.stack = append(r.stack, nested)
			continue
		}
		return &messagePart{header: e.Header, body: e.Body, err: err}, nil
	}
	return nil, io.EOF
}

// isInlinePart reports whether a part is body text rather than an attachment,
// by the rule go-message's mail reader uses: a stated disposition decides it,
// and text with none stated is body.
func isInlinePart(h message.Header) bool {
	contentType, _, _ := h.ContentType()
	disposition, _, _ := h.ContentDisposition()

	return disposition == "inline" ||
		(disposition != "attachment" && strings.HasPrefix(contentType, "text/"))
}

// attachmentFilename names a part, preferring the disposition's filename over
// the content type's name, which RFC 2183 discourages but plenty of mail uses.
func attachmentFilename(h message.Header) string {
	if _, params, err := h.ContentDisposition(); err == nil {
		if filename := params["filename"]; filename != "" {
			return filename
		}
	}
	_, params, _ := h.ContentType()
	return params["name"]
}

// referencesFrom reads the References header out of a fetched header block.
// It exists because the IMAP envelope carries Message-ID and In-Reply-To but
// not References, which is the header that names a message's whole ancestry
// and so the one that reaches the far end of a thread.
func referencesFrom(headerBlock []byte) []string {
	// The block is headers and a blank line, so there is no body to fail on:
	// an error here means the headers themselves were unreadable, and there is
	// nothing to recover.
	e, err := message.Read(bytes.NewReader(headerBlock))
	if e == nil || err != nil {
		return nil
	}

	refs, err := mail.NewReader(e).Header.MsgIDList("References")
	if err != nil {
		return nil
	}
	return refs
}

// contentTypeFromFilename returns a MIME type based on file extension.
func contentTypeFromFilename(filename string) string {
	ct := mime.TypeByExtension(filepath.Ext(filename))
	if ct == "" {
		return "application/octet-stream"
	}
	return ct
}
