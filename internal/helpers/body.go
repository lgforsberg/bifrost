package helpers

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// ErrNoBody reports that no body was given at all, as opposed to one that was
// given and could not be read. The difference matters because a message
// carried entirely by its HTML part has no plain-text source and is still
// perfectly valid.
var ErrNoBody = errors.New("usage: provide --body, --body-file, --body-html, or pipe body via stdin")

// BodyFlags is the set of body flags shared by every command that composes a
// message, registered in one place so the four of them cannot drift apart.
type BodyFlags struct {
	fs       *flag.FlagSet
	text     *string
	textFile *string
	html     *string
	htmlFile *string
}

// RegisterBodyFlags adds the body flags to fs. The description varies because
// "reply body" reads better than "message body" under `reply --help`.
func RegisterBodyFlags(fs *flag.FlagSet, textDescription string) *BodyFlags {
	return &BodyFlags{
		fs:       fs,
		text:     fs.String("body", "", textDescription),
		textFile: fs.String("body-file", "", "read body from file"),
		html:     fs.String("body-html", "", "HTML body, sent alongside the plain text one"),
		htmlFile: fs.String("body-html-file", "", "read the HTML body from file"),
	}
}

// Read returns the plain-text and HTML bodies. Call it after fs.Parse.
//
// Either may be empty but not both. When only HTML is given the text body is
// left empty on purpose: composition derives the plain-text alternative from
// the markup, which is a better fallback than anything guessed here.
func (b *BodyFlags) Read() (text, html string, err error) {
	html, err = b.readHTML()
	if err != nil {
		return "", "", err
	}

	text, err = ReadBody(*b.text, b.wasSet("body"), *b.textFile)
	if err != nil {
		// Only the complaint about having nothing at all is waived, and only
		// when the HTML body answers it. A source that was named and could
		// not be read is still an error.
		if html != "" && errors.Is(err, ErrNoBody) {
			return "", html, nil
		}
		return "", "", err
	}
	return text, html, nil
}

// readHTML never falls back to stdin. A pipe cannot say which of the two
// bodies it meant, and quietly picking one would be worse than asking.
func (b *BodyFlags) readHTML() (string, error) {
	if b.wasSet("body-html") {
		return *b.html, nil
	}
	if *b.htmlFile != "" {
		data, err := os.ReadFile(*b.htmlFile)
		if err != nil {
			return "", fmt.Errorf("reading HTML body file: %w", err)
		}
		return string(data), nil
	}
	return "", nil
}

func (b *BodyFlags) wasSet(name string) bool {
	set := false
	b.fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

// ReadBody returns the message body from the first available source:
// 1. --body flag: value "-" reads stdin; any other value (including "") is used literally
// 2. --body-file flag: read from path
// 3. stdin (if not a terminal and no --body/--body-file was given)
//
// The bodyFlagSet parameter indicates whether --body was explicitly passed,
// distinguishing --body "" (send empty body) from omitting --body entirely.
//
// Returns ErrNoBody when no source was given at all.
func ReadBody(bodyFlag string, bodyFlagSet bool, bodyFileFlag string) (string, error) {
	if bodyFlagSet {
		if bodyFlag == "-" {
			return readStdin()
		}
		return bodyFlag, nil
	}

	if bodyFileFlag != "" {
		data, err := os.ReadFile(bodyFileFlag)
		if err != nil {
			return "", fmt.Errorf("reading body file: %w", err)
		}
		return string(data), nil
	}

	if term.IsTerminal(int(os.Stdin.Fd())) {
		return "", ErrNoBody
	}

	return readStdin()
}

func readStdin() (string, error) {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("reading stdin: %w", err)
	}
	return strings.TrimRight(string(data), "\n"), nil
}
