package helpers

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// read parses args through a fresh flag set, which is how the commands use it.
func read(t *testing.T, args ...string) (text, html string, err error) {
	t.Helper()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(nil)
	bodies := RegisterBodyFlags(fs, "message body")
	if parseErr := fs.Parse(args); parseErr != nil {
		t.Fatalf("parsing %v: %v", args, parseErr)
	}
	return bodies.Read()
}

func TestBodyFlags_Read(t *testing.T) {
	dir := t.TempDir()
	textPath := filepath.Join(dir, "body.txt")
	htmlPath := filepath.Join(dir, "body.html")
	if err := os.WriteFile(textPath, []byte("from a file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(htmlPath, []byte("<p>from a file</p>"), 0o600); err != nil {
		t.Fatal(err)
	}

	for name, tc := range map[string]struct {
		args           []string
		wantText, want string
	}{
		"text alone": {
			args:     []string{"--body", "hello"},
			wantText: "hello",
		},
		"html alone leaves the text body empty for composition to derive": {
			args: []string{"--body-html", "<p>hello</p>"},
			want: "<p>hello</p>",
		},
		"both are kept as given": {
			args:     []string{"--body", "hello", "--body-html", "<p>hello</p>"},
			wantText: "hello",
			want:     "<p>hello</p>",
		},
		"both from files": {
			args:     []string{"--body-file", textPath, "--body-html-file", htmlPath},
			wantText: "from a file",
			want:     "<p>from a file</p>",
		},
		"an explicit empty text body is honoured": {
			args:     []string{"--body", "", "--body-html", "<p>hello</p>"},
			wantText: "",
			want:     "<p>hello</p>",
		},
		"the flag wins over the file": {
			args:     []string{"--body-html", "<p>flag</p>", "--body-html-file", htmlPath},
			wantText: "",
			want:     "<p>flag</p>",
		},
	} {
		t.Run(name, func(t *testing.T) {
			text, html, err := read(t, tc.args...)
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if text != tc.wantText {
				t.Errorf("text = %q, want %q", text, tc.wantText)
			}
			if html != tc.want {
				t.Errorf("html = %q, want %q", html, tc.want)
			}
		})
	}
}

// A source that was named and cannot be read is an error, never a silent
// empty body. That is the half of ErrNoBody's contract that must not be
// waived when an HTML body is present.
func TestBodyFlags_ReadReportsAnUnreadableFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.html")

	if _, _, err := read(t, "--body-html-file", missing); err == nil {
		t.Error("a missing HTML body file was not reported")
	} else if !strings.Contains(err.Error(), "reading HTML body file") {
		t.Errorf("error = %v, want it to name the HTML body file", err)
	}

	_, _, err := read(t, "--body-file", missing, "--body-html", "<p>hi</p>")
	if err == nil {
		t.Error("a missing body file was excused because an HTML body was given")
	}
	if errors.Is(err, ErrNoBody) {
		t.Errorf("error = %v, want a read failure rather than ErrNoBody", err)
	}
}
