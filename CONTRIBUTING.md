# Contributing

Thanks for working on Bifrost. This guide covers the dev workflow and the
common extension points. For the mental model, read
[`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) first.

## Prerequisites

- Go 1.25 or newer.

## Workflow

```bash
make build     # build ./bin/bifrost
make test      # go test ./...
make fmt       # gofmt -w .
make vet       # go vet ./...
make tidy      # go mod tidy
```

Before every commit, all of these must be clean:

```bash
make fmt && make vet && make test && make build
```

`go mod tidy` should produce no diff.

## Adding a CLI command

Commands follow a consistent shape — copy `internal/commands/inbox.go` as a
template.

1. **Create `internal/commands/<name>.go`** in `package commands` with an
   exported function:

   ```go
   func Name(g *cmdutil.GlobalFlags, args []string) error {
       fs := flag.NewFlagSet("name", flag.ContinueOnError)
       folder := fs.String("folder", "INBOX", "folder to act on")
       if err := fs.Parse(args); err != nil {
           return fmt.Errorf("usage: %w", err) // → exit code 2
       }

       client, _, err := helpers.ConnectIMAP(g.Ctx, g.Config, g.Account, g.Logger)
       if err != nil {
           return err
       }
       defer client.Close()

       // ... call into package mail ...

       if g.JSON {
           return output.PrintJSON(os.Stdout, result)
       }
       // ... human-readable output via output.PrintTable / fmt ...
       return nil
   }
   ```

2. **Register it** in `cmd/bifrost/main.go`: add a `case` to the dispatch
   switch and a line to `printUsage()`.

3. **Respect the invariants:**
   - Support `--json` (structured output to stdout).
   - Never prompt or block on interactive input.
   - Wrap flag/usage problems with a `usage:` prefix; wrap operational failures
     so they carry a `mail` sentinel error where appropriate.
   - For multi-UID operations, use `helpers.ValidateUIDs` and report
     `skippedUids` (see `bulkResult` in `inbox.go`).

## Adding a library capability

Put reusable IMAP/SMTP/MIME logic in `package mail`, not in `internal/`.

- Choose the file by responsibility (see the table in `docs/ARCHITECTURE.md`).
- Keep it pure: return values and errors, never `os.Exit` or print to stdout.
- Reuse or add a sentinel in `errors.go` so callers can `errors.Is` the result.
- Update [`mail/README.md`](mail/README.md) with the new signature.
- Add a unit test (`*_test.go`) alongside the code.

## Error codes

`main.handleError` maps `mail` sentinels to the CLI's stable error codes and
exit codes. If you introduce a new failure category, add the sentinel in
`mail/errors.go` and the mapping in `main.handleError` together.

| Sentinel | Code | Exit |
|----------|------|------|
| `ErrNotFound` | `NOT_FOUND` | 1 |
| `ErrAlreadyExists` | `ALREADY_EXISTS` | 1 |
| `ErrAuthFailed` | `AUTH_FAILED` | 1 |
| `ErrConnectionFailed` | `CONNECTION_FAILED` | 1 |
| `ErrSendRejected` | `SEND_REJECTED` | 1 |
| `ErrInvalidConfig` | `CONFIG_ERROR` | 2 |
| (`usage:` prefix) | `USAGE_ERROR` | 2 |
| (cancelled context) | `INTERRUPTED` | 1 |

## Testing against a real server

Unit tests run offline. For end-to-end checks, point a config at any IMAP/SMTP
account (a local Dovecot/Postfix or a Docker mail server works well) and drive
the built binary:

```bash
BIFROST_CONFIG=./test-config.json ./bin/bifrost --json inbox --limit 5
```

Keep real credentials out of the repo — `config.json`, `pass-*`, and `*.pass`
are gitignored.

## Commits and versioning

- Keep commits focused; run the full check suite first.
- Record notable changes in [`CHANGELOG.md`](CHANGELOG.md) (Keep a Changelog format).
- Versioning follows [SemVer](https://semver.org/). The version string lives in
  `cmd/bifrost/main.go` (`const version`); bump it and tag releases `vX.Y.Z`.

A release is one commit carrying the change, the version bump and the changelog
entry, tagged at that commit:

```bash
git tag -a v1.2.3 -m "One line on what shipped"
git push origin main --follow-tags
```

Tag as you release, not in a later sweep: the changelog's compare links point at
these tags, and `go get` resolves library versions from them.

Now that `v1.x` tags exist, `package mail` is a published API. A breaking change
to it needs a `/v2` module path, so prefer adding to a result type over changing
a signature.
