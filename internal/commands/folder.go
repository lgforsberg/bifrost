package commands

import (
	"fmt"

	"github.com/lgforsberg/bifrost/internal/cmdutil"
	"github.com/lgforsberg/bifrost/internal/helpers"
	"github.com/lgforsberg/bifrost/internal/output"
)

func Folder(g *cmdutil.GlobalFlags, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: folder <list|create|rename|delete>")
	}

	sub := args[0]
	subArgs := args[1:]

	switch sub {
	case "list":
		return folderList(g)
	case "create":
		if len(subArgs) < 1 {
			return fmt.Errorf("usage: folder create <name>")
		}
		return folderCreate(g, subArgs[0])
	case "rename":
		if len(subArgs) < 2 {
			return fmt.Errorf("usage: folder rename <old> <new>")
		}
		return folderRename(g, subArgs[0], subArgs[1])
	case "delete":
		if len(subArgs) < 1 {
			return fmt.Errorf("usage: folder delete <name>")
		}
		return folderDelete(g, subArgs[0])
	case "help", "--help", "-h":
		return fmt.Errorf("usage: folder <list|create|rename|delete>")
	default:
		return fmt.Errorf("usage: unknown subcommand %q (list, create, rename, delete)", sub)
	}
}

func folderList(g *cmdutil.GlobalFlags) error {
	client, _, err := helpers.ConnectIMAP(g.Ctx, g.Config, g.Account, g.Logger)
	if err != nil {
		return err
	}
	defer client.Close()

	folders, err := client.ListFolders(g.Ctx)
	if err != nil {
		return err
	}

	if g.JSON {
		return output.PrintJSON(g.Out(), folders)
	}

	for _, f := range folders {
		attrs := ""
		if len(f.Attributes) > 0 {
			attrs = " ("
			for i, a := range f.Attributes {
				if i > 0 {
					attrs += ", "
				}
				attrs += a
			}
			attrs += ")"
		}
		fmt.Fprintf(g.Out(), "%s%s\n", f.Name, attrs)
	}
	return nil
}

func folderCreate(g *cmdutil.GlobalFlags, name string) error {
	client, _, err := helpers.ConnectIMAP(g.Ctx, g.Config, g.Account, g.Logger)
	if err != nil {
		return err
	}
	defer client.Close()

	if err := client.CreateFolder(g.Ctx, name); err != nil {
		return err
	}

	if g.JSON {
		return output.PrintJSON(g.Out(), map[string]string{"status": "created", "name": name})
	} else {
		fmt.Fprintf(g.Out(), "Folder %q created.\n", name)
	}
	return nil
}

func folderRename(g *cmdutil.GlobalFlags, old, newName string) error {
	client, _, err := helpers.ConnectIMAP(g.Ctx, g.Config, g.Account, g.Logger)
	if err != nil {
		return err
	}
	defer client.Close()

	if err := client.RenameFolder(g.Ctx, old, newName); err != nil {
		return err
	}

	if g.JSON {
		return output.PrintJSON(g.Out(), map[string]string{"status": "renamed", "old": old, "new": newName})
	} else {
		fmt.Fprintf(g.Out(), "Folder %q renamed to %q.\n", old, newName)
	}
	return nil
}

func folderDelete(g *cmdutil.GlobalFlags, name string) error {
	client, _, err := helpers.ConnectIMAP(g.Ctx, g.Config, g.Account, g.Logger)
	if err != nil {
		return err
	}
	defer client.Close()

	if err := client.DeleteFolder(g.Ctx, name); err != nil {
		return err
	}

	if g.JSON {
		return output.PrintJSON(g.Out(), map[string]string{"status": "deleted", "name": name})
	} else {
		fmt.Fprintf(g.Out(), "Folder %q deleted.\n", name)
	}
	return nil
}
