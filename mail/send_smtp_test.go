package mail

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// testSMTPServer is a minimal in-process SMTP server that records what it was
// handed. It covers the delivery half of the send path; the scripted IMAP side
// is T-013.
type testSMTPServer struct {
	host string
	port int

	mu       sync.Mutex
	username string
	envFrom  string
	envRcpt  []string
	data     []byte

	once      sync.Once
	delivered chan struct{}
}

func newTestSMTPServer(t *testing.T) *testSMTPServer {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	host, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("splitting listener address: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parsing port: %v", err)
	}

	ts := &testSMTPServer{host: host, port: port, delivered: make(chan struct{})}

	srv := smtp.NewServer(smtp.BackendFunc(func(*smtp.Conn) (smtp.Session, error) {
		return &testSMTPSession{srv: ts}, nil
	}))
	srv.Domain = "localhost"
	srv.AllowInsecureAuth = true
	srv.ReadTimeout = 10 * time.Second
	srv.WriteTimeout = 10 * time.Second

	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	return ts
}

type testSMTPSession struct {
	srv *testSMTPServer
}

func (s *testSMTPSession) AuthMechanisms() []string { return []string{sasl.Plain} }

func (s *testSMTPSession) Auth(string) (sasl.Server, error) {
	return sasl.NewPlainServer(func(_, username, _ string) error {
		s.srv.mu.Lock()
		defer s.srv.mu.Unlock()
		s.srv.username = username
		return nil
	}), nil
}

func (s *testSMTPSession) Mail(from string, _ *smtp.MailOptions) error {
	s.srv.mu.Lock()
	defer s.srv.mu.Unlock()
	s.srv.envFrom = from
	return nil
}

func (s *testSMTPSession) Rcpt(to string, _ *smtp.RcptOptions) error {
	s.srv.mu.Lock()
	defer s.srv.mu.Unlock()
	s.srv.envRcpt = append(s.srv.envRcpt, to)
	return nil
}

func (s *testSMTPSession) Data(r io.Reader) error {
	b, err := io.ReadAll(r)
	s.srv.mu.Lock()
	s.srv.data = b
	s.srv.mu.Unlock()
	s.srv.once.Do(func() { close(s.srv.delivered) })
	return err
}

func (s *testSMTPSession) Reset()        {}
func (s *testSMTPSession) Logout() error { return nil }

// Exercises delivery against a real server: that an unencrypted account can
// reach one at all, that the reported Message-ID is the one that went out, and
// that blind recipients are addressed in the envelope without being named in
// the message the server receives.
func TestSend_DeliversOverPlaintext(t *testing.T) {
	srv := newTestSMTPServer(t)

	account := AccountConfig{
		Address:        "alice@example.com",
		SMTPHost:       srv.host,
		SMTPPort:       srv.port,
		SMTPEncryption: "none",
		Password:       "secret",
	}
	opts := SendOptions{
		From:     Address{Address: "alice@example.com"},
		To:       []Address{{Address: "bob@example.com"}},
		Cc:       []Address{{Address: "carol@example.com"}},
		Bcc:      []Address{{Address: "dave@example.com"}},
		Subject:  "Hello",
		TextBody: "Body",
	}

	res, err := Send(context.Background(), account, nil, opts, false, discardLogger())
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if res.MessageID == "" {
		t.Error("SendResult.MessageID is empty, want the id the message went out with")
	}
	if len(res.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", res.Warnings)
	}

	select {
	case <-srv.delivered:
	case <-time.After(10 * time.Second):
		t.Fatal("the server never received the message")
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()

	if srv.username != "alice@example.com" {
		t.Errorf("authenticated as %q, want the account username", srv.username)
	}
	if srv.envFrom != "alice@example.com" {
		t.Errorf("MAIL FROM = %q, want the sender address", srv.envFrom)
	}
	for _, want := range []string{"bob@example.com", "carol@example.com", "dave@example.com"} {
		if !containsStr(srv.envRcpt, want) {
			t.Errorf("%s missing from the envelope recipients %v", want, srv.envRcpt)
		}
	}

	raw := string(srv.data)
	if strings.Contains(raw, "dave@example.com") {
		t.Error("the delivered message discloses the blind recipient")
	}
	if !strings.Contains(raw, res.MessageID) {
		t.Errorf("the delivered message does not carry the reported Message-ID %q", res.MessageID)
	}
	if !strings.Contains(raw, "Subject: Hello") {
		t.Error("the delivered message is missing its subject")
	}
}

func TestSend_RefusesWithoutRecipients(t *testing.T) {
	opts := SendOptions{
		From:     Address{Address: "alice@example.com"},
		Subject:  "Nobody",
		TextBody: "Body",
	}

	// No SMTP server configured: the check has to happen before any dialling.
	_, err := Send(context.Background(), AccountConfig{}, nil, opts, false, discardLogger())
	if err == nil {
		t.Fatal("Send succeeded with no recipients")
	}
}
