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

func Delete(g *cmdutil.GlobalFlags, args []string) error {
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)
	folder := fs.String("folder", "INBOX", "folder")
	permanent := fs.Bool("permanent", false, "expunge immediately instead of moving to Trash")
	args = helpers.ReorderArgs(args, map[string]bool{"permanent": true})
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: %w", err)
	}

	if fs.NArg() < 1 {
		return fmt.Errorf("usage: delete <uid...>")
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

	var movedTo string
	if *permanent {
		err = client.DeleteMessages(g.Ctx, *folder, v.Existing)
	} else {
		movedTo, err = mail.TrashMessages(g.Ctx, client, *folder, v.Existing)
	}
	if err != nil {
		return err
	}

	// Deleting out of Trash expunges, so the messages are gone either way.
	gone := movedTo == ""

	if g.JSON {
		result := bulkResult("deleted", v)
		result["permanent"] = gone
		if !gone {
			result["movedTo"] = movedTo
		}
		return output.PrintJSON(os.Stdout, result)
	}

	if gone {
		fmt.Printf("Permanently deleted %d message(s).\n", len(v.Existing))
	} else {
		fmt.Printf("Moved %d message(s) to %s.\n", len(v.Existing), movedTo)
	}
	return nil
}
