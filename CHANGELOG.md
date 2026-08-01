# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.4.1] - 2026-08-01

### Fixed

- SMTP failures are classified by status code instead of by searching the
  message for substrings. A refusal worded `550 Not authorized to send as this
  address` was reported as `AUTH_FAILED`, sending the caller to check
  credentials that were never the problem; it now reports `SEND_REJECTED`. A
  4xx reply asks for a retry and is no longer reported as a permanent refusal,
  and a connection that breaks mid-exchange is no longer reported as a refusal
  by a server that never answered.
- A login or greeting that fails because the connection broke reports
  `CONNECTION_FAILED` rather than `AUTH_FAILED`.
- IMAP folder errors read the `ALREADYEXISTS` and `NONEXISTENT` response codes
  off the typed error rather than matching them in the rendered string, so the
  result no longer depends on how the library formats one. The wording fallback
  stays, because plenty of servers answer with a bare `NO` and no code.
- A `QUIT` that fails after the server accepted the message is no longer
  reported as a send failure. The message is delivered by that point, and
  reporting a failure invited a retry that would deliver it twice.

## [1.4.0] - 2026-08-01

### Added

- `send`, `reply`, `forward` and `draft send` report a `warnings` array when a
  step after delivery fails, such as filing the copy in Sent or removing a sent
  draft. Previously those were logged at debug level, so the command reported
  clean success when no Sent copy had been written. The status stays `sent` and
  the exit code stays 0, because the message did go out and a retry would send
  it twice. In table mode the warnings go to stderr.
- `draft send --json` now reports the `messageId` it delivered with, matching
  the other send commands.

### Changed

- **Library callers:** `mail.Send` and `mail.SendDraft` return a `SendResult`
  alongside the error, carrying the Message-ID and any warnings. Both used to
  return only an error.

## [1.3.0] - 2026-08-01

### Added

- `read` reports `bcc` for the messages that carry the header, meaning drafts
  and Sent copies. `mail.Message` gained the matching field, kept off `Envelope`
  because a fetch replaces that with the server's version.

### Fixed

- `draft send` no longer drops blind recipients. The stored draft is parsed and
  re-composed before delivery, but parsing never read the `Bcc` header, so the
  blind copies were discarded while the command reported success.

## [1.2.0] - 2026-08-01

### Added

- `INTERRUPTED` error code. SIGINT now aborts an in-flight command promptly and
  says so, instead of being ignored until the operation finished on its own.

### Fixed

- Network operations can no longer block indefinitely. Neither client library
  bounds an individual round trip, so a server that accepted a connection and
  then stopped responding held bifrost open for as long as the process ran.
  Every read and write now carries a 60 second idle deadline, and the 30 second
  dial timeout is set explicitly rather than left to a library default.
- The `ctx` argument threaded through the library is honoured. Cancelling it
  closes the connection, which is the only way to interrupt the IMAP and SMTP
  commands, since neither takes a context.
- `smtp.encryption: "none"` means no encryption. It was routed through the
  library's STARTTLS helper, so a plaintext-only relay could not be reached.

## [1.1.4] - 2026-08-01

### Security

- `read --save-attachments` can no longer write outside the target directory.
  Attachment names come from the sender and were joined to the directory
  unchanged, so a name such as `../../.ssh/authorized_keys` escaped it. Names
  are now reduced to a bare file name, and files are written mode 0600 into a
  directory created 0700 instead of being world-readable.

### Fixed

- Attachments whose names collide no longer overwrite each other. Reducing
  sender names to a bare file name makes collisions more likely, since distinct
  paths can share a base name, so each file now gets a free name.

## [1.1.3] - 2026-08-01

### Fixed

- Mail in a non-UTF-8 charset is readable again. Only UTF-8 and US-ASCII
  decoders were registered, so an `iso-8859-1` or `windows-1252` message failed
  to parse outright when single-part and silently produced an empty body when
  multipart. Every charset `go-message` supports is now registered.

### Security

- Raised `golang.org/x/text` to v0.40.0 and `golang.org/x/sys` to v0.47.0,
  clearing GO-2026-5970 (an infinite loop on invalid input, newly relevant now
  that untrusted message bytes reach the charset decoders) and GO-2026-5024.

## [1.1.2] - 2026-08-01

### Fixed

- A malformed message no longer hangs the client. `ParseMessage` skipped failing
  MIME parts without any bound, and a truncated or broken body makes the reader
  report the same failure on every call, so a single received message could stall
  `read`, `thread`, and `draft send` indefinitely. Consecutive part failures are
  now capped, and parsing returns the headers and the parts recovered so far.

## [1.1.1] - 2026-08-01

### Security

- Blind-copied recipients are no longer disclosed. `ComposeMessage` wrote a
  `Bcc` header into the delivered message, so every `To` and `Cc` recipient
  could read the full blind-copy list. Blind recipients now travel in the SMTP
  envelope only; the copies filed in Sent and Drafts still carry the header so
  the sender keeps a record.

## [1.1.0] - 2026-07-31

Initial public release of Bifrost as a standalone repository.

### Added

- `bifrost` CLI with commands: `inbox`, `read`, `search`, `thread`, `send`,
  `reply`, `forward`, `delete`, `archive`, `move`, `mark-read`, `mark-unread`,
  `folder`, `accounts`, `draft`, `config`, `version`, `help`.
- `--json` structured output and stable error codes on every command.
- Multi-account config at `~/.bifrost/config.json` with flexible account
  matching and password-file support.
- Reusable `mail` library: IMAP client, SMTP delivery, MIME parse/compose,
  thread reconstruction, and plus-address utilities.
- Documentation: `README.md`, `mail/README.md`, `docs/ARCHITECTURE.md`,
  `CONTRIBUTING.md`, and `AGENTS.md`.

[Unreleased]: https://github.com/lgforsberg/bifrost/compare/v1.4.1...HEAD
[1.4.1]: https://github.com/lgforsberg/bifrost/compare/v1.4.0...v1.4.1
[1.4.0]: https://github.com/lgforsberg/bifrost/compare/v1.3.0...v1.4.0
[1.3.0]: https://github.com/lgforsberg/bifrost/compare/v1.2.0...v1.3.0
[1.2.0]: https://github.com/lgforsberg/bifrost/compare/v1.1.4...v1.2.0
[1.1.4]: https://github.com/lgforsberg/bifrost/compare/v1.1.3...v1.1.4
[1.1.3]: https://github.com/lgforsberg/bifrost/compare/v1.1.2...v1.1.3
[1.1.2]: https://github.com/lgforsberg/bifrost/compare/v1.1.1...v1.1.2
[1.1.1]: https://github.com/lgforsberg/bifrost/compare/v1.1.0...v1.1.1
[1.1.0]: https://github.com/lgforsberg/bifrost/releases/tag/v1.1.0
