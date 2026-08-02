package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/lgforsberg/bifrost/internal/cmdutil"
	"github.com/lgforsberg/bifrost/internal/commands"
	"github.com/lgforsberg/bifrost/internal/config"
	"github.com/lgforsberg/bifrost/internal/output"
	"github.com/lgforsberg/bifrost/mail"
)

// version is what this binary claims to be when the build carries no module
// version of its own, which is the case for every build made from a source
// tree rather than resolved from a tag.
const version = "1.22.0"

// buildInfo is what the running binary actually is. Revision and Modified come
// from the VCS stamps the toolchain embeds and are absent from a build made
// outside a repository, which is why both are omitted when empty.
type buildInfo struct {
	Version  string `json:"version"`
	Revision string `json:"revision,omitempty"`
	Modified bool   `json:"modified,omitempty"`
}

// releaseTag matches the version of a build made from a released tag. The
// toolchain also reports pseudo-versions, which encode a timestamp and a
// commit for anything built between tags, and appends +dirty to either when
// the tree has uncommitted changes. Neither is a release, and neither matches.
var releaseTag = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// describeBuild reports what this binary actually is: a version, and the
// commit it came from when there is one.
//
// The version stamped in by the toolchain wins over the constant, but only for
// a clean build at a release tag. There it is the better answer, because it
// comes from the tag itself and so cannot drift the way a hand-maintained
// constant can. Anywhere else it describes the last tag rather than the source
// that was compiled, which during development is the previous release: the
// constant is what the working tree claims to be, and the commit alongside it
// says which tree that was.
func describeBuild() buildInfo {
	b := buildInfo{Version: version}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return b
	}

	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			b.Revision = setting.Value
		case "vcs.modified":
			b.Modified = setting.Value == "true"
		}
	}

	if !b.Modified && releaseTag.MatchString(info.Main.Version) {
		b.Version = strings.TrimPrefix(info.Main.Version, "v")
	}
	return b
}

// String renders the build for a person to read, naming the commit only when
// there is one and it says something the version does not.
func (b buildInfo) String() string {
	if b.Revision == "" {
		return b.Version
	}

	short := b.Revision
	if len(short) > 12 {
		short = short[:12]
	}
	if b.Modified {
		return fmt.Sprintf("%s (%s, modified)", b.Version, short)
	}
	return fmt.Sprintf("%s (%s)", b.Version, short)
}

func main() {
	globals, args, flagErr := parseGlobalFlags(os.Args[1:])

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

	// Reported only now, so a --json that was parsed before the bad flag still
	// decides the shape of the complaint.
	if flagErr != nil {
		handleError(&globals, flagErr)
	}

	if len(args) == 0 {
		printUsage()
		os.Exit(2)
	}

	cmd := args[0]
	cmdArgs := args[1:]

	switch cmd {
	case "version":
		build := describeBuild()
		if globals.JSON {
			_ = output.PrintJSON(globals.Out(), build)
		} else {
			fmt.Fprintf(globals.Out(), "bifrost %s\n", build)
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

// parseGlobalFlags consumes the flags that precede the command name and hands
// back everything from the command onwards untouched, so a subcommand's own
// flags reach its flag set exactly as they were written.
//
// Both --flag value and --flag=value are taken, with one dash or two, matching
// what the flag package accepts for every other flag in the program. A flag
// that needs a value and is not given one is an error: it used to be ignored,
// which turned `bifrost --account send ...` into a message sent from the
// default account rather than a complaint about the missing name.
func parseGlobalFlags(args []string) (cmdutil.GlobalFlags, []string, error) {
	var g cmdutil.GlobalFlags

	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			return g, args[i+1:], nil
		}

		name, value, hasValue := splitFlag(args[i])

		// Reads the value for a flag that takes one, from after the equals
		// sign or from the next argument. An empty one is refused as firmly
		// as a missing one: `--account="$ACCT"` with the variable unset is
		// the same mistake arriving by a different route, and letting it
		// through would quietly use the default account.
		takeValue := func() error {
			if !hasValue {
				if i+1 >= len(args) {
					return fmt.Errorf("usage: --%s needs a value", name)
				}
				i++
				value = args[i]
			}
			if value == "" {
				return fmt.Errorf("usage: --%s was given an empty value", name)
			}
			return nil
		}

		switch name {
		case "json", "verbose":
			on := true
			if hasValue {
				parsed, err := strconv.ParseBool(value)
				if err != nil {
					return g, nil, fmt.Errorf("usage: --%s takes true or false, not %q", name, value)
				}
				on = parsed
			}
			if name == "json" {
				g.JSON = on
			} else {
				g.Verbose = on
			}
		case "account":
			if err := takeValue(); err != nil {
				return g, nil, err
			}
			g.Account = value
		case "config":
			if err := takeValue(); err != nil {
				return g, nil, err
			}
			g.ConfigPath = value
		case "help", "h":
			// Dispatched as a command of its own, so it passes through.
			return g, args[i:], nil
		default:
			// Anything shaped like a flag in this position was meant to be a
			// global one. Passing it on makes it the command name, and the
			// failure that follows is about the config rather than the typo.
			if name != "" {
				return g, nil, fmt.Errorf("usage: unknown global option %q", args[i])
			}
			return g, args[i:], nil
		}
	}
	return g, nil, nil
}

// splitFlag breaks an argument into a flag name and, if it carried one after
// an equals sign, its value. Anything that is not a flag reports an empty
// name, which no case matches.
func splitFlag(arg string) (name, value string, hasValue bool) {
	if len(arg) < 2 || arg[0] != '-' {
		return "", "", false
	}
	name = strings.TrimPrefix(arg[1:], "-")
	if i := strings.IndexByte(name, '='); i >= 0 {
		return name[:i], name[i+1:], true
	}
	return name, "", false
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
