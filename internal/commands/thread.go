package commands

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/lgforsberg/bifrost/internal/cmdutil"
	"github.com/lgforsberg/bifrost/internal/helpers"
	"github.com/lgforsberg/bifrost/internal/output"
)

func Thread(g *cmdutil.GlobalFlags, args []string) error {
	fs := flag.NewFlagSet("thread", flag.ContinueOnError)
	foldersStr := fs.String("folders", "INBOX,Sent", "comma-separated folders to search across")
	noAttachments := fs.Bool("no-attachments", false, "exclude attachment data")
	args = helpers.ReorderArgs(args, map[string]bool{"no-attachments": true})
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

	if *noAttachments {
		for i := range messages {
			for j := range messages[i].Attachments {
				messages[i].Attachments[j].Data = nil
			}
		}
	}

	if g.JSON {
		return output.PrintJSON(os.Stdout, messages)
	}

	for i, msg := range messages {
		if i > 0 {
			fmt.Println("---")
		}
		fmt.Printf("UID:     %d\n", msg.UID)
		if msg.Folder != "" {
			fmt.Printf("Folder:  %s\n", msg.Folder)
		}
		fmt.Printf("From:    %s\n", msg.From.String())
		fmt.Printf("Date:    %s\n", msg.Date.Format("Mon, 02 Jan 2006 15:04:05 -0700"))
		fmt.Printf("Subject: %s\n\n", msg.Subject)
		fmt.Println(msg.TextBody)
	}
	return nil
}
