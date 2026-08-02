package commands

import (
	"flag"
	"fmt"

	"github.com/lgforsberg/bifrost/internal/cmdutil"
	"github.com/lgforsberg/bifrost/internal/helpers"
	"github.com/lgforsberg/bifrost/internal/output"
	"github.com/lgforsberg/bifrost/mail"
)

func Read(g *cmdutil.GlobalFlags, args []string) error {
	fs := flag.NewFlagSet("read", flag.ContinueOnError)
	folder := fs.String("folder", "INBOX", "folder")
	peek := fs.Bool("peek", false, "don't mark message as read")
	withData := fs.Bool("with-attachment-data", false, "include attachment bytes in JSON output")
	noAttachments := fs.Bool("no-attachments", false, "exclude attachment data (now the default)")
	saveAttachments := fs.String("save-attachments", "", "save attachments to directory")
	args = helpers.ReorderArgs(args, map[string]bool{
		"peek":                 true,
		"no-attachments":       true,
		"with-attachment-data": true,
	})
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

	// Attachment bytes are left out unless asked for: base64 makes a single PDF
	// larger than the message carrying it, which is a poor trade for a reader
	// that mostly wants to know an attachment is there. Filename, type and size
	// still come through, and --save-attachments writes the bytes to disk.
	if !*withData || *noAttachments {
		for i := range msg.Attachments {
			msg.Attachments[i].Data = nil
		}
	}

	if g.JSON {
		return output.PrintJSON(g.Out(), msg)
	}

	fmt.Fprintf(g.Out(), "UID:     %d\n", msg.UID)
	fmt.Fprintf(g.Out(), "From:    %s\n", msg.From.String())
	if len(msg.ReplyTo) > 0 {
		fmt.Fprintf(g.Out(), "Reply-To: %s\n", formatAddresses(msg.ReplyTo))
	}
	fmt.Fprintf(g.Out(), "To:      %s\n", formatAddresses(msg.To))
	if len(msg.Cc) > 0 {
		fmt.Fprintf(g.Out(), "Cc:      %s\n", formatAddresses(msg.Cc))
	}
	// Only ever set on a draft or a Sent copy, where the sender needs to see
	// who was blind-copied.
	if len(msg.Bcc) > 0 {
		fmt.Fprintf(g.Out(), "Bcc:     %s\n", formatAddresses(msg.Bcc))
	}
	fmt.Fprintf(g.Out(), "Date:    %s\n", msg.Date.Format("Mon, 02 Jan 2006 15:04:05 -0700"))
	fmt.Fprintf(g.Out(), "Subject: %s\n", msg.Subject)
	if len(msg.Attachments) > 0 {
		fmt.Fprintf(g.Out(), "Attachments: ")
		for i, att := range msg.Attachments {
			if i > 0 {
				fmt.Fprint(g.Out(), ", ")
			}
			fmt.Fprintf(g.Out(), "%s (%s, %d bytes)", att.Filename, att.ContentType, att.Size)
		}
		fmt.Fprintln(g.Out())
	}
	fmt.Fprintln(g.Out())
	fmt.Fprintln(g.Out(), msg.TextBody)
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
