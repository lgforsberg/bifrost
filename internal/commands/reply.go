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

func Reply(g *cmdutil.GlobalFlags, args []string) error {
	fs := flag.NewFlagSet("reply", flag.ContinueOnError)
	fromAddr := fs.String("from", "", "override From address (e.g. user+tag@domain)")
	bodyFlag := fs.String("body", "", "reply body")
	bodyFile := fs.String("body-file", "", "read body from file")
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

	quoteOriginal := g.Config.Defaults.QuoteReplies && !*noQuote
	opts := mail.BuildReply(*acct, original, body, *all, quoteOriginal)
	if *fromAddr != "" {
		opts.From.Address = *fromAddr
	}
	msgID := mail.GenerateMessageID()
	opts.MessageID = msgID

	saveToSent := g.Config.Defaults.SaveToSent && !*noSave
	if err := mail.Send(g.Ctx, *acct, client, opts, saveToSent, g.Logger); err != nil {
		return err
	}

	if g.JSON {
		return output.PrintJSON(os.Stdout, map[string]string{"status": "sent", "messageId": msgID})
	} else {
		fmt.Println("Reply sent.")
	}
	return nil
}
