package commands

import (
	"flag"
	"fmt"
	"unicode/utf8"

	"github.com/lgforsberg/bifrost/internal/cmdutil"
	"github.com/lgforsberg/bifrost/internal/helpers"
	"github.com/lgforsberg/bifrost/internal/output"
)

func Inbox(g *cmdutil.GlobalFlags, args []string) error {
	fs := flag.NewFlagSet("inbox", flag.ContinueOnError)
	folder := fs.String("folder", "INBOX", "folder to list")
	limit := fs.Int("limit", 20, "max messages to return")
	offset := fs.Int("offset", 0, "skip N newest messages")
	withTotal := fs.Bool("with-total", false, "wrap the result to report how many messages the folder holds")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: %w", err)
	}

	client, _, err := helpers.ConnectIMAP(g.Ctx, g.Config, g.Account, g.Logger)
	if err != nil {
		return err
	}
	defer client.Close()

	page, err := client.ListEnvelopePage(g.Ctx, *folder, *limit, *offset)
	if err != nil {
		return err
	}
	envelopes := page.Messages

	if g.JSON {
		if *withTotal {
			return output.PrintJSON(g.Out(), page)
		}
		return output.PrintJSON(g.Out(), envelopes)
	}

	if len(envelopes) == 0 {
		fmt.Fprintln(g.Out(), "No messages.")
		return nil
	}

	headers := []string{"UID", "FLAGS", "DATE", "FROM", "SUBJECT"}
	rows := make([][]string, len(envelopes))
	for i, env := range envelopes {
		flags := ""
		for _, f := range env.Flags {
			switch f {
			case "\\Seen":
				flags += "R"
			case "\\Flagged":
				flags += "F"
			case "\\Answered":
				flags += "A"
			case "\\Draft":
				flags += "D"
			}
		}
		from := env.From.Address
		if env.From.Name != "" {
			from = env.From.Name
		}
		rows[i] = []string{
			fmt.Sprintf("%d", env.UID),
			flags,
			env.Date.Format("2006-01-02 15:04"),
			truncate(from, 25),
			truncate(env.Subject, 60),
		}
	}
	output.PrintTable(g.Out(), headers, rows)
	if *withTotal {
		fmt.Fprintf(g.Out(), "\nShowing %d of %d.\n", len(envelopes), page.Total)
	}
	return nil
}

// truncate shortens s to at most max characters, marking the cut with an
// ellipsis. It counts runes rather than bytes: a byte slice would cut a
// multi-byte character in half and emit the replacement glyph, and it would
// also shorten a subject in Greek or Japanese to a fraction of the column it
// was given. Runes are the unit fmt pads with, so this and the table agree.
func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	// Counting first avoids allocating the slice for the overwhelming
	// majority of values, which are short enough to pass through.
	if utf8.RuneCountInString(s) <= max {
		return s
	}

	r := []rune(s)
	if max <= len(ellipsis) {
		return string(r[:max])
	}
	return string(r[:max-len(ellipsis)]) + ellipsis
}

const ellipsis = "..."

// bulkResult builds a JSON response for bulk UID operations, including skipped UIDs when relevant.
func bulkResult(status string, v *helpers.UIDValidation) map[string]any {
	result := map[string]any{"status": status, "uids": v.Existing}
	if len(v.Skipped) > 0 {
		result["skippedUids"] = v.Skipped
	}
	return result
}
