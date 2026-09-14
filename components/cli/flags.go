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
	// of the process — with *Args (the peel result). Typical use:
	// process-wide setup derived from global flags (log level, UI
	// language), the componentized equivalent of cobra's
	// PersistentPreRunE. The pre node is not selectable as a command and
	// never runs on the usage path.
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

// Peel splits argv into the declared global flags and the argv that
// remains, producing the Args the cli master hands down. It is the only
// way an Args that carries global flags comes into being: the master
// calls it with its own flag block, and a host that dispatches outside
// the tree — or a test that runs a node directly — calls it with the
// same schema.
//
// Flags are peeled anywhere in the vector (POSIX-style); everything
// undeclared, including tokens that merely look like flags, is preserved
// in Args.Rest in original order. A declared value flag whose value is
// missing is a loud error.
func Peel(argv []string, flags map[string]FlagSpec) (*Args, error) {
	a := &Args{
		flags:     make(map[string]string, len(flags)),
		spellings: make(map[string]string, 2*len(flags)),
	}
	for name, spec := range flags {
		for _, sp := range flagSpellings(name, spec) {
			a.spellings[sp] = name
		}
	}
	for i := 0; i < len(argv); i++ {
		tok := argv[i]
		name, val, hasVal, ok := matchFlag(tok, flags)
		if !ok {
			a.rest = append(a.rest, tok)
			continue
		}
		spec := flags[name]
		if spec.Kind == "value" {
			if hasVal {
				a.flags[name] = val
				continue
			}
			// Bare value flag: consume the next token as its value.
			if i+1 >= len(argv) {
				return nil, fmt.Errorf("cli: flag %s requires a value", spelling(name))
			}
			i++
			a.flags[name] = argv[i]
			continue
		}
		// Bool flag: bare form is true; "=x" stores x verbatim (the
		// conventional false spelling is "-x=false").
		if hasVal {
			a.flags[name] = val
		} else {
			a.flags[name] = "true"
		}
	}
	a.used = make([]bool, len(a.rest))
	return a, nil
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
