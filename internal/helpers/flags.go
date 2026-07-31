package helpers

import (
	"fmt"
	"strconv"
	"strings"
)

// StringSliceFlag implements flag.Value for repeatable string flags.
// Usage: fs.Var(&mySlice, "to", "recipient address (repeatable)")
type StringSliceFlag []string

func (f *StringSliceFlag) String() string {
	return strings.Join(*f, ", ")
}

func (f *StringSliceFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

// ReorderArgs moves flags before positional arguments so Go's flag package
// can parse interspersed flags. Go's flag.FlagSet.Parse stops at the first
// non-flag argument, so "reply 1 --body text" would leave --body unparsed.
// This reorders to "reply --body text 1".
//
// knownBoolFlags lists flag names that don't consume the next argument.
func ReorderArgs(args []string, knownBoolFlags map[string]bool) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
			name := strings.TrimLeft(a, "-")
			if eq := strings.IndexByte(name, '='); eq >= 0 {
				continue // --flag=value already self-contained
			}
			if !knownBoolFlags[name] && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		} else {
			positional = append(positional, a)
		}
	}
	return append(flags, positional...)
}

// ParseUIDs parses one or more UID arguments into uint32 values.
func ParseUIDs(args []string) ([]uint32, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("usage: at least one UID is required")
	}
	uids := make([]uint32, 0, len(args))
	for _, arg := range args {
		n, err := strconv.ParseUint(arg, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid UID %q: %w", arg, err)
		}
		uids = append(uids, uint32(n))
	}
	return uids, nil
}
