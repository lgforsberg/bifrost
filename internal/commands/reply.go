package commands

import (
	"flag"
	"fmt"

	"github.com/lgforsberg/bifrost/internal/cmdutil"
	"github.com/lgforsberg/bifrost/internal/helpers"
	"github.com/lgforsberg/bifrost/mail"
)

func Reply(g *cmdutil.GlobalFlags, args []string) error {
	fs := flag.NewFlagSet("reply", flag.ContinueOnError)
	fromAddr := fs.String("from", "", "override From address (e.g. user+tag@domain)")
	bodies := helpers.RegisterBodyFlags(fs, "reply body")
	all := fs.Bool("all", false, "reply to all recipients")
	noQuote := fs.Bool("no-quote", false, "don't quote original message")
	noSave := fs.Bool("no-save", false, "don't save to Sent")
	folder := fs.String("folder", "INBOX", "folder containing the message")
	args = helpers.ReorderArgs(args, map[string]bool{"all": true, "no-quote": true, "no-save": true})
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: %w", err)
	}

	if fs.NArg() < 1 {
		return fmt.Errorf("usage: reply <uid>")
	}

	uids, err := helpers.ParseUIDs(fs.Args()[:1])
	if err != nil {
		return err
	}
	uid := uids[0]

	body, htmlBody, err := bodies.Read()
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

	quoteOriginal := g.Config.Defaults.QuoteReplies && !*noQuote
	opts := mail.BuildReply(*acct, original, body, *all, quoteOriginal)
	if htmlBody != "" {
		opts.HTMLBody = htmlBody
		if quoteOriginal {
			opts.HTMLBody += mail.QuoteBodyHTML(original)
		}
	}
	if *fromAddr != "" {
		opts.From.Address = *fromAddr
	}
	saveToSent := g.Config.Defaults.SaveToSent && !*noSave
	res, err := mail.Send(g.Ctx, *acct, client, opts, saveToSent, g.Logger)
	if err != nil {
		return err
	}

	return reportSend(g, res, "Reply sent.")
}
