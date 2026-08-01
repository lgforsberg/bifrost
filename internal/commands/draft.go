package commands

import (
	"flag"
	"fmt"
	"os"

	"github.com/lgforsberg/bifrost/internal/cmdutil"
	"github.com/lgforsberg/bifrost/internal/helpers"
	"github.com/lgforsberg/bifrost/internal/output"
	"github.com/lgforsberg/bifrost/mail"
)

func Draft(g *cmdutil.GlobalFlags, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: draft <save|list|send|delete>")
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
	case "delete":
		return draftDelete(g, subArgs)
	case "help", "--help", "-h":
		return fmt.Errorf("usage: draft <save|list|send|delete>")
	default:
		return fmt.Errorf("usage: unknown subcommand %q (save, list, send, delete)", sub)
	}
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
	bodyFlag := fs.String("body", "", "message body")
	bodyFile := fs.String("body-file", "", "read body from file")
	approval := fs.Bool("approval", false, "mark draft for approval ($PendingApproval keyword)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: %w", err)
	}

	bodySet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "body" {
			bodySet = true
		}
	})
	body, err := helpers.ReadBody(*bodyFlag, bodySet, *bodyFile)
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
		return output.PrintJSON(os.Stdout, map[string]any{"status": status, "uid": uid})
	} else {
		if *approval {
			fmt.Println("Draft saved and marked for approval.")
		} else {
			fmt.Println("Draft saved.")
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

	draftsFolder, err := client.FindSpecialFolder(g.Ctx, "\\Drafts")
	if err != nil {
		draftsFolder = "Drafts"
	}

	envelopes, err := client.ListEnvelopes(g.Ctx, draftsFolder, *limit, *offset)
	if err != nil {
		return err
	}

	if g.JSON {
		return output.PrintJSON(os.Stdout, envelopes)
	}

	if len(envelopes) == 0 {
		fmt.Println("No drafts.")
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
	output.PrintTable(os.Stdout, headers, rows)
	return nil
}

func draftSend(g *cmdutil.GlobalFlags, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: draft send <uid>")
	}

	uids, err := helpers.ParseUIDs(args[:1])
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

	res, err := mail.SendDraft(g.Ctx, *acct, client, uids[0], g.Logger)
	if err != nil {
		return err
	}

	return reportSend(g, res, "Draft sent.")
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

	draftsFolder, err := client.FindSpecialFolder(g.Ctx, "\\Drafts")
	if err != nil {
		draftsFolder = "Drafts"
	}

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
		return output.PrintJSON(os.Stdout, result)
	}

	if gone {
		fmt.Println("Draft permanently deleted.")
	} else {
		fmt.Printf("Draft moved to %s.\n", movedTo)
	}
	return nil
}
