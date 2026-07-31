package helpers

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// ReadBody returns the message body from the first available source:
// 1. --body flag: value "-" reads stdin; any other value (including "") is used literally
// 2. --body-file flag: read from path
// 3. stdin (if not a terminal and no --body/--body-file was given)
//
// The bodyFlagSet parameter indicates whether --body was explicitly passed,
// distinguishing --body "" (send empty body) from omitting --body entirely.
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
		return "", fmt.Errorf("usage: provide --body, --body-file, or pipe body via stdin")
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
