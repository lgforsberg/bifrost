package commands

import (
	"flag"
	"fmt"

	"github.com/lgforsberg/bifrost/internal/cmdutil"
	"github.com/lgforsberg/bifrost/internal/helpers"
	"github.com/lgforsberg/bifrost/internal/output"
	"github.com/lgforsberg/bifrost/mail"
)

func Draft(g *cmdutil.GlobalFlags, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: draft <save|update|list|send|approve|delete>")
	}

	sub := args[0]
	subArgs := args[1:]

	switch sub {
	case "save":
		return draftSave(g, subArgs)
	case "update":
		return draftUpdate(g, subArgs)
	case "list":
		return draftList(g, subArgs)
	case "send":
		return draftSend(g, subArgs)
	case "approve":
		return draftApprove(g, subArgs)
	case "delete":
		return draftDelete(g, subArgs)
	case "help", "--help", "-h":
		return g.Usage("usage: draft <save|update|list|send|approve|delete>")
	default:
		return fmt.Errorf("usage: unknown subcommand %q (save, update, list, send, approve, delete)", sub)
	}
}

// draftsFolderFor names the Drafts folder, falling back to the conventional
// name when the server advertises nothing. The fallback is not created here:
// every caller is about to act on a draft that is supposed to exist already,
// so an empty guess failing is the right outcome.
func draftsFolderFor(g *cmdutil.GlobalFlags, client *mail.IMAPClient) string {
	folder, err := client.FindSpecialFolder(g.Ctx, "\\Drafts")
	if err != nil {
		return "Drafts"
	}
	return folder
}

// draftContent is the flag set that describes a draft's contents. `save` and
// `update` compose the same message from the same flags and differ only in
// what becomes of it, so they register them from one place.
type draftContent struct {
	to, cc, bcc, attach helpers.StringSliceFlag
	from                *string
	subject             *string
	bodies              *helpers.BodyFlags
	approval            *bool
}

func registerDraftContent(fs *flag.FlagSet) *draftContent {
	d := &draftContent{}
	fs.Var(&d.to, "to", "recipient (repeatable)")
	fs.Var(&d.cc, "cc", "CC recipient (repeatable)")
	fs.Var(&d.bcc, "bcc", "BCC recipient (repeatable)")
	fs.Var(&d.attach, "attach", "attachment file path (repeatable)")
	d.from = fs.String("from", "", "override From address (e.g. user+tag@domain)")
	d.subject = fs.String("subject", "", "message subject")
	d.bodies = helpers.RegisterBodyFlags(fs, "message body")
	d.approval = fs.Bool("approval", false, "mark draft for approval ($PendingApproval keyword)")
	return d
}

// options builds the message the flags describe. Call it after fs.Parse.
func (d *draftContent) options(g *cmdutil.GlobalFlags) (mail.SendOptions, error) {
	body, htmlBody, err := d.bodies.Read()
	if err != nil {
		return mail.SendOptions{}, err
	}

	acct, err := helpers.ResolveAccount(g.Config, g.Account)
	if err != nil {
		return mail.SendOptions{}, err
	}

	attachments, err := loadAttachments(d.attach)
	if err != nil {
		return mail.SendOptions{}, err
	}

	from := mail.Address{Name: acct.DisplayName, Address: acct.Address}
	if *d.from != "" {
		from.Address = *d.from
	}

	return mail.SendOptions{
		From:        from,
		To:          parseAddressFlags(d.to),
		Cc:          parseAddressFlags(d.cc),
		Bcc:         parseAddressFlags(d.bcc),
		Subject:     *d.subject,
		TextBody:    body,
		HTMLBody:    htmlBody,
		Attachments: attachments,
	}, nil
}

func draftSave(g *cmdutil.GlobalFlags, args []string) error {
	fs := flag.NewFlagSet("draft save", flag.ContinueOnError)
	content := registerDraftContent(fs)
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: %w", err)
	}
	approval := content.approval

	opts, err := content.options(g)
	if err != nil {
		return err
	}

	client, _, err := helpers.ConnectIMAP(g.Ctx, g.Config, g.Account, g.Logger)
	if err != nil {
		return err
	}
	defer client.Close()

	var keywords []string
	if *approval {
		keywords = append(keywords, mail.KeywordPendingApproval)
	}

	uid, err := mail.SaveDraft(g.Ctx, client, opts, keywords...)
	if err != nil {
		return err
	}

	status := "saved"
	if *approval {
		status = "pending_approval"
	}

	if g.JSON {
		return output.PrintJSON(g.Out(), map[string]any{"status": status, "uid": uid})
	} else {
		if *approval {
			fmt.Fprintln(g.Out(), "Draft saved and marked for approval.")
		} else {
			fmt.Fprintln(g.Out(), "Draft saved.")
		}
	}
	return nil
}

// draftUpdate replaces a draft with a revised one. IMAP cannot alter a stored
// message, so this is an append followed by a delete rather than an edit, and
// the new draft gets a new UID.
//
// It takes the same flags as `save` and replaces the draft wholesale: what is
// not given is not carried over. Merging would need a rule for every field
// about what an absent flag means, and "the draft is what I just described" is
// the one an agent revising its own text already has the material for.
func draftUpdate(g *cmdutil.GlobalFlags, args []string) error {
	fs := flag.NewFlagSet("draft update", flag.ContinueOnError)
	content := registerDraftContent(fs)
	args = helpers.ReorderArgs(args, map[string]bool{"approval": true})
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: %w", err)
	}

	if fs.NArg() < 1 {
		return fmt.Errorf("usage: draft update [options] <uid>")
	}
	uids, err := helpers.ParseUIDs(fs.Args()[:1])
	if err != nil {
		return err
	}
	old := uids[0]

	opts, err := content.options(g)
	if err != nil {
		return err
	}

	client, _, err := helpers.ConnectIMAP(g.Ctx, g.Config, g.Account, g.Logger)
	if err != nil {
		return err
	}
	defer client.Close()

	folder := draftsFolderFor(g, client)

	// Reading the old draft's flags settles two things before anything is
	// written: that there is a draft at that UID to replace, and whether it
	// was waiting on approval.
	oldFlags, err := client.FetchFlags(g.Ctx, folder, old)
	if err != nil {
		return err
	}

	// A revision of a draft that was awaiting approval is still awaiting it.
	// Dropping the keyword here would let a revision walk out of the queue
	// that the original was put in.
	pending := *content.approval || mail.HasKeyword(oldFlags, mail.KeywordPendingApproval)

	var keywords []string
	if pending {
		keywords = append(keywords, mail.KeywordPendingApproval)
	}

	uid, err := mail.SaveDraft(g.Ctx, client, opts, keywords...)
	if err != nil {
		return err
	}

	// The revision is saved by this point. A failure to remove what it
	// replaced leaves two drafts, which is worth reporting but is not worth
	// failing over: retrying would append a third.
	var warnings []string
	if err := client.DeleteMessage(g.Ctx, folder, old); err != nil {
		warnings = append(warnings,
			fmt.Sprintf("the revision was saved as uid %d, but draft %d is still in %s: %v",
				uid, old, folder, err))
	}

	status := "updated"
	if pending {
		status = "pending_approval"
	}

	if g.JSON {
		result := map[string]any{"status": status, "uid": uid, "previousUid": old}
		if len(warnings) > 0 {
			result["warnings"] = warnings
		}
		return output.PrintJSON(g.Out(), result)
	}

	fmt.Fprintf(g.Out(), "Draft %d replaced by %d.\n", old, uid)
	if pending {
		fmt.Fprintln(g.Out(), "It is marked for approval.")
	}
	for _, w := range warnings {
		fmt.Fprintln(g.Err(), "warning:", w)
	}
	return nil
}

func draftList(g *cmdutil.GlobalFlags, args []string) error {
	fs := flag.NewFlagSet("draft list", flag.ContinueOnError)
	limit := fs.Int("limit", 20, "max number of drafts")
	offset := fs.Int("offset", 0, "skip first N drafts")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: %w", err)
	}

	client, _, err := helpers.ConnectIMAP(g.Ctx, g.Config, g.Account, g.Logger)
	if err != nil {
		return err
	}
	defer client.Close()

	draftsFolder := draftsFolderFor(g, client)

	envelopes, err := client.ListEnvelopes(g.Ctx, draftsFolder, *limit, *offset)
	if err != nil {
		return err
	}

	if g.JSON {
		return output.PrintJSON(g.Out(), envelopes)
	}

	if len(envelopes) == 0 {
		fmt.Fprintln(g.Out(), "No drafts.")
		return nil
	}

	headers := []string{"UID", "DATE", "TO", "SUBJECT"}
	rows := make([][]string, len(envelopes))
	for i, env := range envelopes {
		toStr := ""
		if len(env.To) > 0 {
			toStr = env.To[0].Address
		}
		rows[i] = []string{
			fmt.Sprintf("%d", env.UID),
			env.Date.Format("2006-01-02 15:04"),
			truncate(toStr, 30),
			truncate(env.Subject, 50),
		}
	}
	output.PrintTable(g.Out(), headers, rows)
	return nil
}

func draftSend(g *cmdutil.GlobalFlags, args []string) error {
	fs := flag.NewFlagSet("draft send", flag.ContinueOnError)
	force := fs.Bool("force", false, "send even if the draft is still awaiting approval")
	noSave := fs.Bool("no-save", false, "don't save to Sent")
	args = helpers.ReorderArgs(args, map[string]bool{"force": true, "no-save": true})
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: %w", err)
	}

	if fs.NArg() < 1 {
		return fmt.Errorf("usage: draft send [--force] [--no-save] <uid>")
	}

	uids, err := helpers.ParseUIDs(fs.Args()[:1])
	if err != nil {
		return err
	}

	acct, err := helpers.ResolveAccount(g.Config, g.Account)
	if err != nil {
		return err
	}

	client, _, err := helpers.ConnectIMAP(g.Ctx, g.Config, g.Account, g.Logger)
	if err != nil {
		return err
	}
	defer client.Close()

	draftsFolder := draftsFolderFor(g, client)

	if !*force {
		flags, err := client.FetchFlags(g.Ctx, draftsFolder, uids[0])
		if err != nil {
			return err
		}
		if mail.HasKeyword(flags, mail.KeywordPendingApproval) {
			return fmt.Errorf(
				"draft %d is awaiting approval: run 'draft approve %d' first, or 'draft send --force %d': %w",
				uids[0], uids[0], uids[0], mail.ErrPendingApproval)
		}
	}

	saveToSent := g.Config.Defaults.SaveToSent && !*noSave
	res, err := mail.SendDraftWithOptions(g.Ctx, *acct, client, uids[0],
		mail.SendDraftOptions{SaveToSent: saveToSent}, g.Logger)
	if err != nil {
		return err
	}

	return reportSend(g, res, "Draft sent.")
}

func draftApprove(g *cmdutil.GlobalFlags, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: draft approve <uid>")
	}

	uids, err := helpers.ParseUIDs(args[:1])
	if err != nil {
		return err
	}

	client, _, err := helpers.ConnectIMAP(g.Ctx, g.Config, g.Account, g.Logger)
	if err != nil {
		return err
	}
	defer client.Close()

	draftsFolder := draftsFolderFor(g, client)

	// Fetched first so approving a UID that is not there fails rather than
	// reporting success for a message that does not exist: STORE against a
	// missing UID is silently fine.
	if _, err := client.FetchFlags(g.Ctx, draftsFolder, uids[0]); err != nil {
		return err
	}

	if err := client.RemoveKeyword(g.Ctx, draftsFolder, uids[:1], mail.KeywordPendingApproval); err != nil {
		return err
	}

	if g.JSON {
		return output.PrintJSON(g.Out(), map[string]any{"status": "approved", "uid": uids[0]})
	}
	fmt.Fprintf(g.Out(), "Draft %d approved.\n", uids[0])
	return nil
}

func draftDelete(g *cmdutil.GlobalFlags, args []string) error {
	fs := flag.NewFlagSet("draft delete", flag.ContinueOnError)
	permanent := fs.Bool("permanent", false, "expunge immediately instead of moving to Trash")
	args = helpers.ReorderArgs(args, map[string]bool{"permanent": true})
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: %w", err)
	}

	if fs.NArg() < 1 {
		return fmt.Errorf("usage: draft delete <uid>")
	}

	uids, err := helpers.ParseUIDs(fs.Args()[:1])
	if err != nil {
		return err
	}

	client, _, err := helpers.ConnectIMAP(g.Ctx, g.Config, g.Account, g.Logger)
	if err != nil {
		return err
	}
	defer client.Close()

	draftsFolder := draftsFolderFor(g, client)

	// Same bargain as the delete command: an unsent draft is work, and losing
	// it to a mistyped UID should be recoverable.
	var movedTo string
	if *permanent {
		err = client.DeleteMessage(g.Ctx, draftsFolder, uids[0])
	} else {
		movedTo, err = mail.TrashMessages(g.Ctx, client, draftsFolder, uids[:1])
	}
	if err != nil {
		return err
	}

	gone := movedTo == ""

	if g.JSON {
		result := map[string]any{"status": "deleted", "permanent": gone}
		if !gone {
			result["movedTo"] = movedTo
		}
		return output.PrintJSON(g.Out(), result)
	}

	if gone {
		fmt.Fprintln(g.Out(), "Draft permanently deleted.")
	} else {
		fmt.Fprintf(g.Out(), "Draft moved to %s.\n", movedTo)
	}
	return nil
}
