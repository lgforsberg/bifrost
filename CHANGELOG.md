# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/lgforsberg/bifrost/compare/v1.1.0...HEAD
[1.1.0]: https://github.com/lgforsberg/bifrost/releases/tag/v1.1.0
