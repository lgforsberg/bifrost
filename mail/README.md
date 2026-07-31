# mail

Go library for IMAP, SMTP, and MIME operations — the engine behind the Bifrost CLI.

**Import:** `github.com/lgforsberg/bifrost/mail`
**Package:** `mail`

## Dependencies

| Dependency | Purpose |
|-----------|---------|
| `go-imap/v2` | IMAP client protocol |
| `go-message` | MIME parsing and generation |
| `go-smtp` | SMTP delivery |
| `go-sasl` | SASL authentication (PLAIN, LOGIN) |
| `google/uuid` | Message-ID generation |

---

## Data Types

### Address

Email address with optional display name.

```go
type Address struct {
    Name    string `json:"name"`
    Address string `json:"address"`
}

func (a Address) String() string  // "Name <addr>" or just "addr"
```

### AccountConfig

IMAP/SMTP connection credentials.

```go
type AccountConfig struct {
    Address        string
    DisplayName    string
    IMAPHost       string
    IMAPPort       int    // default 993
    IMAPEncryption string // "tls", "starttls", "none"
    SMTPHost       string
    SMTPPort       int    // default 587
    SMTPEncryption string // "starttls", "tls", "none"
    Username       string // defaults to Address if empty
    Password       string
}

func (a *AccountConfig) EffectiveUsername() string
```

### Envelope

Lightweight message metadata from IMAP FETCH (no body content).

```go
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
```

### Message

Full message content. Embeds Envelope (promotes UID, Subject, From, etc.).

```go
type Message struct {
    Envelope
    TextBody    string       `json:"textBody"`
    HTMLBody    string       `json:"htmlBody,omitempty"`
    Attachments []Attachment `json:"attachments,omitempty"`
    MessageID   string       `json:"messageId"`
    InReplyTo   string       `json:"inReplyTo,omitempty"`
    References  []string     `json:"references,omitempty"`
    Folder      string       `json:"folder,omitempty"`
}
```

### Folder

IMAP folder metadata.

```go
type Folder struct {
    Name       string   `json:"name"`
    Delimiter  string   `json:"delimiter"`
    Attributes []string `json:"attributes,omitempty"`
}
```

### Attachment

File attachment — `Data` is nil when fetched with `--no-attachments` or listed via envelope.

```go
type Attachment struct {
    Filename    string `json:"filename"`
    ContentType string `json:"contentType"`
    Size        int64  `json:"size"`
    Data        []byte `json:"data,omitempty"`
}
```

### SendOptions

Compose parameters for outbound messages.

```go
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
    MessageID   string // auto-generated if empty
}
```

### SearchCriteria

Server-side IMAP SEARCH parameters.

```go
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
```

### ErrorResponse

Structured error for JSON output in CLI tools.

```go
type ErrorResponse struct {
    Error   bool   `json:"error"`
    Code    string `json:"code"`
    Message string `json:"message"`
}
```

---

## Sentinel Errors

```go
var ErrNotFound         // message/folder not found
var ErrAlreadyExists    // folder already exists
var ErrAuthFailed       // IMAP/SMTP authentication failed
var ErrConnectionFailed // could not connect to server
var ErrSendRejected     // SMTP server rejected the message
var ErrInvalidConfig    // invalid or missing configuration
```

Use `errors.Is(err, mail.ErrNotFound)` for matching.

---

## IMAP Client

### Construction

```go
func NewIMAPClient(config AccountConfig, logger *slog.Logger) *IMAPClient
```

Creates a client. Call `Connect` before using any methods.

### Connection Lifecycle

```go
func (c *IMAPClient) Connect(ctx context.Context) error
func (c *IMAPClient) Close() error
```

### Listing

```go
func (c *IMAPClient) ListFolders(ctx context.Context) ([]Folder, error)
func (c *IMAPClient) ListEnvelopes(ctx context.Context, folder string, limit, offset int) ([]Envelope, error)
```

`ListEnvelopes` returns newest-first, supporting pagination via `offset`.

### Fetching

```go
func (c *IMAPClient) FetchMessage(ctx context.Context, folder string, uid uint32, peek bool) (*Message, error)
```

When `peek` is true, the `\Seen` flag is not set.

### Search

```go
func (c *IMAPClient) Search(ctx context.Context, folder string, criteria SearchCriteria) ([]Envelope, error)
```

Server-side IMAP SEARCH. Supports text fields, date ranges, flags, and IMAP keywords.

### Threading

```go
func (c *IMAPClient) FetchThread(ctx context.Context, folders []string, uid uint32) ([]Message, error)
```

Reconstructs a conversation by following `In-Reply-To` and `References` headers across multiple folders. Returns messages sorted chronologically.

### Flag Operations

```go
func (c *IMAPClient) MarkRead(ctx context.Context, folder string, uid uint32) error
func (c *IMAPClient) MarkReadBatch(ctx context.Context, folder string, uids []uint32) error
func (c *IMAPClient) MarkUnread(ctx context.Context, folder string, uid uint32) error
func (c *IMAPClient) MarkUnreadBatch(ctx context.Context, folder string, uids []uint32) error
```

### Message Operations

```go
func (c *IMAPClient) DeleteMessage(ctx context.Context, folder string, uid uint32) error
func (c *IMAPClient) DeleteMessages(ctx context.Context, folder string, uids []uint32) error
func (c *IMAPClient) MoveMessage(ctx context.Context, uid uint32, from, to string) error
func (c *IMAPClient) MoveMessages(ctx context.Context, uids []uint32, from, to string) error
func (c *IMAPClient) AppendMessage(ctx context.Context, folder string, message []byte, flags []string) (uint32, error)
func (c *IMAPClient) CheckUIDsExist(ctx context.Context, folder string, uids []uint32) ([]uint32, error)
```

`AppendMessage` returns the server-assigned UID (via UIDPLUS). `CheckUIDsExist` returns the subset of UIDs that exist in the folder.

### Folder Operations

```go
func (c *IMAPClient) CreateFolder(ctx context.Context, name string) error
func (c *IMAPClient) RenameFolder(ctx context.Context, oldName, newName string) error
func (c *IMAPClient) DeleteFolder(ctx context.Context, name string) error
func (c *IMAPClient) FolderExists(ctx context.Context, name string) (bool, error)
func (c *IMAPClient) EnsureFolder(ctx context.Context, name string) error
func (c *IMAPClient) FindSpecialFolder(ctx context.Context, attr string) (string, error)
```

`FindSpecialFolder` looks up server-advertised special-use folders by attribute (e.g. `\\Sent`, `\\Drafts`, `\\Trash`).

---

## SMTP

```go
func SmtpDeliver(ctx context.Context, config AccountConfig, from string, composedMsg []byte, recipients []string, logger *slog.Logger) error
```

Delivers a pre-composed RFC 2822 message via SMTP. The `from` parameter is the envelope sender (may differ from the header From for plus addressing).

---

## MIME

### Parsing

```go
func ParseMessage(r io.Reader) (*Message, error)
```

Parses a raw RFC 2822 message into a `Message`. Walks MIME multipart structure, extracts `text/plain`, `text/html`, and attachments.

### Composing

```go
func ComposeMessage(opts SendOptions) ([]byte, error)
```

Builds a complete RFC 2822 message from `SendOptions`. Generates MIME multipart when attachments are present, sets proper headers (Date, Message-ID, MIME-Version, Content-Type).

---

## High-Level Operations

Convenience functions that combine IMAP and SMTP operations.

```go
func Send(ctx context.Context, account AccountConfig, imap *IMAPClient, opts SendOptions, saveToSent bool, logger *slog.Logger) error
```

Composes, delivers via SMTP, and optionally saves a copy to the Sent folder.

```go
func BuildReply(account AccountConfig, original *Message, body string, replyAll bool, quoteOriginal bool) SendOptions
func BuildForward(account AccountConfig, original *Message, to []Address, body string) SendOptions
```

Pure functions — construct `SendOptions` for reply/forward with correct headers, quoting, and attachments. No I/O. Pass the result to `Send`.

```go
func Archive(ctx context.Context, imap *IMAPClient, folder string, uids []uint32) error
```

Moves messages to the Archive folder, creating it if it doesn't exist.

```go
func SaveDraft(ctx context.Context, imap *IMAPClient, opts SendOptions, keywords ...string) (uint32, error)
func SendDraft(ctx context.Context, account AccountConfig, imap *IMAPClient, uid uint32, logger *slog.Logger) error
```

`SaveDraft` saves to Drafts with `\Draft` and `\Seen` flags, plus optional IMAP keywords (e.g. `$PendingApproval`). Returns server-assigned UID.

`SendDraft` fetches a draft, delivers it, removes it from Drafts, and saves to Sent.

---

## Threading Utilities

```go
func BuildReplyHeaders(original *Message) (inReplyTo string, references []string)
func StripSubjectPrefix(subject string) string
func ReplySubject(original string) string   // "Re: <stripped subject>"
func ForwardSubject(original string) string // "Fwd: <stripped subject>"
func QuoteBody(original *Message) string
```

`BuildReplyHeaders` generates correct `In-Reply-To` and `References` chains for Gmail and Apple Mail threading compatibility.

`QuoteBody` formats the original message as a quoted reply block, falling back to stripped HTML if no plain text body exists.

---

## Address Utilities

```go
func NormalizeAddress(addr string) string       // "user+tag@domain" → "user@domain"
func NormalizeAddressLower(addr string) string  // same, lowercased
func ParsePlusAddress(addr string) (local, tag, domain string)
func GenerateMessageID() string
```

---

## Usage Example

```go
package main

import (
    "context"
    "log/slog"
    "os"

    "github.com/lgforsberg/bifrost/mail"
)

func main() {
    logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

    account := mail.AccountConfig{
        Address:  "you@example.com",
        IMAPHost: "imap.example.com", IMAPPort: 993,
        SMTPHost: "smtp.example.com", SMTPPort: 587,
        Password: "...",
    }

    client := mail.NewIMAPClient(account, logger)
    ctx := context.Background()

    if err := client.Connect(ctx); err != nil {
        panic(err)
    }
    defer client.Close()

    // List recent messages
    envelopes, _ := client.ListEnvelopes(ctx, "INBOX", 10, 0)

    // Read a message without marking it as seen
    msg, _ := client.FetchMessage(ctx, "INBOX", envelopes[0].UID, true)

    // Reply
    opts := mail.BuildReply(account, msg, "Thanks!", false, true)
    mail.Send(ctx, account, client, opts, true, logger)
}
```
