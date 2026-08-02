package mail

import (
	"fmt"
	"strings"
	"time"
)

type Address struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}

func (a Address) String() string {
	if a.Name == "" {
		return a.Address
	}
	return fmt.Sprintf("%s <%s>", a.Name, a.Address)
}

type AccountConfig struct {
	Address        string
	DisplayName    string
	IMAPHost       string
	IMAPPort       int
	IMAPEncryption string // "tls", "starttls", "none"
	SMTPHost       string
	SMTPPort       int
	SMTPEncryption string
	Username       string // defaults to Address if empty
	Password       string

	// Special folder overrides. Empty means "ask the server", which is the
	// right answer almost always; these exist for accounts where the server
	// advertises nothing and the conventional names do not fit either.
	SentFolder    string
	DraftsFolder  string
	TrashFolder   string
	ArchiveFolder string
}

// SpecialFolderOverride returns the folder configured for a special-use
// attribute, or "" when the account leaves the choice to the server.
func (a *AccountConfig) SpecialFolderOverride(attr string) string {
	switch strings.ToLower(attr) {
	case "\\sent":
		return a.SentFolder
	case "\\drafts":
		return a.DraftsFolder
	case "\\trash":
		return a.TrashFolder
	case "\\archive":
		return a.ArchiveFolder
	}
	return ""
}

func (a *AccountConfig) EffectiveUsername() string {
	if a.Username != "" {
		return a.Username
	}
	return a.Address
}

type Envelope struct {
	UID     uint32    `json:"uid"`
	Subject string    `json:"subject"`
	From    Address   `json:"from"`
	To      []Address `json:"to"`
	Cc      []Address `json:"cc,omitempty"`
	Date    time.Time `json:"date"`
	Flags   []string  `json:"flags"`
	Size    uint32    `json:"size"`
}

// FolderStatus is what IMAP STATUS reports about a mailbox: how much is in it,
// without selecting it or fetching a single envelope.
//
// The counts are pointers because STATUS items are optional and a server need
// not return one that was asked for. Nil means the server did not say, which
// is not the same as none, and flattening the two would turn silence into a
// confident zero.
type FolderStatus struct {
	Name        string  `json:"name"`
	Total       *uint32 `json:"total,omitempty"`
	Unseen      *uint32 `json:"unseen,omitempty"`
	UIDNext     uint32  `json:"uidNext,omitempty"`
	UIDValidity uint32  `json:"uidValidity,omitempty"`
}

// EnvelopePage is a window onto a larger set: the envelopes that were asked
// for, and how many there were before the limit was applied. Total is the
// point of it, since a bare page cannot tell a caller whether it is looking at
// everything or at the first twenty of nine hundred.
//
// For a folder listing Total is how many messages the folder holds; for a
// search it is how many matched. Both are free, being already known by the
// time the page is built.
type EnvelopePage struct {
	Total    uint32     `json:"total"`
	Limit    int        `json:"limit"`
	Offset   int        `json:"offset"`
	Messages []Envelope `json:"messages"`
}

type Message struct {
	Envelope
	// Bcc sits here rather than on Envelope because it is only ever known from
	// the message source, and only on copies we wrote ourselves: FetchMessage
	// replaces the embedded Envelope with the server's own.
	Bcc []Address `json:"bcc,omitempty"`
	// ReplyTo is likewise read from the source, since the server's envelope
	// does not carry it.
	ReplyTo     []Address    `json:"replyTo,omitempty"`
	TextBody    string       `json:"textBody"`
	HTMLBody    string       `json:"htmlBody,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
	MessageID   string       `json:"messageId"`
	InReplyTo   string       `json:"inReplyTo,omitempty"`
	References  []string     `json:"references,omitempty"`
	Folder      string       `json:"folder,omitempty"`
	// Warnings records what did not survive parsing intact: a body that
	// stopped part way, a part read without being decoded, a part skipped
	// altogether. Empty for the overwhelming majority of mail. When it is not,
	// the message is still worth reading, but anything acting on the body
	// should know it may be incomplete before it replies or files it away.
	Warnings []string `json:"warnings,omitempty"`
}

// SendResult reports the outcome of a delivery. Handing the message to the
// server either works or returns an error, but the steps that follow (filing a
// copy in Sent, removing the draft that was just sent) can fail on their own
// without the message being un-sent. Those are reported as warnings rather
// than errors, because failing the call would invite a retry and a duplicate.
type SendResult struct {
	MessageID string   `json:"messageId"`
	Warnings  []string `json:"warnings,omitempty"`
}

type Folder struct {
	Name       string   `json:"name"`
	Delimiter  string   `json:"delimiter"`
	Attributes []string `json:"attributes,omitempty"`
}

type Attachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
	Data        []byte `json:"data,omitempty"`
}

type SendOptions struct {
	From        Address
	To          []Address
	Cc          []Address
	Bcc         []Address
	Subject     string
	TextBody    string
	HTMLBody    string
	Attachments []Attachment
	InReplyTo   string
	References  []string
	MessageID   string // If set, used as the Message-ID header; otherwise auto-generated
}

// KeywordPendingApproval marks a draft that should not go out until someone
// has looked at it. The CLI refuses to send a draft carrying it, and callers
// of SendDraft can apply the same gate with HasKeyword.
const KeywordPendingApproval = "$PendingApproval"

// HasKeyword reports whether a message carries a keyword. IMAP keywords are
// case insensitive, and servers do vary in what they hand back.
func HasKeyword(flags []string, keyword string) bool {
	for _, f := range flags {
		if strings.EqualFold(f, keyword) {
			return true
		}
	}
	return false
}

type SearchCriteria struct {
	From     string
	To       string
	Subject  string
	Body     string
	Since    *time.Time
	Before   *time.Time
	Unseen   bool
	Flagged  bool
	Keywords []string // IMAP keywords (e.g. "$PendingApproval")
	Limit    int
}

type ErrorResponse struct {
	Error   bool   `json:"error"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// NormalizeAddress strips a plus-address tag: "user+tag@domain" -> "user@domain".
// Returns the address unchanged if no tag is present.
func NormalizeAddress(addr string) string {
	local, _, domain := ParsePlusAddress(addr)
	return local + "@" + domain
}

// NormalizeAddressLower is like NormalizeAddress but also lowercases for comparison.
func NormalizeAddressLower(addr string) string {
	return strings.ToLower(NormalizeAddress(addr))
}

// GenerateMessageIDFor creates a Message-ID for mail sent from address, using
// the sender's own domain on the right hand side. That is the domain RFC 5322
// asks for, and unlike a hostname it discloses nothing the recipient did not
// already have.
func GenerateMessageIDFor(address string) string {
	return messageIDFor(address)
}

// GenerateMessageID creates a Message-ID with no sender to derive a domain
// from, so it falls back to a fixed one.
//
// Deprecated: use GenerateMessageIDFor, which produces an ID rooted in the
// sender's domain. This once used the machine's hostname, which leaked the
// name of the sending host into a header every recipient keeps.
func GenerateMessageID() string {
	return messageIDFor("")
}

// ParsePlusAddress splits an address into local part, tag, and domain.
// For "user+tag@domain" returns ("user", "tag", "domain").
// For "user@domain" returns ("user", "", "domain").
func ParsePlusAddress(addr string) (local, tag, domain string) {
	at := strings.LastIndex(addr, "@")
	if at < 0 {
		return addr, "", ""
	}
	domain = addr[at+1:]
	localFull := addr[:at]
	plus := strings.Index(localFull, "+")
	if plus < 0 {
		return localFull, "", domain
	}
	return localFull[:plus], localFull[plus+1:], domain
}
