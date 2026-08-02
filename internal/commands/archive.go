package commands

import (
	"flag"
	"fmt"

	"github.com/lgforsberg/bifrost/internal/cmdutil"
	"github.com/lgforsberg/bifrost/internal/helpers"
	"github.com/lgforsberg/bifrost/internal/output"
	"github.com/lgforsberg/bifrost/mail"
)

func Archive(g *cmdutil.GlobalFlags, args []string) error {
	fs := flag.NewFlagSet("archive", flag.ContinueOnError)
	folder := fs.String("folder", "INBOX", "folder")
	args = helpers.ReorderArgs(args, nil)
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: %w", err)
	}

	if fs.NArg() < 1 {
		return fmt.Errorf("usage: archive <uid...>")
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

	if err := mail.Archive(g.Ctx, client, *folder, v.Existing); err != nil {
		return err
	}

	if g.JSON {
		return output.PrintJSON(g.Out(), bulkResult("archived", v))
	} else {
		fmt.Fprintf(g.Out(), "Archived %d message(s).\n", len(v.Existing))
	}
	return nil
}
