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
		return fmt.Errorf("usage: draft <save|list|send|approve|delete>")
	}

	sub := args[0]
	subArgs := args[1:]

	switch sub {
	case "save":
		return draftSave(g, subArgs)
	case "list":
		return draftList(g, subArgs)
	case "send":
		return draftSend(g, subArgs)
	case "approve":
		return draftApprove(g, subArgs)
	case "delete":
		return draftDelete(g, subArgs)
	case "help", "--help", "-h":
		return g.Usage("usage: draft <save|list|send|approve|delete>")
	default:
		return fmt.Errorf("usage: unknown subcommand %q (save, list, send, approve, delete)", sub)
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

func draftSave(g *cmdutil.GlobalFlags, args []string) error {
	fs := flag.NewFlagSet("draft save", flag.ContinueOnError)
	var toFlag, ccFlag, bccFlag, attachFlag helpers.StringSliceFlag
	fs.Var(&toFlag, "to", "recipient (repeatable)")
	fs.Var(&ccFlag, "cc", "CC recipient (repeatable)")
	fs.Var(&bccFlag, "bcc", "BCC recipient (repeatable)")
	fs.Var(&attachFlag, "attach", "attachment file path (repeatable)")
	fromAddr := fs.String("from", "", "override From address (e.g. user+tag@domain)")
	subject := fs.String("subject", "", "message subject")
	bodies := helpers.RegisterBodyFlags(fs, "message body")
	approval := fs.Bool("approval", false, "mark draft for approval ($PendingApproval keyword)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: %w", err)
	}

	body, htmlBody, err := bodies.Read()
	if err != nil {
		return err
	}

	acct, err := helpers.ResolveAccount(g.Config, g.Account)
	if err != nil {
		return err
	}

	attachments, err := loadAttachments(attachFlag)
	if err != nil {
		return err
	}

	from := mail.Address{Name: acct.DisplayName, Address: acct.Address}
	if *fromAddr != "" {
		from.Address = *fromAddr
	}

	opts := mail.SendOptions{
		From:        from,
		To:          parseAddressFlags(toFlag),
		Cc:          parseAddressFlags(ccFlag),
		Bcc:         parseAddressFlags(bccFlag),
		Subject:     *subject,
		TextBody:    body,
		HTMLBody:    htmlBody,
		Attachments: attachments,
	}

	client, _, err := helpers.ConnectIMAP(g.Ctx, g.Config, g.Account, g.Logger)
	if err != nil {
		return err
	}
	defer client.Close()

	var keywords []string
	if *approval {
		keywords = append(keywords, "$PendingApproval")
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
