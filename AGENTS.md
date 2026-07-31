# AGENTS.md

Orientation for anyone — human or AI agent — working on this repository.

## What this is

Bifrost is a command-line email client (`bifrost`) built on a reusable Go
library (`package mail`). It talks IMAP/SMTP/MIME to any standards-compliant
provider. The CLI is deliberately non-interactive: no TTY assumptions, no
editor spawning, no prompts, and `--json` on every command.

## Where things live

| Path | What |
|------|------|
| `cmd/bifrost/main.go` | Entry point: global-flag parsing, command dispatch, error→exit-code mapping, usage text |
| `internal/commands/` | One file per CLI command; each exports `Func(g *cmdutil.GlobalFlags, args []string) error` |
| `internal/config/` | Loads `~/.bifrost/config.json`, resolves accounts, provides the `config init` template |
| `internal/helpers/` | Shared command plumbing: `ConnectIMAP`, `ResolveAccount`, `ValidateUIDs`, body/flag parsing |
| `internal/cmdutil/` | `GlobalFlags` struct (context, logger, config, output mode) |
| `internal/output/` | `PrintJSON` and `PrintTable` — the only two output shapes |
| `mail/` | Public library: IMAP client, SMTP delivery, MIME parse/compose, threading, address utils |

Read [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) for how these fit together
and [`CONTRIBUTING.md`](CONTRIBUTING.md) for the step-by-step recipe to add a
command or library method.

## Invariants (don't break these)

- **Every command supports `--json`.** Machine-readable output goes to stdout via `output.PrintJSON`; human output via `output.PrintTable`.
- **The CLI never blocks on interactive input.** Message bodies come from `--body`, `--body-file`, or piped stdin only (see `internal/helpers`).
- **Errors wrap sentinels from `package mail`** (`mail.ErrNotFound`, etc.) so `main.handleError` can map them to stable JSON error codes and exit codes. A message prefixed `usage:` maps to exit code 2.
- **`package mail` is a pure library.** No `os.Exit`, no printing to stdout, no CLI concerns — it returns values and errors. CLI-only logic belongs in `internal/`.
- **Config stays provider-neutral.** No hardcoded hosts, domains, or credentials. Default path is `~/.bifrost/config.json` (override: `--config`, `BIFROST_CONFIG`).

## Verify your changes

```bash
make fmt && make vet && make test && make build
```

All four must pass before committing. `go mod tidy` should produce no diff.
