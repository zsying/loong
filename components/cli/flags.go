package cli

import (
	"fmt"
	"strings"
)

// Config is the cli master's config block (decoded in Build).
type Config struct {
	// Default names the child command activated on a bare invocation
	// (no command token in the argv). Empty keeps the legacy behavior:
	// a bare invocation prints usage.
	Default string `yaml:"default,omitempty"`
	// Pre names a lazy child activated once — before the first command
	// of the process — with *Globals. Typical use: process-wide setup
	// derived from global flags (log level, UI language), the
	// componentized equivalent of cobra's PersistentPreRunE. The pre
	// node is not selectable as a command and never runs on the usage
	// path.
	Pre string `yaml:"pre,omitempty"`
	// Flags declares the global flags peeled from the argv before
	// command selection. Flags are peeled anywhere in the argv
	// (POSIX-style); undeclared tokens — including command-specific
	// flags — pass through untouched.
	Flags map[string]FlagSpec `yaml:"flags,omitempty"`
}

// FlagSpec declares one global flag.
type FlagSpec struct {
	// Short is the optional single-letter alias ("-v" for "verbose").
	Short string `yaml:"short,omitempty"`
	// Kind is "bool" (default): bare form means true, "=x" form stores
	// x verbatim; or "value": "=x" form stores x, bare form consumes
	// the next argv token (dangling — no next token — is an error).
	Kind string `yaml:"kind,omitempty"`
}

// Globals is what the peel produces: the values of the declared flags
// plus the argv that remains for the command. It is passed to the pre
// node via Scope.Args; commands receive only Rest ([]string) — the
// dual Args contract.
type Globals struct {
	// Flags holds one entry per declared flag seen in the argv. Bool
	// flags store "true" unless spelled "-x=false"; value flags store
	// their value. Absent flags are absent from the map.
	Flags map[string]string
	// Rest is the argv left after peeling, in original order.
	Rest []string
}

// flagSpellings lists the conventional argv spellings of one flag: the
// long form of its name and, when declared, its short alias (see
// spelling for the dash-count convention).
func flagSpellings(name string, spec FlagSpec) []string {
	s := []string{spelling(name)}
	if spec.Short != "" {
		s = append(s, spelling(spec.Short))
	}
	return s
}

// peel splits args into the declared global flags and the rest. Flags
// are peeled anywhere in the vector; everything undeclared — including
// tokens that merely look like flags — is preserved in Globals.Rest in
// original order.
func peel(args []string, flags map[string]FlagSpec) (*Globals, error) {
	g := &Globals{Flags: make(map[string]string, len(flags))}
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, val, hasVal, ok := matchFlag(a, flags)
		if !ok {
			g.Rest = append(g.Rest, a)
			continue
		}
		spec := flags[name]
		if spec.Kind == "value" {
			if hasVal {
				g.Flags[name] = val
				continue
			}
			// Bare value flag: consume the next token as its value.
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cli: flag %s requires a value", spelling(name))
			}
			i++
			g.Flags[name] = args[i]
			continue
		}
		// Bool flag: bare form is true; "=x" stores x verbatim (the
		// conventional false spelling is "-x=false").
		if hasVal {
			g.Flags[name] = val
		} else {
			g.Flags[name] = "true"
		}
	}
	return g, nil
}

// matchFlag reports which declared flag the argv token is, in either
// its long or short spelling, and its "=value" part when present.
func matchFlag(a string, flags map[string]FlagSpec) (name, val string, hasVal, ok bool) {
	for name, spec := range flags {
		for _, s := range flagSpellings(name, spec) {
			if a == s {
				return name, "", false, true
			}
			if strings.HasPrefix(a, s+"=") {
				return name, strings.TrimPrefix(a, s+"="), true, true
			}
		}
	}
	return "", "", false, false
}

// selectCommand picks the command token from the peeled rest: the
// first non-flag token. A leading flag means "no command" — the caller
// decides between the default command (receiving the whole rest) and
// usage.
func selectCommand(rest []string) (cmdID string, cmdArgs []string) {
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		return rest[0], rest[1:]
	}
	return "", rest
}
