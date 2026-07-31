package commands

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lgforsberg/bifrost/internal/cmdutil"
	"github.com/lgforsberg/bifrost/internal/helpers"
	"github.com/lgforsberg/bifrost/internal/output"
	"github.com/lgforsberg/bifrost/mail"
)

func Send(g *cmdutil.GlobalFlags, args []string) error {
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	var toFlag, ccFlag, bccFlag, attachFlag helpers.StringSliceFlag
	fs.Var(&toFlag, "to", "recipient (repeatable)")
	fs.Var(&ccFlag, "cc", "CC recipient (repeatable)")
	fs.Var(&bccFlag, "bcc", "BCC recipient (repeatable)")
	fs.Var(&attachFlag, "attach", "attachment file path (repeatable)")
	fromAddr := fs.String("from", "", "override From address (e.g. user+tag@domain)")
	subject := fs.String("subject", "", "message subject")
	bodyFlag := fs.String("body", "", "message body")
	bodyFile := fs.String("body-file", "", "read body from file")
	noSave := fs.Bool("no-save", false, "don't save to Sent")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: %w", err)
	}

	if len(toFlag) == 0 {
		return fmt.Errorf("usage: --to is required")
	}
	if *subject == "" {
		return fmt.Errorf("usage: --subject is required")
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

	msgID := mail.GenerateMessageID()
	opts := mail.SendOptions{
		From:        from,
		To:          parseAddressFlags(toFlag),
		Cc:          parseAddressFlags(ccFlag),
		Bcc:         parseAddressFlags(bccFlag),
		Subject:     *subject,
		TextBody:    body,
		Attachments: attachments,
		MessageID:   msgID,
	}

	client, _, err := helpers.ConnectIMAP(g.Ctx, g.Config, g.Account, g.Logger)
	if err != nil {
		return err
	}
	defer client.Close()

	saveToSent := g.Config.Defaults.SaveToSent && !*noSave
	if err := mail.Send(g.Ctx, *acct, client, opts, saveToSent, g.Logger); err != nil {
		return err
	}

	if g.JSON {
		return output.PrintJSON(os.Stdout, map[string]string{"status": "sent", "messageId": msgID})
	} else {
		fmt.Println("Message sent.")
	}
	return nil
}

func parseAddressFlags(flags []string) []mail.Address {
	addrs := make([]mail.Address, len(flags))
	for i, f := range flags {
		addrs[i] = mail.Address{Address: f}
	}
	return addrs
}

func loadAttachments(paths []string) ([]mail.Attachment, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	attachments := make([]mail.Attachment, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading attachment %s: %w", path, err)
		}
		filename := filepath.Base(path)
		attachments = append(attachments, mail.Attachment{
			Filename: filename,
			Data:     data,
			Size:     int64(len(data)),
		})
	}
	return attachments, nil
}
