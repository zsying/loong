package cli

import "strings"

// spellings returns both accepted spellings of a flag name: the long
// "--name" form and the short "-name" form.
func spellings(name string) [2]string {
	return [2]string{"--" + name, "-" + name}
}

// HasFlag reports whether args contains any of the given flag names,
// in either the long "--name" or the short "-name" spelling, bare or
// valued ("-o=out" counts as flag "o"). Pass the long and short forms
// together to accept both spellings of the same option:
//
//	HasFlag(args, "h", "html")  // matches "-h" or "--html"
func HasFlag(args []string, names ...string) bool {
	for _, a := range args {
		for _, name := range names {
			for _, s := range spellings(name) {
				if a == s || strings.HasPrefix(a, s+"=") {
					return true
				}
			}
		}
	}
	return false
}

// Flag returns the value of the first "-name=value" or "--name=value"
// argument among the given flag names, or ok=false when absent. As
// with HasFlag, pass the long and short forms together to accept both
// spellings of the same option:
//
//	Flag(args, "o", "output")  // matches "-o=out" or "--output=out"
func Flag(args []string, names ...string) (string, bool) {
	for _, a := range args {
		for _, name := range names {
			for _, s := range spellings(name) {
				if strings.HasPrefix(a, s+"=") {
					return strings.TrimPrefix(a, s+"="), true
				}
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
