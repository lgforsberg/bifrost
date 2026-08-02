// Package testimap starts an in-process IMAP server for tests, in the spirit
// of net/http/httptest. It depends on nothing else in this module, so both the
// mail package and the command layer can use it without an import cycle.
package testimap

import (
	"net"
	"strconv"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"slices"
)

// Hooks make the memory server do two things it has no way to express on its
// own, both of which real servers do.
type Hooks struct {
	// Listing replaces what LIST reports. imapmemserver never sets
	// special-use attributes, so this is the only way to hand a client a
	// mailbox that says what it is for. Mailboxes still have to exist for
	// anything beyond resolving a name.
	Listing []imap.ListData

	// RefuseDelete fails STORE +\Deleted, which is how a mailbox nobody may
	// delete from behaves. Only that flag is refused, so appending and
	// reading still work.
	RefuseDelete bool
}

// Server is a running IMAP server and the user account on it.
type Server struct {
	Host string
	Port int
	User *imapmemserver.User
}

// Start brings up a server holding one user with an empty INBOX, and stops it
// when the test finishes. The server speaks IMAP4rev2, which is what brings
// MOVE and UIDPLUS.
func Start(t *testing.T, username, password string, hooks Hooks) *Server {
	t.Helper()

	user := imapmemserver.NewUser(username, password)
	if err := user.Create("INBOX", nil); err != nil {
		t.Fatalf("creating INBOX: %v", err)
	}

	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return &hookedSession{
				UserSession: imapmemserver.NewUserSession(user),
				hooks:       hooks,
			}, nil, nil
		},
		Caps:         imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapIMAP4rev2: {}},
		InsecureAuth: true,
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	host, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("splitting listener address: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parsing port: %v", err)
	}

	return &Server{Host: host, Port: port, User: user}
}

// hookedSession is the embedding imapmemserver documents: everything is
// promoted from UserSession, including MOVE, and the hooks override two
// commands.
type hookedSession struct {
	*imapmemserver.UserSession
	hooks Hooks
}

var _ imapserver.SessionIMAP4rev2 = (*hookedSession)(nil)

func (s *hookedSession) List(w *imapserver.ListWriter, ref string, patterns []string, options *imap.ListOptions) error {
	if s.hooks.Listing == nil {
		return s.UserSession.List(w, ref, patterns, options)
	}
	for i := range s.hooks.Listing {
		if err := w.WriteList(&s.hooks.Listing[i]); err != nil {
			return err
		}
	}
	return nil
}

func (s *hookedSession) Store(w *imapserver.FetchWriter, numSet imap.NumSet, flags *imap.StoreFlags, options *imap.StoreOptions) error {
	if s.hooks.RefuseDelete && flags != nil && slices.Contains(flags.Flags, imap.FlagDeleted) {
		return &imap.Error{
			Type: imap.StatusResponseTypeNo,
			Text: "Permission denied",
		}
	}
	return s.UserSession.Store(w, numSet, flags, options)
}
