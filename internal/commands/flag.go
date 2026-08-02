package commands

import (
	"flag"
	"fmt"

	"github.com/lgforsberg/bifrost/internal/cmdutil"
	"github.com/lgforsberg/bifrost/internal/helpers"
	"github.com/lgforsberg/bifrost/internal/output"
)

func Flag(g *cmdutil.GlobalFlags, args []string) error {
	return setFlagged(g, args, true)
}

func Unflag(g *cmdutil.GlobalFlags, args []string) error {
	return setFlagged(g, args, false)
}

// setFlagged is both commands: they differ only in which way the flag goes,
// and splitting them would duplicate the argument handling to no end.
func setFlagged(g *cmdutil.GlobalFlags, args []string, on bool) error {
	name, status, verb := "flag", "flagged", "Flagged"
	if !on {
		name, status, verb = "unflag", "unflagged", "Unflagged"
	}

	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	folder := fs.String("folder", "INBOX", "folder")
	args = helpers.ReorderArgs(args, nil)
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: %w", err)
	}

	if fs.NArg() < 1 {
		return fmt.Errorf("usage: %s <uid...>", name)
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

	if on {
		err = client.FlagBatch(g.Ctx, *folder, v.Existing)
	} else {
		err = client.UnflagBatch(g.Ctx, *folder, v.Existing)
	}
	if err != nil {
		return err
	}

	if g.JSON {
		return output.PrintJSON(g.Out(), bulkResult(status, v))
	}
	fmt.Fprintf(g.Out(), "%s %d message(s).\n", verb, len(v.Existing))
	return nil
}
