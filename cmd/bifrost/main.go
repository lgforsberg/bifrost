package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"

	"github.com/lgforsberg/bifrost/internal/cmdutil"
	"github.com/lgforsberg/bifrost/internal/commands"
	"github.com/lgforsberg/bifrost/internal/config"
	"github.com/lgforsberg/bifrost/internal/output"
	"github.com/lgforsberg/bifrost/mail"
)

const version = "1.17.1"

func main() {
	globals, args := parseGlobalFlags(os.Args[1:])

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	globals.Ctx = ctx

	var handler slog.Handler
	if globals.Verbose {
		handler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
	} else {
		handler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 1})
	}
	globals.Logger = slog.New(handler)

	if len(args) == 0 {
		printUsage()
		os.Exit(2)
	}

	cmd := args[0]
	cmdArgs := args[1:]

	switch cmd {
	case "version":
		if globals.JSON {
			_ = output.PrintJSON(os.Stdout, map[string]string{"version": version})
		} else {
			fmt.Printf("bifrost %s\n", version)
		}
		return
	case "help", "--help", "-h":
		printUsage()
		return
	}

	if cmd == "config" {
		if err := commands.Config(&globals, cmdArgs); err != nil {
			handleError(&globals, err)
		}
		return
	}

	cfg, err := config.Load(globals.ConfigPath)
	if err != nil {
		// Finding out what a command takes should not require an account.
		// Every command parses its flags before it looks at the config, so an
		// empty one is enough to get the usage text out; anything that
		// actually runs still fails on the original error.
		if !wantsHelp(cmdArgs) {
			handleError(&globals, err)
			return
		}
		cfg = &config.Config{}
	}
	globals.Config = cfg

	switch cmd {
	case "inbox":
		err = commands.Inbox(&globals, cmdArgs)
	case "read":
		err = commands.Read(&globals, cmdArgs)
	case "search":
		err = commands.Search(&globals, cmdArgs)
	case "thread":
		err = commands.Thread(&globals, cmdArgs)
	case "send":
		err = commands.Send(&globals, cmdArgs)
	case "reply":
		err = commands.Reply(&globals, cmdArgs)
	case "forward":
		err = commands.Forward(&globals, cmdArgs)
	case "delete":
		err = commands.Delete(&globals, cmdArgs)
	case "archive":
		err = commands.Archive(&globals, cmdArgs)
	case "move":
		err = commands.Move(&globals, cmdArgs)
	case "mark-read":
		err = commands.MarkRead(&globals, cmdArgs)
	case "mark-unread":
		err = commands.MarkUnread(&globals, cmdArgs)
	case "flag":
		err = commands.Flag(&globals, cmdArgs)
	case "unflag":
		err = commands.Unflag(&globals, cmdArgs)
	case "folder":
		err = commands.Folder(&globals, cmdArgs)
	case "accounts":
		err = commands.Accounts(&globals, cmdArgs)
	case "draft":
		err = commands.Draft(&globals, cmdArgs)
	default:
		err = fmt.Errorf("usage: unknown command %q", cmd)
	}

	if err != nil {
		handleError(&globals, err)
	}
}

func parseGlobalFlags(args []string) (cmdutil.GlobalFlags, []string) {
	var g cmdutil.GlobalFlags
	var remaining []string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--account":
			if i+1 < len(args) {
				i++
				g.Account = args[i]
			}
		case "--json":
			g.JSON = true
		case "--verbose":
			g.Verbose = true
		case "--config":
			if i+1 < len(args) {
				i++
				g.ConfigPath = args[i]
			}
		default:
			remaining = append(remaining, args[i:]...)
			return g, remaining
		}
	}
	return g, remaining
}

// wantsHelp reports whether the arguments ask what a command takes rather than
// asking it to do anything, which decides whether a broken config is worth
// reporting. "help" counts only in first position, where it is a subcommand
// (`folder help`); anywhere else it is a plausible value for a flag.
func wantsHelp(args []string) bool {
	if len(args) > 0 && args[0] == "help" {
		return true
	}
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "-h" || a == "--help" {
			return true
		}
	}
	return false
}

func handleError(g *cmdutil.GlobalFlags, err error) {
	// Asking for usage is not a failure. The flag package has already written
	// the text by this point, so there is nothing to add and nothing to
	// report; exiting 0 is what separates "you asked" from "you got it wrong",
	// which still exits 2.
	if errors.Is(err, flag.ErrHelp) {
		os.Exit(0)
	}

	code, exitCode, err := classifyError(g, err)

	if g.JSON {
		_ = output.PrintJSON(g.Out(), mail.ErrorResponse{
			Error:   true,
			Code:    code,
			Message: err.Error(),
		})
	} else {
		fmt.Fprintf(g.Err(), "error: %v\n", err)
	}
	os.Exit(exitCode)
}

// classifyError maps an error to the code and exit status the CLI reports for
// it, and to the error it describes, which an interrupt rewrites. Separate
// from handleError so it can be tested without the os.Exit.
func classifyError(g *cmdutil.GlobalFlags, err error) (code string, exitCode int, reported error) {
	// Decided before the command did any work, so nothing that happened
	// afterwards changes what was wrong with the invocation. Exit code 2 has
	// to hold here or a caller cannot tell a mistyped command from a failed
	// one.
	if strings.HasPrefix(err.Error(), "usage:") {
		return "USAGE_ERROR", 2, err
	}

	// An interrupt drops the connection underneath the running command, so
	// whatever error surfaced is a symptom rather than the cause.
	if g.Ctx != nil && g.Ctx.Err() != nil {
		return "INTERRUPTED", 1, fmt.Errorf("interrupted: %w", err)
	}

	code = "UNKNOWN"
	exitCode = 1

	switch {
	case errors.Is(err, mail.ErrNotFound):
		code = "NOT_FOUND"
	case errors.Is(err, mail.ErrAlreadyExists):
		code = "ALREADY_EXISTS"
	case errors.Is(err, mail.ErrAuthFailed):
		code = "AUTH_FAILED"
	case errors.Is(err, mail.ErrConnectionFailed):
		code = "CONNECTION_FAILED"
	case errors.Is(err, mail.ErrSendRejected):
		code = "SEND_REJECTED"
	case errors.Is(err, mail.ErrPendingApproval):
		code = "PENDING_APPROVAL"
	case errors.Is(err, mail.ErrInvalidConfig):
		code = "CONFIG_ERROR"
		exitCode = 2
	}

	return code, exitCode, err
}

func printUsage() {
	fmt.Print(`Usage: bifrost [global options] <command> [options]

Commands:
  inbox        List messages in a folder
  read         Read a message by UID
  search       Search messages (server-side IMAP SEARCH)
  thread       View a conversation thread
  send         Compose and send a message
  reply        Reply to a message
  forward      Forward a message
  delete       Move messages to Trash (--permanent to expunge)
  archive      Archive messages
  move         Move messages to another folder
  mark-read    Mark messages as read
  mark-unread  Mark messages as unread
  flag         Flag messages (\Flagged)
  unflag       Clear the flag on messages
  folder       Manage folders (list, status, create, rename, delete)
  accounts     List configured accounts
  draft        Manage drafts (save, list, send, approve, delete)
  config       Configuration management (init)
  version      Print version
  help         Show this help

Global options:
  --account NAME    Use a specific account
  --json            Output as JSON
  --verbose         Verbose logging to stderr
  --config PATH     Config file path (default: ~/.bifrost/config.json)
`)
}
