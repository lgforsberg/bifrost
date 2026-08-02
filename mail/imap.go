package mail

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"slices"
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

	conn, err := dial(ctx, host, c.config.IMAPPort, c.config.IMAPEncryption, c.config.Timeout)
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
		envelopes = append(envelopes, imapEnvelopeToEnvelope(messages[i], folder))
	}
	page.Messages = envelopes
	return page, nil
}

// FolderStatus asks the server how much a folder holds. STATUS does not
// select the mailbox and returns no message data, so this is the cheap way to
// ask "how many unread?" rather than listing envelopes and counting them.
func (c *IMAPClient) FolderStatus(ctx context.Context, folder string) (FolderStatus, error) {
	c.logger.Debug("reading folder status", "folder", folder)

	data, err := c.client.Status(folder, &imap.StatusOptions{
		NumMessages: true,
		NumUnseen:   true,
		UIDNext:     true,
		UIDValidity: true,
	}).Wait()
	if err != nil {
		if isNoSuchMailbox(err) {
			return FolderStatus{}, fmt.Errorf("folder %q: %w", folder, ErrNotFound)
		}
		return FolderStatus{}, fmt.Errorf("STATUS %s: %w", folder, err)
	}

	return FolderStatus{
		Name:        folder,
		Total:       data.NumMessages,
		Unseen:      data.NumUnseen,
		UIDNext:     uint32(data.UIDNext),
		UIDValidity: data.UIDValidity,
	}, nil
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

	return body, imapEnvelopeToEnvelope(msg, folder), nil
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

// SearchFoldersPage runs the same search over several folders and merges the
// results into one page, newest last, as a single-folder search returns them.
// Total counts every match across every folder, before the limit.
//
// Each result carries the folder it came from, which is not decoration: a UID
// only means something inside one mailbox, so a merged result cannot be acted
// on without it.
//
// The folders are searched in turn on the one connection, since IMAP has a
// single selected mailbox and there is nothing to run in parallel. The limit
// is applied twice, once per folder and once to the merge, so a search of
// twenty folders does not fetch twenty pages worth of envelopes to discard
// most of them.
func (c *IMAPClient) SearchFoldersPage(ctx context.Context, folders []string, criteria SearchCriteria) (EnvelopePage, error) {
	if len(folders) == 1 {
		return c.SearchPage(ctx, folders[0], criteria)
	}

	page := EnvelopePage{Limit: criteria.Limit, Messages: []Envelope{}}
	var total int64

	for _, folder := range folders {
		if err := ctx.Err(); err != nil {
			return EnvelopePage{}, err
		}

		found, err := c.SearchPage(ctx, folder, criteria)
		if err != nil {
			return EnvelopePage{}, fmt.Errorf("searching %s: %w", folder, err)
		}
		total += int64(found.Total)
		page.Messages = append(page.Messages, found.Messages...)
	}
	page.Total = clampToUint32(total)

	// Date is the only ordering that means anything across mailboxes, UIDs
	// being unrelated between them. Folder and UID break ties so that two runs
	// of the same search agree.
	slices.SortFunc(page.Messages, func(a, b Envelope) int {
		if c := a.Date.Compare(b.Date); c != 0 {
			return c
		}
		if c := strings.Compare(a.Folder, b.Folder); c != 0 {
			return c
		}
		return cmp.Compare(a.UID, b.UID)
	})

	if criteria.Limit > 0 && len(page.Messages) > criteria.Limit {
		page.Messages = page.Messages[len(page.Messages)-criteria.Limit:]
	}
	return page, nil
}

// SearchPage is Search plus how many messages matched before the limit cut the
// result down. A search that quietly returns its limit looks the same as one
// that found exactly that many.
func (c *IMAPClient) SearchPage(ctx context.Context, folder string, criteria SearchCriteria) (EnvelopePage, error) {
	c.logger.Debug("searching", "folder", folder)

	if _, err := c.client.Select(folder, nil).Wait(); err != nil {
		if isNoSuchMailbox(err) {
			return EnvelopePage{}, fmt.Errorf("folder %q: %w", folder, ErrNotFound)
		}
		return EnvelopePage{}, fmt.Errorf("SELECT %s: %w", folder, err)
	}

	sc := buildIMAPSearchCriteria(criteria)
	data, err := c.client.UIDSearch(sc, nil).Wait()
	if err != nil {
		return EnvelopePage{}, fmt.Errorf("UID SEARCH: %w", err)
	}

	allUIDs := data.AllUIDs()
	page := EnvelopePage{
		Total:    clampToUint32(int64(len(allUIDs))),
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
		envelopes = append(envelopes, imapEnvelopeToEnvelope(messages[i], folder))
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

	// The identifiers to search for first: what the target calls itself, and
	// everything it names as an ancestor. A message that carries none is a
	// thread of one, there being nothing to match anything else against.
	frontier := dedupeIDs(append(append([]string{target.MessageID}, target.References...), target.InReplyTo), nil)
	if len(frontier) == 0 {
		return []Message{*target}, nil
	}

	found := map[string]threadMember{}
	if target.MessageID != "" {
		found[target.MessageID] = threadMember{folder: folders[0], uid: uid}
	}

	// Each round searches for what the previous one turned up. A thread whose
	// members all carry the full ancestry is exhausted in one, but clients
	// that truncate References leave members reachable only through their
	// neighbours, and a single hop used to lose them.
	searched := map[string]bool{}
	for round := 0; round < maxThreadRounds && len(frontier) > 0; round++ {
		for _, id := range frontier {
			searched[id] = true
		}
		frontier = dedupeIDs(c.expandThread(ctx, folders, frontier, found), searched)

		if ctx.Err() != nil || len(found) >= maxThreadMessages {
			break
		}
	}

	return c.fetchThreadBodies(ctx, found, target), nil
}

// expandThread searches every folder for the identifiers in frontier, records
// where each match lives, and returns the identifiers those matches bring in.
//
// Discovery never fetches a body. What it needs is the envelope, which the
// server has already parsed, and the References header, which the envelope
// does not carry. The version before this fetched every hit in full,
// attachments included, and did it once per search that matched.
func (c *IMAPClient) expandThread(ctx context.Context, folders, frontier []string, found map[string]threadMember) []string {
	var next []string

	for _, folder := range folders {
		if ctx.Err() != nil || len(found) >= maxThreadMessages {
			return next
		}
		if _, err := c.client.Select(folder, nil).Wait(); err != nil {
			c.logger.Debug("skipping folder for thread search", "folder", folder, "err", err)
			continue
		}

		for _, id := range frontier {
			if ctx.Err() != nil || len(found) >= maxThreadMessages {
				return next
			}

			candidates, err := c.searchThreadHeaders(folder, id)
			if err != nil {
				c.logger.Debug("thread search failed", "folder", folder, "id", id, "err", err)
				continue
			}

			for _, cand := range candidates {
				// Without an identifier there is nothing to key the message
				// by and nothing to expand from, so it cannot take part.
				if cand.messageID == "" {
					continue
				}
				if _, already := found[cand.messageID]; already {
					continue
				}

				found[cand.messageID] = threadMember{folder: folder, uid: cand.uid}
				next = append(next, cand.messageID)
				next = append(next, cand.references...)

				if len(found) >= maxThreadMessages {
					c.logger.Debug("thread truncated", "limit", maxThreadMessages)
					return next
				}
			}
		}
	}
	return next
}

// searchThreadHeaders finds messages in the selected folder that mention id in
// any of the three headers that tie a thread together, and reads back only
// enough of each to carry on.
func (c *IMAPClient) searchThreadHeaders(folder, id string) ([]threadCandidate, error) {
	// Three searches rather than one with a nested OR. A SEARCH that returns
	// identifiers is cheap and the FETCH that follows is not, so the union is
	// what matters, and it is fetched once. Nesting OR is legal but not worth
	// depending on when the saving is a round trip.
	var uids []imap.UID
	seen := map[imap.UID]bool{}

	for _, header := range []string{"Message-Id", "References", "In-Reply-To"} {
		data, err := c.client.UIDSearch(&imap.SearchCriteria{
			Header: []imap.SearchCriteriaHeaderField{{Key: header, Value: id}},
		}, nil).Wait()
		if err != nil {
			return nil, fmt.Errorf("UID SEARCH %s in %s: %w", header, folder, err)
		}
		for _, u := range data.AllUIDs() {
			if !seen[u] {
				seen[u] = true
				uids = append(uids, u)
			}
		}
	}

	if len(uids) == 0 {
		return nil, nil
	}

	messages, err := c.client.Fetch(imap.UIDSetNum(uids...), &imap.FetchOptions{
		UID:      true,
		Envelope: true,
		BodySection: []*imap.FetchItemBodySection{{
			Specifier:    imap.PartSpecifierHeader,
			HeaderFields: []string{"References"},
			Peek:         true,
		}},
	}).Collect()
	if err != nil {
		return nil, fmt.Errorf("FETCH thread headers in %s: %w", folder, err)
	}

	candidates := make([]threadCandidate, 0, len(messages))
	for _, msg := range messages {
		cand := threadCandidate{uid: uint32(msg.UID)}
		if msg.Envelope != nil {
			cand.messageID = msg.Envelope.MessageID
			cand.references = append(cand.references, msg.Envelope.InReplyTo...)
		}
		for _, section := range msg.BodySection {
			cand.references = append(cand.references, referencesFrom(section.Bytes)...)
			break
		}
		candidates = append(candidates, cand)
	}
	return candidates, nil
}

// fetchThreadBodies pulls the full message for each member discovery located,
// oldest first. The target is already in hand and is not fetched twice.
func (c *IMAPClient) fetchThreadBodies(ctx context.Context, found map[string]threadMember, target *Message) []Message {
	messages := make([]Message, 0, len(found)+1)
	messages = append(messages, *target)

	for id, at := range found {
		if ctx.Err() != nil {
			break
		}
		if id == target.MessageID {
			continue
		}

		msg, err := c.FetchMessage(ctx, at.folder, at.uid, true)
		if err != nil {
			c.logger.Debug("thread fetch failed", "folder", at.folder, "uid", at.uid, "err", err)
			continue
		}
		msg.Folder = at.folder
		messages = append(messages, *msg)
	}

	// Identifier breaks the tie so that two runs of the same command agree,
	// which map iteration on its own would not give.
	slices.SortFunc(messages, func(a, b Message) int {
		if c := a.Date.Compare(b.Date); c != 0 {
			return c
		}
		return strings.Compare(a.MessageID, b.MessageID)
	})
	return messages
}

// dedupeIDs returns the non-empty identifiers in ids, each once, leaving out
// any that excluded already covers.
func dedupeIDs(ids []string, excluded map[string]bool) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))

	for _, id := range ids {
		if id == "" || seen[id] || excluded[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// SelectableFolders names every folder that can actually hold messages. It is
// ListFolders minus the ones a server reports \Noselect, which are containers
// for other folders and cannot be selected, let alone searched.
//
// Nothing else is filtered. A server that presents a virtual folder holding
// copies of everything, as Gmail's All Mail does, will have it listed here and
// a search across all folders will find each message twice: once where it
// lives and once there. Leaving it out would make "all folders" untrue in a
// way the caller could not see.
func (c *IMAPClient) SelectableFolders(ctx context.Context) ([]string, error) {
	folders, err := c.ListFolders(ctx)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(folders))
	for _, f := range folders {
		if slices.ContainsFunc(f.Attributes, func(a string) bool {
			return strings.EqualFold(a, "\\Noselect")
		}) {
			continue
		}
		names = append(names, f.Name)
	}
	return names, nil
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

func imapEnvelopeToEnvelope(msg *imapclient.FetchMessageBuffer, folder string) Envelope {
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
		Folder:  folder,
		Subject: subject,
		From:    from,
		To:      to,
		Cc:      cc,
		Date:    date,
		Flags:   flags,
		Size:    clampToUint32(msg.RFC822Size),
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

const (
	// maxThreadRounds bounds the reference expansion. It is a backstop rather
	// than the real limit: an identifier is only ever searched for once, so
	// the walk runs out of new ones and stops on its own. What it costs is
	// round trips, and a thread whose members carry the full ancestry, which
	// is what the standard asks for, is done in one round however high this
	// is. The rounds are spent on chains left by clients that truncate
	// References, where each message reaches only its neighbour and the walk
	// advances one hop in each direction per round.
	maxThreadRounds = 16

	// maxThreadMessages is the real bound on the work. It stops a mailing
	// list or a reference loop turning one command into an unbounded
	// download.
	maxThreadMessages = 200
)

// threadMember is where a message was found, before its body is fetched.
type threadMember struct {
	folder string
	uid    uint32
}

// threadCandidate is what discovery learns about a message without reading it:
// what it calls itself, and what it says came before.
type threadCandidate struct {
	uid        uint32
	messageID  string
	references []string
}

// clampToUint32 narrows a count or size the protocol hands over as a wider
// type. Nothing an IMAP server legitimately reports comes near the ceiling, so
// a value that does is a broken server or our own bug; saturating says the
// number was too large, where wrapping would produce a small one that looks
// entirely plausible. A negative, which only a broken server sends, reads as
// zero rather than as four billion.
func clampToUint32(n int64) uint32 {
	switch {
	case n < 0:
		return 0
	case n > math.MaxUint32:
		return math.MaxUint32
	default:
		return uint32(n)
	}
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
