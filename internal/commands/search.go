package commands

import (
	"flag"
	"fmt"
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
	var keywords helpers.StringSliceFlag
	fs.Var(&keywords, "keyword", "IMAP keyword such as $PendingApproval (repeatable, all must match)")
	var folders helpers.StringSliceFlag
	fs.Var(&folders, "folder", "folder to search (repeatable, default INBOX)")
	allFolders := fs.Bool("all-folders", false, "search every folder the server will let us select")
	limit := fs.Int("limit", 50, "max results")
	withTotal := fs.Bool("with-total", false, "wrap the result to report how many messages matched")
	args = helpers.ReorderArgs(args, map[string]bool{"unread": true, "flagged": true,
		"all-folders": true, "with-total": true})
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: %w", err)
	}

	if *allFolders && len(folders) > 0 {
		return fmt.Errorf("usage: --all-folders and --folder ask for different things; use one")
	}

	hasCriteria := *from != "" || *to != "" || *subject != "" || *body != "" ||
		*unread || *flagged || *since != "" || *before != "" || len(keywords) > 0
	if !hasCriteria {
		return fmt.Errorf("usage: at least one search criterion is required (--from, --to, --subject, --body, --unread, --flagged, --keyword, --since, --before)")
	}

	criteria := mail.SearchCriteria{
		From:     *from,
		To:       *to,
		Subject:  *subject,
		Body:     *body,
		Unseen:   *unread,
		Flagged:  *flagged,
		Keywords: keywords,
		Limit:    *limit,
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

	targets, err := searchTargets(g, client, folders, *allFolders)
	if err != nil {
		return err
	}

	page, err := client.SearchFoldersPage(g.Ctx, targets, criteria)
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
		fmt.Fprintln(g.Out(), "No matching messages.")
		return nil
	}

	// A UID means nothing without the folder it belongs to, so results drawn
	// from more than one get a column saying which. One folder needs no such
	// column: it was named on the command line.
	showFolder := len(targets) > 1

	headers := []string{"UID", "FLAGS", "DATE", "FROM", "SUBJECT"}
	if showFolder {
		headers = []string{"UID", "FOLDER", "FLAGS", "DATE", "FROM", "SUBJECT"}
	}

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

		row := []string{fmt.Sprintf("%d", env.UID)}
		if showFolder {
			row = append(row, truncate(env.Folder, 20))
		}
		rows[i] = append(row,
			flags,
			env.Date.Format("2006-01-02 15:04"),
			truncate(fromStr, 25),
			truncate(env.Subject, 60),
		)
	}
	output.PrintTable(g.Out(), headers, rows)
	if *withTotal {
		fmt.Fprintf(g.Out(), "\nShowing %d of %d matches.\n", len(envelopes), page.Total)
	}
	return nil
}

// searchTargets works out which folders to search: the ones named, or every
// one the server will let us select, or INBOX when nothing was asked for.
//
// Duplicates are dropped rather than searched twice, since --folder INBOX
// --folder INBOX would otherwise report every match in it twice over.
func searchTargets(g *cmdutil.GlobalFlags, client *mail.IMAPClient, named []string, all bool) ([]string, error) {
	if all {
		found, err := client.SelectableFolders(g.Ctx)
		if err != nil {
			return nil, err
		}
		if len(found) == 0 {
			return nil, fmt.Errorf("no folders can be searched on this account: %w", mail.ErrNotFound)
		}
		return found, nil
	}

	if len(named) == 0 {
		return []string{"INBOX"}, nil
	}

	seen := make(map[string]bool, len(named))
	targets := make([]string, 0, len(named))
	for _, f := range named {
		if seen[f] {
			continue
		}
		seen[f] = true
		targets = append(targets, f)
	}
	return targets, nil
}
