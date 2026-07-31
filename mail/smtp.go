package mail

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

// SmtpDeliver sends a pre-composed message to the given recipients via SMTP.
// The from parameter is the envelope sender (MAIL FROM) — use opts.From.Address
// so --from overrides propagate to the SMTP envelope for sender-login checks.
func SmtpDeliver(ctx context.Context, config AccountConfig, from string, composedMsg []byte, recipients []string, logger *slog.Logger) error {
	addr := fmt.Sprintf("%s:%d", config.SMTPHost, config.SMTPPort)
	username := config.EffectiveUsername()

	logger.Debug("delivering via SMTP",
		"addr", addr,
		"from", from,
		"recipients", recipients,
		"encryption", config.SMTPEncryption,
		"size", len(composedMsg),
	)

	auth := sasl.NewPlainClient("", username, config.Password)
	r := bytes.NewReader(composedMsg)

	var err error
	switch config.SMTPEncryption {
	case "starttls":
		err = smtp.SendMail(addr, auth, from, recipients, r)
	case "tls":
		err = smtp.SendMailTLS(addr, auth, from, recipients, r)
	case "none":
		err = smtp.SendMail(addr, auth, from, recipients, r)
	default:
		return fmt.Errorf("unsupported SMTP encryption %q: %w", config.SMTPEncryption, ErrInvalidConfig)
	}

	if err != nil {
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

	logger.Debug("SMTP delivery complete", "recipients", recipients)
	return nil
}
