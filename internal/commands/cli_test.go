package commands

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lgforsberg/bifrost/internal/cmdutil"
	"github.com/lgforsberg/bifrost/internal/config"
	"github.com/lgforsberg/bifrost/internal/testimap"
	"github.com/lgforsberg/bifrost/mail"
)

const (
	testAddress  = "user@example.com"
	testPassword = "secret"
)

// testCLI runs commands the way main does, against an in-process IMAP server
// and a config file on disk, with the output captured instead of printed.
// Everything between parsing flags and the bytes a caller would see is real.
type testCLI struct {
	g   *cmdutil.GlobalFlags
	out *bytes.Buffer
	err *bytes.Buffer
	srv *testimap.Server
}

func newTestCLI(t *testing.T) *testCLI {
	t.Helper()

	srv := testimap.Start(t, testAddress, testPassword, testimap.Hooks{})

	// A config file rather than a hand-built struct, so account resolution and
	// the defaults are exercised too. SMTP points nowhere: these tests do not
	// send.
	path := filepath.Join(t.TempDir(), "config.json")
	contents := fmt.Sprintf(`{
		"accounts": [{
			"address": %q,
			"default": true,
			"imap": {"host": %q, "port": %d, "encryption": "none"},
			"smtp": {"host": "127.0.0.1", "port": 1, "encryption": "none"},
			"password": %q
		}]
	}`, testAddress, srv.Host, srv.Port, testPassword)
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	return &testCLI{
		g: &cmdutil.GlobalFlags{
			JSON:       true,
			Ctx:        context.Background(),
			Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
			Config:     cfg,
			ConfigPath: path,
			Stdout:     out,
			Stderr:     errOut,
		},
		out: out,
		err: errOut,
		srv: srv,
	}
}

// seed appends messages to a folder, creating it if needed, and returns their
// UIDs in the order given.
func (c *testCLI) seed(t *testing.T, folder string, subjects ...string) []uint32 {
	t.Helper()

	acct, err := config.DefaultAccount(c.g.Config)
	if err != nil {
		t.Fatalf("resolving the account: %v", err)
	}
	client := mail.NewIMAPClient(*acct, c.g.Logger)
	if err := client.Connect(c.g.Ctx); err != nil {
		t.Fatalf("connecting to seed messages: %v", err)
	}
	defer client.Close()

	if err := client.EnsureFolder(c.g.Ctx, folder); err != nil {
		t.Fatalf("ensuring %s: %v", folder, err)
	}

	uids := make([]uint32, 0, len(subjects))
	for _, subject := range subjects {
		raw := "From: alice@example.com\r\nTo: " + testAddress +
			"\r\nSubject: " + subject + "\r\n\r\nBody of " + subject + "\r\n"
		uid, err := client.AppendMessage(c.g.Ctx, folder, []byte(raw), []string{"\\Seen"})
		if err != nil {
			t.Fatalf("appending %q: %v", subject, err)
		}
		uids = append(uids, uid)
	}
	return uids
}

// decodeObject reads what the command wrote as a JSON object.
// seedRaw appends a message verbatim, for the shapes seed cannot express:
// damaged MIME, odd encodings, anything where the exact bytes are the point.
func (c *testCLI) seedRaw(t *testing.T, folder, raw string) uint32 {
	t.Helper()

	acct, err := config.DefaultAccount(c.g.Config)
	if err != nil {
		t.Fatalf("resolving the account: %v", err)
	}
	client := mail.NewIMAPClient(*acct, c.g.Logger)
	if err := client.Connect(c.g.Ctx); err != nil {
		t.Fatalf("connecting to seed a message: %v", err)
	}
	defer client.Close()

	if err := client.EnsureFolder(c.g.Ctx, folder); err != nil {
		t.Fatalf("ensuring %s: %v", folder, err)
	}
	uid, err := client.AppendMessage(c.g.Ctx, folder, []byte(raw), []string{"\\Seen"})
	if err != nil {
		t.Fatalf("appending the message: %v", err)
	}
	return uid
}

func (c *testCLI) decodeObject(t *testing.T) map[string]any {
	t.Helper()

	var v map[string]any
	if err := json.Unmarshal(c.out.Bytes(), &v); err != nil {
		t.Fatalf("output is not a JSON object: %v\n%s", err, c.out.String())
	}
	return v
}

// decodeArray reads what the command wrote as a JSON array.
func (c *testCLI) decodeArray(t *testing.T) []map[string]any {
	t.Helper()

	var v []map[string]any
	if err := json.Unmarshal(c.out.Bytes(), &v); err != nil {
		t.Fatalf("output is not a JSON array: %v\n%s", err, c.out.String())
	}
	return v
}

func hasKeys(t *testing.T, obj map[string]any, keys ...string) {
	t.Helper()

	for _, k := range keys {
		if _, ok := obj[k]; !ok {
			t.Errorf("missing key %q in %v", k, obj)
		}
	}
}

func TestInbox_JSON(t *testing.T) {
	cli := newTestCLI(t)
	cli.seed(t, "INBOX", "First", "Second")

	if err := Inbox(cli.g, nil); err != nil {
		t.Fatalf("inbox: %v", err)
	}

	envelopes := cli.decodeArray(t)
	if len(envelopes) != 2 {
		t.Fatalf("got %d envelopes, want 2", len(envelopes))
	}
	// Newest first, which is what an agent triaging a mailbox depends on.
	if envelopes[0]["subject"] != "Second" {
		t.Errorf("first envelope is %v, want the newest", envelopes[0]["subject"])
	}
	hasKeys(t, envelopes[0], "uid", "subject", "from", "date", "flags")
}

func TestInbox_EmptyFolderIsAnEmptyArray(t *testing.T) {
	cli := newTestCLI(t)

	if err := Inbox(cli.g, nil); err != nil {
		t.Fatalf("inbox: %v", err)
	}

	// Not null: a caller iterating the result should not have to special-case
	// an empty mailbox.
	if got := cli.out.String(); got != "[]\n" {
		t.Errorf("output = %q, want an empty array", got)
	}
}

// The default shape is the contract every existing script depends on, so the
// interesting assertion is that asking for the total does not change it for
// anyone who did not ask.
func TestInbox_WithTotalIsOptIn(t *testing.T) {
	cli := newTestCLI(t)
	cli.seed(t, "INBOX", "First", "Second", "Third")

	if err := Inbox(cli.g, []string{"--limit", "2"}); err != nil {
		t.Fatalf("inbox: %v", err)
	}
	if got := cli.decodeArray(t); len(got) != 2 {
		t.Fatalf("plain inbox returned %d envelopes, want a bare array of 2", len(got))
	}
	cli.out.Reset()

	if err := Inbox(cli.g, []string{"--limit", "2", "--with-total"}); err != nil {
		t.Fatalf("inbox --with-total: %v", err)
	}
	page := cli.decodeObject(t)
	if page["total"] != float64(3) {
		t.Errorf("total = %v, want 3", page["total"])
	}
	if page["limit"] != float64(2) {
		t.Errorf("limit = %v, want 2", page["limit"])
	}
	if page["offset"] != float64(0) {
		t.Errorf("offset = %v, want 0", page["offset"])
	}
	messages, ok := page["messages"].([]any)
	if !ok {
		t.Fatalf("messages is %T, want an array", page["messages"])
	}
	if len(messages) != 2 {
		t.Errorf("got %d messages, want 2", len(messages))
	}
}

// Reporting the total is only worth anything if it counts the matches rather
// than what survived the limit.
func TestSearch_WithTotalCountsBeyondTheLimit(t *testing.T) {
	cli := newTestCLI(t)
	cli.seed(t, "INBOX", "Invoice 1", "Invoice 2", "Invoice 3")

	if err := Search(cli.g, []string{"--subject", "Invoice", "--limit", "1", "--with-total"}); err != nil {
		t.Fatalf("search --with-total: %v", err)
	}

	page := cli.decodeObject(t)
	if page["total"] != float64(3) {
		t.Errorf("total = %v, want all 3 matches", page["total"])
	}
	if messages := page["messages"].([]any); len(messages) != 1 {
		t.Errorf("got %d messages, want the 1 asked for", len(messages))
	}
}

func TestInbox_WithTotalOnAnEmptyFolder(t *testing.T) {
	cli := newTestCLI(t)

	if err := Inbox(cli.g, []string{"--with-total"}); err != nil {
		t.Fatalf("inbox --with-total: %v", err)
	}

	page := cli.decodeObject(t)
	if page["total"] != float64(0) {
		t.Errorf("total = %v, want 0", page["total"])
	}
	// Still an array rather than null, same as the bare form.
	if messages, ok := page["messages"].([]any); !ok || len(messages) != 0 {
		t.Errorf("messages = %v, want an empty array", page["messages"])
	}
}

// A message damaged in transit must still read, and must say that it is
// damaged. Reading one used to fail outright, which left an agent with no way
// to tell a broken message from a broken mailbox.
func TestRead_DamagedMessageStillReadsAndSaysSo(t *testing.T) {
	cli := newTestCLI(t)
	// An encoding with no decoder, which the reader used to refuse entirely.
	uid := cli.seedRaw(t, "INBOX", "From: alice@example.com\r\n"+
		"To: "+testAddress+"\r\n"+
		"Subject: Ancient client\r\n"+
		"Content-Type: text/plain; charset=utf-8\r\n"+
		"Content-Transfer-Encoding: x-uuencode\r\n"+
		"\r\n"+
		"the body survived\r\n")

	if err := Read(cli.g, []string{fmt.Sprintf("%d", uid)}); err != nil {
		t.Fatalf("read: %v", err)
	}

	msg := cli.decodeObject(t)
	if subject, _ := msg["subject"].(string); subject != "Ancient client" {
		t.Errorf("subject = %q, want the headers through", subject)
	}
	if body, _ := msg["textBody"].(string); !strings.Contains(body, "the body survived") {
		t.Errorf("textBody = %q, want the undecoded bytes", body)
	}
	warnings, ok := msg["warnings"].([]any)
	if !ok || len(warnings) == 0 {
		t.Fatalf("warnings = %v, want the damage reported", msg["warnings"])
	}
}

// In table mode the warning belongs on stderr, so a piped body is still a body.
func TestRead_WarningsStayOffStdout(t *testing.T) {
	cli := newTestCLI(t)
	cli.g.JSON = false
	uid := cli.seedRaw(t, "INBOX", "From: alice@example.com\r\n"+
		"Subject: Odd label\r\n"+
		"Content-Type: text/plain; charset=x-nonesuch\r\n"+
		"\r\n"+
		"readable anyway\r\n")

	if err := Read(cli.g, []string{fmt.Sprintf("%d", uid)}); err != nil {
		t.Fatalf("read: %v", err)
	}

	if !strings.Contains(cli.out.String(), "readable anyway") {
		t.Errorf("stdout missing the body:\n%s", cli.out.String())
	}
	if strings.Contains(cli.out.String(), "warning:") {
		t.Errorf("warning leaked onto stdout:\n%s", cli.out.String())
	}
	if !strings.Contains(cli.err.String(), "warning:") {
		t.Errorf("stderr missing the warning:\n%s", cli.err.String())
	}
}

// The common case has to stay quiet, or the warnings are noise.
func TestRead_CleanMessageCarriesNoWarnings(t *testing.T) {
	cli := newTestCLI(t)
	uids := cli.seed(t, "INBOX", "Ordinary")

	if err := Read(cli.g, []string{fmt.Sprintf("%d", uids[0])}); err != nil {
		t.Fatalf("read: %v", err)
	}

	if warnings, present := cli.decodeObject(t)["warnings"]; present {
		t.Errorf("warnings = %v, want the key absent for a clean message", warnings)
	}
}

// The point of --raw is that `read --raw 42 > msg.eml` produces a file other
// mail software opens, so stdout carries the bytes and nothing else.
func TestRead_RawWritesTheSourceAndNothingElse(t *testing.T) {
	cli := newTestCLI(t)
	cli.g.JSON = false
	source := "From: alice@example.com\r\n" +
		"To: " + testAddress + "\r\n" +
		"Subject: Verbatim\r\n" +
		"\r\n" +
		"exactly these bytes\r\n"
	uid := cli.seedRaw(t, "INBOX", source)

	if err := Read(cli.g, []string{"--raw", fmt.Sprintf("%d", uid)}); err != nil {
		t.Fatalf("read --raw: %v", err)
	}

	got := cli.out.String()
	if !strings.Contains(got, "Subject: Verbatim") || !strings.Contains(got, "exactly these bytes") {
		t.Errorf("stdout is not the message source:\n%q", got)
	}
	// The parsed view labels its fields; the raw one must not.
	if strings.Contains(got, "UID:") {
		t.Errorf("stdout carries formatting that would corrupt a .eml:\n%q", got)
	}
}

// A message that will not parse cleanly is exactly when the raw source is
// wanted, so --raw must not go anywhere near the parser.
func TestRead_RawWorksOnAMessageThatBarelyParses(t *testing.T) {
	cli := newTestCLI(t)
	cli.g.JSON = false
	uid := cli.seedRaw(t, "INBOX", "From: alice@example.com\r\n"+
		"Subject: Ancient client\r\n"+
		"Content-Transfer-Encoding: x-uuencode\r\n"+
		"\r\n"+
		"undecodable payload\r\n")

	if err := Read(cli.g, []string{"--raw", fmt.Sprintf("%d", uid)}); err != nil {
		t.Fatalf("read --raw: %v", err)
	}
	if !strings.Contains(cli.out.String(), "undecodable payload") {
		t.Errorf("stdout missing the source:\n%q", cli.out.String())
	}
}

// JSON has to stay JSON, and the source has to survive bytes that are not
// valid UTF-8, hence base64 rather than a string.
func TestRead_RawJSONIsBase64(t *testing.T) {
	cli := newTestCLI(t)
	source := "From: alice@example.com\r\nSubject: Bytes\r\n\r\nbody\r\n"
	uid := cli.seedRaw(t, "INBOX", source)

	if err := Read(cli.g, []string{"--raw", fmt.Sprintf("%d", uid)}); err != nil {
		t.Fatalf("read --raw: %v", err)
	}

	obj := cli.decodeObject(t)
	encoded, ok := obj["raw"].(string)
	if !ok {
		t.Fatalf("raw is %T, want a base64 string", obj["raw"])
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("raw is not base64: %v", err)
	}
	if !strings.Contains(string(decoded), "Subject: Bytes") {
		t.Errorf("decoded raw = %q, want the source", string(decoded))
	}
	if obj["size"] != float64(len(decoded)) {
		t.Errorf("size = %v, want %d", obj["size"], len(decoded))
	}
}

// Accepting the flag and leaving the directory empty would be worse than
// refusing it.
func TestRead_RawRefusesToPretendItCanSaveAttachments(t *testing.T) {
	cli := newTestCLI(t)
	uids := cli.seed(t, "INBOX", "Anything")

	err := Read(cli.g, []string{"--raw", "--save-attachments", t.TempDir(), fmt.Sprintf("%d", uids[0])})
	if err == nil {
		t.Fatal("expected a usage error for --raw with --save-attachments")
	}
	if !strings.HasPrefix(err.Error(), "usage:") {
		t.Errorf("error = %q, want a usage error so the exit code is 2", err)
	}
}

// Flagging is only worth anything if search can find what was flagged, which
// is the whole reason the gap mattered: the filter existed with nothing to
// filter on.
func TestFlag_RoundTripThroughSearch(t *testing.T) {
	cli := newTestCLI(t)
	uids := cli.seed(t, "INBOX", "Needs a look", "Ignore me")
	target := fmt.Sprintf("%d", uids[0])

	if err := Flag(cli.g, []string{target}); err != nil {
		t.Fatalf("flag: %v", err)
	}
	cli.out.Reset()

	if err := Search(cli.g, []string{"--flagged"}); err != nil {
		t.Fatalf("search --flagged: %v", err)
	}
	found := cli.decodeArray(t)
	if len(found) != 1 {
		t.Fatalf("search found %d flagged messages, want 1", len(found))
	}
	if subject, _ := found[0]["subject"].(string); subject != "Needs a look" {
		t.Errorf("flagged subject = %q, want the one that was flagged", subject)
	}
	cli.out.Reset()

	if err := Unflag(cli.g, []string{target}); err != nil {
		t.Fatalf("unflag: %v", err)
	}
	cli.out.Reset()

	if err := Search(cli.g, []string{"--flagged"}); err != nil {
		t.Fatalf("search --flagged after unflag: %v", err)
	}
	if found := cli.decodeArray(t); len(found) != 0 {
		t.Errorf("search still finds %d flagged messages after unflag", len(found))
	}
}

func TestFlag_JSONReportsWhatItTouched(t *testing.T) {
	cli := newTestCLI(t)
	uids := cli.seed(t, "INBOX", "One", "Two")

	if err := Flag(cli.g, []string{fmt.Sprintf("%d", uids[0]), fmt.Sprintf("%d", uids[1])}); err != nil {
		t.Fatalf("flag: %v", err)
	}

	obj := cli.decodeObject(t)
	if obj["status"] != "flagged" {
		t.Errorf("status = %v, want flagged", obj["status"])
	}
	if touched, _ := obj["uids"].([]any); len(touched) != 2 {
		t.Errorf("uids = %v, want both", obj["uids"])
	}
}

// IMAP does not mind clearing a flag that was never set, and neither should we.
func TestUnflag_OnAnUnflaggedMessageIsFine(t *testing.T) {
	cli := newTestCLI(t)
	uids := cli.seed(t, "INBOX", "Never flagged")

	if err := Unflag(cli.g, []string{fmt.Sprintf("%d", uids[0])}); err != nil {
		t.Fatalf("unflag: %v", err)
	}
}

func TestFlag_MissingUIDIsAUsageError(t *testing.T) {
	cli := newTestCLI(t)

	err := Flag(cli.g, nil)
	if err == nil || !strings.HasPrefix(err.Error(), "usage:") {
		t.Fatalf("error = %v, want a usage error", err)
	}
}

// The point of STATUS is answering "how many unread?" without listing
// anything, so the unseen count is the assertion that matters.
func TestFolderStatus_CountsWithoutListing(t *testing.T) {
	cli := newTestCLI(t)
	uids := cli.seed(t, "INBOX", "One", "Two", "Three")

	// seed marks everything \Seen, so make one unread to count.
	if err := MarkUnread(cli.g, []string{fmt.Sprintf("%d", uids[0])}); err != nil {
		t.Fatalf("mark-unread: %v", err)
	}
	cli.out.Reset()

	if err := Folder(cli.g, []string{"status"}); err != nil {
		t.Fatalf("folder status: %v", err)
	}

	obj := cli.decodeObject(t)
	if obj["name"] != "INBOX" {
		t.Errorf("name = %v, want INBOX", obj["name"])
	}
	if obj["total"] != float64(3) {
		t.Errorf("total = %v, want 3", obj["total"])
	}
	if obj["unseen"] != float64(1) {
		t.Errorf("unseen = %v, want 1", obj["unseen"])
	}
	if _, ok := obj["uidNext"]; !ok {
		t.Error("uidNext missing, which is what a caller pages from")
	}
}

func TestFolderStatus_NamedFolder(t *testing.T) {
	cli := newTestCLI(t)
	cli.seed(t, "Projects", "Only one here")

	if err := Folder(cli.g, []string{"status", "Projects"}); err != nil {
		t.Fatalf("folder status Projects: %v", err)
	}

	obj := cli.decodeObject(t)
	if obj["name"] != "Projects" {
		t.Errorf("name = %v, want Projects", obj["name"])
	}
	if obj["total"] != float64(1) {
		t.Errorf("total = %v, want 1", obj["total"])
	}
}

func TestFolderStatus_UnknownFolderIsNotFound(t *testing.T) {
	cli := newTestCLI(t)

	err := Folder(cli.g, []string{"status", "No Such Folder"})
	if !errors.Is(err, mail.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

// main exits 0 on flag.ErrHelp rather than treating it as a usage error, so
// what a command returns for --help has to keep wrapping it.
func TestCommands_HelpReturnsErrHelp(t *testing.T) {
	cli := newTestCLI(t)

	for name, run := range map[string]func(*cmdutil.GlobalFlags, []string) error{
		"inbox":  Inbox,
		"read":   Read,
		"search": Search,
		"flag":   Flag,
	} {
		t.Run(name, func(t *testing.T) {
			err := run(cli.g, []string{"--help"})
			if !errors.Is(err, flag.ErrHelp) {
				t.Errorf("error = %v, want it to wrap flag.ErrHelp so main can exit 0", err)
			}
		})
	}
}

// Asking a subcommand what it takes is a question, not a mistake, so it must
// not come back as the exit-2 usage error that a wrong invocation does.
func TestSubcommands_HelpIsNotAnError(t *testing.T) {
	cli := newTestCLI(t)
	cli.g.JSON = false

	for name, run := range map[string]func(*cmdutil.GlobalFlags, []string) error{
		"folder": Folder,
		"draft":  Draft,
		"config": Config,
	} {
		t.Run(name, func(t *testing.T) {
			cli.err.Reset()
			if err := run(cli.g, []string{"help"}); err != nil {
				t.Fatalf("%s help returned %v, want nil", name, err)
			}
			if !strings.Contains(cli.err.String(), "usage:") {
				t.Errorf("no usage text written:\n%s", cli.err.String())
			}
		})
	}
}

func TestRead_JSON(t *testing.T) {
	cli := newTestCLI(t)
	uids := cli.seed(t, "INBOX", "Hello")

	if err := Read(cli.g, []string{fmt.Sprint(uids[0])}); err != nil {
		t.Fatalf("read: %v", err)
	}

	msg := cli.decodeObject(t)
	if msg["subject"] != "Hello" {
		t.Errorf("subject = %v", msg["subject"])
	}
	hasKeys(t, msg, "uid", "subject", "from", "to", "date", "textBody")
}

func TestSearch_JSON(t *testing.T) {
	cli := newTestCLI(t)
	cli.seed(t, "INBOX", "Invoice 42", "Lunch")

	if err := Search(cli.g, []string{"--subject", "Invoice"}); err != nil {
		t.Fatalf("search: %v", err)
	}

	results := cli.decodeArray(t)
	if len(results) != 1 || results[0]["subject"] != "Invoice 42" {
		t.Errorf("results = %v, want the one invoice", results)
	}
}

// The T-008 contract, at the boundary a caller actually sees.
func TestDelete_JSONReportsTheMove(t *testing.T) {
	cli := newTestCLI(t)
	uids := cli.seed(t, "INBOX", "Doomed")

	if err := Delete(cli.g, []string{fmt.Sprint(uids[0])}); err != nil {
		t.Fatalf("delete: %v", err)
	}

	result := cli.decodeObject(t)
	if result["status"] != "deleted" {
		t.Errorf("status = %v, want deleted", result["status"])
	}
	if result["permanent"] != false {
		t.Errorf("permanent = %v, want false", result["permanent"])
	}
	if result["movedTo"] != "Trash" {
		t.Errorf("movedTo = %v, want Trash", result["movedTo"])
	}
}

func TestDelete_PermanentSaysSo(t *testing.T) {
	cli := newTestCLI(t)
	uids := cli.seed(t, "INBOX", "Doomed")

	if err := Delete(cli.g, []string{"--permanent", fmt.Sprint(uids[0])}); err != nil {
		t.Fatalf("delete --permanent: %v", err)
	}

	result := cli.decodeObject(t)
	if result["permanent"] != true {
		t.Errorf("permanent = %v, want true", result["permanent"])
	}
	if _, ok := result["movedTo"]; ok {
		t.Errorf("movedTo is set on a permanent delete: %v", result)
	}
}

func TestArchive_JSON(t *testing.T) {
	cli := newTestCLI(t)
	uids := cli.seed(t, "INBOX", "Filed")

	if err := Archive(cli.g, []string{fmt.Sprint(uids[0])}); err != nil {
		t.Fatalf("archive: %v", err)
	}

	result := cli.decodeObject(t)
	if result["status"] != "archived" {
		t.Errorf("status = %v, want archived", result["status"])
	}
}

func TestMarkRead_JSON(t *testing.T) {
	cli := newTestCLI(t)
	uids := cli.seed(t, "INBOX", "Unread")

	if err := MarkRead(cli.g, []string{fmt.Sprint(uids[0])}); err != nil {
		t.Fatalf("markread: %v", err)
	}

	result := cli.decodeObject(t)
	if result["status"] != "marked_read" {
		t.Errorf("status = %v", result["status"])
	}
}

func TestFolderList_JSON(t *testing.T) {
	cli := newTestCLI(t)
	cli.seed(t, "Projects")

	if err := Folder(cli.g, []string{"list"}); err != nil {
		t.Fatalf("folder list: %v", err)
	}

	folders := cli.decodeArray(t)
	var names []string
	for _, f := range folders {
		names = append(names, fmt.Sprint(f["name"]))
	}
	if len(folders) == 0 {
		t.Fatal("no folders listed")
	}
	hasKeys(t, folders[0], "name")

	found := false
	for _, n := range names {
		if n == "Projects" {
			found = true
		}
	}
	if !found {
		t.Errorf("listed %v, want Projects among them", names)
	}
}

func TestAccounts_JSON(t *testing.T) {
	cli := newTestCLI(t)

	if err := Accounts(cli.g, nil); err != nil {
		t.Fatalf("accounts: %v", err)
	}

	accounts := cli.decodeArray(t)
	if len(accounts) != 1 {
		t.Fatalf("got %d accounts, want 1", len(accounts))
	}
	if accounts[0]["address"] != testAddress {
		t.Errorf("address = %v", accounts[0]["address"])
	}
	if accounts[0]["default"] != true {
		t.Errorf("the only account is not marked default: %v", accounts[0])
	}
}

func TestDraft_SaveThenListThenDelete(t *testing.T) {
	cli := newTestCLI(t)

	if err := Draft(cli.g, []string{"save", "--to", "bob@example.com", "--subject", "Later", "--body", "Text"}); err != nil {
		t.Fatalf("draft save: %v", err)
	}
	saved := cli.decodeObject(t)
	hasKeys(t, saved, "status", "uid")
	uid := fmt.Sprint(int(saved["uid"].(float64)))

	cli.out.Reset()
	if err := Draft(cli.g, []string{"list"}); err != nil {
		t.Fatalf("draft list: %v", err)
	}
	drafts := cli.decodeArray(t)
	if len(drafts) != 1 || drafts[0]["subject"] != "Later" {
		t.Errorf("drafts = %v, want the one saved", drafts)
	}

	cli.out.Reset()
	if err := Draft(cli.g, []string{"delete", uid}); err != nil {
		t.Fatalf("draft delete: %v", err)
	}
	deleted := cli.decodeObject(t)
	if deleted["movedTo"] != "Trash" {
		t.Errorf("movedTo = %v, want Trash", deleted["movedTo"])
	}
}

// --body-html without --body is the shape an agent composing HTML will
// reach for. The message still has to carry a readable plain-text half.
func TestDraft_SaveWithHTMLBodyOnly(t *testing.T) {
	cli := newTestCLI(t)

	err := Draft(cli.g, []string{"save", "--to", "bob@example.com", "--subject", "Report",
		"--body-html", "<p>Revenue is <b>up</b> &amp; to the right</p>"})
	if err != nil {
		t.Fatalf("draft save --body-html: %v", err)
	}
	uid := fmt.Sprint(int(cli.decodeObject(t)["uid"].(float64)))

	cli.out.Reset()
	if err := Read(cli.g, []string{"--folder", "Drafts", "--peek", uid}); err != nil {
		t.Fatalf("read: %v", err)
	}
	msg := cli.decodeObject(t)

	if got, ok := msg["htmlBody"].(string); !ok || !strings.Contains(got, "<b>up</b>") {
		t.Errorf("htmlBody = %v, want the markup as given", msg["htmlBody"])
	}
	text, _ := msg["textBody"].(string)
	if !strings.Contains(text, "Revenue is up & to the right") {
		t.Errorf("textBody = %q, want prose derived from the HTML", text)
	}
}

// Both halves given means both halves sent, untouched.
func TestDraft_SaveWithBothBodies(t *testing.T) {
	cli := newTestCLI(t)

	err := Draft(cli.g, []string{"save", "--to", "bob@example.com", "--subject", "Report",
		"--body", "Revenue is up.", "--body-html", "<p>Revenue is <b>up</b></p>"})
	if err != nil {
		t.Fatalf("draft save: %v", err)
	}
	uid := fmt.Sprint(int(cli.decodeObject(t)["uid"].(float64)))

	cli.out.Reset()
	if err := Read(cli.g, []string{"--folder", "Drafts", "--peek", uid}); err != nil {
		t.Fatalf("read: %v", err)
	}
	msg := cli.decodeObject(t)

	if text, _ := msg["textBody"].(string); !strings.Contains(text, "Revenue is up.") {
		t.Errorf("textBody = %q, want the text that was given", text)
	}
	if got, _ := msg["htmlBody"].(string); !strings.Contains(got, "<b>up</b>") {
		t.Errorf("htmlBody = %q, want the markup that was given", got)
	}
}

// The point of the approval keyword is that something can come back later and
// ask what is waiting. Until --keyword existed, nothing could.
func TestSearch_FindsDraftsAwaitingApproval(t *testing.T) {
	cli := newTestCLI(t)

	if err := Draft(cli.g, []string{"save", "--approval", "--to", "boss@example.com", "--subject", "Needs a look", "--body", "Text"}); err != nil {
		t.Fatalf("draft save --approval: %v", err)
	}
	cli.out.Reset()
	if err := Draft(cli.g, []string{"save", "--to", "bob@example.com", "--subject", "Ordinary", "--body", "Text"}); err != nil {
		t.Fatalf("draft save: %v", err)
	}
	cli.out.Reset()

	if err := Search(cli.g, []string{"--folder", "Drafts", "--keyword", "$PendingApproval"}); err != nil {
		t.Fatalf("search --keyword: %v", err)
	}

	results := cli.decodeArray(t)
	if len(results) != 1 {
		t.Fatalf("found %d drafts, want only the one awaiting approval: %v", len(results), results)
	}
	if results[0]["subject"] != "Needs a look" {
		t.Errorf("found %v", results[0]["subject"])
	}
}

// The keyword was advisory until now: anything could send a draft that was
// still waiting on a human.
func TestDraftSend_RefusesADraftAwaitingApproval(t *testing.T) {
	cli := newTestCLI(t)

	if err := Draft(cli.g, []string{"save", "--approval", "--to", "boss@example.com", "--subject", "Needs a look", "--body", "Text"}); err != nil {
		t.Fatalf("draft save --approval: %v", err)
	}
	uid := fmt.Sprint(int(cli.decodeObject(t)["uid"].(float64)))
	cli.out.Reset()

	err := Draft(cli.g, []string{"send", uid})
	if err == nil {
		t.Fatal("sent a draft that was still awaiting approval")
	}
	if !errors.Is(err, mail.ErrPendingApproval) {
		t.Errorf("error %v should wrap mail.ErrPendingApproval", err)
	}
	// The way out has to be in the message, or the gate is just a wall.
	if !strings.Contains(err.Error(), "approve") || !strings.Contains(err.Error(), "--force") {
		t.Errorf("error %q should name both ways forward", err)
	}
	if cli.out.Len() != 0 {
		t.Errorf("wrote to stdout despite refusing: %q", cli.out.String())
	}
}

func TestDraftApprove_ClearsTheKeyword(t *testing.T) {
	cli := newTestCLI(t)

	if err := Draft(cli.g, []string{"save", "--approval", "--to", "boss@example.com", "--subject", "Needs a look", "--body", "Text"}); err != nil {
		t.Fatalf("draft save --approval: %v", err)
	}
	uid := fmt.Sprint(int(cli.decodeObject(t)["uid"].(float64)))
	cli.out.Reset()

	if err := Draft(cli.g, []string{"approve", uid}); err != nil {
		t.Fatalf("draft approve: %v", err)
	}
	approved := cli.decodeObject(t)
	if approved["status"] != "approved" {
		t.Errorf("status = %v", approved["status"])
	}
	cli.out.Reset()

	// The queue is empty again.
	if err := Search(cli.g, []string{"--folder", "Drafts", "--keyword", "$PendingApproval"}); err != nil {
		t.Fatalf("search: %v", err)
	}
	if results := cli.decodeArray(t); len(results) != 0 {
		t.Errorf("still awaiting approval: %v", results)
	}
}

func TestDraftApprove_UnknownUIDIsNotFound(t *testing.T) {
	cli := newTestCLI(t)
	cli.seed(t, "Drafts")

	err := Draft(cli.g, []string{"approve", "9999"})
	if err == nil {
		t.Fatal("approved a draft that does not exist")
	}
	if !errors.Is(err, mail.ErrNotFound) {
		t.Errorf("error %v should wrap mail.ErrNotFound", err)
	}
}

func TestDraftSend_ForceOverridesTheGate(t *testing.T) {
	cli := newTestCLI(t)

	if err := Draft(cli.g, []string{"save", "--approval", "--to", "boss@example.com", "--subject", "Urgent", "--body", "Text"}); err != nil {
		t.Fatalf("draft save --approval: %v", err)
	}
	uid := fmt.Sprint(int(cli.decodeObject(t)["uid"].(float64)))
	cli.out.Reset()

	// SMTP points at a closed port, so the send fails at delivery. That is
	// past the gate, which is the whole point: the refusal above never gets
	// this far.
	err := Draft(cli.g, []string{"send", "--force", uid})
	if err == nil {
		t.Fatal("expected the send to fail at delivery")
	}
	if errors.Is(err, mail.ErrPendingApproval) {
		t.Errorf("--force did not get past the approval gate: %v", err)
	}
}

func TestSearch_RequiresACriterion(t *testing.T) {
	cli := newTestCLI(t)

	err := Search(cli.g, []string{"--folder", "INBOX"})
	if err == nil {
		t.Fatal("search with no criteria succeeded")
	}
	if !strings.HasPrefix(err.Error(), "usage:") {
		t.Errorf("error %q should be a usage error", err)
	}
	if !strings.Contains(err.Error(), "--keyword") {
		t.Errorf("the list of criteria in %q should mention --keyword", err)
	}
}

// Every command has to be able to say "no such message" the same way, since
// the code it maps to is what a caller branches on.
func TestCommands_MissingUIDIsNotFound(t *testing.T) {
	commands := map[string]func(*cmdutil.GlobalFlags, []string) error{
		"read":    Read,
		"delete":  Delete,
		"archive": Archive,
		"markread": func(g *cmdutil.GlobalFlags, args []string) error {
			return MarkRead(g, args)
		},
	}

	for name, run := range commands {
		t.Run(name, func(t *testing.T) {
			cli := newTestCLI(t)
			cli.seed(t, "INBOX", "Something else")

			err := run(cli.g, []string{"9999"})
			if err == nil {
				t.Fatal("acting on a missing uid succeeded")
			}
			if !errorIsNotFound(err) {
				t.Errorf("error %v should wrap mail.ErrNotFound", err)
			}
			if cli.out.Len() != 0 {
				t.Errorf("wrote to stdout despite failing: %q", cli.out.String())
			}
		})
	}
}

func TestCommands_MissingArgumentIsAUsageError(t *testing.T) {
	commands := map[string]func(*cmdutil.GlobalFlags, []string) error{
		"read":    Read,
		"delete":  Delete,
		"archive": Archive,
		"move":    Move,
	}

	for name, run := range commands {
		t.Run(name, func(t *testing.T) {
			cli := newTestCLI(t)

			err := run(cli.g, nil)
			if err == nil {
				t.Fatal("running with no arguments succeeded")
			}
			// The prefix is what main turns into exit code 2.
			if len(err.Error()) < 6 || err.Error()[:6] != "usage:" {
				t.Errorf("error %q should start with usage:", err)
			}
		})
	}
}

func errorIsNotFound(err error) bool {
	return errors.Is(err, mail.ErrNotFound)
}
