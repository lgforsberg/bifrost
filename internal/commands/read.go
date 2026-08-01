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

func Read(g *cmdutil.GlobalFlags, args []string) error {
	fs := flag.NewFlagSet("read", flag.ContinueOnError)
	folder := fs.String("folder", "INBOX", "folder")
	peek := fs.Bool("peek", false, "don't mark message as read")
	noAttachments := fs.Bool("no-attachments", false, "exclude attachment data")
	saveAttachments := fs.String("save-attachments", "", "save attachments to directory")
	args = helpers.ReorderArgs(args, map[string]bool{"peek": true, "no-attachments": true})
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: %w", err)
	}

	if fs.NArg() < 1 {
		return fmt.Errorf("usage: read <uid>")
	}

	uids, err := helpers.ParseUIDs(fs.Args()[:1])
	if err != nil {
		return err
	}
	uid := uids[0]

	usePeek := *peek || g.Config.Defaults.PeekOnRead

	client, _, err := helpers.ConnectIMAP(g.Ctx, g.Config, g.Account, g.Logger)
	if err != nil {
		return err
	}
	defer client.Close()

	msg, err := client.FetchMessage(g.Ctx, *folder, uid, usePeek)
	if err != nil {
		return err
	}

	if *saveAttachments != "" {
		if err := helpers.SaveAttachments(*saveAttachments, msg.Attachments); err != nil {
			return err
		}
	}

	if *noAttachments {
		for i := range msg.Attachments {
			msg.Attachments[i].Data = nil
		}
	}

	if g.JSON {
		return output.PrintJSON(os.Stdout, msg)
	}

	fmt.Printf("UID:     %d\n", msg.UID)
	fmt.Printf("From:    %s\n", msg.From.String())
	fmt.Printf("To:      %s\n", formatAddresses(msg.To))
	if len(msg.Cc) > 0 {
		fmt.Printf("Cc:      %s\n", formatAddresses(msg.Cc))
	}
	// Only ever set on a draft or a Sent copy, where the sender needs to see
	// who was blind-copied.
	if len(msg.Bcc) > 0 {
		fmt.Printf("Bcc:     %s\n", formatAddresses(msg.Bcc))
	}
	fmt.Printf("Date:    %s\n", msg.Date.Format("Mon, 02 Jan 2006 15:04:05 -0700"))
	fmt.Printf("Subject: %s\n", msg.Subject)
	if len(msg.Attachments) > 0 {
		fmt.Printf("Attachments: ")
		for i, att := range msg.Attachments {
			if i > 0 {
				fmt.Print(", ")
			}
			fmt.Printf("%s (%s, %d bytes)", att.Filename, att.ContentType, att.Size)
		}
		fmt.Println()
	}
	fmt.Println()
	fmt.Println(msg.TextBody)
	return nil
}

func formatAddresses(addrs []mail.Address) string {
	parts := make([]string, len(addrs))
	for i, a := range addrs {
		parts[i] = a.String()
	}
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += ", "
		}
		result += p
	}
	return result
}
