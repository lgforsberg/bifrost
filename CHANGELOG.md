# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/lgforsberg/bifrost/compare/v1.1.4...HEAD
[1.1.4]: https://github.com/lgforsberg/bifrost/compare/v1.1.3...v1.1.4
[1.1.3]: https://github.com/lgforsberg/bifrost/compare/v1.1.2...v1.1.3
[1.1.2]: https://github.com/lgforsberg/bifrost/compare/v1.1.1...v1.1.2
[1.1.1]: https://github.com/lgforsberg/bifrost/compare/v1.1.0...v1.1.1
[1.1.0]: https://github.com/lgforsberg/bifrost/releases/tag/v1.1.0
