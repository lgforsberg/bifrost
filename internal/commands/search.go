package commands

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/lgforsberg/bifrost/internal/cmdutil"
	"github.com/lgforsberg/bifrost/internal/helpers"
	"github.com/lgforsberg/bifrost/internal/output"
	"github.com/lgforsberg/bifrost/mail"
)

func Search(g *cmdutil.GlobalFlags, args []string) error {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	from := fs.String("from", "", "search From header")
	to := fs.String("to", "", "search To header")
	subject := fs.String("subject", "", "search Subject header")
	body := fs.String("body", "", "full-text body search")
	since := fs.String("since", "", "messages since date (YYYY-MM-DD)")
	before := fs.String("before", "", "messages before date (YYYY-MM-DD)")
	unread := fs.Bool("unread", false, "only unseen messages")
	flagged := fs.Bool("flagged", false, "only flagged messages")
	folder := fs.String("folder", "INBOX", "folder to search")
	limit := fs.Int("limit", 50, "max results")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: %w", err)
	}

	hasCriteria := *from != "" || *to != "" || *subject != "" || *body != "" ||
		*unread || *flagged || *since != "" || *before != ""
	if !hasCriteria {
		return fmt.Errorf("usage: at least one search criterion is required (--from, --to, --subject, --body, --unread, --flagged, --since, --before)")
	}

	criteria := mail.SearchCriteria{
		From:    *from,
		To:      *to,
		Subject: *subject,
		Body:    *body,
		Unseen:  *unread,
		Flagged: *flagged,
		Limit:   *limit,
	}

	if *since != "" {
		t, err := time.Parse("2006-01-02", *since)
		if err != nil {
			return fmt.Errorf("usage: invalid --since date: %w", err)
		}
		criteria.Since = &t
	}
	if *before != "" {
		t, err := time.Parse("2006-01-02", *before)
		if err != nil {
			return fmt.Errorf("usage: invalid --before date: %w", err)
		}
		criteria.Before = &t
	}

	client, _, err := helpers.ConnectIMAP(g.Ctx, g.Config, g.Account, g.Logger)
	if err != nil {
		return err
	}
	defer client.Close()

	envelopes, err := client.Search(g.Ctx, *folder, criteria)
	if err != nil {
		return err
	}

	if g.JSON {
		return output.PrintJSON(os.Stdout, envelopes)
	}

	if len(envelopes) == 0 {
		fmt.Println("No matching messages.")
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
			}
		}
		fromStr := env.From.Address
		if env.From.Name != "" {
			fromStr = env.From.Name
		}
		rows[i] = []string{
			fmt.Sprintf("%d", env.UID),
			flags,
			env.Date.Format("2006-01-02 15:04"),
			truncate(fromStr, 25),
			truncate(env.Subject, 60),
		}
	}
	output.PrintTable(os.Stdout, headers, rows)
	return nil
}
