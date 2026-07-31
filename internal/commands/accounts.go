package commands

import (
	"fmt"
	"os"

	"github.com/lgforsberg/bifrost/internal/cmdutil"
	"github.com/lgforsberg/bifrost/internal/config"
	"github.com/lgforsberg/bifrost/internal/output"
)

type accountOutput struct {
	Address     string `json:"address"`
	DisplayName string `json:"displayName"`
	Default     bool   `json:"default"`
	IMAPHost    string `json:"imapHost"`
	SMTPHost    string `json:"smtpHost"`
}

func Accounts(g *cmdutil.GlobalFlags, args []string) error {
	if g.JSON {
		accounts := make([]accountOutput, len(g.Config.Accounts))
		for i, a := range g.Config.Accounts {
			accounts[i] = accountOutput{
				Address:     a.Address,
				DisplayName: a.DisplayName,
				Default:     config.IsDefaultAccount(g.Config, i),
				IMAPHost:    a.IMAPHost,
				SMTPHost:    a.SMTPHost,
			}
		}
		return output.PrintJSON(os.Stdout, accounts)
	}

	headers := []string{"ADDRESS", "NAME", "IMAP", "SMTP"}
	rows := make([][]string, len(g.Config.Accounts))
	for i, a := range g.Config.Accounts {
		marker := ""
		if config.IsDefaultAccount(g.Config, i) {
			marker = " (default)"
		}
		rows[i] = []string{
			a.Address + marker,
			a.DisplayName,
			fmt.Sprintf("%s:%d", a.IMAPHost, a.IMAPPort),
			fmt.Sprintf("%s:%d", a.SMTPHost, a.SMTPPort),
		}
	}
	output.PrintTable(os.Stdout, headers, rows)
	return nil
}
