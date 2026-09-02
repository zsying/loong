package cli

import "strings"

// spelling renders the conventional form of a flag name: a
// single-letter short flag is written "-x" and a word is written
// "--word" (GNU/POSIX style). The dash count is fixed per name, so
// "-html" and "--h" never alias "--html" or "-h".
func spelling(name string) string {
	if len(name) == 1 {
		return "-" + name
	}
	return "--" + name
}

// HasFlag reports whether args contains any of the given flag names,
// each in its conventional spelling — short "-x" for single letters,
// long "--word" for words — bare or valued ("-o=out" counts as flag
// "o"). List the short and long names of one option together to
// accept both spellings:
//
//	HasFlag(args, "h", "html")  // matches "-h" or "--html"
func HasFlag(args []string, names ...string) bool {
	for _, a := range args {
		for _, name := range names {
			s := spelling(name)
			if a == s || strings.HasPrefix(a, s+"=") {
				return true
			}
		}
	}
	return false
}

// Flag returns the value of the first valued flag among the given
// names, each in its conventional spelling, or ok=false when absent.
// As with HasFlag, list the short and long names of one option
// together to accept both spellings:
//
//	Flag(args, "o", "output")  // matches "-o=out" or "--output=out"
func Flag(args []string, names ...string) (string, bool) {
	for _, a := range args {
		for _, name := range names {
			s := spelling(name)
			if strings.HasPrefix(a, s+"=") {
				return strings.TrimPrefix(a, s+"="), true
			}
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
