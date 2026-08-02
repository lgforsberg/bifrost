package commands

import (
	"flag"
	"fmt"

	"github.com/lgforsberg/bifrost/internal/cmdutil"
	"github.com/lgforsberg/bifrost/internal/helpers"
	"github.com/lgforsberg/bifrost/internal/output"
)

func Move(g *cmdutil.GlobalFlags, args []string) error {
	fs := flag.NewFlagSet("move", flag.ContinueOnError)
	toFolder := fs.String("to", "", "destination folder (required)")
	folder := fs.String("folder", "INBOX", "source folder")
	fromFolder := fs.String("from", "", "source folder (alias for --folder)")
	args = helpers.ReorderArgs(args, nil)
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: %w", err)
	}

	if *fromFolder != "" {
		*folder = *fromFolder
	}

	if *toFolder == "" {
		return fmt.Errorf("usage: --to is required")
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: move <uid...>")
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

	if err := client.MoveMessages(g.Ctx, v.Existing, *folder, *toFolder); err != nil {
		return err
	}

	if g.JSON {
		result := bulkResult("moved", v)
		result["to"] = *toFolder
		return output.PrintJSON(g.Out(), result)
	} else {
		fmt.Fprintf(g.Out(), "Moved %d message(s) to %s.\n", len(v.Existing), *toFolder)
	}
	return nil
}
