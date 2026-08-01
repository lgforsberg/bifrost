# Task Ledger

**This file is the single source of truth for what we are working on.** If a task is not
here, it is not tracked. If it is here, its phase and metadata are current.

> **Next free ID: `T-036`** · IDs are never reused · last full verification sweep **2026-08-01**

## How to use this file

**Phases answer "when", not "by when".** No dates, no deadlines: an item moves phase when
reality changes, not when a calendar does. **Order within a phase is meaningful:** top of the
list is what we pick up first.

| Phase | Meaning |
|-------|---------|
| **NOW** | Being worked, or has a live clock that will force the issue shortly. |
| **NEXT** | Ready to start the moment NOW clears. No unknowns, no blockers. |
| **LATER** | Agreed it should happen. Not sequenced, no one is waiting on it. |
| **GATED** | Cannot proceed by our choice alone: waiting on a third party, a time window, or a decision. |
| **PARKED** | No commitment. Kept so the thinking is not lost. Each has a revisit trigger. |
| **DONE** | Shipped within the last week, with evidence. Entries roll off after seven days. |

**Gates** are markers, not phases; a task can be in NEXT *and* need sign-off:

- 🔒 needs operator sign-off before any production or data change
- 👤 depends on a customer or third party
- 🧭 needs a decision from the owner (not a technical blocker)
- ⏳ waiting on a time window (quota reset, observation period)

**Every live task carries the same metadata line**, so it stays greppable and editable:

```
↳ since <first recorded> · pushed <n> · size <S|M|L> · verified <last confirmed real> · ref <detail doc>
```

`size` is a rough effort hint so we can pick work to fit the time available: **S** under an
hour, **M** a working session, **L** multi-session. Pure decisions carry `size -`.

Multi-part tasks may carry sub-checkboxes so partial progress is visible. A task closes only
when all of its boxes are ticked.

**DONE is a shipping record, not an archive.** A finished task moves to DONE, newest first.
Keep the title short (at most two to four lines of body), and replace the live metadata line
with a single evidence line:

```
↳ done <date> · evidence: <commit, query result, or observed behaviour>
```

Narrative detail belongs in the closing commit and the topic doc, not in DONE. Only shipped
work enters DONE. A task that is deleted, abandoned, or found already solved is removed from
the file entirely; the removing commit must name the ID and reason in the form
`T-NNN: deleted, <reason>` so `git log --grep=T-NNN` still finds its fate after it is gone.

Whenever you edit DONE (ship a task, or run a sweep), also delete any entry whose `done` date
is older than seven days. That is the only roll-off trigger; a quiet week leaves stale entries
until the next ship or sweep. After roll-off, this file's git history and the referenced
commits are the record. IDs still never come back: a `T-NNN` means the same thing in an old
commit message as it does today.

## The two rules that keep this file honest

**1. `pushed` counts deferrals, and three is the limit.** Every time a task moves to a later
phase, increment it and say why in one clause. At `pushed 3` the task must be resolved one of
three ways: do it, delete it, or move it to PARKED with an explicit revisit trigger. It may
not quietly slide a fourth time. Backlogs rot because deferral is free; this makes it cost
something.

**2. `verified` is a claim about reality, and it expires.** It records when we last confirmed
the task is still real and still described correctly. Re-verify before acting on anything
older than about a month, and re-verify the whole file periodically; every sweep so far has
found items already solved elsewhere or described inaccurately. Record the sweep date at the
top of the file. A sweep also rolls off DONE entries older than seven days.

Detail belongs in topic docs, not here. A task is one line plus a pointer.

---

## NOW


## NEXT

- **T-035** `draft delete` still expunges, so the two commands named delete now mean different
  things. Either give it the same `--permanent` treatment as T-008 or say why a draft is
  different. Note `SendDraft` removing a draft it just delivered should stay permanent.
  ↳ since 2026-08-01 · pushed 0 · size S · verified 2026-08-01 · ref `internal/commands/draft.go:draftDelete`
- **T-010** Wire the `sentFolder`/`draftsFolder` config options into special-folder resolution
  or delete them: they are parsed, documented in the README, and never read anywhere.
  ↳ since 2026-08-01 · pushed 0 · size S · verified 2026-08-01 · ref `internal/config/config.go:17`
- **T-013** Integration test suite. The two verified critical bugs would both have been caught
  here; `imap.go` (the largest file) is essentially untested and the command layer is at 0%.
  T-006 seeded an in-process SMTP server (`mail/send_smtp_test.go`) to build on.
  - [ ] fake/scripted IMAP server covering FetchMessage, DeleteMessages, Search, special folders
  - [ ] the T-006 warning paths: a Sent append that fails, a draft that will not delete
  - [ ] golden-file tests for each command's JSON output and error codes
  ↳ since 2026-08-01 · pushed 0 · size L · verified 2026-08-01 · ref `mail/imap.go`
- **T-014** Docs accuracy pass: README claims per-account defaults that do not exist and does not
  mention the go-imap beta dependency. Fix the claims or implement them (per-account defaults may
  become its own task).
  ↳ since 2026-08-01 · pushed 0 · size S · verified 2026-08-01 · ref `README.md`

## LATER

- **T-034** `draft send` ignores the `saveToSent` default and has no `--no-save`, so it always
  files a Sent copy while `send`/`reply`/`forward` all honour the setting. Either thread the
  config through `SendDraft` or document the difference deliberately.
  ↳ since 2026-08-01 · pushed 0 · size S · verified 2026-08-01 · ref `mail/operations.go:SendDraft`
- **T-033** Make the network timeouts configurable. T-004 hardcodes a 30s dial and a 60s idle
  deadline, which is right for interactive use but arbitrary: a large attachment over a slow
  link is fine (the deadline is per read, not total) but an agent may want to fail faster, and
  T-029's IDLE support would need the read deadline lifted entirely. A `--timeout` global flag
  or a config default, threaded into `dial`.
  ↳ since 2026-08-01 · pushed 0 · size S · verified 2026-08-01 · ref `mail/dial.go:dialTimeout`
- **T-015** `flag`/`unflag` commands to set and clear `\Flagged`; search can already filter on
  it but nothing can set it, and star-as-todo is a core agent pattern.
  ↳ since 2026-08-01 · pushed 0 · size S · verified 2026-08-01 · ref `internal/commands/markread.go`
- **T-016** Expose IMAP keyword search (`search --keyword`); `SearchCriteria.Keywords` exists in
  the library but the CLI never surfaces it, so `$PendingApproval` drafts cannot be queried.
  ↳ since 2026-08-01 · pushed 0 · size S · verified 2026-08-01 · ref `internal/commands/search.go`
- **T-017** `draft send` should refuse (or warn without `--force`) when the draft still carries
  `$PendingApproval`; the keyword is purely advisory today, which defeats the approval workflow.
  ↳ since 2026-08-01 · pushed 0 · size S · verified 2026-08-01 · ref `internal/commands/draft.go:174`
- **T-018** `folder status` command exposing IMAP STATUS (total, unseen, uidnext) so an agent
  can ask "how many unread?" without listing envelopes.
  ↳ since 2026-08-01 · pushed 0 · size S · verified 2026-08-01 · ref `internal/commands/folder.go`
- **T-019** `read --raw` to emit the RFC 822 source (.eml) for archival, forward-as-attachment,
  and debugging parse issues.
  ↳ since 2026-08-01 · pushed 0 · size S · verified 2026-08-01 · ref `internal/commands/read.go`
- **T-020** HTML sending: `--body-html` on `send`/`reply`/`forward`/`draft save`; the library
  already composes multipart/alternative, the CLI just never exposes `HTMLBody`.
  ↳ since 2026-08-01 · pushed 0 · size S · verified 2026-08-01 · ref `internal/commands/send.go`
- **T-021** Cross-folder search (repeatable `--folder` or `--all-folders`); triage across
  folders currently needs one invocation per folder.
  ↳ since 2026-08-01 · pushed 0 · size M · verified 2026-08-01 · ref `internal/commands/search.go`
- **T-022** 🧭 Pagination metadata in `inbox`/`search` JSON (folder total is already returned by
  SELECT and discarded). Changes the output shape from a bare array, needs a call on format.
  ↳ since 2026-08-01 · pushed 0 · size S · verified 2026-08-01 · ref `mail/imap.go:ListEnvelopes`
- **T-023** `draft update <uid>`: append the revised draft and delete the old one in a single
  command so agent revision loops are not save-new-then-delete-old by hand.
  ↳ since 2026-08-01 · pushed 0 · size M · verified 2026-08-01 · ref `internal/commands/draft.go`
- **T-024** Make the command layer testable: thread an `io.Writer` through `GlobalFlags` instead
  of printing to `os.Stdout` directly, then add command unit tests. Feeds T-013.
  ↳ since 2026-08-01 · pushed 0 · size M · verified 2026-08-01 · ref `internal/cmdutil/cmdutil.go`
- **T-025** `FetchThread` rework: iterative reference expansion (current single hop misses
  distant thread members), envelope-only discovery instead of full-body fetches, and
  `slices.SortFunc` over the hand-rolled insertion sort.
  ↳ since 2026-08-01 · pushed 0 · size M · verified 2026-08-01 · ref `mail/imap.go:FetchThread`
- **T-032** Fall back to raw bytes instead of dropping a part the reader cannot fully decode.
  A body read that fails part way discards the readable prefix (a truncated message reads as
  empty), and a part with an unhandled `Content-Transfer-Encoding` is skipped outright. Decide
  separately whether a partial attachment should be surfaced or withheld as a corrupt file.
  ↳ since 2026-08-01 · pushed 0 · size S · verified 2026-08-01 · ref `mail/mime.go:ParseMessage part walk`
- **T-026** Code hygiene batch: rune-safe `truncate`; `filepath.Ext` over `fileExtension`;
  unchecked `int64`→`uint32` casts; global flag parser silently drops a missing
  `--account`/`--config` value; version from build info instead of a hand-maintained const;
  Message-ID should not embed the real hostname.
  ↳ since 2026-08-01 · pushed 0 · size M · verified 2026-08-01 · ref `internal/commands/inbox.go:78`

## GATED

- **T-027** 🧭 OAuth2 (XOAUTH2/OAUTHBEARER) for IMAP and SMTP; Gmail and Microsoft 365 have
  retired basic auth, so PLAIN-only locks out the two biggest providers. Gate: decide the token
  acquisition model first (external helper command in config vs built-in refresh flow).
  ↳ since 2026-08-01 · pushed 0 · size L · verified 2026-08-01 · ref `mail/imap.go:Connect`
- **T-028** 👤 Move off `go-imap v2.0.0-beta.8` when upstream ships a stable v2; until then,
  pin deliberately and note the beta status in the README (covered by T-014).
  ↳ since 2026-08-01 · pushed 0 · size S · verified 2026-08-01 · ref `go.mod`

## PARKED

- **T-029** `watch --timeout` primitive on IMAP IDLE (exit on new mail, still non-interactive)
  so agents do not busy-loop `inbox`. Revisit trigger: a real agent workflow needs push-style
  mail, and not before T-013 exists to test it.
  ↳ since 2026-08-01 · pushed 0 · size M · verified 2026-08-01 · ref `mail/imap.go`
- **T-030** Connection reuse or batch mode; every invocation pays a full TLS + login handshake.
  Revisit trigger: measured handshake latency actually hurting a real agent workload.
  ↳ since 2026-08-01 · pushed 0 · size L · verified 2026-08-01 · ref `internal/helpers/connect.go`

## DONE

- **T-011** Replies follow `Reply-To`. Fixed a duplicate-recipient bug in `--all` found while
  reworking the same addressing code.
  ↳ done 2026-08-01 · evidence: `TestBuildReply_HonorsReplyTo` covers the mailing-list case that
  was going to the poster instead of the list; `TestBuildReply_AllDoesNotRepeatARecipient` fails
  on the old code, which listed a sender who was also in To twice. v1.7.0
- **T-012** `read` and `thread` omit attachment bytes from JSON unless `--with-attachment-data`
  is given. Owner chose opt-in over opt-out. Applied to `thread` as well, where a shared
  attachment was being repeated once per message in the conversation.
  ↳ done 2026-08-01 · evidence: `data` is dropped by the existing `omitempty` tag, so filename,
  contentType and size still come through; `--no-attachments` kept working rather than removed,
  so nothing passing it breaks. v1.6.0
- **T-008** `delete` moves to Trash; `--permanent` expunges. Owner chose to change the default
  rather than hide the safe path behind a flag, since an agent working from UIDs had no undo.
  ↳ done 2026-08-01 · evidence: `bifrost delete 42` now reports `movedTo`; deleting from Trash
  expunges, as there is nowhere further to move to. `TestReorderArgs/bool_flag_before_a_positional`
  covers the trap that a bool flag left out of the reorder map eats the UID after it. Follow-up
  T-035 filed for `draft delete`, which still expunges. v1.5.0
- **T-009** `archive` follows the server's `\Archive` folder. Special-folder matching moved into
  a pure `matchSpecialFolder`, which made the resolution order testable.
  ↳ done 2026-08-01 · evidence: `TestMatchSpecialFolder` pins that an advertised attribute beats
  a conventionally named folder, which is the localized case that was broken: a Swedish account
  with `Arkiv` was getting a second `Archive` folder created next to it. v1.4.2
- **T-007** Errors are classified from status codes, not substrings. SMTP maps on the reply code
  and the stage it failed at; IMAP reads the response code off the typed error, keeping the
  wording as a fallback for servers that send a bare `NO`.
  ↳ done 2026-08-01 · evidence: the old classifier called both `550 Not authorized to send as
  this address` and `553 Sender address not authorized` AUTH_FAILED, which sends an agent to
  check working credentials; `TestSmtpDeliver_ClassifiesByStatusCode` pins those to
  SEND_REJECTED and keeps 4xx off it, and `TestSmtpDeliver_BrokenConnectionIsNotARejection`
  covers a server that never answers. v1.4.1
- **T-006** A send that could not be filed in Sent says so. `Send` and `SendDraft` return a
  `SendResult` carrying the Message-ID and any warnings; the commands print those to stderr, or
  as a `warnings` array in JSON. Delivery still reports success and exit 0, since retrying a
  delivered message would send it twice.
  ↳ done 2026-08-01 · evidence: `TestSend_DeliversOverPlaintext` runs the send path against an
  in-process SMTP server and checks the reported Message-ID is the one delivered; the warning
  paths themselves need the IMAP side of T-013 to cover. Library callers change: both functions
  gained a return value. v1.4.0
- **T-005** `draft send` keeps its blind recipients. `ParseMessage` now reads the `Bcc` header
  back off the stored draft, which T-001 had already made sure was written, and `read` reports
  it for the copies that carry it.
  ↳ done 2026-08-01 · evidence: `TestDraftRoundTrip_PreservesBcc` walks save, parse and
  re-compose, asserting the blind recipient reaches the SMTP envelope and never the delivered
  bytes; it reports an empty Bcc without the parse change; v1.3.0
- **T-004** The network layer has real timeouts and honours cancellation. Both clients now run
  over a connection carrying a 60s idle deadline on every read and write, and cancelling the
  context drops that connection, which is the only way to interrupt commands that take no
  context of their own.
  - [x] dial timeouts and read/write deadlines on IMAP and SMTP connections
  - [x] honor `ctx` in library operations
  - [x] Ctrl-C observably aborts an in-flight command
  ↳ done 2026-08-01 · evidence: against a listener that accepts and stays silent, the old path
  was still blocked 5s after cancellation while `TestIMAPConnect_CancellationInterruptsSilentServer`
  returns in 0.1s; `bifrost --json inbox` under SIGINT exits immediately with `INTERRUPTED`
  where it used to hang; deadline and cancellation mechanics covered under `-race`; v1.2.0
- **T-003** Attachments can no longer be written outside the target directory. Sender-supplied
  names are reduced to a bare file name, collisions are suffixed rather than overwritten, and
  the save routine moved to `internal/helpers` where it is directly testable.
  ↳ done 2026-08-01 · evidence: `TestSaveAttachments_ContainsHostileNames` covers traversal,
  absolute, Windows and degenerate names; a naive-join reproduction wrote clean outside the
  temp root, the new path does not; also closes the dedupe bug listed under T-026; v1.1.4
- **T-031** Mail in a non-UTF-8 charset parses again. Registering the go-message charset
  decoders fixes `iso-8859-1` and `windows-1252` messages, which errored out when single-part
  and read as empty when multipart.
  ↳ done 2026-08-01 · evidence: `TestParseMessage_DecodesLegacyCharsets` and
  `TestParseMessage_LegacyCharsetInMultipart` fail without the import and pass with it; the
  x/text this pulls in was raised to v0.40.0, leaving govulncheck clean; v1.1.3
- **T-002** A malformed message no longer hangs the parser. Consecutive MIME part failures are
  capped, so a truncated or broken body returns the headers and recovered parts instead of
  spinning on a repeated error.
  ↳ done 2026-08-01 · evidence: `TestParseMessage_MalformedMultipartDoesNotHang` covers three
  inputs that each hung before, all now returning instantly; removing the bound makes every case
  fail on the 5s guard; v1.1.2
- **T-001** Blind-copied recipients are no longer disclosed. `ComposeMessage` omits the `Bcc`
  header, delivery relies on the SMTP envelope, and only the copies filed in Sent and Drafts
  keep the header as the sender's record.
  ↳ done 2026-08-01 · evidence: `TestComposeMessage_OmitsBccHeader` (all three compose paths),
  `TestComposeMessage_BccReachesEnvelope`, `TestComposeMessage_ServerCopyKeepsBcc`; the test that
  originally reproduced the leak now passes, and reintroducing the header fails the suite; v1.1.1
