package mail

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// Send composes a message, delivers it via SMTP, and optionally saves to Sent.
func Send(ctx context.Context, account AccountConfig, imap *IMAPClient, opts SendOptions, saveToSent bool, logger *slog.Logger) error {
	composed, err := ComposeMessage(opts)
	if err != nil {
		return fmt.Errorf("composing message: %w", err)
	}

	recipients := collectRecipients(opts)
	if len(recipients) == 0 {
		return fmt.Errorf("no recipients specified: %w", ErrInvalidConfig)
	}
	if err := SmtpDeliver(ctx, account, opts.From.Address, composed, recipients, logger); err != nil {
		return err
	}

	if saveToSent && imap != nil {
		sentFolder, err := imap.FindSpecialFolder(ctx, "\\Sent")
		if err != nil {
			logger.Debug("sent folder not found, skipping save", "err", err)
			return nil
		}
		if _, err := imap.AppendMessage(ctx, sentFolder, composed, []string{"\\Seen"}); err != nil {
			logger.Debug("failed to save to sent", "err", err)
		}
	}

	return nil
}

// BuildReply constructs SendOptions for a reply to the given message.
// No I/O — returns SendOptions for Send().
func BuildReply(account AccountConfig, original *Message, body string, replyAll bool, quoteOriginal bool) SendOptions {
	inReplyTo, references := BuildReplyHeaders(original)

	to := []Address{original.From}

	var cc []Address
	if replyAll {
		myAddr := strings.ToLower(NormalizeAddress(account.Address))

		for _, a := range original.To {
			if strings.ToLower(NormalizeAddress(a.Address)) != myAddr {
				to = append(to, a)
			}
		}
		for _, a := range original.Cc {
			if strings.ToLower(NormalizeAddress(a.Address)) != myAddr {
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

// Archive moves messages to the "Archive" folder, creating it if needed. Batch UIDs.
func Archive(ctx context.Context, imap *IMAPClient, folder string, uids []uint32) error {
	if err := imap.EnsureFolder(ctx, "Archive"); err != nil {
		return fmt.Errorf("ensuring Archive folder: %w", err)
	}
	return imap.MoveMessages(ctx, uids, folder, "Archive")
}

// SaveDraft composes a message and saves it to the Drafts folder with \Draft flag.
// Extra keywords (e.g. "$PendingApproval") are stored as additional IMAP flags.
func SaveDraft(ctx context.Context, imap *IMAPClient, opts SendOptions, keywords ...string) (uint32, error) {
	composed, err := ComposeMessage(opts)
	if err != nil {
		return 0, fmt.Errorf("composing draft: %w", err)
	}

	draftsFolder, err := imap.FindSpecialFolder(ctx, "\\Drafts")
	if err != nil {
		draftsFolder = "Drafts"
		if err := imap.EnsureFolder(ctx, draftsFolder); err != nil {
			return 0, fmt.Errorf("ensuring Drafts folder: %w", err)
		}
	}

	flags := []string{"\\Draft", "\\Seen"}
	flags = append(flags, keywords...)

	uid, err := imap.AppendMessage(ctx, draftsFolder, composed, flags)
	if err != nil {
		return 0, fmt.Errorf("saving draft: %w", err)
	}

	return uid, nil
}

// SendDraft fetches a draft, delivers it, removes from Drafts, and saves to Sent.
func SendDraft(ctx context.Context, account AccountConfig, imap *IMAPClient, uid uint32, logger *slog.Logger) error {
	draftsFolder, err := imap.FindSpecialFolder(ctx, "\\Drafts")
	if err != nil {
		draftsFolder = "Drafts"
	}

	msg, err := imap.FetchMessage(ctx, draftsFolder, uid, true)
	if err != nil {
		return fmt.Errorf("fetching draft: %w", err)
	}

	// Re-compose from the parsed draft to ensure well-formed output
	opts := SendOptions{
		From:        msg.From,
		To:          msg.To,
		Cc:          msg.Cc,
		Subject:     msg.Subject,
		TextBody:    msg.TextBody,
		HTMLBody:    msg.HTMLBody,
		Attachments: msg.Attachments,
		InReplyTo:   msg.InReplyTo,
		References:  msg.References,
	}

	composed, err := ComposeMessage(opts)
	if err != nil {
		return fmt.Errorf("composing draft for send: %w", err)
	}

	recipients := collectRecipients(opts)
	if len(recipients) == 0 {
		return fmt.Errorf("draft has no recipients: %w", ErrInvalidConfig)
	}
	if err := SmtpDeliver(ctx, account, opts.From.Address, composed, recipients, logger); err != nil {
		return err
	}

	// Remove from Drafts
	if err := imap.DeleteMessage(ctx, draftsFolder, uid); err != nil {
		logger.Debug("failed to delete sent draft", "err", err)
	}

	// Save to Sent
	sentFolder, err := imap.FindSpecialFolder(ctx, "\\Sent")
	if err == nil {
		if _, err := imap.AppendMessage(ctx, sentFolder, composed, []string{"\\Seen"}); err != nil {
			logger.Debug("failed to save to sent", "err", err)
		}
	}

	return nil
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
