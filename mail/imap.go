package mail

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

type IMAPClient struct {
	config AccountConfig
	logger *slog.Logger
	client *imapclient.Client

	// release stops the watcher that drops the connection on cancellation.
	release func()
}

func NewIMAPClient(config AccountConfig, logger *slog.Logger) *IMAPClient {
	return &IMAPClient{config: config, logger: logger}
}

func (c *IMAPClient) Connect(ctx context.Context) error {
	host := c.config.IMAPHost
	addr := net.JoinHostPort(host, strconv.Itoa(c.config.IMAPPort))
	c.logger.Debug("connecting to IMAP", "addr", addr, "encryption", c.config.IMAPEncryption)

	conn, err := dial(ctx, host, c.config.IMAPPort, c.config.IMAPEncryption)
	if err != nil {
		return err
	}
	// Watch from here rather than after login, so an interrupt lands during the
	// handshake too.
	release := closeOnCancel(ctx, conn)

	opts := &imapclient.Options{TLSConfig: tlsConfigFor(host)}
	var client *imapclient.Client
	if c.config.IMAPEncryption == "starttls" {
		client, err = imapclient.NewStartTLS(conn, opts)
		if err != nil {
			release()
			conn.Close()
			return fmt.Errorf("starttls %s: %w: %w", addr, err, ErrConnectionFailed)
		}
	} else {
		client = imapclient.New(conn, opts)
	}

	username := c.config.EffectiveUsername()
	if err := client.Login(username, c.config.Password).Wait(); err != nil {
		release()
		client.Close()
		// The server refusing the credentials is an auth failure; the exchange
		// breaking part way through is not, and saying so sends the caller off
		// checking a password that was never the problem.
		if !isStatusResponse(err) {
			return fmt.Errorf("login as %s: %w: %w", username, err, ErrConnectionFailed)
		}
		return fmt.Errorf("login as %s: %w: %w", username, err, ErrAuthFailed)
	}

	c.logger.Debug("IMAP connected and authenticated", "user", username)
	c.client = client
	c.release = release
	return nil
}

func (c *IMAPClient) Close() error {
	if c.release != nil {
		c.release()
		c.release = nil
	}
	if c.client == nil {
		return nil
	}
	if err := c.client.Logout().Wait(); err != nil {
		c.client.Close()
		return err
	}
	return c.client.Close()
}

func (c *IMAPClient) ListFolders(ctx context.Context) ([]Folder, error) {
	c.logger.Debug("listing folders")
	mailboxes, err := c.client.List("", "*", nil).Collect()
	if err != nil {
		return nil, fmt.Errorf("LIST: %w", err)
	}

	folders := make([]Folder, 0, len(mailboxes))
	for _, mbox := range mailboxes {
		attrs := make([]string, len(mbox.Attrs))
		for i, a := range mbox.Attrs {
			attrs[i] = string(a)
		}
		folders = append(folders, Folder{
			Name:       mbox.Mailbox,
			Delimiter:  string(mbox.Delim),
			Attributes: attrs,
		})
	}
	return folders, nil
}

func (c *IMAPClient) ListEnvelopes(ctx context.Context, folder string, limit, offset int) ([]Envelope, error) {
	page, err := c.ListEnvelopePage(ctx, folder, limit, offset)
	if err != nil {
		return nil, err
	}
	return page.Messages, nil
}

// ListEnvelopePage is ListEnvelopes plus the folder's message count, which
// SELECT reports anyway. Without it a caller cannot tell a full page from the
// last one.
func (c *IMAPClient) ListEnvelopePage(ctx context.Context, folder string, limit, offset int) (EnvelopePage, error) {
	c.logger.Debug("listing envelopes", "folder", folder, "limit", limit, "offset", offset)

	mbox, err := c.client.Select(folder, nil).Wait()
	if err != nil {
		if isNoSuchMailbox(err) {
			return EnvelopePage{}, fmt.Errorf("folder %q: %w", folder, ErrNotFound)
		}
		return EnvelopePage{}, fmt.Errorf("SELECT %s: %w", folder, err)
	}

	total := mbox.NumMessages
	page := EnvelopePage{Total: total, Limit: limit, Offset: offset, Messages: []Envelope{}}

	if total == 0 || limit <= 0 {
		return page, nil
	}

	end := int(total) - offset
	if end <= 0 {
		return page, nil
	}
	start := end - limit + 1
	if start < 1 {
		start = 1
	}

	seqSet := new(imap.SeqSet)
	seqSet.AddRange(uint32(start), uint32(end))

	fetchOpts := &imap.FetchOptions{
		Envelope:   true,
		Flags:      true,
		UID:        true,
		RFC822Size: true,
	}

	messages, err := c.client.Fetch(*seqSet, fetchOpts).Collect()
	if err != nil {
		return EnvelopePage{}, fmt.Errorf("FETCH envelopes: %w", err)
	}

	// Return newest-first
	envelopes := make([]Envelope, 0, len(messages))
	for i := len(messages) - 1; i >= 0; i-- {
		envelopes = append(envelopes, imapEnvelopeToEnvelope(messages[i]))
	}
	page.Messages = envelopes
	return page, nil
}

func (c *IMAPClient) FetchMessage(ctx context.Context, folder string, uid uint32, peek bool) (*Message, error) {
	source, env, err := c.fetchSource(ctx, folder, uid, peek)
	if err != nil {
		return nil, err
	}

	parsed, err := ParseMessage(bytes.NewReader(source))
	if err != nil {
		return nil, fmt.Errorf("parsing message uid %d: %w", uid, err)
	}
	parsed.Envelope = env
	return parsed, nil
}

// FetchRaw returns the message exactly as the server holds it, RFC 822 source
// and all. Nothing is parsed, which is the point: it archives as a .eml, it
// forwards whole, and when a message will not parse it is the only view that
// cannot be wrong about what arrived.
func (c *IMAPClient) FetchRaw(ctx context.Context, folder string, uid uint32, peek bool) ([]byte, error) {
	source, _, err := c.fetchSource(ctx, folder, uid, peek)
	return source, err
}

// fetchSource pulls one message's bytes along with the server's own envelope,
// which is the part FetchMessage trusts over anything it parses itself.
func (c *IMAPClient) fetchSource(ctx context.Context, folder string, uid uint32, peek bool) ([]byte, Envelope, error) {
	c.logger.Debug("fetching message", "folder", folder, "uid", uid, "peek", peek)

	if _, err := c.client.Select(folder, nil).Wait(); err != nil {
		return nil, Envelope{}, fmt.Errorf("SELECT %s: %w", folder, err)
	}

	bodySection := &imap.FetchItemBodySection{
		Peek: peek,
	}
	fetchOpts := &imap.FetchOptions{
		Envelope:    true,
		Flags:       true,
		UID:         true,
		RFC822Size:  true,
		BodySection: []*imap.FetchItemBodySection{bodySection},
	}

	uidSet := imap.UIDSetNum(imap.UID(uid))
	messages, err := c.client.Fetch(uidSet, fetchOpts).Collect()
	if err != nil {
		return nil, Envelope{}, fmt.Errorf("FETCH uid %d: %w", uid, err)
	}
	if len(messages) == 0 {
		return nil, Envelope{}, fmt.Errorf("message uid %d in %s: %w", uid, folder, ErrNotFound)
	}

	msg := messages[0]

	// Find the body section data in the buffer
	var body []byte
	for _, bs := range msg.BodySection {
		body = bs.Bytes
		break
	}
	if body == nil {
		return nil, Envelope{}, fmt.Errorf("no body returned for uid %d: %w", uid, ErrNotFound)
	}

	return body, imapEnvelopeToEnvelope(msg), nil
}

func (c *IMAPClient) DeleteMessages(ctx context.Context, folder string, uids []uint32) error {
	c.logger.Debug("deleting messages", "folder", folder, "uids", uids)

	if _, err := c.client.Select(folder, nil).Wait(); err != nil {
		return fmt.Errorf("SELECT %s: %w", folder, err)
	}

	uidSet := toUIDSet(uids)
	storeFlags := &imap.StoreFlags{
		Op:     imap.StoreFlagsAdd,
		Flags:  []imap.Flag{imap.FlagDeleted},
		Silent: true,
	}
	if err := c.client.Store(uidSet, storeFlags, nil).Close(); err != nil {
		return fmt.Errorf("STORE +\\Deleted: %w", err)
	}

	expungeCmd := c.client.UIDExpunge(uidSet)
	for expungeCmd.Next() != 0 {
	}
	if err := expungeCmd.Close(); err != nil {
		return fmt.Errorf("UID EXPUNGE: %w", err)
	}
	return nil
}

func (c *IMAPClient) DeleteMessage(ctx context.Context, folder string, uid uint32) error {
	return c.DeleteMessages(ctx, folder, []uint32{uid})
}

func (c *IMAPClient) MoveMessages(ctx context.Context, uids []uint32, from, to string) error {
	c.logger.Debug("moving messages", "from", from, "to", to, "uids", uids)

	if _, err := c.client.Select(from, nil).Wait(); err != nil {
		return fmt.Errorf("SELECT %s: %w", from, err)
	}

	uidSet := toUIDSet(uids)
	if _, err := c.client.Move(uidSet, to).Wait(); err != nil {
		return fmt.Errorf("MOVE to %s: %w", to, err)
	}
	return nil
}

func (c *IMAPClient) MoveMessage(ctx context.Context, uid uint32, from, to string) error {
	return c.MoveMessages(ctx, []uint32{uid}, from, to)
}

func (c *IMAPClient) MarkReadBatch(ctx context.Context, folder string, uids []uint32) error {
	return c.storeFlags(folder, uids, imap.StoreFlagsAdd, imap.FlagSeen)
}

func (c *IMAPClient) MarkRead(ctx context.Context, folder string, uid uint32) error {
	return c.MarkReadBatch(ctx, folder, []uint32{uid})
}

func (c *IMAPClient) MarkUnreadBatch(ctx context.Context, folder string, uids []uint32) error {
	return c.storeFlags(folder, uids, imap.StoreFlagsDel, imap.FlagSeen)
}

func (c *IMAPClient) MarkUnread(ctx context.Context, folder string, uid uint32) error {
	return c.MarkUnreadBatch(ctx, folder, []uint32{uid})
}

// FlagBatch sets \Flagged, the star every mail client puts next to a message
// worth coming back to. Search can already filter on it, which is what makes
// it useful as a marker rather than just decoration.
func (c *IMAPClient) FlagBatch(ctx context.Context, folder string, uids []uint32) error {
	return c.storeFlags(folder, uids, imap.StoreFlagsAdd, imap.FlagFlagged)
}

// UnflagBatch clears \Flagged. Clearing it where it was never set is not an
// error, which is what IMAP does.
func (c *IMAPClient) UnflagBatch(ctx context.Context, folder string, uids []uint32) error {
	return c.storeFlags(folder, uids, imap.StoreFlagsDel, imap.FlagFlagged)
}

// AddKeyword sets an IMAP keyword such as $PendingApproval on messages.
func (c *IMAPClient) AddKeyword(ctx context.Context, folder string, uids []uint32, keyword string) error {
	return c.storeFlags(folder, uids, imap.StoreFlagsAdd, imap.Flag(keyword))
}

// RemoveKeyword clears an IMAP keyword. Clearing one that was never set is not
// an error, which is what IMAP does.
func (c *IMAPClient) RemoveKeyword(ctx context.Context, folder string, uids []uint32, keyword string) error {
	return c.storeFlags(folder, uids, imap.StoreFlagsDel, imap.Flag(keyword))
}

// FetchFlags reads the flags of one message without pulling its body, for
// deciding what to do with a message rather than reading it.
func (c *IMAPClient) FetchFlags(ctx context.Context, folder string, uid uint32) ([]string, error) {
	if _, err := c.client.Select(folder, nil).Wait(); err != nil {
		return nil, fmt.Errorf("SELECT %s: %w", folder, err)
	}

	messages, err := c.client.Fetch(toUIDSet([]uint32{uid}), &imap.FetchOptions{
		Flags: true,
		UID:   true,
	}).Collect()
	if err != nil {
		return nil, fmt.Errorf("FETCH flags: %w", err)
	}
	if len(messages) == 0 {
		return nil, fmt.Errorf("message uid %d in %s: %w", uid, folder, ErrNotFound)
	}

	flags := make([]string, len(messages[0].Flags))
	for i, f := range messages[0].Flags {
		flags[i] = string(f)
	}
	return flags, nil
}

func (c *IMAPClient) storeFlags(folder string, uids []uint32, op imap.StoreFlagsOp, flag imap.Flag) error {
	if _, err := c.client.Select(folder, nil).Wait(); err != nil {
		return fmt.Errorf("SELECT %s: %w", folder, err)
	}
	storeFlags := &imap.StoreFlags{
		Op:     op,
		Flags:  []imap.Flag{flag},
		Silent: true,
	}
	if err := c.client.Store(toUIDSet(uids), storeFlags, nil).Close(); err != nil {
		return fmt.Errorf("STORE: %w", err)
	}
	return nil
}

// CheckUIDsExist returns which of the given UIDs actually exist in the folder.
func (c *IMAPClient) CheckUIDsExist(ctx context.Context, folder string, uids []uint32) ([]uint32, error) {
	if _, err := c.client.Select(folder, nil).Wait(); err != nil {
		if isNoSuchMailbox(err) {
			return nil, fmt.Errorf("folder %q: %w", folder, ErrNotFound)
		}
		return nil, fmt.Errorf("SELECT %s: %w", folder, err)
	}

	uidSet := toUIDSet(uids)
	fetchOpts := &imap.FetchOptions{UID: true}

	messages, err := c.client.Fetch(uidSet, fetchOpts).Collect()
	if err != nil {
		return nil, fmt.Errorf("FETCH UIDs: %w", err)
	}

	existing := make([]uint32, 0, len(messages))
	for _, msg := range messages {
		existing = append(existing, uint32(msg.UID))
	}
	return existing, nil
}

func (c *IMAPClient) CreateFolder(ctx context.Context, name string) error {
	c.logger.Debug("creating folder", "name", name)
	if err := c.client.Create(name, nil).Wait(); err != nil {
		return classifyFolderError("create", name, err)
	}
	return nil
}

func (c *IMAPClient) RenameFolder(ctx context.Context, oldName, newName string) error {
	c.logger.Debug("renaming folder", "old", oldName, "new", newName)
	if err := c.client.Rename(oldName, newName, nil).Wait(); err != nil {
		return classifyFolderError("rename", oldName, err)
	}
	return nil
}

func (c *IMAPClient) DeleteFolder(ctx context.Context, name string) error {
	c.logger.Debug("deleting folder", "name", name)
	if err := c.client.Delete(name).Wait(); err != nil {
		return classifyFolderError("delete", name, err)
	}
	return nil
}

func classifyFolderError(op, name string, err error) error {
	switch imapResponseCode(err) {
	case imap.ResponseCodeAlreadyExists:
		return fmt.Errorf("folder %q already exists: %w", name, ErrAlreadyExists)
	case imap.ResponseCodeNonExistent:
		return fmt.Errorf("folder %q does not exist: %w", name, ErrNotFound)
	}

	// Response codes are optional and plenty of servers answer with a bare NO,
	// so the wording is still worth a look before giving up.
	switch {
	case isNoSuchMailbox(err):
		return fmt.Errorf("folder %q does not exist: %w", name, ErrNotFound)
	case mentionsAlreadyExists(err):
		return fmt.Errorf("folder %q already exists: %w", name, ErrAlreadyExists)
	}
	return fmt.Errorf("folder %s %q: %w", op, name, err)
}

// imapResponseCode returns the code the server attached to a tagged NO or BAD,
// or the empty string if this is not a status response or carried no code.
func imapResponseCode(err error) imap.ResponseCode {
	var imapErr *imap.Error
	if errors.As(err, &imapErr) {
		return imapErr.Code
	}
	return ""
}

// isStatusResponse reports whether the server answered at all, as opposed to
// the exchange breaking underneath us.
func isStatusResponse(err error) bool {
	var imapErr *imap.Error
	return errors.As(err, &imapErr)
}

func (c *IMAPClient) AppendMessage(ctx context.Context, folder string, message []byte, flags []string) (uint32, error) {
	c.logger.Debug("appending message", "folder", folder, "size", len(message))

	imapFlags := make([]imap.Flag, len(flags))
	for i, f := range flags {
		imapFlags[i] = imap.Flag(f)
	}

	appendOpts := &imap.AppendOptions{
		Flags: imapFlags,
	}

	appendCmd := c.client.Append(folder, int64(len(message)), appendOpts)
	if _, err := appendCmd.Write(message); err != nil {
		return 0, fmt.Errorf("APPEND write: %w", err)
	}
	if err := appendCmd.Close(); err != nil {
		return 0, fmt.Errorf("APPEND close: %w", err)
	}
	data, err := appendCmd.Wait()
	if err != nil {
		return 0, fmt.Errorf("APPEND %s: %w", folder, err)
	}
	if data != nil {
		return uint32(data.UID), nil
	}
	return 0, nil
}

func (c *IMAPClient) Search(ctx context.Context, folder string, criteria SearchCriteria) ([]Envelope, error) {
	page, err := c.SearchPage(ctx, folder, criteria)
	if err != nil {
		return nil, err
	}
	return page.Messages, nil
}

// SearchPage is Search plus how many messages matched before the limit cut the
// result down. A search that quietly returns its limit looks the same as one
// that found exactly that many.
func (c *IMAPClient) SearchPage(ctx context.Context, folder string, criteria SearchCriteria) (EnvelopePage, error) {
	c.logger.Debug("searching", "folder", folder)

	if _, err := c.client.Select(folder, nil).Wait(); err != nil {
		return EnvelopePage{}, fmt.Errorf("SELECT %s: %w", folder, err)
	}

	sc := buildIMAPSearchCriteria(criteria)
	data, err := c.client.UIDSearch(sc, nil).Wait()
	if err != nil {
		return EnvelopePage{}, fmt.Errorf("UID SEARCH: %w", err)
	}

	allUIDs := data.AllUIDs()
	page := EnvelopePage{
		Total:    uint32(len(allUIDs)),
		Limit:    criteria.Limit,
		Messages: []Envelope{},
	}

	if len(allUIDs) == 0 {
		return page, nil
	}

	if criteria.Limit > 0 && len(allUIDs) > criteria.Limit {
		allUIDs = allUIDs[len(allUIDs)-criteria.Limit:]
	}

	uidSet := new(imap.UIDSet)
	for _, uid := range allUIDs {
		uidSet.AddNum(uid)
	}

	fetchOpts := &imap.FetchOptions{
		Envelope:   true,
		Flags:      true,
		UID:        true,
		RFC822Size: true,
	}

	messages, err := c.client.Fetch(*uidSet, fetchOpts).Collect()
	if err != nil {
		return EnvelopePage{}, fmt.Errorf("FETCH search results: %w", err)
	}

	envelopes := make([]Envelope, 0, len(messages))
	for i := len(messages) - 1; i >= 0; i-- {
		envelopes = append(envelopes, imapEnvelopeToEnvelope(messages[i]))
	}
	page.Messages = envelopes
	return page, nil
}

func (c *IMAPClient) FetchThread(ctx context.Context, folders []string, uid uint32) ([]Message, error) {
	c.logger.Debug("fetching thread", "folders", folders, "uid", uid)

	if len(folders) == 0 {
		folders = []string{"INBOX", "Sent"}
	}

	target, err := c.FetchMessage(ctx, folders[0], uid, true)
	if err != nil {
		return nil, fmt.Errorf("fetching target message: %w", err)
	}
	target.Folder = folders[0]

	messageIDs := make(map[string]bool)
	if target.MessageID != "" {
		messageIDs[target.MessageID] = true
	}
	for _, ref := range target.References {
		messageIDs[ref] = true
	}
	if target.InReplyTo != "" {
		messageIDs[target.InReplyTo] = true
	}

	if len(messageIDs) == 0 {
		return []Message{*target}, nil
	}

	seen := make(map[string]*Message)
	if target.MessageID != "" {
		seen[target.MessageID] = target
	}

	for _, folder := range folders {
		if ctx.Err() != nil {
			break
		}
		for mid := range messageIDs {
			if ctx.Err() != nil {
				break
			}
			if mid == "" {
				continue
			}
			c.searchAndCollect(ctx, folder, &imap.SearchCriteria{
				Header: []imap.SearchCriteriaHeaderField{{Key: "Message-ID", Value: mid}},
			}, seen)
			c.searchAndCollect(ctx, folder, &imap.SearchCriteria{
				Header: []imap.SearchCriteriaHeaderField{{Key: "References", Value: mid}},
			}, seen)
			c.searchAndCollect(ctx, folder, &imap.SearchCriteria{
				Header: []imap.SearchCriteriaHeaderField{{Key: "In-Reply-To", Value: mid}},
			}, seen)
		}
	}

	messages := make([]Message, 0, len(seen))
	for _, msg := range seen {
		messages = append(messages, *msg)
	}

	// Insertion sort by date ascending
	for i := 1; i < len(messages); i++ {
		for j := i; j > 0 && messages[j].Date.Before(messages[j-1].Date); j-- {
			messages[j], messages[j-1] = messages[j-1], messages[j]
		}
	}

	return messages, nil
}

func (c *IMAPClient) searchAndCollect(ctx context.Context, folder string, sc *imap.SearchCriteria, seen map[string]*Message) {
	if ctx.Err() != nil {
		return
	}
	if _, err := c.client.Select(folder, nil).Wait(); err != nil {
		c.logger.Debug("skipping folder for thread search", "folder", folder, "err", err)
		return
	}

	data, err := c.client.UIDSearch(sc, nil).Wait()
	if err != nil {
		c.logger.Debug("thread search failed", "folder", folder, "err", err)
		return
	}

	for _, uid := range data.AllUIDs() {
		msg, err := c.FetchMessage(ctx, folder, uint32(uid), true)
		if err != nil {
			c.logger.Debug("thread fetch failed", "folder", folder, "uid", uid, "err", err)
			continue
		}
		if msg.MessageID != "" {
			if _, exists := seen[msg.MessageID]; !exists {
				msg.Folder = folder
				seen[msg.MessageID] = msg
			}
		}
	}
}

func (c *IMAPClient) FindSpecialFolder(ctx context.Context, attr string) (string, error) {
	// An explicit override wins outright, including over what the server
	// advertises. It exists precisely for accounts where that is wrong, so
	// second-guessing it would defeat the point. Whether the folder exists is
	// left to the caller, which either creates it or fails naming it.
	if name := c.config.SpecialFolderOverride(attr); name != "" {
		return name, nil
	}

	folders, err := c.ListFolders(ctx)
	if err != nil {
		return "", err
	}
	if name, ok := matchSpecialFolder(folders, attr); ok {
		return name, nil
	}
	return "", fmt.Errorf("special folder %s: %w", attr, ErrNotFound)
}

// specialFolderFallbacks lists conventional names to try when the server does
// not advertise the attribute, in preference order.
var specialFolderFallbacks = map[string][]string{
	"\\Archive": {"Archive", "Archives"},
	"\\Sent":    {"Sent", "Sent Messages", "Sent Items"},
	"\\Drafts":  {"Drafts", "Draft"},
	"\\Trash":   {"Trash", "Deleted Items", "Deleted Messages"},
	"\\Junk":    {"Junk", "Spam", "Junk E-mail"},
}

// matchSpecialFolder resolves a special-use attribute to a folder name. What
// the server advertises always wins, because the conventional English names
// are wrong on any localized account.
func matchSpecialFolder(folders []Folder, attr string) (string, bool) {
	for _, f := range folders {
		for _, a := range f.Attributes {
			if strings.EqualFold(a, attr) {
				return f.Name, true
			}
		}
	}

	for _, name := range specialFolderFallbacks[attr] {
		for _, f := range folders {
			if strings.EqualFold(f.Name, name) {
				return f.Name, true
			}
		}
	}
	return "", false
}

// --- helpers ---

func toUIDSet(uids []uint32) imap.UIDSet {
	imapUIDs := make([]imap.UID, len(uids))
	for i, u := range uids {
		imapUIDs[i] = imap.UID(u)
	}
	return imap.UIDSetNum(imapUIDs...)
}

func imapEnvelopeToEnvelope(msg *imapclient.FetchMessageBuffer) Envelope {
	env := msg.Envelope

	from := Address{}
	if env != nil && len(env.From) > 0 {
		from = Address{
			Name:    env.From[0].Name,
			Address: env.From[0].Addr(),
		}
	}

	var to []Address
	if env != nil {
		for _, a := range env.To {
			to = append(to, Address{Name: a.Name, Address: a.Addr()})
		}
	}

	var cc []Address
	if env != nil {
		for _, a := range env.Cc {
			cc = append(cc, Address{Name: a.Name, Address: a.Addr()})
		}
	}

	flags := make([]string, len(msg.Flags))
	for i, f := range msg.Flags {
		flags[i] = string(f)
	}

	subject := ""
	var date time.Time
	if env != nil {
		subject = env.Subject
		date = env.Date
	}

	return Envelope{
		UID:     uint32(msg.UID),
		Subject: subject,
		From:    from,
		To:      to,
		Cc:      cc,
		Date:    date,
		Flags:   flags,
		Size:    uint32(msg.RFC822Size),
	}
}

func buildIMAPSearchCriteria(criteria SearchCriteria) *imap.SearchCriteria {
	sc := &imap.SearchCriteria{}

	if criteria.From != "" {
		sc.Header = append(sc.Header, imap.SearchCriteriaHeaderField{Key: "From", Value: criteria.From})
	}
	if criteria.To != "" {
		sc.Header = append(sc.Header, imap.SearchCriteriaHeaderField{Key: "To", Value: criteria.To})
	}
	if criteria.Subject != "" {
		sc.Header = append(sc.Header, imap.SearchCriteriaHeaderField{Key: "Subject", Value: criteria.Subject})
	}
	if criteria.Body != "" {
		sc.Body = []string{criteria.Body}
	}
	if criteria.Since != nil {
		sc.Since = *criteria.Since
	}
	if criteria.Before != nil {
		sc.Before = *criteria.Before
	}
	if criteria.Unseen {
		sc.NotFlag = append(sc.NotFlag, imap.FlagSeen)
	}
	if criteria.Flagged {
		sc.Flag = append(sc.Flag, imap.FlagFlagged)
	}
	for _, kw := range criteria.Keywords {
		sc.Flag = append(sc.Flag, imap.Flag(kw))
	}

	return sc
}

// isNoSuchMailbox reports whether the server said the mailbox is missing. The
// NONEXISTENT response code is the reliable signal; the wording below is the
// fallback for the many servers that do not send one.
func isNoSuchMailbox(err error) bool {
	if imapResponseCode(err) == imap.ResponseCodeNonExistent {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "no such mailbox") ||
		strings.Contains(s, "doesn't exist") ||
		strings.Contains(s, "not found") ||
		strings.Contains(s, "nonexistent")
}

// mentionsAlreadyExists is the wording fallback for ALREADYEXISTS.
func mentionsAlreadyExists(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "already exists")
}
