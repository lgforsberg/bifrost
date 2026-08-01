package mail

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

// SmtpDeliver sends a pre-composed message to the given recipients via SMTP.
// The from parameter is the envelope sender (MAIL FROM) — use opts.From.Address
// so --from overrides propagate to the SMTP envelope for sender-login checks.
func SmtpDeliver(ctx context.Context, config AccountConfig, from string, composedMsg []byte, recipients []string, logger *slog.Logger) error {
	host := config.SMTPHost
	addr := net.JoinHostPort(host, strconv.Itoa(config.SMTPPort))
	username := config.EffectiveUsername()

	logger.Debug("delivering via SMTP",
		"addr", addr,
		"from", from,
		"recipients", recipients,
		"encryption", config.SMTPEncryption,
		"size", len(composedMsg),
	)

	conn, err := dial(ctx, host, config.SMTPPort, config.SMTPEncryption)
	if err != nil {
		return err
	}
	release := closeOnCancel(ctx, conn)
	defer release()

	var client *smtp.Client
	if config.SMTPEncryption == "starttls" {
		client, err = smtp.NewClientStartTLS(conn, tlsConfigFor(host))
		if err != nil {
			conn.Close()
			return fmt.Errorf("SMTP starttls %s: %w: %w", addr, err, ErrConnectionFailed)
		}
	} else {
		client = smtp.NewClient(conn)
	}
	defer client.Close()

	if ok, _ := client.Extension("AUTH"); !ok {
		return fmt.Errorf("SMTP server %s does not support AUTH: %w", addr, ErrAuthFailed)
	}
	if err := client.Auth(sasl.NewPlainClient("", username, config.Password)); err != nil {
		return classifySMTPError(err)
	}
	if err := client.SendMail(from, recipients, bytes.NewReader(composedMsg)); err != nil {
		return classifySMTPError(err)
	}
	if err := client.Quit(); err != nil {
		return classifySMTPError(err)
	}

	logger.Debug("SMTP delivery complete", "recipients", recipients)
	return nil
}

// classifySMTPError maps a delivery failure onto a sentinel. The matching is
// textual because the server's reply reaches us as an opaque string.
func classifySMTPError(err error) error {
	errStr := strings.ToLower(err.Error())
	switch {
	case strings.Contains(errStr, "authentication") || strings.Contains(errStr, "auth"):
		return fmt.Errorf("SMTP auth: %w: %w", err, ErrAuthFailed)
	case strings.Contains(errStr, "550") || strings.Contains(errStr, "553") || strings.Contains(errStr, "rejected"):
		return fmt.Errorf("SMTP send: %w: %w", err, ErrSendRejected)
	case strings.Contains(errStr, "dial") || strings.Contains(errStr, "connection") || strings.Contains(errStr, "timeout"):
		return fmt.Errorf("SMTP connect: %w: %w", err, ErrConnectionFailed)
	default:
		return fmt.Errorf("SMTP send: %w", err)
	}
}
