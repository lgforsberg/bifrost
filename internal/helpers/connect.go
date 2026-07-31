package helpers

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/lgforsberg/bifrost/internal/config"
	"github.com/lgforsberg/bifrost/mail"
)

// ResolveAccount finds the account specified by name or returns the default.
func ResolveAccount(cfg *config.Config, accountFlag string) (*mail.AccountConfig, error) {
	if accountFlag != "" {
		return config.AccountByAddress(cfg, accountFlag)
	}
	return config.DefaultAccount(cfg)
}

// ConnectIMAP resolves account, creates an IMAP client, connects, and returns it.
// Caller is responsible for calling Close().
func ConnectIMAP(ctx context.Context, cfg *config.Config, accountFlag string, logger *slog.Logger) (*mail.IMAPClient, *mail.AccountConfig, error) {
	acct, err := ResolveAccount(cfg, accountFlag)
	if err != nil {
		return nil, nil, err
	}
	client := mail.NewIMAPClient(*acct, logger)
	if err := client.Connect(ctx); err != nil {
		return nil, nil, err
	}
	return client, acct, nil
}

// UIDValidation holds the result of UID validation: which exist and which were skipped.
type UIDValidation struct {
	Existing []uint32
	Skipped  []uint32
}

// ValidateUIDs checks which of the requested UIDs exist in the folder.
// Returns existing and skipped UIDs. Errors if none exist.
func ValidateUIDs(ctx context.Context, client *mail.IMAPClient, folder string, uids []uint32) (*UIDValidation, error) {
	existing, err := client.CheckUIDsExist(ctx, folder, uids)
	if err != nil {
		return nil, err
	}
	if len(existing) == 0 {
		if len(uids) == 1 {
			return nil, fmt.Errorf("message uid %d in %s: %w", uids[0], folder, mail.ErrNotFound)
		}
		return nil, fmt.Errorf("no messages found with the specified UIDs in %s: %w", folder, mail.ErrNotFound)
	}

	existSet := make(map[uint32]bool, len(existing))
	for _, u := range existing {
		existSet[u] = true
	}
	var skipped []uint32
	for _, u := range uids {
		if !existSet[u] {
			skipped = append(skipped, u)
		}
	}

	return &UIDValidation{Existing: existing, Skipped: skipped}, nil
}
