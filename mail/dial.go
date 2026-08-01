package mail

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"time"
)

const (
	// dialTimeout bounds establishing the connection, including the TLS
	// handshake for implicit TLS.
	dialTimeout = 30 * time.Second

	// ioTimeout bounds a single read or write once connected. It is an idle
	// timeout rather than a total one: a large message keeps resetting it as
	// long as bytes keep moving, but a server that goes quiet trips it.
	ioTimeout = 60 * time.Second
)

// dial opens a connection to host:port for the given encryption mode. Implicit
// TLS is negotiated here; for "starttls" the caller upgrades the returned
// connection itself, using tlsConfigFor to name the certificate.
func dial(ctx context.Context, host string, port int, encryption string) (net.Conn, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	dialer := &net.Dialer{Timeout: dialTimeout}

	var conn net.Conn
	var err error
	switch encryption {
	case "tls":
		tlsDialer := &tls.Dialer{NetDialer: dialer, Config: tlsConfigFor(host)}
		conn, err = tlsDialer.DialContext(ctx, "tcp", addr)
	case "starttls", "none":
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	default:
		return nil, fmt.Errorf("unsupported encryption %q: %w", encryption, ErrInvalidConfig)
	}
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w: %w", addr, err, ErrConnectionFailed)
	}

	return &deadlineConn{Conn: conn, timeout: ioTimeout}, nil
}

// tlsConfigFor names the certificate we expect to be presented. The Dial
// helpers in both client libraries derive this from the address they were
// given, but the constructors that take an existing connection do not, so
// handing them a nil config would leave the name empty.
func tlsConfigFor(host string) *tls.Config {
	return &tls.Config{ServerName: host}
}

// deadlineConn arms a fresh deadline before every read and write. Neither
// client library bounds an individual round trip, so without this a server
// that accepts a connection and then stops responding leaves the caller
// blocked for as long as the process runs.
type deadlineConn struct {
	net.Conn
	timeout time.Duration
}

func (c *deadlineConn) Read(b []byte) (int, error) {
	if err := c.Conn.SetReadDeadline(time.Now().Add(c.timeout)); err != nil {
		return 0, err
	}
	return c.Conn.Read(b)
}

func (c *deadlineConn) Write(b []byte) (int, error) {
	if err := c.Conn.SetWriteDeadline(time.Now().Add(c.timeout)); err != nil {
		return 0, err
	}
	return c.Conn.Write(b)
}

// closeOnCancel closes conn when ctx is cancelled. The IMAP and SMTP commands
// take no context and block until the server answers, so dropping the
// connection underneath them is what turns an interrupt into a prompt exit.
// The returned function releases the watcher once the work is done; it leaves
// the connection alone unless ctx was actually cancelled.
func closeOnCancel(ctx context.Context, conn net.Conn) (release func()) {
	watch, release := context.WithCancel(ctx)
	go func() {
		<-watch.Done()
		if ctx.Err() != nil {
			conn.Close()
		}
	}()
	return release
}
