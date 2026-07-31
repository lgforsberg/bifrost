package commands

import (
	"flag"
	"fmt"
	"os"

	"github.com/lgforsberg/bifrost/internal/cmdutil"
	"github.com/lgforsberg/bifrost/internal/helpers"
	"github.com/lgforsberg/bifrost/internal/output"
)

func Inbox(g *cmdutil.GlobalFlags, args []string) error {
	fs := flag.NewFlagSet("inbox", flag.ContinueOnError)
	folder := fs.String("folder", "INBOX", "folder to list")
	limit := fs.Int("limit", 20, "max messages to return")
	offset := fs.Int("offset", 0, "skip N newest messages")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: %w", err)
	}

	client, _, err := helpers.ConnectIMAP(g.Ctx, g.Config, g.Account, g.Logger)
	if err != nil {
		return err
	}
	defer client.Close()

	envelopes, err := client.ListEnvelopes(g.Ctx, *folder, *limit, *offset)
	if err != nil {
		return err
	}

	if g.JSON {
		return output.PrintJSON(os.Stdout, envelopes)
	}

	if len(envelopes) == 0 {
		fmt.Println("No messages.")
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
		subject := env.Subject
		if len(subject) > 60 {
			subject = subject[:57] + "..."
		}
		rows[i] = []string{
			fmt.Sprintf("%d", env.UID),
			flags,
			env.Date.Format("2006-01-02 15:04"),
			truncate(from, 25),
			subject,
		}
	}
	output.PrintTable(os.Stdout, headers, rows)
	return nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

// bulkResult builds a JSON response for bulk UID operations, including skipped UIDs when relevant.
func bulkResult(status string, v *helpers.UIDValidation) map[string]any {
	result := map[string]any{"status": status, "uids": v.Existing}
	if len(v.Skipped) > 0 {
		result["skippedUids"] = v.Skipped
	}
	return result
}
