package mail

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

const (
	testIMAPUser = "user@example.com"
	testIMAPPass = "secret"
)

// newTestIMAPClient starts an in-memory IMAP server and returns a client
// connected to it, plus the user so a test can seed mailboxes directly. The
// server speaks IMAP4rev2, which is what brings MOVE and UIDPLUS: the client
// needs both.
func newTestIMAPClient(t *testing.T, account AccountConfig) (*IMAPClient, *imapmemserver.User) {
	t.Helper()

	user := imapmemserver.NewUser(testIMAPUser, testIMAPPass)
	if err := user.Create("INBOX", nil); err != nil {
		t.Fatalf("creating INBOX: %v", err)
	}

	mem := imapmemserver.New()
	mem.AddUser(user)

	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return mem.NewSession(), nil, nil
		},
		Caps:         imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapIMAP4rev2: {}},
		InsecureAuth: true,
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	host, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("splitting listener address: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parsing port: %v", err)
	}

	account.Address = testIMAPUser
	account.Password = testIMAPPass
	account.IMAPHost = host
	account.IMAPPort = port
	account.IMAPEncryption = "none"

	client := NewIMAPClient(account, discardLogger())
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("connecting to the test server: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	return client, user
}

// appendTestMessage puts a minimal message in a mailbox and returns its UID.
func appendTestMessage(t *testing.T, c *IMAPClient, folder, subject string) uint32 {
	t.Helper()

	raw := "From: alice@example.com\r\n" +
		"To: " + testIMAPUser + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"\r\n" +
		"Body of " + subject + "\r\n"

	uid, err := c.AppendMessage(context.Background(), folder, []byte(raw), []string{"\\Seen"})
	if err != nil {
		t.Fatalf("appending to %s: %v", folder, err)
	}
	return uid
}

func folderNames(t *testing.T, c *IMAPClient) []string {
	t.Helper()

	folders, err := c.ListFolders(context.Background())
	if err != nil {
		t.Fatalf("listing folders: %v", err)
	}
	names := make([]string, len(folders))
	for i, f := range folders {
		names[i] = f.Name
	}
	return names
}

func TestIMAP_AppendAndFetchRoundTrip(t *testing.T) {
	client, _ := newTestIMAPClient(t, AccountConfig{})
	ctx := context.Background()

	uid := appendTestMessage(t, client, "INBOX", "Hello")

	msg, err := client.FetchMessage(ctx, "INBOX", uid, true)
	if err != nil {
		t.Fatalf("FetchMessage: %v", err)
	}
	if msg.Subject != "Hello" {
		t.Errorf("Subject = %q, want Hello", msg.Subject)
	}
	if msg.From.Address != "alice@example.com" {
		t.Errorf("From = %q", msg.From.Address)
	}
}

// T-008: delete moves to Trash rather than expunging.
func TestTrashMessages_MovesOutOfTheSourceFolder(t *testing.T) {
	client, _ := newTestIMAPClient(t, AccountConfig{})
	ctx := context.Background()

	uid := appendTestMessage(t, client, "INBOX", "Doomed")

	movedTo, err := TrashMessages(ctx, client, "INBOX", []uint32{uid})
	if err != nil {
		t.Fatalf("TrashMessages: %v", err)
	}
	if movedTo != "Trash" {
		t.Errorf("moved to %q, want Trash", movedTo)
	}

	remaining, err := client.CheckUIDsExist(ctx, "INBOX", []uint32{uid})
	if err != nil {
		t.Fatalf("CheckUIDsExist: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("the message is still in INBOX: %v", remaining)
	}

	envelopes, err := client.ListEnvelopes(ctx, "Trash", 10, 0)
	if err != nil {
		t.Fatalf("listing Trash: %v", err)
	}
	if len(envelopes) != 1 || envelopes[0].Subject != "Doomed" {
		t.Errorf("Trash holds %+v, want the one message", envelopes)
	}
}

// Deleting out of Trash has nowhere further to go, so it expunges.
func TestTrashMessages_FromTrashExpunges(t *testing.T) {
	client, _ := newTestIMAPClient(t, AccountConfig{})
	ctx := context.Background()

	if err := client.EnsureFolder(ctx, "Trash"); err != nil {
		t.Fatalf("creating Trash: %v", err)
	}
	uid := appendTestMessage(t, client, "Trash", "Already binned")

	movedTo, err := TrashMessages(ctx, client, "Trash", []uint32{uid})
	if err != nil {
		t.Fatalf("TrashMessages: %v", err)
	}
	if movedTo != "" {
		t.Errorf("reported a move to %q, want an expunge", movedTo)
	}

	remaining, err := client.CheckUIDsExist(ctx, "Trash", []uint32{uid})
	if err != nil {
		t.Fatalf("CheckUIDsExist: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("the message survived the expunge: %v", remaining)
	}
}

// T-009: Archive is created on a server that has none, and mail lands in it.
func TestArchive_CreatesTheFolderWhenAbsent(t *testing.T) {
	client, _ := newTestIMAPClient(t, AccountConfig{})
	ctx := context.Background()

	uid := appendTestMessage(t, client, "INBOX", "Filed away")

	if err := Archive(ctx, client, "INBOX", []uint32{uid}); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	envelopes, err := client.ListEnvelopes(ctx, "Archive", 10, 0)
	if err != nil {
		t.Fatalf("listing Archive: %v", err)
	}
	if len(envelopes) != 1 {
		t.Fatalf("Archive holds %d messages, want 1", len(envelopes))
	}
	if envelopes[0].Subject != "Filed away" {
		t.Errorf("Archive holds %q", envelopes[0].Subject)
	}
}

// T-010: a configured override decides the folder, without the server needing
// to advertise anything.
func TestArchive_UsesTheConfiguredFolder(t *testing.T) {
	client, _ := newTestIMAPClient(t, AccountConfig{ArchiveFolder: "Arkiv"})
	ctx := context.Background()

	uid := appendTestMessage(t, client, "INBOX", "Filed away")

	if err := Archive(ctx, client, "INBOX", []uint32{uid}); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	envelopes, err := client.ListEnvelopes(ctx, "Arkiv", 10, 0)
	if err != nil {
		t.Fatalf("listing Arkiv: %v", err)
	}
	if len(envelopes) != 1 {
		t.Errorf("Arkiv holds %d messages, want 1", len(envelopes))
	}

	for _, name := range folderNames(t, client) {
		if name == "Archive" {
			t.Error("an English-named Archive folder was created alongside the configured one")
		}
	}
}

func TestFindSpecialFolder_OverrideSkipsTheServer(t *testing.T) {
	client, _ := newTestIMAPClient(t, AccountConfig{SentFolder: "Skickat"})

	// Nothing named Skickat exists: an override is taken at its word, and the
	// caller decides what to do about a folder that is not there.
	got, err := client.FindSpecialFolder(context.Background(), "\\Sent")
	if err != nil {
		t.Fatalf("FindSpecialFolder: %v", err)
	}
	if got != "Skickat" {
		t.Errorf("resolved to %q, want the configured Skickat", got)
	}
}

func TestFindSpecialFolder_UnknownAttributeIsNotFound(t *testing.T) {
	client, _ := newTestIMAPClient(t, AccountConfig{})

	_, err := client.FindSpecialFolder(context.Background(), "\\Sent")
	if err == nil {
		t.Fatal("resolved \\Sent on a server with only an INBOX")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error %v should report not found", err)
	}
}

// smtpAccount points an account at the test SMTP server, leaving IMAP to the
// client passed alongside it.
func smtpAccount(srv *testSMTPServer) AccountConfig {
	return AccountConfig{
		Address:        testIMAPUser,
		Password:       testIMAPPass,
		SMTPHost:       srv.host,
		SMTPPort:       srv.port,
		SMTPEncryption: "none",
	}
}

func testSendOptions(subject string) SendOptions {
	return SendOptions{
		From:     Address{Address: testIMAPUser},
		To:       []Address{{Address: "bob@example.com"}},
		Subject:  subject,
		TextBody: "Body of " + subject,
	}
}

func TestSend_FilesACopyInSent(t *testing.T) {
	smtpSrv := newTestSMTPServer(t, smtpFailures{})
	client, user := newTestIMAPClient(t, AccountConfig{})
	ctx := context.Background()

	if err := user.Create("Sent", nil); err != nil {
		t.Fatalf("creating Sent: %v", err)
	}

	res, err := Send(ctx, smtpAccount(smtpSrv), client, testSendOptions("Filed"), true, discardLogger())
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", res.Warnings)
	}

	envelopes, err := client.ListEnvelopes(ctx, "Sent", 10, 0)
	if err != nil {
		t.Fatalf("listing Sent: %v", err)
	}
	if len(envelopes) != 1 {
		t.Fatalf("Sent holds %d messages, want 1", len(envelopes))
	}
	if envelopes[0].Subject != "Filed" {
		t.Errorf("Sent holds %q", envelopes[0].Subject)
	}
}

// T-006's reason for existing: the message is gone, so the copy failing is a
// warning and not an error. Returning one would invite a retry and a second
// delivery.
func TestSend_WarnsWhenTheSentCopyFails(t *testing.T) {
	smtpSrv := newTestSMTPServer(t, smtpFailures{})
	client, _ := newTestIMAPClient(t, AccountConfig{})

	// The server has only an INBOX, and nothing creates a Sent folder.
	res, err := Send(context.Background(), smtpAccount(smtpSrv), client, testSendOptions("Nowhere to file"), true, discardLogger())
	if err != nil {
		t.Fatalf("Send failed over a copy that could not be filed: %v", err)
	}
	if res.MessageID == "" {
		t.Error("no message id, so the caller cannot tell what went out")
	}
	if len(res.Warnings) != 1 {
		t.Fatalf("warnings = %v, want one about the Sent copy", res.Warnings)
	}
	if !strings.Contains(res.Warnings[0], "delivered") {
		t.Errorf("warning %q should say the message was delivered anyway", res.Warnings[0])
	}
}

func TestDraft_SaveThenSendLeavesNothingBehind(t *testing.T) {
	smtpSrv := newTestSMTPServer(t, smtpFailures{})
	client, user := newTestIMAPClient(t, AccountConfig{})
	ctx := context.Background()

	if err := user.Create("Sent", nil); err != nil {
		t.Fatalf("creating Sent: %v", err)
	}

	uid, err := SaveDraft(ctx, client, testSendOptions("Later"))
	if err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	res, err := SendDraft(ctx, smtpAccount(smtpSrv), client, uid, discardLogger())
	if err != nil {
		t.Fatalf("SendDraft: %v", err)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", res.Warnings)
	}

	remaining, err := client.CheckUIDsExist(ctx, "Drafts", []uint32{uid})
	if err != nil {
		t.Fatalf("CheckUIDsExist: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("the draft is still in Drafts after being sent: %v", remaining)
	}

	envelopes, err := client.ListEnvelopes(ctx, "Sent", 10, 0)
	if err != nil {
		t.Fatalf("listing Sent: %v", err)
	}
	if len(envelopes) != 1 || envelopes[0].Subject != "Later" {
		t.Errorf("Sent holds %+v, want the sent draft", envelopes)
	}
}

func TestFolderOperations(t *testing.T) {
	client, _ := newTestIMAPClient(t, AccountConfig{})
	ctx := context.Background()

	if err := client.CreateFolder(ctx, "Projects"); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	// The response code path from T-007, over the wire this time.
	err := client.CreateFolder(ctx, "Projects")
	if err == nil {
		t.Fatal("creating a folder twice succeeded")
	}
	if !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("error %v should report that the folder exists", err)
	}

	if err := client.RenameFolder(ctx, "Projects", "Work"); err != nil {
		t.Fatalf("RenameFolder: %v", err)
	}
	if err := client.DeleteFolder(ctx, "Work"); err != nil {
		t.Fatalf("DeleteFolder: %v", err)
	}

	err = client.DeleteFolder(ctx, "Work")
	if err == nil {
		t.Fatal("deleting a missing folder succeeded")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error %v should report not found", err)
	}
}
