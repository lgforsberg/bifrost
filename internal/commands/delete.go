package commands

import (
	"flag"
	"fmt"
	"os"

	"github.com/lgforsberg/bifrost/internal/cmdutil"
	"github.com/lgforsberg/bifrost/internal/helpers"
	"github.com/lgforsberg/bifrost/internal/output"
)

func Delete(g *cmdutil.GlobalFlags, args []string) error {
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)
	folder := fs.String("folder", "INBOX", "folder")
	args = helpers.ReorderArgs(args, nil)
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

	if err := client.DeleteMessages(g.Ctx, *folder, v.Existing); err != nil {
		return err
	}

	if g.JSON {
		return output.PrintJSON(os.Stdout, bulkResult("deleted", v))
	} else {
		fmt.Printf("Deleted %d message(s).\n", len(v.Existing))
	}
	return nil
}
