package helpers

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/lgforsberg/bifrost/mail"
)

// SaveAttachments writes each attachment into dir, deriving a safe, unique file
// name from the sender-supplied one. The directory is created if needed.
func SaveAttachments(dir string, attachments []mail.Attachment) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating attachment directory: %w", err)
	}

	taken := make(map[string]bool, len(attachments))
	for i, att := range attachments {
		name := uniqueAttachmentName(
			safeAttachmentName(att.Filename, fmt.Sprintf("attachment-%d.bin", i)),
			taken,
		)

		dest := filepath.Join(dir, name)
		if filepath.Dir(dest) != filepath.Clean(dir) {
			return fmt.Errorf("attachment %q would be written outside %s", att.Filename, dir)
		}
		if err := os.WriteFile(dest, att.Data, 0600); err != nil {
			return fmt.Errorf("saving attachment %s: %w", name, err)
		}
	}
	return nil
}

// safeAttachmentName reduces a sender-supplied filename to a bare file name so
// it cannot escape the directory it is written into. Both separators are
// flattened first, because a Windows-style path is a legal file name on Unix
// and would otherwise survive intact. Names carrying nothing usable, such as
// ".." or a bare separator, fall back.
func safeAttachmentName(name, fallback string) string {
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))

	switch name {
	case "", ".", "..", "/":
		return fallback
	}
	if strings.ContainsRune(name, 0) {
		return fallback
	}
	return name
}

// uniqueAttachmentName probes for a free name so that attachments reducing to
// the same file name do not overwrite each other. Sanitizing makes such
// collisions more likely, since distinct paths can share a base name.
func uniqueAttachmentName(name string, taken map[string]bool) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)

	candidate := name
	for i := 1; taken[candidate]; i++ {
		candidate = fmt.Sprintf("%s-%d%s", base, i, ext)
	}
	taken[candidate] = true
	return candidate
}
