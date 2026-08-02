package mail

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/lgforsberg/bifrost/internal/testimap"
)

const (
	testIMAPUser = "user@example.com"
	testIMAPPass = "secret"
)

func newTestIMAPClient(t *testing.T, account AccountConfig) (*IMAPClient, *imapmemserver.User) {
	t.Helper()
	return newTestIMAPClientWithHooks(t, account, testimap.Hooks{})
}

// newTestIMAPClientWithHooks starts an in-process IMAP server and returns a
// client connected to it, plus the user so a test can seed mailboxes directly.
func newTestIMAPClientWithHooks(t *testing.T, account AccountConfig, hooks testimap.Hooks) (*IMAPClient, *imapmemserver.User) {
	t.Helper()

	srv := testimap.Start(t, testIMAPUser, testIMAPPass, hooks)

	account.Address = testIMAPUser
	account.Password = testIMAPPass
	account.IMAPHost = srv.Host
	account.IMAPPort = srv.Port
	account.IMAPEncryption = "none"

	client := NewIMAPClient(account, discardLogger())
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("connecting to the test server: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	return client, srv.User
}

// appendTestMessage puts a minimal message in a mailbox and returns its UID.
func appendTestMessage(t *testing.T, c *IMAPClient, folder, subject string) uint32 {
	t.Helper()
	return appendTestMessageWithFlags(t, c, folder, subject, []string{"\\Seen"})
}

func appendTestMessageWithFlags(t *testing.T, c *IMAPClient, folder, subject string, flags []string) uint32 {
	t.Helper()

	raw := "From: alice@example.com\r\n" +
		"To: " + testIMAPUser + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"\r\n" +
		"Body of " + subject + "\r\n"

	return appendRawTestMessage(t, c, folder, raw, flags)
}

func appendRawTestMessage(t *testing.T, c *IMAPClient, folder, raw string, flags []string) uint32 {
	t.Helper()

	uid, err := c.AppendMessage(context.Background(), folder, []byte(raw), flags)
	if err != nil {
		t.Fatalf("appending to %s: %v", folder, err)
	}
	return uid
}

func subjectsOf(envelopes []Envelope) []string {
	subjects := make([]string, len(envelopes))
	for i, e := range envelopes {
		subjects[i] = e.Subject
	}
	return subjects
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

// send, reply and forward all honour saveToSent; draft send used to file a
// copy no matter what. The draft still has to leave Drafts either way, since
// declining to archive is not declining to send.
func TestSendDraftWithOptions_CanDeclineTheSentCopy(t *testing.T) {
	smtpSrv := newTestSMTPServer(t, smtpFailures{})
	client, user := newTestIMAPClient(t, AccountConfig{})
	ctx := context.Background()

	if err := user.Create("Sent", nil); err != nil {
		t.Fatalf("creating Sent: %v", err)
	}

	uid, err := SaveDraft(ctx, client, testSendOptions("Unfiled"))
	if err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	res, err := SendDraftWithOptions(ctx, smtpAccount(smtpSrv), client, uid,
		SendDraftOptions{SaveToSent: false}, discardLogger())
	if err != nil {
		t.Fatalf("SendDraftWithOptions: %v", err)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", res.Warnings)
	}

	envelopes, err := client.ListEnvelopes(ctx, "Sent", 10, 0)
	if err != nil {
		t.Fatalf("listing Sent: %v", err)
	}
	if len(envelopes) != 0 {
		t.Errorf("Sent holds %+v, want nothing filed", envelopes)
	}

	remaining, err := client.CheckUIDsExist(ctx, "Drafts", []uint32{uid})
	if err != nil {
		t.Fatalf("CheckUIDsExist: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("the draft is still in Drafts: %v", remaining)
	}
}

// The published signature has to keep behaving as it always did.
func TestSendDraft_StillFilesTheCopyByDefault(t *testing.T) {
	smtpSrv := newTestSMTPServer(t, smtpFailures{})
	client, user := newTestIMAPClient(t, AccountConfig{})
	ctx := context.Background()

	if err := user.Create("Sent", nil); err != nil {
		t.Fatalf("creating Sent: %v", err)
	}

	uid, err := SaveDraft(ctx, client, testSendOptions("Filed"))
	if err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	if _, err := SendDraft(ctx, smtpAccount(smtpSrv), client, uid, discardLogger()); err != nil {
		t.Fatalf("SendDraft: %v", err)
	}

	envelopes, err := client.ListEnvelopes(ctx, "Sent", 10, 0)
	if err != nil {
		t.Fatalf("listing Sent: %v", err)
	}
	if len(envelopes) != 1 || envelopes[0].Subject != "Filed" {
		t.Errorf("Sent holds %+v, want the sent draft", envelopes)
	}
}

// The attribute is what a real server sends and imapmemserver cannot, so
// until now only the pure matcher covered this. Here it comes off the wire.
func TestFindSpecialFolder_ReadsAttributesFromTheServer(t *testing.T) {
	client, _ := newTestIMAPClientWithHooks(t, AccountConfig{}, testimap.Hooks{
		Listing: []imap.ListData{
			{Mailbox: "INBOX", Delim: '/'},
			// A decoy: the conventional English name, on a mailbox that does
			// not claim to be the archive.
			{Mailbox: "Archive", Delim: '/'},
			{Mailbox: "Arkiv", Delim: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrArchive}},
			{Mailbox: "Skickat", Delim: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrSent}},
			{Mailbox: "Papperskorg", Delim: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrTrash}},
			{Mailbox: "Utkast", Delim: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrDrafts}},
		},
	})

	for attr, want := range map[string]string{
		"\\Archive": "Arkiv",
		"\\Sent":    "Skickat",
		"\\Trash":   "Papperskorg",
		"\\Drafts":  "Utkast",
	} {
		got, err := client.FindSpecialFolder(context.Background(), attr)
		if err != nil {
			t.Errorf("FindSpecialFolder(%s): %v", attr, err)
			continue
		}
		if got != want {
			t.Errorf("%s resolved to %q, want %q", attr, got, want)
		}
	}
}

// T-006's other warning path. The message is delivered and filed, and only the
// draft cannot be cleaned up, which is not worth failing a send over.
func TestSendDraft_WarnsWhenTheDraftWillNotDelete(t *testing.T) {
	smtpSrv := newTestSMTPServer(t, smtpFailures{})
	client, user := newTestIMAPClientWithHooks(t, AccountConfig{}, testimap.Hooks{RefuseDelete: true})
	ctx := context.Background()

	if err := user.Create("Sent", nil); err != nil {
		t.Fatalf("creating Sent: %v", err)
	}

	uid, err := SaveDraft(ctx, client, testSendOptions("Stuck"))
	if err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	res, err := SendDraft(ctx, smtpAccount(smtpSrv), client, uid, discardLogger())
	if err != nil {
		t.Fatalf("SendDraft failed over a draft it could not remove: %v", err)
	}
	if len(res.Warnings) != 1 {
		t.Fatalf("warnings = %v, want one about the draft", res.Warnings)
	}
	if !strings.Contains(res.Warnings[0], "sent") || !strings.Contains(res.Warnings[0], "Drafts") {
		t.Errorf("warning %q should say the message was sent and name the folder", res.Warnings[0])
	}

	remaining, err := client.CheckUIDsExist(ctx, "Drafts", []uint32{uid})
	if err != nil {
		t.Fatalf("CheckUIDsExist: %v", err)
	}
	if len(remaining) != 1 {
		t.Error("the warning claimed the draft was left behind, but it is gone")
	}

	envelopes, err := client.ListEnvelopes(ctx, "Sent", 10, 0)
	if err != nil {
		t.Fatalf("listing Sent: %v", err)
	}
	if len(envelopes) != 1 {
		t.Errorf("Sent holds %d messages, want the one that went out", len(envelopes))
	}
}

func TestSearch_Criteria(t *testing.T) {
	client, _ := newTestIMAPClient(t, AccountConfig{})
	ctx := context.Background()

	appendTestMessage(t, client, "INBOX", "Invoice 42")
	appendTestMessage(t, client, "INBOX", "Lunch on Friday")
	appendTestMessageWithFlags(t, client, "INBOX", "Unread thing", nil)

	tests := map[string]struct {
		criteria SearchCriteria
		want     []string
	}{
		"by subject": {
			criteria: SearchCriteria{Subject: "Invoice"},
			want:     []string{"Invoice 42"},
		},
		"by sender, newest first": {
			criteria: SearchCriteria{From: "alice@example.com"},
			want:     []string{"Unread thing", "Lunch on Friday", "Invoice 42"},
		},
		"unseen only": {
			criteria: SearchCriteria{Unseen: true},
			want:     []string{"Unread thing"},
		},
		"limit keeps the most recent": {
			criteria: SearchCriteria{From: "alice@example.com", Limit: 2},
			want:     []string{"Unread thing", "Lunch on Friday"},
		},
		"no match": {
			criteria: SearchCriteria{Subject: "nothing like this"},
			want:     []string{},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			envelopes, err := client.Search(ctx, "INBOX", tt.criteria)
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			if got := subjectsOf(envelopes); !slices.Equal(got, tt.want) {
				t.Errorf("found %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFetchThread_CollectsTheReply(t *testing.T) {
	client, _ := newTestIMAPClient(t, AccountConfig{})
	ctx := context.Background()

	original := "From: alice@example.com\r\n" +
		"To: " + testIMAPUser + "\r\n" +
		"Subject: Proposal\r\n" +
		"Message-ID: <first@example.com>\r\n" +
		"Date: Mon, 01 Jun 2026 09:00:00 +0000\r\n" +
		"\r\nThe proposal.\r\n"
	reply := "From: " + testIMAPUser + "\r\n" +
		"To: alice@example.com\r\n" +
		"Subject: Re: Proposal\r\n" +
		"Message-ID: <second@example.com>\r\n" +
		"In-Reply-To: <first@example.com>\r\n" +
		"References: <first@example.com>\r\n" +
		"Date: Mon, 01 Jun 2026 10:00:00 +0000\r\n" +
		"\r\nAgreed.\r\n"

	uid := appendRawTestMessage(t, client, "INBOX", original, []string{"\\Seen"})
	appendRawTestMessage(t, client, "INBOX", reply, []string{"\\Seen"})

	thread, err := client.FetchThread(ctx, []string{"INBOX"}, uid)
	if err != nil {
		t.Fatalf("FetchThread: %v", err)
	}
	if len(thread) != 2 {
		t.Fatalf("thread has %d messages, want 2", len(thread))
	}
	// Oldest first, so a reader follows the conversation forwards.
	if thread[0].Subject != "Proposal" || thread[1].Subject != "Re: Proposal" {
		t.Errorf("thread reads %q then %q", thread[0].Subject, thread[1].Subject)
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
