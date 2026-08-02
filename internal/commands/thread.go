package commands

import (
	"flag"
	"fmt"
	"strings"

	"github.com/lgforsberg/bifrost/internal/cmdutil"
	"github.com/lgforsberg/bifrost/internal/helpers"
	"github.com/lgforsberg/bifrost/internal/output"
)

func Thread(g *cmdutil.GlobalFlags, args []string) error {
	fs := flag.NewFlagSet("thread", flag.ContinueOnError)
	foldersStr := fs.String("folders", "INBOX,Sent", "comma-separated folders to search across")
	withData := fs.Bool("with-attachment-data", false, "include attachment bytes in JSON output")
	noAttachments := fs.Bool("no-attachments", false, "exclude attachment data (now the default)")
	args = helpers.ReorderArgs(args, map[string]bool{
		"no-attachments":       true,
		"with-attachment-data": true,
	})
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: %w", err)
	}

	if fs.NArg() < 1 {
		return fmt.Errorf("usage: thread <uid>")
	}

	uids, err := helpers.ParseUIDs(fs.Args()[:1])
	if err != nil {
		return err
	}
	uid := uids[0]

	folders := strings.Split(*foldersStr, ",")
	for i := range folders {
		folders[i] = strings.TrimSpace(folders[i])
	}

	client, _, err := helpers.ConnectIMAP(g.Ctx, g.Config, g.Account, g.Logger)
	if err != nil {
		return err
	}
	defer client.Close()

	messages, err := client.FetchThread(g.Ctx, folders, uid)
	if err != nil {
		return err
	}

	// Excluded by default, as in read, and more so here: a thread multiplies
	// every attachment by the number of messages carrying it.
	if !*withData || *noAttachments {
		for i := range messages {
			for j := range messages[i].Attachments {
				messages[i].Attachments[j].Data = nil
			}
		}
	}

	if g.JSON {
		return output.PrintJSON(g.Out(), messages)
	}

	for i, msg := range messages {
		if i > 0 {
			fmt.Fprintln(g.Out(), "---")
		}
		for _, w := range msg.Warnings {
			fmt.Fprintf(g.Err(), "warning: uid %d: %s\n", msg.UID, w)
		}
		fmt.Fprintf(g.Out(), "UID:     %d\n", msg.UID)
		if msg.Folder != "" {
			fmt.Fprintf(g.Out(), "Folder:  %s\n", msg.Folder)
		}
		fmt.Fprintf(g.Out(), "From:    %s\n", msg.From.String())
		fmt.Fprintf(g.Out(), "Date:    %s\n", msg.Date.Format("Mon, 02 Jan 2006 15:04:05 -0700"))
		fmt.Fprintf(g.Out(), "Subject: %s\n\n", msg.Subject)
		fmt.Fprintln(g.Out(), msg.TextBody)
	}
	return nil
}
