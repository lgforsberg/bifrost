package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
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
