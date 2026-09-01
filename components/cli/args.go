package cli

import "strings"

// HasFlag reports whether "--name" is present in args.
func HasFlag(args []string, name string) bool {
	flag := "--" + name
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

// Flag returns the value of "--name=value", or ok=false when absent.
func Flag(args []string, name string) (string, bool) {
	prefix := "--" + name + "="
	for _, a := range args {
		if strings.HasPrefix(a, prefix) {
			return strings.TrimPrefix(a, prefix), true
		}
	}
	return "", false
}

// Positional returns the arguments that are not flags (no leading
// dash), in order.
func Positional(args []string) []string {
	var out []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			out = append(out, a)
		}
	}
	return out
}
