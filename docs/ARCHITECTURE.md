# Architecture

Bifrost is one Go module with two consumers of a shared core:

```
        cmd/bifrost   (the CLI binary)
              │
        internal/     (CLI-only glue: commands, config, helpers, output)
              │
          mail/       (public library: IMAP + SMTP + MIME + threading)
              │
   go-imap/v2 · go-message · go-smtp · go-sasl · google/uuid
```

The `mail` package knows nothing about the CLI. Everything CLI-specific
(flag parsing, config files, table rendering, exit codes) lives under
`internal/`. This split is what lets `mail` be imported cleanly by other
projects.

## Request lifecycle (a command run)

1. **`cmd/bifrost/main.go`** parses *global* flags (`--account`, `--json`,
   `--verbose`, `--config`) which must precede the command name, sets up a
   `context.Context` cancelled on `SIGINT`, and builds a `slog.Logger`
   (quiet unless `--verbose`).
2. It loads config (except for `config` and `version`/`help`, which don't
   need it) into a `cmdutil.GlobalFlags`, then dispatches on the command name
   to a function in `internal/commands`.
3. The command parses its *own* flags with a `flag.FlagSet`, wrapping parse
   errors as `fmt.Errorf("usage: %w", err)` so they become exit code 2.
4. For IMAP work it calls `helpers.ConnectIMAP`, which resolves the account
   and returns a connected `*mail.IMAPClient` (the command `defer`s
   `Close()`).
5. It calls into `package mail`, then emits results: `output.PrintJSON` when
   `g.JSON`, otherwise `output.PrintTable` / plain text.
6. Any returned error bubbles back to `main.handleError`, which maps `mail`
   sentinel errors to a stable `code`, prints structured JSON (if `--json`)
   or `error: <msg>` to stderr, and exits.

## Key types and helpers

| Symbol | Location | Role |
|--------|----------|------|
| `cmdutil.GlobalFlags` | `internal/cmdutil` | Carries ctx, logger, loaded config, and output mode into every command |
| `config.Config` / `config.Load` | `internal/config` | Parses JSON config, applies defaults, resolves password files |
| `config.AccountByAddress` / `DefaultAccount` | `internal/config` | Account matching (exact → substring → ambiguity error) |
| `helpers.ConnectIMAP` | `internal/helpers` | Resolve account + connect IMAP in one call |
| `helpers.ValidateUIDs` | `internal/helpers` | For bulk ops: splits requested UIDs into `Existing` / `Skipped`, errors if none exist |
| `output.PrintJSON` / `PrintTable` | `internal/output` | The two output renderers |

## The `mail` package

One responsibility per file:

| File | Contents |
|------|----------|
| `types.go` | `Address`, `AccountConfig`, `Envelope`, `Message`, `Folder`, `Attachment`, `SendOptions`, `SearchCriteria`, `ErrorResponse` |
| `errors.go` | Sentinel errors (`ErrNotFound`, `ErrAuthFailed`, …) matched via `errors.Is` |
| `imap.go` | `IMAPClient`: connect, list, fetch, search, flags, move/delete, folder ops |
| `smtp.go` | `SmtpDeliver` — send a pre-composed RFC 2822 message |
| `mime.go` | `ParseMessage` / `ComposeMessage` — MIME multipart in and out |
| `compose.go` | Message building helpers used by compose/reply/forward |
| `operations.go` | High-level combos: `Send`, `BuildReply`, `BuildForward`, `Archive`, `SaveDraft`, `SendDraft` |
| `thread.go` | Thread reconstruction + reply/forward subject and header utilities |
| `folder.go` | Folder listing/creation/lookup, special-use detection |

See [`mail/README.md`](../mail/README.md) for the full public API reference.

## Design decisions

- **Non-interactive by contract.** Body input is `--body` / `--body-file` /
  piped stdin, in that precedence. If none is available and stdin is a TTY, the
  command errors rather than hanging. This makes the tool safe for scripts and
  agents.
- **Structured errors over strings.** Operational failures wrap a `mail`
  sentinel so callers get a stable `code` (e.g. `NOT_FOUND`) regardless of the
  underlying message. Usage errors are signalled by a `usage:` prefix → exit 2.
- **Bulk ops are forgiving.** Delete/move/mark accept multiple UIDs and report
  `skippedUids` for any that didn't exist instead of failing the whole batch.
- **Special folders are discovered, not assumed.** Sent/Drafts/Archive are
  resolved via IMAP special-use attributes (with config overrides) rather than
  hardcoded names.
