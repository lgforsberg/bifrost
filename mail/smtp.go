package mail

import (
	"bytes"
	"context"
	"errors"
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

	conn, err := dial(ctx, host, config.SMTPPort, config.SMTPEncryption, config.Timeout)
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

	// Greet explicitly. Extension swallows the greeting error, so a connection
	// that died on arrival would otherwise be reported as a server that does
	// not support AUTH. The name matches what the library sends by default.
	if err := client.Hello("localhost"); err != nil {
		return classifySMTPError("greeting", err, ErrConnectionFailed)
	}
	ok, offered := client.Extension("AUTH")
	if !ok {
		return fmt.Errorf("SMTP server %s does not support AUTH: %w", addr, ErrAuthFailed)
	}

	mechanism, saslClient, err := config.saslExchange(ctx)
	if err != nil {
		return err
	}
	if saslClient == nil {
		saslClient = sasl.NewPlainClient("", username, config.Password)
	} else if !mechanismOffered(mechanism, strings.Fields(offered)) {
		// Same reasoning as on the IMAP side: a mechanism the server never
		// offered and a token with the wrong scope both come back as an
		// authentication failure, and they are fixed in different places.
		return fmt.Errorf("SMTP server %s does not offer %s, only %s: %w",
			addr, mechanism, offered, ErrAuthFailed)
	}

	if err := client.Auth(saslClient); err != nil {
		if detail := saslFailureDetail(saslClient); detail != "" {
			return classifySMTPError("auth", fmt.Errorf("%w (server said: %s)", err, detail), ErrAuthFailed)
		}
		return classifySMTPError("auth", err, ErrAuthFailed)
	}
	if err := client.SendMail(from, recipients, bytes.NewReader(composedMsg)); err != nil {
		return classifySMTPError("send", err, ErrSendRejected)
	}

	// The server has accepted the message by this point. Failing to close down
	// cleanly does not un-deliver it, and reporting an error here would invite
	// a retry that delivers it twice.
	if err := client.Quit(); err != nil {
		logger.Debug("SMTP quit failed after the message was accepted", "err", err)
	}

	logger.Debug("SMTP delivery complete", "recipients", recipients)
	return nil
}

// classifySMTPError maps a failure at the named stage onto a sentinel. A reply
// from the server carries a status code and tells us what it thought of the
// request; anything else means the exchange itself broke, whatever stage it
// reached. The rejected sentinel applies when the server answered and refused
// permanently: a 4xx is the server asking for a retry, not a refusal.
func classifySMTPError(stage string, err error, rejected error) error {
	var smtpErr *smtp.SMTPError
	if !errors.As(err, &smtpErr) {
		return fmt.Errorf("SMTP %s: %w: %w", stage, err, ErrConnectionFailed)
	}
	if smtpErr.Code >= 500 {
		return fmt.Errorf("SMTP %s: %w: %w", stage, err, rejected)
	}
	return fmt.Errorf("SMTP %s: %w", stage, err)
}
