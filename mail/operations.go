package mail

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// Send composes a message, delivers it via SMTP, and optionally saves to Sent.
// A failure to file the Sent copy is reported in the result, not returned: the
// message is already delivered by then.
func Send(ctx context.Context, account AccountConfig, imap *IMAPClient, opts SendOptions, saveToSent bool, logger *slog.Logger) (SendResult, error) {
	// Pinned up front so the delivered bytes and the archived Sent copy, which
	// are composed separately, share one Message-ID.
	if opts.MessageID == "" {
		opts.MessageID = messageIDFor(opts.From.Address)
	}
	result := SendResult{MessageID: opts.MessageID}

	composed, err := ComposeMessage(opts)
	if err != nil {
		return result, fmt.Errorf("composing message: %w", err)
	}

	recipients := collectRecipients(opts)
	if len(recipients) == 0 {
		return result, fmt.Errorf("no recipients specified: %w", ErrInvalidConfig)
	}
	if err := SmtpDeliver(ctx, account, opts.From.Address, composed, recipients, logger); err != nil {
		return result, err
	}

	if saveToSent && imap != nil {
		result.Warnings = append(result.Warnings, appendToSent(ctx, imap, opts, composed)...)
	}

	return result, nil
}

// appendToSent files a copy of a delivered message in the Sent folder and
// describes anything that went wrong instead of failing, since nothing here
// can un-send the message. The archived copy keeps the Bcc header that the
// delivered bytes omit, so the sender retains a record of who was blind-copied.
func appendToSent(ctx context.Context, imap *IMAPClient, opts SendOptions, delivered []byte) []string {
	sentFolder, err := imap.FindSpecialFolder(ctx, "\\Sent")
	if err != nil {
		return []string{fmt.Sprintf("message was delivered, but no Sent folder was found to file a copy in: %v", err)}
	}

	var warnings []string
	archived := delivered
	if len(opts.Bcc) > 0 {
		withBcc, err := composeMessage(opts, true)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("the copy filed in %s does not record the Bcc recipients: %v", sentFolder, err))
		} else {
			archived = withBcc
		}
	}

	if _, err := imap.AppendMessage(ctx, sentFolder, archived, []string{"\\Seen"}); err != nil {
		warnings = append(warnings, fmt.Sprintf("message was delivered, but the copy could not be filed in %s: %v", sentFolder, err))
	}
	return warnings
}

// BuildReply constructs SendOptions for a reply to the given message.
// No I/O — returns SendOptions for Send().
func BuildReply(account AccountConfig, original *Message, body string, replyAll bool, quoteOriginal bool) SendOptions {
	inReplyTo, references := BuildReplyHeaders(original)

	to := replyTargets(original)

	// Addressing the same person twice is harmless but looks careless, and
	// reply-all readily produces it: the sender is often in To as well.
	addressed := make(map[string]bool, len(to))
	for _, a := range to {
		addressed[strings.ToLower(NormalizeAddress(a.Address))] = true
	}

	var cc []Address
	if replyAll {
		addressed[strings.ToLower(NormalizeAddress(account.Address))] = true

		for _, a := range original.To {
			key := strings.ToLower(NormalizeAddress(a.Address))
			if !addressed[key] {
				addressed[key] = true
				to = append(to, a)
			}
		}
		for _, a := range original.Cc {
			key := strings.ToLower(NormalizeAddress(a.Address))
			if !addressed[key] {
				addressed[key] = true
				cc = append(cc, a)
			}
		}
	}

	textBody := body
	if quoteOriginal {
		textBody = body + QuoteBody(original)
	}

	return SendOptions{
		From:       Address{Name: account.DisplayName, Address: account.Address},
		To:         to,
		Cc:         cc,
		Subject:    ReplySubject(original.Subject),
		TextBody:   textBody,
		InReplyTo:  inReplyTo,
		References: references,
	}
}

// replyTargets is where a reply is addressed. Reply-To wins when the sender
// set one, which is how mailing lists route answers back to the list and how
// send-as setups keep them off the mailbox that did the sending. It is a list,
// so all of it is honoured.
func replyTargets(original *Message) []Address {
	if len(original.ReplyTo) > 0 {
		return append([]Address(nil), original.ReplyTo...)
	}
	return []Address{original.From}
}

// BuildForward constructs SendOptions for forwarding a message.
// Carries original attachments. No I/O.
func BuildForward(account AccountConfig, original *Message, to []Address, body string) SendOptions {
	var textBody string
	if body != "" {
		textBody = body + "\n"
	}
	textBody += QuoteBody(original)

	return SendOptions{
		From:        Address{Name: account.DisplayName, Address: account.Address},
		To:          to,
		Subject:     ForwardSubject(original.Subject),
		TextBody:    textBody,
		Attachments: original.Attachments,
	}
}

// Archive moves messages to the server's archive folder. The \Archive
// special-use attribute decides where that is; only when the server advertises
// nothing does this fall back to a folder literally named Archive, creating it
// if needed. Batch UIDs.
func Archive(ctx context.Context, imap *IMAPClient, folder string, uids []uint32) error {
	target, err := resolveOrCreate(ctx, imap, "\\Archive", "Archive")
	if err != nil {
		return err
	}
	return imap.MoveMessages(ctx, uids, folder, target)
}

// resolveOrCreate names the folder for a special-use attribute and makes sure
// it is there. A name the server listed exists by definition; a configured
// override or a fallback guess may not, and the commands that use this create
// their destination rather than refusing to file anything.
func resolveOrCreate(ctx context.Context, imap *IMAPClient, attr, fallback string) (string, error) {
	name := imap.config.SpecialFolderOverride(attr)
	if name == "" {
		if resolved, err := imap.FindSpecialFolder(ctx, attr); err == nil {
			return resolved, nil
		}
		name = fallback
	}

	if err := imap.EnsureFolder(ctx, name); err != nil {
		return "", fmt.Errorf("ensuring folder %q for %s: %w", name, attr, err)
	}
	return name, nil
}

// TrashMessages moves messages to the server's Trash folder, which is what
// deleting mail normally means: recoverable until the trash is emptied. Trash
// is created if the server advertises none. Messages already in Trash have
// nowhere further to go and are expunged instead, in which case movedTo is
// empty. Batch UIDs.
func TrashMessages(ctx context.Context, imap *IMAPClient, folder string, uids []uint32) (movedTo string, err error) {
	trash, err := resolveOrCreate(ctx, imap, "\\Trash", "Trash")
	if err != nil {
		return "", err
	}

	if strings.EqualFold(folder, trash) {
		return "", imap.DeleteMessages(ctx, folder, uids)
	}
	return trash, imap.MoveMessages(ctx, uids, folder, trash)
}

// SaveDraft composes a message and saves it to the Drafts folder with \Draft flag.
// Extra keywords (e.g. KeywordPendingApproval) are stored as additional IMAP flags.
func SaveDraft(ctx context.Context, imap *IMAPClient, opts SendOptions, keywords ...string) (uint32, error) {
	// A stored draft keeps the Bcc header so the server-side copy records the
	// full recipient list.
	composed, err := composeMessage(opts, true)
	if err != nil {
		return 0, fmt.Errorf("composing draft: %w", err)
	}

	draftsFolder, err := resolveOrCreate(ctx, imap, "\\Drafts", "Drafts")
	if err != nil {
		return 0, err
	}

	flags := []string{"\\Draft", "\\Seen"}
	flags = append(flags, keywords...)

	uid, err := imap.AppendMessage(ctx, draftsFolder, composed, flags)
	if err != nil {
		return 0, fmt.Errorf("saving draft: %w", err)
	}

	return uid, nil
}

// SendDraft fetches a draft, delivers it, removes from Drafts, and saves to
// Sent. Failures in the two cleanup steps are reported in the result rather
// than returned, since the message has already gone out by then.
// SendDraftOptions carries the choices SendDraft cannot infer. It exists so
// this can grow without disturbing SendDraft's signature, which is published.
type SendDraftOptions struct {
	// SaveToSent files a copy in Sent after delivery, the same choice Send
	// takes as an argument. SendDraft, which predates this, always does.
	SaveToSent bool
}

// SendDraft delivers a draft and files a copy in Sent. Use
// SendDraftWithOptions to decide that second part.
func SendDraft(ctx context.Context, account AccountConfig, imap *IMAPClient, uid uint32, logger *slog.Logger) (SendResult, error) {
	return SendDraftWithOptions(ctx, account, imap, uid, SendDraftOptions{SaveToSent: true}, logger)
}

// SendDraftWithOptions delivers the draft at uid, removes it from Drafts, and
// files a copy in Sent if asked to. Steps after delivery are reported as
// warnings rather than errors: the message has gone out either way, and
// failing the call would invite a retry and a second copy.
func SendDraftWithOptions(ctx context.Context, account AccountConfig, imap *IMAPClient, uid uint32, sendOpts SendDraftOptions, logger *slog.Logger) (SendResult, error) {
	draftsFolder, err := imap.FindSpecialFolder(ctx, "\\Drafts")
	if err != nil {
		draftsFolder = "Drafts"
	}

	msg, err := imap.FetchMessage(ctx, draftsFolder, uid, true)
	if err != nil {
		return SendResult{}, fmt.Errorf("fetching draft: %w", err)
	}

	// Re-compose from the parsed draft to ensure well-formed output. The
	// Message-ID is pinned here so the delivered bytes and the archived Sent
	// copy, which are composed separately, share one identity.
	opts := SendOptions{
		From:        msg.From,
		To:          msg.To,
		Cc:          msg.Cc,
		Bcc:         msg.Bcc,
		Subject:     msg.Subject,
		TextBody:    msg.TextBody,
		HTMLBody:    msg.HTMLBody,
		Attachments: msg.Attachments,
		InReplyTo:   msg.InReplyTo,
		References:  msg.References,
		MessageID:   messageIDFor(msg.From.Address),
	}

	result := SendResult{MessageID: opts.MessageID}

	composed, err := ComposeMessage(opts)
	if err != nil {
		return result, fmt.Errorf("composing draft for send: %w", err)
	}

	recipients := collectRecipients(opts)
	if len(recipients) == 0 {
		return result, fmt.Errorf("draft has no recipients: %w", ErrInvalidConfig)
	}
	if err := SmtpDeliver(ctx, account, opts.From.Address, composed, recipients, logger); err != nil {
		return result, err
	}

	if err := imap.DeleteMessage(ctx, draftsFolder, uid); err != nil {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("message was sent, but the draft is still in %s: %v", draftsFolder, err))
	}

	if sendOpts.SaveToSent {
		result.Warnings = append(result.Warnings, appendToSent(ctx, imap, opts, composed)...)
	}

	return result, nil
}

func collectRecipients(opts SendOptions) []string {
	seen := make(map[string]bool)
	var recipients []string
	for _, lists := range [][]Address{opts.To, opts.Cc, opts.Bcc} {
		for _, a := range lists {
			addr := strings.ToLower(a.Address)
			if !seen[addr] {
				seen[addr] = true
				recipients = append(recipients, a.Address)
			}
		}
	}
	return recipients
}
