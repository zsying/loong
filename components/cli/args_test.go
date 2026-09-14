package cli

import (
	"reflect"
	"strings"
	"testing"

	"github.com/zsying/loong"
)

// testFlags is a global flag schema in miniature — a bool with a short
// alias and a value flag with a long spelling only — mirroring the shape
// an app declares in its loong.cli node.
var testFlags = map[string]FlagSpec{
	"verbose": {Short: "v", Kind: "bool"},
	"lang":    {Kind: "value"},
}

// argsT builds an Args the way the master does, by peeling an argv, so
// every getter test drives the same path a real command does.
func argsT(t *testing.T, argv ...string) *Args {
	t.Helper()
	a, err := Peel(argv, testFlags)
	if err != nil {
		t.Fatalf("Peel(%v): %v", argv, err)
	}
	return a
}

// TestArgsOfIsTheOnePayload pins the replacement for the dual contract:
// a node reads its arguments through one type, a nil payload (startup,
// service activation, Kernel.Activate) is honestly empty, and a foreign
// payload is a loud error naming both types — the old
// `args, _ := ctx.Args.([]string)` read exactly that mismatch as "the
// command has no arguments".
func TestArgsOfIsTheOnePayload(t *testing.T) {
	a, err := ArgsOf(&loong.Scope{})
	if err != nil {
		t.Fatalf("nil payload: %v, want an empty Args", err)
	}
	if a == nil {
		t.Fatal("nil payload: got a nil Args, want an empty one")
	}
	if got := a.Rest(); len(got) != 0 {
		t.Errorf("nil payload Rest = %v, want empty", got)
	}
	if got := a.Positional(); len(got) != 0 {
		t.Errorf("nil payload Positional = %v, want empty", got)
	}
	if a.Bool("verbose") {
		t.Error("an empty Args reported a flag")
	}

	// The same value comes back out, not a copy: the pre node and the
	// command must see one activation's arguments.
	if got, err := ArgsOf(&loong.Scope{Args: a}); err != nil || got != a {
		t.Errorf("ArgsOf(*Args) = %v, %v; want the same pointer", got, err)
	}

	// A string payload is what a loong.web node gets (the listen
	// address), so it is the shape a node wired to the wrong parent sees.
	_, err = ArgsOf(&loong.Scope{Args: "127.0.0.1:8080"})
	if err == nil {
		t.Fatal("a string payload was accepted; want an error naming the types")
	}
	if !strings.Contains(err.Error(), "string") || !strings.Contains(err.Error(), "*cli.Args") {
		t.Errorf("error does not name both the actual and the wanted type: %v", err)
	}
}

// TestArgsStringAcceptsBothForms pins the parsing a cobra-style CLI
// expects and the old helper did not do: "=value" and the POSIX space
// form are one flag, in either spelling — the spellings of one option
// are the names listed together, as they always were.
func TestArgsStringAcceptsBothForms(t *testing.T) {
	cases := []struct {
		name  string
		argv  []string
		names []string
		want  string
	}{
		{"long = form", []string{"--output=out"}, []string{"o", "output"}, "out"},
		{"long space form", []string{"--output", "out"}, []string{"o", "output"}, "out"},
		{"short = form", []string{"-o=out"}, []string{"o", "output"}, "out"},
		{"short space form", []string{"-o", "out"}, []string{"o", "output"}, "out"},
		{"short spelling only", []string{"-o", "out"}, []string{"o"}, "out"},
		{"long spelling only", []string{"--output=out"}, []string{"output"}, "out"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := argsT(t, tc.argv...)
			got, ok := a.String(tc.names...)
			if !ok || got != tc.want {
				t.Errorf("String%v over %v = %q,%v, want %q", tc.names, tc.argv, got, ok, tc.want)
			}
			// Whatever form was used, the flag counts as read.
			if unused := a.Unused(); len(unused) != 0 {
				t.Errorf("Unused = %v after reading the flag", unused)
			}
		})
	}

	a := argsT(t, "--other=1")
	if got, ok := a.String("o", "output"); ok {
		t.Errorf("String over an unrelated flag = %q, want absent", got)
	}
	// The spelling is the name: an alias is accepted because it is
	// listed, not because the getter guesses it.
	if got, ok := argsT(t, "--output=out").String("o"); ok {
		t.Errorf(`String("o") = %q for --output; want absent — "o" and "output" are two names`, got)
	}
}

// TestArgsBoolIsNotAStringCompare pins the getter the flag map made
// callers write by hand: presence is true, "-x=false" is false, absence
// is false — no `== "true"` in the caller.
func TestArgsBoolIsNotAStringCompare(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		want bool
	}{
		{"long bare", []string{"--html"}, true},
		{"short bare", []string{"-h"}, true},
		{"explicitly false", []string{"--html=false"}, false},
		{"explicitly true", []string{"--html=true"}, true},
		{"absent", []string{"name"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := argsT(t, tc.argv...).Bool("h", "html"); got != tc.want {
				t.Errorf("Bool(h, html) over %v = %v, want %v", tc.argv, got, tc.want)
			}
		})
	}

	// A short spelling must not leak into a longer flag.
	if argsT(t, "--html").Bool("he") {
		t.Error(`Bool("he") matched --html`)
	}
	// A dash-count mismatch is not a match: "-html" is not "--html".
	if argsT(t, "-html").Bool("html") || argsT(t, "--h").Bool("h") {
		t.Error("a mixed spelling matched its conventional counterpart")
	}
}

// TestArgsReadsThePeeledGlobals pins the other half of the fix: a global
// flag is removed from the argv by the master, so a command learns it
// only from the payload — and by any of its declared spellings, not just
// the one the tree happens to use for `short`.
func TestArgsReadsThePeeledGlobals(t *testing.T) {
	a := argsT(t, "-v", "--lang", "en", "run", "skill-x")

	if !a.Bool("verbose") {
		t.Error("Bool(verbose) = false: the command cannot see the peeled -v")
	}
	if !a.Bool("v") {
		t.Error(`Bool("v") = false: a global is readable by its short spelling too`)
	}
	if got, ok := a.String("lang"); !ok || got != "en" {
		t.Errorf("String(lang) = %q,%v, want en", got, ok)
	}
	if got := a.Rest(); !reflect.DeepEqual(got, []string{"run", "skill-x"}) {
		t.Errorf("Rest = %v, want [run skill-x]: a peeled flag must not come back", got)
	}
	if got := a.Unused(); len(got) != 0 {
		t.Errorf("Unused = %v, want none: a peeled flag is not the command's leftover", got)
	}

	// Being declared is not being given: a global the argv did not carry
	// is absent, not present-and-empty.
	absent := argsT(t, "run")
	if v, ok := absent.String("lang"); ok {
		t.Errorf("a declared-but-absent global read as %q,present", v)
	}
	if absent.Bool("v", "verbose") {
		t.Error("a declared-but-absent bool global read as set")
	}
}

// TestArgsPositionalAndArg pins the positional view: flags are skipped,
// Arg folds the bounds check in, and "--" ends flag parsing so a value
// that looks like a flag can still be passed.
func TestArgsPositionalAndArg(t *testing.T) {
	a := argsT(t, "--lang", "en", "show", "theme")
	if got := a.Positional(); !reflect.DeepEqual(got, []string{"show", "theme"}) {
		t.Errorf("Positional = %v, want [show theme]", got)
	}
	if got, ok := a.Arg(0); !ok || got != "show" {
		t.Errorf("Arg(0) = %q,%v, want show", got, ok)
	}
	if got, ok := a.Arg(1); !ok || got != "theme" {
		t.Errorf("Arg(1) = %q,%v, want theme", got, ok)
	}
	if got, ok := a.Arg(2); ok {
		t.Errorf("Arg(2) = %q, want out of range", got)
	}
	if got, ok := a.Arg(-1); ok {
		t.Errorf("Arg(-1) = %q, want out of range", got)
	}

	b := argsT(t, "--", "--not-a-flag", "-x")
	if got := b.Positional(); !reflect.DeepEqual(got, []string{"--not-a-flag", "-x"}) {
		t.Errorf("Positional after -- = %v, want both tokens", got)
	}
	if b.Bool("x") {
		t.Error("a flag after -- was read as a flag")
	}
	if got := b.Unused(); len(got) != 0 {
		t.Errorf("Unused after -- = %v, want none: those tokens are positional", got)
	}
}

// TestArgsUnusedFindsTheTypo pins the diagnostic that makes a getter's
// silence visible: a flag nobody claimed is reported, while a flag that
// was read — or the value it consumed — is not.
func TestArgsUnusedFindsTheTypo(t *testing.T) {
	a := argsT(t, "-v", "run", "-m", "msg", "--htlm")

	if got, ok := a.String("m", "message"); !ok || got != "msg" {
		t.Fatalf("String(m, message) = %q,%v, want msg", got, ok)
	}
	if !a.Bool("verbose") {
		t.Fatal("Bool(verbose) = false; want the peeled -v")
	}
	if got := a.Unused(); !reflect.DeepEqual(got, []string{"--htlm"}) {
		t.Errorf("Unused = %v, want [--htlm]", got)
	}
}

// TestArgsDeriveCarriesTheGlobalsDown pins the forwarding rule: a node
// that dispatches further — the group is one — narrows the argv without
// losing what was peeled above it, and without inheriting the parent's
// consumption marks.
func TestArgsDeriveCarriesTheGlobalsDown(t *testing.T) {
	parent := argsT(t, "-v", "--lang", "en", "config", "show", "theme")
	rest := parent.Rest()
	child := parent.Derive(rest[1:])

	if !child.Bool("v", "verbose") {
		t.Error("Derive dropped the peeled globals: a leaf under a group cannot see -v")
	}
	if got, ok := child.String("lang"); !ok || got != "en" {
		t.Errorf("child String(lang) = %q,%v, want en", got, ok)
	}
	if got := child.Positional(); !reflect.DeepEqual(got, []string{"show", "theme"}) {
		t.Errorf("child Positional = %v, want [show theme]", got)
	}
	if got := child.Unused(); len(got) != 0 {
		t.Errorf("child Unused = %v, want none", got)
	}

	// The parent's marks stay the parent's: reading a flag twice must not
	// hide a typo from the child.
	parent.Bool("v")
	if got := child.Unused(); len(got) != 0 {
		t.Errorf("child Unused = %v after the parent read a flag", got)
	}
}

// TestArgsRestIsACopy pins that the raw view is read-only by
// construction: a caller editing the slice it got back cannot desync the
// marks Unused reports.
func TestArgsRestIsACopy(t *testing.T) {
	a := argsT(t, "one", "--two")
	rest := a.Rest()
	rest[0] = "mutated"
	if got := a.Positional(); !reflect.DeepEqual(got, []string{"one"}) {
		t.Errorf("Positional = %v after mutating the Rest copy; want [one]", got)
	}
	if got := a.Unused(); !reflect.DeepEqual(got, []string{"--two"}) {
		t.Errorf("Unused = %v, want [--two]", got)
	}
}
