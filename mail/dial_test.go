package mail

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestDeadlineConn_ReadTimesOut(t *testing.T) {
	local, remote := net.Pipe()
	defer remote.Close()

	conn := &deadlineConn{Conn: local, timeout: 50 * time.Millisecond}

	start := time.Now()
	_, err := conn.Read(make([]byte, 1))
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("Read error = %v, want a deadline error", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Read blocked for %v before giving up", elapsed)
	}
}

func TestDeadlineConn_WriteTimesOut(t *testing.T) {
	local, remote := net.Pipe()
	defer remote.Close()

	// Nothing reads from the pipe, so the write has nowhere to go.
	conn := &deadlineConn{Conn: local, timeout: 50 * time.Millisecond}

	_, err := conn.Write([]byte("hello"))
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("Write error = %v, want a deadline error", err)
	}
}

// The timeout is per read, not for the transfer as a whole, so a slow but
// steady stream must not be cut off.
func TestDeadlineConn_IdleTimeoutResetsOnActivity(t *testing.T) {
	local, remote := net.Pipe()
	defer remote.Close()

	conn := &deadlineConn{Conn: local, timeout: 200 * time.Millisecond}

	go func() {
		for i := 0; i < 5; i++ {
			time.Sleep(50 * time.Millisecond)
			if _, err := remote.Write([]byte("x")); err != nil {
				return
			}
		}
	}()

	buf := make([]byte, 1)
	for i := 0; i < 5; i++ {
		if _, err := conn.Read(buf); err != nil {
			t.Fatalf("read %d of a steady stream failed: %v", i, err)
		}
	}
}

func TestCloseOnCancel_ClosesOnCancellation(t *testing.T) {
	local, remote := net.Pipe()
	defer remote.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	release := closeOnCancel(ctx, local)
	defer release()

	// This read would block forever; cancelling has to break it.
	done := make(chan error, 1)
	go func() {
		_, err := local.Read(make([]byte, 1))
		done <- err
	}()

	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("read succeeded, want the connection to have been closed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling the context did not close the connection")
	}
}

func TestCloseOnCancel_ReleaseLeavesConnectionOpen(t *testing.T) {
	local, remote := net.Pipe()
	defer remote.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	release := closeOnCancel(ctx, local)
	release()

	go io.Copy(io.Discard, remote)
	if _, err := local.Write([]byte("still open")); err != nil {
		t.Errorf("connection was closed after release: %v", err)
	}
}

func TestDial_RejectsUnknownEncryption(t *testing.T) {
	_, err := dial(context.Background(), "localhost", 143, "sslv2")
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("dial error = %v, want ErrInvalidConfig", err)
	}
}

// A server that accepts the connection and then says nothing used to leave
// Connect blocked for the life of the process, because the IMAP commands take
// no context. Cancelling now has to unblock it.
func TestIMAPConnect_CancellationInterruptsSilentServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		accepted <- conn // hold it open without ever sending a greeting
	}()

	host, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("splitting listener address: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parsing port: %v", err)
	}

	client := NewIMAPClient(AccountConfig{
		Address:        "user@example.com",
		IMAPHost:       host,
		IMAPPort:       port,
		IMAPEncryption: "none",
		Password:       "secret",
	}, discardLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- client.Connect(ctx) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Connect succeeded against a server that never replied")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Connect did not return: cancellation is not reaching the connection")
	}

	select {
	case conn := <-accepted:
		conn.Close()
	default:
	}
}
