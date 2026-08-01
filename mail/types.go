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

type Message struct {
	Envelope
	// Bcc sits here rather than on Envelope because it is only ever known from
	// the message source, and only on copies we wrote ourselves: FetchMessage
	// replaces the embedded Envelope with the server's own.
	Bcc         []Address    `json:"bcc,omitempty"`
	TextBody    string       `json:"textBody"`
	HTMLBody    string       `json:"htmlBody,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
	MessageID   string       `json:"messageId"`
	InReplyTo   string       `json:"inReplyTo,omitempty"`
	References  []string     `json:"references,omitempty"`
	Folder      string       `json:"folder,omitempty"`
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

// GenerateMessageID creates a globally unique Message-ID for email headers.
func GenerateMessageID() string {
	return generateMessageID()
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
