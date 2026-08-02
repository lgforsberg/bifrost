package commands

import (
	"flag"
	"fmt"

	"github.com/lgforsberg/bifrost/internal/cmdutil"
	"github.com/lgforsberg/bifrost/internal/helpers"
	"github.com/lgforsberg/bifrost/internal/output"
)

func MarkRead(g *cmdutil.GlobalFlags, args []string) error {
	fs := flag.NewFlagSet("mark-read", flag.ContinueOnError)
	folder := fs.String("folder", "INBOX", "folder")
	args = helpers.ReorderArgs(args, nil)
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: %w", err)
	}

	if fs.NArg() < 1 {
		return fmt.Errorf("usage: mark-read <uid...>")
	}

	uids, err := helpers.ParseUIDs(fs.Args())
	if err != nil {
		return err
	}

	client, _, err := helpers.ConnectIMAP(g.Ctx, g.Config, g.Account, g.Logger)
	if err != nil {
		return err
	}
	defer client.Close()

	v, err := helpers.ValidateUIDs(g.Ctx, client, *folder, uids)
	if err != nil {
		return err
	}

	if err := client.MarkReadBatch(g.Ctx, *folder, v.Existing); err != nil {
		return err
	}

	if g.JSON {
		return output.PrintJSON(g.Out(), bulkResult("marked_read", v))
	} else {
		fmt.Fprintf(g.Out(), "Marked %d message(s) as read.\n", len(v.Existing))
	}
	return nil
}

func MarkUnread(g *cmdutil.GlobalFlags, args []string) error {
	fs := flag.NewFlagSet("mark-unread", flag.ContinueOnError)
	folder := fs.String("folder", "INBOX", "folder")
	args = helpers.ReorderArgs(args, nil)
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: %w", err)
	}

	if fs.NArg() < 1 {
		return fmt.Errorf("usage: mark-unread <uid...>")
	}

	uids, err := helpers.ParseUIDs(fs.Args())
	if err != nil {
		return err
	}

	client, _, err := helpers.ConnectIMAP(g.Ctx, g.Config, g.Account, g.Logger)
	if err != nil {
		return err
	}
	defer client.Close()

	v, err := helpers.ValidateUIDs(g.Ctx, client, *folder, uids)
	if err != nil {
		return err
	}

	if err := client.MarkUnreadBatch(g.Ctx, *folder, v.Existing); err != nil {
		return err
	}

	if g.JSON {
		return output.PrintJSON(g.Out(), bulkResult("marked_unread", v))
	} else {
		fmt.Fprintf(g.Out(), "Marked %d message(s) as unread.\n", len(v.Existing))
	}
	return nil
}
