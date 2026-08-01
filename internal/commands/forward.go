package commands

import (
	"flag"
	"fmt"

	"github.com/lgforsberg/bifrost/internal/cmdutil"
	"github.com/lgforsberg/bifrost/internal/helpers"
	"github.com/lgforsberg/bifrost/mail"
)

func Forward(g *cmdutil.GlobalFlags, args []string) error {
	fs := flag.NewFlagSet("forward", flag.ContinueOnError)
	var toFlag, attachFlag helpers.StringSliceFlag
	fs.Var(&toFlag, "to", "recipient (repeatable)")
	fs.Var(&attachFlag, "attach", "additional attachment (repeatable)")
	fromAddr := fs.String("from", "", "override From address (e.g. user+tag@domain)")
	bodyFlag := fs.String("body", "", "forwarding body")
	bodyFile := fs.String("body-file", "", "read body from file")
	noSave := fs.Bool("no-save", false, "don't save to Sent")
	folder := fs.String("folder", "INBOX", "folder containing the message")
	args = helpers.ReorderArgs(args, map[string]bool{"no-save": true})
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: %w", err)
	}

	if fs.NArg() < 1 {
		return fmt.Errorf("usage: forward <uid>")
	}
	if len(toFlag) == 0 {
		return fmt.Errorf("usage: --to is required")
	}

	uids, err := helpers.ParseUIDs(fs.Args()[:1])
	if err != nil {
		return err
	}
	uid := uids[0]

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

	client, _, err := helpers.ConnectIMAP(g.Ctx, g.Config, g.Account, g.Logger)
	if err != nil {
		return err
	}
	defer client.Close()

	original, err := client.FetchMessage(g.Ctx, *folder, uid, true)
	if err != nil {
		return err
	}

	opts := mail.BuildForward(*acct, original, parseAddressFlags(toFlag), body)
	if *fromAddr != "" {
		opts.From.Address = *fromAddr
	}
	extra, err := loadAttachments(attachFlag)
	if err != nil {
		return err
	}
	opts.Attachments = append(opts.Attachments, extra...)

	saveToSent := g.Config.Defaults.SaveToSent && !*noSave
	res, err := mail.Send(g.Ctx, *acct, client, opts, saveToSent, g.Logger)
	if err != nil {
		return err
	}

	return reportSend(g, res, "Message forwarded.")
}
