package helpers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lgforsberg/bifrost/mail"
)

// Attachment names come from the sender, so they must never place a file
// outside the requested directory.
func TestSaveAttachments_ContainsHostileNames(t *testing.T) {
	tests := map[string]struct {
		filename string
		wantFile string
	}{
		"parent traversal":   {"../../escaped.txt", "escaped.txt"},
		"deep traversal":     {"../../../.ssh/authorized_keys", "authorized_keys"},
		"absolute path":      {"/etc/passwd", "passwd"},
		"windows path":       {`C:\Windows\System32\evil.dll`, "evil.dll"},
		"trailing separator": {"payload/", "payload"},
		"empty name":         {"", "attachment-0.bin"},
		"dot":                {".", "attachment-0.bin"},
		"dotdot":             {"..", "attachment-0.bin"},
		"bare separator":     {"/", "attachment-0.bin"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "out")

			err := SaveAttachments(dir, []mail.Attachment{
				{Filename: tt.filename, Data: []byte("payload")},
			})
			if err != nil {
				t.Fatalf("SaveAttachments error: %v", err)
			}

			got, err := os.ReadDir(dir)
			if err != nil {
				t.Fatalf("reading attachment dir: %v", err)
			}
			if len(got) != 1 || got[0].Name() != tt.wantFile {
				var names []string
				for _, e := range got {
					names = append(names, e.Name())
				}
				t.Fatalf("directory contains %v, want [%s]", names, tt.wantFile)
			}

			// Nothing may exist beside the target directory.
			outside, err := os.ReadDir(root)
			if err != nil {
				t.Fatalf("reading root: %v", err)
			}
			if len(outside) != 1 || outside[0].Name() != "out" {
				t.Errorf("attachment escaped into the parent directory: %v", outside)
			}
		})
	}
}

func TestSaveAttachments_UniqueNames(t *testing.T) {
	dir := t.TempDir()

	// The first two collide outright; the third collides with the name
	// generated for the second, and is suffixed from its own base name.
	err := SaveAttachments(dir, []mail.Attachment{
		{Filename: "a/report.pdf", Data: []byte("first")},
		{Filename: "b/report.pdf", Data: []byte("second")},
		{Filename: "report-1.pdf", Data: []byte("third")},
	})
	if err != nil {
		t.Fatalf("SaveAttachments error: %v", err)
	}

	want := map[string]string{
		"report.pdf":     "first",
		"report-1.pdf":   "second",
		"report-1-1.pdf": "third",
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading attachment dir: %v", err)
	}
	if len(entries) != len(want) {
		t.Fatalf("wrote %d files, want %d (an attachment was overwritten)", len(entries), len(want))
	}
	for name, content := range want {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Errorf("reading %s: %v", name, err)
			continue
		}
		if string(data) != content {
			t.Errorf("%s = %q, want %q", name, data, content)
		}
	}
}

func TestSaveAttachments_Permissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "out")

	err := SaveAttachments(dir, []mail.Attachment{
		{Filename: "secret.txt", Data: []byte("payload")},
	})
	if err != nil {
		t.Fatalf("SaveAttachments error: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "secret.txt"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("attachment mode = %04o, want 0600", perm)
	}
}
