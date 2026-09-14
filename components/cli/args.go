package cli

import (
	"fmt"
	"strings"

	"github.com/zsying/loong"
)

// Args is the one payload the cli master hands to every node it
// activates: the global flags it peeled from the argv, plus the argv
// that remains. It replaces the older dual contract — a pre node got
// *Globals, a command got a bare []string — so every node reads its
// arguments through the same getters, and a node wired to something
// other than a cli master is told so instead of silently seeing no
// arguments at all.
//
// A getter matches a flag by any accepted spelling of it and looks in
// two places: the peel result first (a global flag was declared
// centrally and removed from the argv, so it is only visible here), then
// the remaining argv. Reading a flag marks it consumed, which is what
// makes Unused able to report a misspelled one — so an Args carries
// per-activation state and must not be shared across goroutines.
type Args struct {
	// flags holds the peeled global flags by their declared name — the
	// name in the master's flags block. A bool flag stores "true" unless
	// the argv spelled it "-x=false"; a value flag stores its value.
	flags map[string]string
	// spellings maps every accepted argv spelling of a global flag
	// ("-v", "--verbose") to its declared name, so a node reads a peeled
	// flag by whichever spelling it knows the flag by.
	spellings map[string]string
	// rest is the argv left after peeling, in original order.
	rest []string
	// used marks the rest entries a getter consumed; parallel to rest.
	used []bool
}

// ArgsOf returns the activation arguments a node received. A nil payload
// — startup, service activation, Kernel.Activate — is an empty Args,
// because "no arguments" is the honest answer there. Any other type is
// an error naming both, because the alternative is the silent
// `args, _ := ctx.Args.([]string)` that reads a mismatch as "none".
func ArgsOf(ctx *loong.Scope) (*Args, error) {
	if ctx.Args == nil {
		return &Args{}, nil
	}
	if a, ok := ctx.Args.(*Args); ok {
		return a, nil
	}
	return nil, fmt.Errorf("cli: activation payload is %T, want *cli.Args (is this node activated by a loong.cli master?)", ctx.Args)
}

// Rest returns the argv left after the global flags were peeled, in
// original order: the raw view the getters consume from. It is a copy,
// so the getters stay the only writers of what Unused reports.
func (a *Args) Rest() []string {
	return append([]string(nil), a.rest...)
}

// Positional returns the argv tokens that are not flags, in order. A
// "--" ends flag parsing: it is dropped and everything after it is
// positional, even when it looks like a flag — that is how a caller
// passes a value that must not be read as one.
//
// The view is purely syntactic: Positional cannot know which flags take
// a value (that is the command's schema, not the argv's), so it reports
// every non-flag token. Read the flags first when a value could be
// mistaken for one.
func (a *Args) Positional() []string {
	var out []string
	for i := 0; i < len(a.rest); i++ {
		if tok := a.rest[i]; tok == "--" {
			return append(out, a.rest[i+1:]...)
		} else if !strings.HasPrefix(tok, "-") {
			out = append(out, tok)
		}
	}
	return out
}

// Arg returns the i-th positional argument, folding the bounds check
// into the call:
//
//	name, ok := args.Arg(0) // the first positional, if there is one
func (a *Args) Arg(i int) (string, bool) {
	pos := a.Positional()
	if i < 0 || i >= len(pos) {
		return "", false
	}
	return pos[i], true
}

// Bool reports whether any of the named flags was set to a true value.
// List the short and long spellings of one option together to accept
// both:
//
//	args.Bool("l", "loud") // matches "-l" or "--loud"
//
// The bare form is true and the "=false" form is false: a flag is a
// switch, not a string for the caller to compare.
func (a *Args) Bool(names ...string) bool {
	for _, name := range names {
		sp := spelling(name)
		if v, ok := a.global(sp); ok {
			return v != "false"
		}
		i, val, hasVal, ok := a.matchRest(sp)
		if !ok {
			continue
		}
		a.claim(i)
		if !hasVal {
			return true
		}
		return val != "false"
	}
	return false
}

// String returns the value of the first of the named flags that is
// present, or ok=false when none is. Both the "=value" form and the
// POSIX space form are accepted, in either spelling:
//
//	args.String("o", "output") // "-o=out", "-o out", "--output=out", "--output out"
//
// A flag spelled without a value contributes an empty value and ok=true;
// only the caller knows whether that is an error.
func (a *Args) String(names ...string) (string, bool) {
	for _, name := range names {
		sp := spelling(name)
		if v, ok := a.global(sp); ok {
			return v, true
		}
		i, val, hasVal, ok := a.matchRest(sp)
		if !ok {
			continue
		}
		a.claim(i)
		if hasVal {
			return val, true
		}
		if i+1 < len(a.rest) {
			a.claim(i + 1)
			return a.rest[i+1], true
		}
		return "", true
	}
	return "", false
}

// Unused returns the flag-shaped tokens in Rest that no getter consumed:
// the misspelled or unknown flag, which otherwise passes silently. A
// "--" and everything after it are positional and never reported, and a
// global flag is not Rest's business — the master peeled it. Read the
// flags first; Unused reports what the getters have not accounted for.
func (a *Args) Unused() []string {
	var out []string
	for i, tok := range a.rest {
		if tok == "--" {
			break
		}
		if i < len(a.used) && a.used[i] {
			continue
		}
		if strings.HasPrefix(tok, "-") {
			out = append(out, tok)
		}
	}
	return out
}

// Derive builds the Args for a child activation: the same peeled globals
// and a new argv tail. It is how a group forwards to the child it
// selected, and how any node that dispatches further narrows the
// arguments without losing what was peeled above it.
func (a *Args) Derive(rest []string) *Args {
	return &Args{
		flags:     a.flags,
		spellings: a.spellings,
		rest:      append([]string(nil), rest...),
		used:      make([]bool, len(rest)),
	}
}

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

// global returns the peeled value of a flag by one of its argv
// spellings. A declared flag that was not on the argv is absent, exactly
// as it is in Rest — being declared is not being given.
func (a *Args) global(sp string) (string, bool) {
	name, ok := a.spellings[sp]
	if !ok {
		return "", false
	}
	v, ok := a.flags[name]
	return v, ok
}

// matchRest finds a flag in the remaining argv by one of its spellings,
// in either the bare or the "=value" form. Scanning stops at a "--",
// which is where the argv says the flags are over.
func (a *Args) matchRest(sp string) (idx int, val string, hasVal, ok bool) {
	for i, tok := range a.rest {
		switch {
		case tok == "--":
			return 0, "", false, false
		case tok == sp:
			return i, "", false, true
		case strings.HasPrefix(tok, sp+"="):
			return i, strings.TrimPrefix(tok, sp+"="), true, true
		}
	}
	return 0, "", false, false
}

// claim marks a rest entry as consumed, when the index is in range.
func (a *Args) claim(i int) {
	if i >= 0 && i < len(a.used) {
		a.used[i] = true
	}
}
