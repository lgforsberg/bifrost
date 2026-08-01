# Bifrost

A fast, scriptable command-line email client — and the Go library it's built on.

Bifrost speaks IMAP, SMTP, and MIME against any standards-compliant mail provider.
The CLI is purpose-built for non-interactive and agent-driven use: no TTY assumptions,
no editor spawning, no interactive prompts, and structured `--json` output on every command.

```
bifrost inbox --limit 10
bifrost --json search --unread | jq '.[].subject'
echo "On it." | bifrost reply 42
```

## Features

- **IMAP** — folder management, envelope listing with pagination, message fetch, move/delete/flag, server-side `SEARCH`
- **SMTP** — RFC 2822 composition, MIME multipart with attachments, plus-address (`user+tag@`) envelope handling
- **Threading** — reconstruct conversations across folders via `In-Reply-To`/`References`; reply/forward headers compatible with Gmail and Apple Mail
- **Drafts** — save/list/send/delete, with an optional approval keyword workflow
- **Multi-account** — flexible account matching, per-account folder overrides
- **JSON everywhere** — machine-readable output and structured error codes for scripting and automation
- **Reusable library** — the same engine that powers the CLI is importable as `github.com/lgforsberg/bifrost/mail`

## Install

```bash
go install github.com/lgforsberg/bifrost/cmd/bifrost@latest
```

Or build from source:

```bash
git clone https://github.com/lgforsberg/bifrost
cd bifrost
make build      # produces ./bin/bifrost
```

Requires Go 1.25 or newer.

## Quick start

```bash
# 1. Generate a config template
bifrost config init

# 2. Edit ~/.bifrost/config.json with your account details

# 3. Go
bifrost inbox
```

## Configuration

Config file: `~/.bifrost/config.json` (override with `--config PATH` or the `BIFROST_CONFIG` env var).
Run `bifrost config init` to generate a template.

```json
{
  "defaults": {
    "quoteReplies": true,
    "peekOnRead": false,
    "saveToSent": true,
    "sentFolder": "",
    "draftsFolder": "",
    "trashFolder": "",
    "archiveFolder": ""
  },
  "accounts": [
    {
      "address": "you@example.com",
      "displayName": "Your Name",
      "default": true,
      "imap": { "host": "imap.example.com", "port": 993, "encryption": "tls" },
      "smtp": { "host": "smtp.example.com", "port": 587, "encryption": "starttls" },
      "password": "",
      "passwordFile": "~/.bifrost/pass-you@example.com",
      "sentFolder": "",
      "draftsFolder": "",
      "trashFolder": "",
      "archiveFolder": ""
    }
  ]
}
```

**Default options:**

| Option | Default | Description |
|--------|---------|-------------|
| `quoteReplies` | `true` | Include quoted original in replies |
| `peekOnRead` | `false` | If true, `read` won't mark messages as seen by default |
| `saveToSent` | `true` | Save sent messages to the Sent folder |
| `sentFolder` | `""` | Override Sent folder name (auto-detected via IMAP if empty) |
| `draftsFolder` | `""` | Override Drafts folder name (auto-detected via IMAP if empty) |
| `trashFolder` | `""` | Override Trash folder name (auto-detected via IMAP if empty) |
| `archiveFolder` | `""` | Override Archive folder name (auto-detected via IMAP if empty) |

Leave the folder overrides empty unless you need them. Bifrost otherwise asks the server which folder is which, via the IMAP special-use attributes, which is correct on localized and renamed accounts too.

An override wins over the server's answer. Commands that file mail somewhere (`archive`, `delete`, `draft save`) create the folder if it is missing, just as they do with the conventional name, so an account can be configured before the folders exist. Filing the copy in Sent is the exception: it creates nothing, and a missing folder is reported as a warning on an otherwise successful send.

**Account fields:**

| Field | Required | Description |
|-------|----------|-------------|
| `address` | Yes | Email address (also used as IMAP/SMTP username) |
| `displayName` | No | Display name for the From header |
| `default` | No | Mark this account as the default |
| `imap.host` | Yes | IMAP server hostname |
| `imap.port` | No | IMAP port (default: 993) |
| `imap.encryption` | No | `tls` (default), `starttls`, or `none` |
| `smtp.host` | Yes | SMTP server hostname |
| `smtp.port` | No | SMTP port (default: 587) |
| `smtp.encryption` | No | `starttls` (default), `tls`, or `none` |
| `password` | * | Inline password |
| `passwordFile` | * | Path to a file containing the password (tilde-expanded) |
| `sentFolder` | No | Override this account's Sent folder |
| `draftsFolder` | No | Override this account's Drafts folder |
| `trashFolder` | No | Override this account's Trash folder |
| `archiveFolder` | No | Override this account's Archive folder |

\* One of `password` or `passwordFile` is required. A `passwordFile` keeps the secret out of the config.

The four folder overrides can be set per account as well as in `defaults`. An account's own value wins, and anything it leaves out falls back to the default, which matters when two accounts are on providers that name their folders differently.

## CLI reference

### Global options

Global flags **must** appear before the command name (`config init` also accepts `--config` after it):

```
bifrost [global options] <command> [command options] [arguments]
```

| Flag | Description |
|------|-------------|
| `--account NAME` | Use a specific account (case-insensitive partial match) |
| `--json` | All output as JSON to stdout (camelCase fields) |
| `--verbose` | Debug logging to stderr |
| `--config PATH` | Config file path (default: `~/.bifrost/config.json`, env: `BIFROST_CONFIG`) |

### Exit codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | Operational error (network, IMAP, SMTP, not found, etc.) |
| `2` | Usage error (bad flags, missing arguments, invalid config) |

### Error format

With `--json`, errors are returned as structured JSON to stdout:

```json
{"error": true, "code": "NOT_FOUND", "message": "message uid 999 in INBOX: not found"}
```

Without `--json`, errors print to stderr as `error: <message>`.

| Code | Meaning |
|------|---------|
| `NOT_FOUND` | Message UID or resource doesn't exist |
| `ALREADY_EXISTS` | Folder already exists |
| `AUTH_FAILED` | IMAP/SMTP authentication failed |
| `CONNECTION_FAILED` | Could not connect to server |
| `SEND_REJECTED` | SMTP server rejected the message |
| `CONFIG_ERROR` | Invalid or missing configuration |
| `USAGE_ERROR` | Bad command-line usage (exit code 2) |
| `INTERRUPTED` | Aborted by SIGINT before completing |
| `UNKNOWN` | Unclassified operational error |

### Commands

| Command | Description |
|---------|-------------|
| `inbox` | List messages in a folder |
| `read` | Read a message by UID |
| `search` | Search messages (server-side IMAP SEARCH) |
| `thread` | View a conversation thread |
| `send` | Compose and send a message |
| `reply` | Reply to a message |
| `forward` | Forward a message |
| `delete` | Delete messages |
| `archive` | Archive messages |
| `move` | Move messages to another folder |
| `mark-read` / `mark-unread` | Change read state |
| `folder` | Manage folders (list, create, rename, delete) |
| `accounts` | List configured accounts |
| `draft` | Manage drafts (save, list, send, delete) |
| `config` | Configuration management (init) |
| `version` / `help` | Version and usage |

#### `inbox` — List messages

```
bifrost inbox [--folder FOLDER] [--limit N] [--offset N]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--folder` | `INBOX` | Folder to list |
| `--limit` | `20` | Max messages to return |
| `--offset` | `0` | Skip N newest messages (for pagination) |

JSON output: array of envelope objects.

#### `read` — Read a message

```
bifrost read [--folder FOLDER] [--peek] [--with-attachment-data] [--save-attachments DIR] <uid>
```

| Flag | Default | Description |
|------|---------|-------------|
| `--folder` | `INBOX` | Folder containing the message |
| `--peek` | `false` | Don't mark the message as read |
| `--with-attachment-data` | `false` | Include attachment bytes in JSON output |
| `--save-attachments` | — | Save attachments to the given directory |

By default `read` marks the message seen. Use `--peek` (or `peekOnRead: true`) to read without side effects.

Attachment bytes are left out of the JSON unless you ask for them: base64 makes a single PDF larger than the message carrying it. Each attachment's `filename`, `contentType` and `size` are always reported, so you can see what is there and then use `--save-attachments` to write the files, or `--with-attachment-data` to inline them. (`--no-attachments` still works and is now redundant.)

`--save-attachments` reduces each sender-supplied filename to a bare file name, so an attachment can never be written outside the given directory. Colliding names are suffixed rather than overwritten. Files are written mode 0600 into a directory created 0700.

#### `search` — Server-side IMAP search

```
bifrost search [--folder FOLDER] [--from X] [--to X] [--subject X] [--body X]
               [--since YYYY-MM-DD] [--before YYYY-MM-DD] [--unread] [--flagged] [--limit N]
```

At least one search criterion is required.

| Flag | Default | Description |
|------|---------|-------------|
| `--folder` | `INBOX` | Folder to search |
| `--from` / `--to` / `--subject` / `--body` | — | Header and full-text matches |
| `--since` / `--before` | — | Date range |
| `--unread` | `false` | Only unseen messages |
| `--flagged` | `false` | Only flagged messages |
| `--limit` | `50` | Max results |

#### `thread` — View conversation thread

```
bifrost thread [--folders FOLDER1,FOLDER2,...] [--with-attachment-data] <uid>
```

Reconstructs a conversation by following `References`/`In-Reply-To` across the given folders (default `INBOX,Sent`). JSON output is chronological, each message tagged with its `folder`. Attachment bytes are excluded by default, as in `read`.

#### `send` — Compose and send

```
bifrost send --to ADDR [--to ADDR...] --subject TEXT [--cc ADDR...] [--bcc ADDR...]
             [--from ADDR] [--body TEXT | --body-file PATH] [--attach PATH...] [--no-save]
```

| Flag | Required | Description |
|------|----------|-------------|
| `--to` | Yes | Recipient (repeatable) |
| `--subject` | Yes | Message subject |
| `--cc` / `--bcc` | No | Additional recipients (repeatable) |
| `--from` | No | Override From address (e.g. `user+tag@domain`) |
| `--body` / `--body-file` | No | Message body (see [Body input](#body-input)) |
| `--attach` | No | Attachment file path (repeatable) |
| `--no-save` | No | Don't save a copy to Sent |

JSON output: `{"status": "sent", "messageId": "uuid@hostname"}`.

`reply`, `forward` and `draft send` report the same shape. When a step after delivery fails, such as filing the copy in Sent or removing a sent draft, the result gains a `warnings` array describing each one. The status stays `sent` and the exit code stays 0, because the message did go out and retrying would send it twice. In table mode those warnings go to stderr, leaving stdout clean.

#### `reply` — Reply to a message

```
bifrost reply [--folder FOLDER] [--all] [--from ADDR] [--no-quote] [--no-save]
              [--body TEXT | --body-file PATH] <uid>
```

Automatically sets `In-Reply-To`/`References`. Quotes the original by default (`quoteReplies`). `--all` replies to all recipients, never addressing the same person twice or copying your own account.

Replies go to the original's `Reply-To` when it has one, falling back to `From`. That is what puts a reply to a mailing list back on the list rather than in the poster's personal mailbox.

#### `forward` — Forward a message

```
bifrost forward --to ADDR [--to ADDR...] [--folder FOLDER] [--from ADDR] [--no-save]
                [--body TEXT | --body-file PATH] [--attach PATH...] <uid>
```

Original attachments are included automatically; `--attach` appends more.

#### `delete` / `archive` / `move` / `mark-read` / `mark-unread`

```
bifrost delete       [--folder FOLDER] [--permanent] <uid> [uid...]
bifrost archive      [--folder FOLDER] <uid> [uid...]
bifrost move --to FOLDER [--folder FOLDER | --from FOLDER] <uid> [uid...]
bifrost mark-read    [--folder FOLDER] <uid> [uid...]
bifrost mark-unread  [--folder FOLDER] <uid> [uid...]
```

All are batch operations. JSON output reports `uids` acted on and `skippedUids` for any that didn't exist:

```json
{"status": "deleted", "uids": [42, 43], "skippedUids": [99999]}
```

`delete` moves messages to Trash, where they can be recovered until the trash is emptied. `--permanent` expunges them instead, which cannot be undone. Deleting from Trash itself expunges, since there is nowhere further to move to. The JSON result reports `permanent` and, when the messages were moved, `movedTo`.

`archive` moves messages to whichever folder the server advertises as `\Archive`, so it follows a renamed or localized archive folder. If the server advertises none, it falls back to a folder named `Archive` or `Archives`, creating `Archive` if neither exists.

#### `folder` — Manage folders

```
bifrost folder list
bifrost folder create <name>
bifrost folder rename <old> <new>
bifrost folder delete <name>
```

Semantic errors: `ALREADY_EXISTS` on duplicate create, `NOT_FOUND` on rename/delete of a missing folder.

#### `accounts` — List configured accounts

```
bifrost accounts
```

JSON output: `[{"address": "...", "displayName": "...", "default": true, "imapHost": "...", "smtpHost": "..."}]`.

#### `draft` — Manage drafts

```
bifrost draft save [--to ADDR...] [--cc ADDR...] [--bcc ADDR...] [--subject TEXT]
                   [--from ADDR] [--approval] [--body TEXT | --body-file PATH] [--attach PATH...]
bifrost draft list [--limit N] [--offset N]
bifrost draft send <uid>
bifrost draft delete [--permanent] <uid>
```

`save` returns the server-assigned UID (via UIDPLUS). `--approval` tags the draft with the `$PendingApproval` IMAP keyword.

`draft delete` moves the draft to Trash, matching `delete`; `--permanent` expunges it. A draft removed by `draft send` is expunged either way, since a copy of it is already in Sent.

#### `config` — Configuration management

```
bifrost config init [--config PATH]
```

Creates a template config. Fails if one already exists.

### Body input

The `send`, `reply`, `forward`, and `draft save` commands accept a body via three sources, in order of precedence:

| Source | How | Notes |
|--------|-----|-------|
| `--body TEXT` | Inline text | `--body ""` sends an empty body; `--body -` reads from stdin |
| `--body-file PATH` | Read from file | |
| Stdin pipe | `echo "text" \| bifrost send ...` | Used automatically when stdin is not a terminal |

Precedence: `--body` > `--body-file` > stdin. If none is available and stdin is a terminal, the command returns a usage error (it never hangs waiting for input).

### Multi-account

Accounts are resolved by `--account`:

1. Exact match (case-insensitive, plus-tag normalized): `--account you@example.com`
2. Partial match (substring): `--account alice` → `alice@example.com`
3. Ambiguity detection: multiple matches return an error listing them

Without `--account`, the account marked `"default": true` is used, falling back to the first account.

## Library

The engine is importable on its own:

```go
import "github.com/lgforsberg/bifrost/mail"
```

See [`mail/README.md`](mail/README.md) for the full API reference. Minimal example:

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

	envelopes, _ := client.ListEnvelopes(ctx, "INBOX", 10, 0)
	msg, _ := client.FetchMessage(ctx, "INBOX", envelopes[0].UID, true)

	opts := mail.BuildReply(account, msg, "Thanks!", false, true)
	res, err := mail.Send(ctx, account, client, opts, true, logger)
	if err != nil {
		panic(err)
	}
	// The message is delivered by this point. Anything in res.Warnings is a
	// follow-up step that failed, such as filing the copy in Sent.
	for _, w := range res.Warnings {
		logger.Warn(w)
	}
}
```

## Project layout

```
bifrost/
├── cmd/bifrost/     CLI entrypoint (package main)
├── mail/            public IMAP/SMTP/MIME library (package mail)
└── internal/        CLI internals (commands, config, helpers, output)
```

## Dependencies

Bifrost builds on the `emersion` mail libraries (`go-imap`, `go-smtp`,
`go-message`, `go-sasl`), plus `google/uuid` and `golang.org/x/term`. Nothing
else, and no C dependencies.

Note that `go-imap/v2` is pinned at a **beta** release. It is the only
maintained IMAP v2 client for Go and the API has been stable in practice, but
the version is deliberate rather than incidental: expect churn if you vendor
it, and see [`TASKS.md`](TASKS.md) T-028 for moving off the beta once upstream
tags a stable v2.

## Development

```bash
make build     # build ./bin/bifrost
make test      # run tests
make fmt       # gofmt -w
make vet       # go vet
make install   # go install ./cmd/bifrost
```

## Documentation

| Doc | Purpose |
|-----|---------|
| [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) | How the code fits together, request lifecycle, design decisions |
| [`CONTRIBUTING.md`](CONTRIBUTING.md) | Dev workflow and step-by-step recipes for adding commands and library methods |
| [`AGENTS.md`](AGENTS.md) | Fast orientation and the invariants to preserve (for humans and AI agents) |
| [`mail/README.md`](mail/README.md) | Full `mail` library API reference |
| [`CHANGELOG.md`](CHANGELOG.md) | Release history |

## Scripting examples

```bash
# Process unread messages
unread=$(bifrost --json search --unread --limit 10)
bifrost --json reply --body "Thanks, I'll handle this." 42
bifrost --json archive 42

# Compose with a piped body
echo "Weekly report attached." | bifrost --json send --to team@example.com --subject "Weekly Report"

# Draft workflow
result=$(bifrost --json draft save --to boss@example.com --subject "Proposal" --body "Draft.")
uid=$(echo "$result" | jq -r '.uid')
bifrost --json draft send "$uid"
```

## License

MIT — see [LICENSE](LICENSE).
