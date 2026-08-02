# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.25.0] - 2026-08-02

### Added

- OAuth2 authentication for IMAP and SMTP, via the `xoauth2` and
  `oauthbearer` SASL mechanisms. Gmail and Microsoft 365 have retired
  password authentication, so those accounts could not be used at all before
  this.
- Accounts set `authMechanism` and `tokenCommand`, where the command prints an
  access token on stdout. Bifrost does not acquire tokens itself: that needs
  provider-specific registration, scopes and a browser consent step, and
  would make Bifrost a secrets store that prompts. A command composes with
  whatever already holds the credentials instead.
- An account authenticates by password or by token, and a config that mixes
  the two is rejected rather than silently resolved.

### Changed

- Authentication failures try to name the right cause, since the failures
  look alike and are fixed in different places: a server that never offered
  the mechanism says so and lists what it does offer, a token helper that
  fails is reported as a helper failure with its own message, and a refused
  token carries the server's explanation, which is where a missing scope is
  named.

## [1.24.1] - 2026-08-02

### Fixed

- An attachment or body part whose transfer encoding has no decoder is now
  kept as raw bytes with a warning, the same as when the encoding labels the
  whole message. Nested in a multipart it was dropped: the MIME reader
  returned nothing for the part, discarding an entity that held the bytes, so
  a file was reported missing rather than undecoded. Parts are now walked one
  layer down, where the bytes survive.

## [1.24.0] - 2026-08-02

### Fixed

- `thread` reached only one hop from the message asked about, so a
  conversation whose members name just their immediate parent, which is what
  a client that truncates `References` leaves behind, was returned with both
  ends missing. Expansion is now iterative: identifiers found on one message
  become the next round's search terms.

### Changed

- Thread discovery reads envelopes and the `References` header instead of
  fetching whole messages. It used to pull each candidate in full,
  attachments and all, once for every search that matched it, which for a
  three-header search over several identifiers meant the same message
  downloaded repeatedly. Full messages are now fetched once each, only for
  confirmed members.
- A thread is capped at 200 messages so a mailing list cannot turn one
  command into an unbounded download.

## [1.23.0] - 2026-08-02

### Added

- The network timeouts are configurable: `timeout` in `defaults` or on an
  account, and `--timeout` for one invocation. A duration such as `"10s"`
  replaces both the 30s dial and the 60s idle deadline, on the reading that
  naming one number means no single wait should exceed it. Left unset, the
  existing defaults are unchanged.

  Shorter suits an agent that would rather fail and retry than wait; longer
  suits a slow link. Zero is rejected rather than read as "wait forever".
- `mail.AccountConfig.Timeout`.

## [1.22.0] - 2026-08-02

### Added

- `draft update <uid>`: replace a draft with a revised one in a single
  command, rather than save-new-then-delete-old by hand. IMAP cannot alter a
  stored message, so the revision gets a new UID, reported alongside the old
  one as `previousUid`.

  A draft awaiting approval stays that way when revised, so a revision cannot
  walk out of the queue the original was put in.

## [1.21.0] - 2026-08-02

### Added

- `search --folder` is repeatable, and `search --all-folders` covers every
  folder the server will let us select. Triage across folders no longer needs
  one invocation per folder. Results are merged by date, and `--limit` and
  `--with-total` apply to the merged set.
- `mail.SearchFoldersPage` and `mail.SelectableFolders`.
- `Envelope.Folder` records where the message is. A UID identifies a message
  only within one mailbox, so a merged result cannot be acted on without it.
  It is set for every listing and search, not just the merged ones.

### Fixed

- `search` on a folder that does not exist now reports `NOT_FOUND`, as `inbox`
  already did, rather than a bare SELECT failure.

## [1.20.0] - 2026-08-02

A batch of small correctness fixes that had accumulated (T-026).

### Fixed

- Outgoing messages no longer disclose the sending machine's hostname. The
  Message-ID now uses the sender's own domain, which is what RFC 5322 asks for
  and what the recipient already knows.
- A global flag given no value was silently ignored, so `bifrost --account
  send ...` sent from the default account instead of complaining. A missing or
  empty value is now a usage error, which also catches `--account "$ACCT"`
  with the variable unset.
- `--flag=value` and single-dash `-flag` now work for the global flags, as
  they already did for every command flag. An unrecognised global option is
  named as such rather than becoming the command name and failing with a
  confusing config error.
- Table output truncated subjects and sender names by bytes, cutting
  multi-byte characters in half and shortening any non-Latin subject to a
  fraction of its column. It now counts characters, and no longer panics on a
  limit below three.
- `Envelope.Size` is clamped rather than wrapped when a server reports a size
  that will not fit in a `uint32`.
- Content type detection used a hand-rolled extension split that returned
  nonsense for a filename with no extension but a dot in its directory.

### Added

- `mail.GenerateMessageIDFor`, which takes the sender's address.
  `mail.GenerateMessageID` is deprecated: it has no sender to derive a domain
  from.
- `version` reports the commit the binary was built from, and whether the tree
  was modified. A version stamped in by the toolchain is preferred over the
  constant, since it cannot drift from the tag.

## [1.19.0] - 2026-08-02

### Added

- `--body-html` and `--body-html-file` on `send`, `reply`, `forward` and
  `draft save`. The library has composed `multipart/alternative` since the
  start; nothing on the CLI ever set `HTMLBody`.
- `mail.QuoteBodyHTML`, the HTML counterpart to `QuoteBody`. `reply` and
  `forward` quote the original in both halves, as a `<blockquote>` in the HTML
  one. An original that was HTML is quoted as its own markup rather than
  flattened to text, minus any script or style blocks.

### Changed

- `ComposeMessage` derives the plain-text alternative from the markup when
  `HTMLBody` is set and `TextBody` is empty. A `multipart/alternative` whose
  text half is blank reads as an empty message in anything that will not render
  HTML. A supplied `TextBody` is still preferred.
- Reply quoting of an HTML original no longer drops a stylesheet or a script
  into the quoted text, resolves entities, and breaks lines where the markup
  does.

## [1.18.0] - 2026-08-02

### Fixed

- `draft send` ignored the `saveToSent` default and filed a copy in Sent no
  matter what, while `send`, `reply` and `forward` all honoured it. It now
  honours the setting and takes `--no-save` like the others.

### Added

- `mail.SendDraftWithOptions` and `mail.SendDraftOptions` carry the choice.
  `mail.SendDraft` keeps its signature and its behaviour of always filing a
  copy, since it is published.

## [1.17.1] - 2026-08-02

### Fixed

- `bifrost inbox --help` printed a config error instead of the flags. Config
  was loaded during dispatch, before any command parsed its arguments, so
  finding out what a command takes required a working account. A missing or
  broken config is now only reported when something actually needs it.
- Asking for usage exited 2 and printed `error: usage: flag: help requested`
  underneath the help text. `--help` and the `help` subcommands now exit 0 and
  say nothing extra; getting the invocation wrong still exits 2.

## [1.17.0] - 2026-08-02

### Added

- `folder status [name]` reports a folder's message count, unseen count,
  uidnext and uidvalidity via IMAP STATUS, which neither selects the mailbox
  nor fetches envelopes. Defaults to `INBOX`. Counts the server declines to
  give are omitted from JSON and shown as `unknown` in table output, rather
  than reported as zero.
- `mail.IMAPClient.FolderStatus` and `mail.FolderStatus`.

## [1.16.0] - 2026-08-02

### Added

- `flag` and `unflag` set and clear `\Flagged` on a batch of messages.
  `search --flagged` could already filter on it but nothing could set it, so
  the marker was readable and not writable. Clearing a flag that was never set
  is not an error.
- `mail.IMAPClient.FlagBatch` and `UnflagBatch`.

## [1.15.0] - 2026-08-02

### Added

- `read --raw` writes the RFC 822 source with no parsing in between, so
  `read --raw 42 > message.eml` produces a file any mail client opens. Useful
  for archiving and forwarding whole, and the only reliable view of a message
  that does not parse. With `--json` the source is base64-encoded, since RFC
  822 source need not be valid UTF-8 and a JSON string would silently replace
  whatever is not. `--raw` refuses `--save-attachments` rather than accepting
  it and leaving the directory empty.
- `mail.IMAPClient.FetchRaw` returns the same bytes to library callers.

## [1.14.0] - 2026-08-02

### Fixed

- A message cut off in transit no longer reads as empty. `io.ReadAll` returns
  the bytes it managed to read alongside the error, and the parser was
  discarding them, so a truncated body or attachment disappeared entirely.
- A charset or `Content-Transfer-Encoding` with no decoder no longer fails the
  whole message. `mail.CreateReader` reports an error for these while still
  holding a readable entity, and for an unknown encoding it drops the entity
  outright; `ParseMessage` now goes through `message.Read` and reads the part
  as raw bytes. Before this, one mislabelled charset made a message unreadable
  down to its headers.
- A truncated attachment is surfaced with a warning rather than silently
  dropped, since a file that disappears is indistinguishable from one that was
  never sent.

### Added

- `mail.Message.Warnings` records anything that did not survive parsing
  intact. `read` and `thread` include it in JSON (absent when empty) and print
  it to stderr in table mode, so a piped body stays a body.

## [1.13.0] - 2026-08-02

### Added

- `inbox --with-total` and `search --with-total` report how many messages there
  were before `--limit` cut the result down, so a full page can be told apart
  from the last one. JSON wraps the array as
  `{"total":N,"limit":N,"offset":N,"messages":[...]}`; table output gains a
  "Showing X of Y" footer. Both counts were already known and discarded, so the
  flag costs no extra round trip.
- `mail.EnvelopePage`, `mail.IMAPClient.ListEnvelopePage`, and
  `mail.IMAPClient.SearchPage` expose the same total to library callers.

### Unchanged

- Without `--with-total`, `inbox` and `search` still emit a bare JSON array.
  The wrapper is opt-in so existing scripts and `jq '.[]'` pipelines keep
  working. `ListEnvelopes` and `Search` are unchanged for the same reason.

## [1.12.0] - 2026-08-02

### Added

- The approval keyword is enforced. `draft send` refuses a draft still tagged
  `$PendingApproval`, with the new `PENDING_APPROVAL` error code and exit 1,
  and names both ways forward. It was purely advisory before, so the workflow
  could be set up and then walked straight past.
- `draft approve <uid>` clears the keyword, which is the other half: a gate
  with no key is just a wall.
- `draft send --force` sends without clearing the keyword, for when the
  approval is not wanted rather than not given.
- `mail.ErrPendingApproval`, `mail.KeywordPendingApproval`, `mail.HasKeyword`,
  and `IMAPClient.AddKeyword`, `RemoveKeyword` and `FetchFlags`. `SendDraft`
  itself is unchanged and still sends what it is given: the gate is the CLI's
  policy, and the pieces to apply it are exported.

### Fixed

- `mail/README.md` had `SendDraft` returning a bare `error`, which stopped
  being true in 1.4.0.

## [1.11.0] - 2026-08-02

### Added

- `search --keyword` exposes IMAP keyword search, repeatable, with a message
  having to carry all of them. `SearchCriteria.Keywords` had been in the
  library since the first release with no way to reach it from the CLI, which
  meant the drafts `draft save --approval` tags with `$PendingApproval` could
  be created but never found again.

## [1.10.2] - 2026-08-02

### Fixed

- A usage error exits 2 even if the process was interrupted. The interrupt
  branch rewrote the message before the `usage:` check ran, so the check no
  longer matched and the exit code became 1. Unreachable in practice, since a
  usage error is decided before any network work, but exit 2 is the contract
  for a bad invocation.

### Added

- Tests for every command's JSON output, driven through real config loading
  and account resolution against an in-process IMAP server, and for the error
  code and exit status of each sentinel. Coverage: `internal/commands` from
  2.9% to 34.6%, `cmd/bifrost` from 0% to 17.6%.

## [1.10.1] - 2026-08-01

### Fixed

- `archive`, `delete` and `draft save` create a configured folder that does not
  exist yet instead of failing with `No such mailbox`. Only the fallback path
  created folders, so setting any of the overrides added in 1.9.0 broke these
  commands on a server that did not already have the folder.

### Added

- Integration tests against an in-process IMAP server, covering append and
  fetch, trash, archive, folder operations and their error codes, and, together
  with the SMTP test server, the full send and draft round trips. Coverage of
  `package mail` goes from 42.9% to 69.5%, and the first run of the harness is
  what found the bug above.

## [1.10.0] - 2026-08-01

### Added

- The four folder overrides can be set per account, not only in `defaults`. An
  account's own value wins and anything it omits falls back to the default,
  which matters when two accounts are on providers that name folders
  differently. The README had claimed per-account defaults since 1.1.0; this
  makes the claim true for the settings where it actually matters.

### Fixed

- README documents the dependencies, including that `go-imap/v2` is pinned at a
  beta release deliberately.
- README notes that `config init` accepts `--config` after the command, the one
  exception to global flags coming first.

## [1.9.1] - 2026-08-01

### Fixed

- `config init --config PATH` writes to the given path. The flag was only
  recognised before the command, so the documented form silently wrote to
  `~/.bifrost/config.json` instead and said nothing. It refused to overwrite an
  existing file, so nothing was lost, but the config landed in the wrong place.
- `config` rejects arguments it does not understand instead of ignoring them.

## [1.9.0] - 2026-08-01

### Fixed

- `sentFolder` and `draftsFolder` do something. Both were parsed, documented
  and then dropped on the way to the account, so configuring either had no
  effect whatsoever.

### Added

- `trashFolder` and `archiveFolder` overrides, for symmetry now that `delete`
  and `archive` resolve those folders. An override wins over the server's
  special-use attribute and is used as given.
- `mail.AccountConfig` carries the four overrides, with
  `SpecialFolderOverride` to read them.

## [1.8.0] - 2026-08-01

### Changed

- `draft delete` moves the draft to Trash, with `--permanent` to expunge, so
  the two commands named delete mean the same thing. A draft removed by
  `draft send` is still expunged, since a copy of it is already in Sent. The
  JSON result reports `permanent` and `movedTo`.
- `draft delete` now parses flags, so `draft delete --permanent 42` works
  rather than reading the flag as a UID.

## [1.7.0] - 2026-08-01

### Fixed

- `reply` honours the original's `Reply-To` instead of always answering the
  `From` address. A reply to a mailing list was going to whoever happened to
  post rather than back to the list, and send-as setups had answers land on the
  wrong mailbox. All addresses in a `Reply-To` list are used.
- `reply --all` no longer addresses the same person twice. The sender is
  usually in `To` as well, so they were being listed in both places.

### Added

- `read` reports `replyTo`, and shows it in table output when the message has
  one.
- `mail.Message` carries a `ReplyTo` field, parsed from the message source.

## [1.6.0] - 2026-08-01

### Changed

- **`read` and `thread` leave attachment bytes out of the JSON by default.**
  Base64 makes a single PDF larger than the message carrying it, so one read
  could fill an agent's context with data it did not ask for. Each attachment's
  `filename`, `contentType` and `size` are still reported, `--save-attachments`
  still writes the files, and `--with-attachment-data` restores the bytes.
  `--no-attachments` still works and is now redundant.

## [1.5.0] - 2026-08-01

### Changed

- **`delete` moves messages to Trash instead of expunging them.** Deleting the
  wrong message used to be unrecoverable, which is a poor default for a client
  driven by agents working from UIDs. `--permanent` restores the old behaviour.
  Deleting from Trash itself still expunges, since there is nowhere further to
  move to, and the JSON result reports `permanent` and `movedTo` so the caller
  can tell which happened. Trash is resolved from the `\Trash` attribute and
  created if the server has none.

### Added

- `mail.TrashMessages` for the same operation from the library.

## [1.4.2] - 2026-08-01

### Fixed

- `archive` resolves the `\Archive` special-use attribute instead of insisting
  on a folder literally named `Archive`. On a localized or renamed account it
  was creating a second, English-named folder beside the real archive and
  moving mail into that. Sent, Drafts, Trash and Junk already worked this way.
  When the server advertises no attribute, `Archive` and `Archives` are tried
  before falling back to creating `Archive`.

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

[Unreleased]: https://github.com/lgforsberg/bifrost/compare/v1.25.0...HEAD
[1.25.0]: https://github.com/lgforsberg/bifrost/compare/v1.24.1...v1.25.0
[1.24.1]: https://github.com/lgforsberg/bifrost/compare/v1.24.0...v1.24.1
[1.24.0]: https://github.com/lgforsberg/bifrost/compare/v1.23.0...v1.24.0
[1.23.0]: https://github.com/lgforsberg/bifrost/compare/v1.22.0...v1.23.0
[1.22.0]: https://github.com/lgforsberg/bifrost/compare/v1.21.0...v1.22.0
[1.21.0]: https://github.com/lgforsberg/bifrost/compare/v1.20.0...v1.21.0
[1.20.0]: https://github.com/lgforsberg/bifrost/compare/v1.19.0...v1.20.0
[1.19.0]: https://github.com/lgforsberg/bifrost/compare/v1.18.0...v1.19.0
[1.18.0]: https://github.com/lgforsberg/bifrost/compare/v1.17.1...v1.18.0
[1.17.1]: https://github.com/lgforsberg/bifrost/compare/v1.17.0...v1.17.1
[1.17.0]: https://github.com/lgforsberg/bifrost/compare/v1.16.0...v1.17.0
[1.16.0]: https://github.com/lgforsberg/bifrost/compare/v1.15.0...v1.16.0
[1.15.0]: https://github.com/lgforsberg/bifrost/compare/v1.14.0...v1.15.0
[1.14.0]: https://github.com/lgforsberg/bifrost/compare/v1.13.0...v1.14.0
[1.13.0]: https://github.com/lgforsberg/bifrost/compare/v1.12.0...v1.13.0
[1.12.0]: https://github.com/lgforsberg/bifrost/compare/v1.11.0...v1.12.0
[1.11.0]: https://github.com/lgforsberg/bifrost/compare/v1.10.2...v1.11.0
[1.10.2]: https://github.com/lgforsberg/bifrost/compare/v1.10.1...v1.10.2
[1.10.1]: https://github.com/lgforsberg/bifrost/compare/v1.10.0...v1.10.1
[1.10.0]: https://github.com/lgforsberg/bifrost/compare/v1.9.1...v1.10.0
[1.9.1]: https://github.com/lgforsberg/bifrost/compare/v1.9.0...v1.9.1
[1.9.0]: https://github.com/lgforsberg/bifrost/compare/v1.8.0...v1.9.0
[1.8.0]: https://github.com/lgforsberg/bifrost/compare/v1.7.0...v1.8.0
[1.7.0]: https://github.com/lgforsberg/bifrost/compare/v1.6.0...v1.7.0
[1.6.0]: https://github.com/lgforsberg/bifrost/compare/v1.5.0...v1.6.0
[1.5.0]: https://github.com/lgforsberg/bifrost/compare/v1.4.2...v1.5.0
[1.4.2]: https://github.com/lgforsberg/bifrost/compare/v1.4.1...v1.4.2
[1.4.1]: https://github.com/lgforsberg/bifrost/compare/v1.4.0...v1.4.1
[1.4.0]: https://github.com/lgforsberg/bifrost/compare/v1.3.0...v1.4.0
[1.3.0]: https://github.com/lgforsberg/bifrost/compare/v1.2.0...v1.3.0
[1.2.0]: https://github.com/lgforsberg/bifrost/compare/v1.1.4...v1.2.0
[1.1.4]: https://github.com/lgforsberg/bifrost/compare/v1.1.3...v1.1.4
[1.1.3]: https://github.com/lgforsberg/bifrost/compare/v1.1.2...v1.1.3
[1.1.2]: https://github.com/lgforsberg/bifrost/compare/v1.1.1...v1.1.2
[1.1.1]: https://github.com/lgforsberg/bifrost/compare/v1.1.0...v1.1.1
[1.1.0]: https://github.com/lgforsberg/bifrost/releases/tag/v1.1.0
