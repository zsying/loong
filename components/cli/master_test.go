package cli

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/zsying/loong"
	"gopkg.in/yaml.v3"
)

// --- master: default + flags + pre ---

// recErr, when non-nil, makes every recorder's Run fail (pre-abort test).
var recErr error

// recorder records what a test command received: got keeps the raw
// payload (an *Args for every node the master activates), runs counts
// invocations (pre idempotency).
type recorder struct {
	loong.Base
	ran  bool
	runs int
	got  any
}

func (c *recorder) Run(ctx *loong.Scope) error {
	c.ran = true
	c.runs++
	c.got = ctx.Args
	return recErr
}

// recArgs reads a recorder's payload as *Args. Every node the master
// activates receives one, so any other type here is precisely the
// mismatch the old `args, _ := ctx.Args.([]string)` swallowed.
func recArgs(t *testing.T, c *recorder) *Args {
	t.Helper()
	if c == nil {
		t.Fatal("the node never ran")
	}
	a, ok := c.got.(*Args)
	if !ok {
		t.Fatalf("payload = %T, want *cli.Args", c.got)
	}
	return a
}

func init() {
	loong.RegisterComponent("test.rec", func() loong.Component { return &recorder{} },
		loong.WithDesc("test recorder command"),
	)
}

// cliNode wires a master node for the tests below: the config block is
// attached as the node's Raw yaml, mirroring what loong.Parse produces.
func cliNode(t *testing.T, cfg string, children ...*loong.Node) (*loong.Node, *loong.Scope) {
	t.Helper()
	var raw yaml.Node
	if cfg != "" {
		if err := yaml.Unmarshal([]byte(cfg), &raw); err != nil {
			t.Fatalf("unmarshal config: %v", err)
		}
	}
	n := &loong.Node{Type: "loong.cli", ID: "cli", Lazy: true, Config: raw, Children: children}
	return n, &loong.Scope{Node: n, Raw: raw}
}

const masterCfg = `
default: tui
pre: globals
flags:
  verbose: { short: v, kind: bool }
  lang: { kind: value }
`

func masterChildren() []*loong.Node {
	return []*loong.Node{
		{Type: "test.rec", ID: "globals", Lazy: true},
		{Type: "test.rec", ID: "tui", Lazy: true},
		{Type: "test.rec", ID: "run", Lazy: true},
		{Type: "test.rec", ID: "daemon", Lazy: true},
	}
}

// buildMaster assembles the kernel (master lazy, so nothing runs yet)
// and Builds the master component against the node's scope. Child
// recorders are read with fetch after runs — components instantiate on
// activation, so a pre-run snapshot would be empty.
func buildMaster(t *testing.T, n *loong.Node, sc *loong.Scope) *Component {
	t.Helper()
	k := loong.New()
	sc.Kernel = k
	if err := k.Assemble(&loong.Node{Type: "test.rec", ID: "root", Children: []*loong.Node{n}}); err != nil {
		t.Fatalf("assemble: %v", err)
	}
	c := &Component{}
	if err := c.Build(sc); err != nil {
		t.Fatalf("master build: %v", err)
	}
	return c
}

// TestMasterDefaultAndPre pins the activation chain: bare invocation
// activates pre (with the peel result) then the default command with the
// remaining args.
func TestMasterDefaultAndPre(t *testing.T) {
	n, sc := cliNode(t, masterCfg, masterChildren()...)
	c := buildMaster(t, n, sc)
	if err := c.masterRun(sc, nil); err != nil {
		t.Fatalf("bare invocation: %v", err)
	}
	got := fetch(t, n, sc.Kernel)
	if !got["globals"].ran || !got["tui"].ran {
		t.Fatalf("pre=%v default=%v, both must run", got["globals"].ran, got["tui"].ran)
	}
	pre := recArgs(t, got["globals"])
	if rest := pre.Rest(); len(rest) != 0 {
		t.Errorf("pre Rest = %v, want empty", rest)
	}
	if v, ok := pre.String("verbose"); ok {
		t.Errorf("pre saw --verbose = %q without it on the argv", v)
	}
	if rest := recArgs(t, got["tui"]).Rest(); len(rest) != 0 {
		t.Errorf("default args = %v, want empty", rest)
	}
}

// fetch re-reads the recorder components from the kernel after runs.
func fetch(t *testing.T, n *loong.Node, k *loong.Kernel) map[string]*recorder {
	t.Helper()
	m := map[string]*recorder{}
	for _, ch := range n.Children {
		if comp, err := k.Component(ch.ID); err == nil {
			if r, ok := comp.(*recorder); ok {
				m[ch.ID] = r
			}
		}
	}
	return m
}

// TestMasterPeelAnywhere pins that declared flags peel wherever they
// appear, in every accepted spelling, that undeclared tokens pass
// through untouched, and that the command — which never sees a peeled
// flag in its own argv — still reads it from the payload.
func TestMasterPeelAnywhere(t *testing.T) {
	cases := []struct {
		name       string
		argv       []string
		wantCmd    string
		wantCmdArg []string
		wantFlags  map[string]string
	}{
		{
			name:       "short bool before command",
			argv:       []string{"-v", "run", "skill-x"},
			wantCmd:    "run",
			wantCmdArg: []string{"skill-x"},
			wantFlags:  map[string]string{"verbose": "true"},
		},
		{
			name:       "long bool after command (anywhere peel)",
			argv:       []string{"daemon", "start", "--verbose"},
			wantCmd:    "daemon",
			wantCmdArg: []string{"start"},
			wantFlags:  map[string]string{"verbose": "true"},
		},
		{
			name:       "value flag, space form, before command",
			argv:       []string{"--lang", "en", "run"},
			wantCmd:    "run",
			wantCmdArg: nil,
			wantFlags:  map[string]string{"lang": "en"},
		},
		{
			name:       "value flag, = form",
			argv:       []string{"--lang=zh-CN", "run"},
			wantCmd:    "run",
			wantCmdArg: nil,
			wantFlags:  map[string]string{"lang": "zh-CN"},
		},
		{
			name:       "bool = form",
			argv:       []string{"-v=false", "run"},
			wantCmd:    "run",
			wantCmdArg: nil,
			wantFlags:  map[string]string{"verbose": "false"},
		},
		{
			name:       "undeclared flags pass through",
			argv:       []string{"run", "-m", "msg", "--continue"},
			wantCmd:    "run",
			wantCmdArg: []string{"-m", "msg", "--continue"},
			wantFlags:  map[string]string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, sc := cliNode(t, masterCfg, masterChildren()...)
			c := buildMaster(t, n, sc)
			if err := c.masterRun(sc, tc.argv); err != nil {
				t.Fatalf("masterRun: %v", err)
			}
			recs := fetch(t, n, sc.Kernel)
			if !recs["globals"].ran {
				t.Fatal("pre did not run")
			}
			cmd := recArgs(t, recs[tc.wantCmd])
			if got := cmd.Rest(); !reflect.DeepEqual(got, tc.wantCmdArg) {
				t.Errorf("cmd Rest = %#v, want %#v", got, tc.wantCmdArg)
			}
			for k, v := range tc.wantFlags {
				got, ok := cmd.String(k)
				if !ok || got != v {
					t.Errorf("command reads %q = %q,%v; want %q — a peeled flag must reach the command", k, got, ok, v)
				}
			}
		})
	}
}

// TestMasterHandsTheCommandItsGlobals pins the fix for the old dual
// contract: a command used to receive only the leftover argv, so a
// global flag — peeled centrally and therefore absent from that argv —
// was unreadable to the very command that had to honour it.
func TestMasterHandsTheCommandItsGlobals(t *testing.T) {
	n, sc := cliNode(t, masterCfg, masterChildren()...)
	c := buildMaster(t, n, sc)
	if err := c.masterRun(sc, []string{"-v", "--lang", "en", "run", "x"}); err != nil {
		t.Fatal(err)
	}
	cmd := recArgs(t, fetch(t, n, sc.Kernel)["run"])
	if !cmd.Bool("v", "verbose") {
		t.Error("the command cannot see -v: the peeled globals were dropped on the way down")
	}
	if got, ok := cmd.String("lang"); !ok || got != "en" {
		t.Errorf("command reads lang = %q,%v, want en", got, ok)
	}
	if got := cmd.Rest(); !reflect.DeepEqual(got, []string{"x"}) {
		t.Errorf("command Rest = %v, want [x]", got)
	}
}

// TestMasterPreNotACommandAndFailFast pins: the pre node is not
// selectable as a command, and a pre failure aborts the whole chain.
func TestMasterPreNotACommandAndFailFast(t *testing.T) {
	n, sc := cliNode(t, masterCfg, masterChildren()...)
	c := buildMaster(t, n, sc)
	err := c.masterRun(sc, []string{"globals"})
	if err == nil || !strings.Contains(err.Error(), "pre") {
		t.Errorf("selecting pre = %v, want pre-not-a-command error", err)
	}

	n2, sc2 := cliNode(t, masterCfg, masterChildren()...)
	c2 := buildMaster(t, n2, sc2)
	// Set after assemble: recErr would fail the root recorder's Run
	// during Assemble otherwise.
	recErr = errors.New("i18n resource missing")
	defer func() { recErr = nil }()
	err = c2.masterRun(sc2, []string{"run"})
	if err == nil || !strings.Contains(err.Error(), "i18n") {
		t.Errorf("pre failure = %v, want propagated", err)
	}
	if r := fetch(t, n2, sc2.Kernel)["run"]; r != nil && r.ran {
		t.Error("command ran although pre failed; want fail-fast abort")
	}
}

// TestMasterPreIdempotent pins the "once per process" semantic: the
// second command does not re-run the pre node (ensureActive cache).
func TestMasterPreIdempotent(t *testing.T) {
	n, sc := cliNode(t, masterCfg, masterChildren()...)
	c := buildMaster(t, n, sc)
	if err := c.masterRun(sc, []string{"run", "a"}); err != nil {
		t.Fatal(err)
	}
	if err := c.masterRun(sc, []string{"run", "b"}); err != nil {
		t.Fatal(err)
	}
	if n := fetch(t, n, sc.Kernel)["globals"].runs; n != 1 {
		t.Errorf("pre ran %d times, want 1", n)
	}
}

// TestMasterUsageSkipsPre pins: bare invocation without a default is a
// usage request (nil error) and does not run the pre node; the pre id
// is also excluded from the printed command list.
func TestMasterUsageSkipsPre(t *testing.T) {
	cfg := `
pre: globals
flags:
  verbose: { short: v, kind: bool }
`
	n, sc := cliNode(t, cfg, masterChildren()...)
	c := buildMaster(t, n, sc)
	if err := c.masterRun(sc, nil); err != nil {
		t.Errorf("bare invocation = %v, want nil (usage)", err)
	}
	recs := fetch(t, n, sc.Kernel)
	if recs["globals"] != nil && recs["globals"].ran {
		t.Error("pre ran on usage path; want untouched")
	}
}

// TestMasterNoConfigLegacy pins the old behavior byte-for-byte: without
// a config block the master activates args[0] with the rest, and bare
// invocation prints usage.
func TestMasterNoConfigLegacy(t *testing.T) {
	n, sc := cliNode(t, "", masterChildren()...)
	c := buildMaster(t, n, sc)
	if err := c.masterRun(sc, []string{"run", "x"}); err != nil {
		t.Fatalf("legacy run: %v", err)
	}
	recs := fetch(t, n, sc.Kernel)
	if recs["globals"] != nil && recs["globals"].ran {
		t.Error("pre ran without config; want untouched legacy behavior")
	}
	if got := recArgs(t, recs["run"]).Rest(); !reflect.DeepEqual(got, []string{"x"}) {
		t.Errorf("args = %#v", got)
	}

	if err := c.masterRun(sc, nil); err != nil {
		t.Errorf("bare invocation = %v, want nil (usage)", err)
	}
}

// TestMasterDanglingValueFlag pins fail-fast on a value flag whose
// value is missing.
func TestMasterDanglingValueFlag(t *testing.T) {
	n, sc := cliNode(t, masterCfg, masterChildren()...)
	c := buildMaster(t, n, sc)
	err := c.masterRun(sc, []string{"run", "--lang"})
	if err == nil || !strings.Contains(err.Error(), "--lang") {
		t.Errorf("dangling --lang = %v, want error", err)
	}
}

// TestMasterBuildValidation pins the loud Build-time checks: pre and
// default must name direct children, flag kinds must be known, and
// spellings must not collide.
func TestMasterBuildValidation(t *testing.T) {
	cases := []struct {
		name    string
		cfg     string
		wantErr string
	}{
		{"pre not a child", "pre: missing\n", "pre"},
		{"default not a child", "default: missing\n", "default"},
		{"unknown kind", "flags:\n  x: { kind: blob }\n", "kind"},
		{"spelling collision", "flags:\n  verbose: { short: v }\n  vend: { short: v }\n", "collision"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, sc := cliNode(t, tc.cfg, masterChildren()...)
			k := loong.New()
			sc.Kernel = k
			if err := k.Assemble(&loong.Node{Type: "test.rec", ID: "root", Children: []*loong.Node{n}}); err != nil {
				t.Fatal(err)
			}
			c := &Component{}
			err := c.Build(sc)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Build = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

// TestMasterHelp pins the built-in help: "-h" / "--help" anywhere, or
// a "help" command token, prints the usage tree (nil error) without
// running the pre node or activating any command.
func TestMasterHelp(t *testing.T) {
	cases := [][]string{
		{"-h"},
		{"--help"},
		{"run", "--help"},
		{"-v", "help"},
		{"help"},
	}
	for _, argv := range cases {
		n, sc := cliNode(t, masterCfg, masterChildren()...)
		c := buildMaster(t, n, sc)
		if err := c.masterRun(sc, argv); err != nil {
			t.Errorf("help %v = %v, want nil (usage)", argv, err)
		}
		for id, r := range fetch(t, n, sc.Kernel) {
			if r.ran {
				t.Errorf("help %v activated %q; want usage only", argv, id)
			}
		}
	}
}

// TestMasterHelpAutoYield pins that the master never steals a spelling
// an app declared: a flag with short "h" keeps "-h", and a child
// command named "help" keeps the token.
func TestMasterHelpAutoYield(t *testing.T) {
	cfg := `
default: tui
pre: globals
flags:
  html: { short: h, kind: bool }
`
	n, sc := cliNode(t, cfg, masterChildren()...)
	c := buildMaster(t, n, sc)
	if err := c.masterRun(sc, []string{"-h"}); err != nil {
		t.Fatalf("-h with declared flag: %v", err)
	}
	recs := fetch(t, n, sc.Kernel)
	if !recs["tui"].ran {
		t.Error("-h did not fall through to the default command")
	}
	if v, ok := recArgs(t, recs["globals"]).String("html"); !ok || v != "true" {
		t.Errorf("html flag = %q,%v, want true", v, ok)
	}

	n2, sc2 := cliNode(t, masterCfg, append(masterChildren(), &loong.Node{Type: "test.rec", ID: "help", Lazy: true})...)
	c2 := buildMaster(t, n2, sc2)
	if err := c2.masterRun(sc2, []string{"help"}); err != nil {
		t.Fatalf("help command: %v", err)
	}
	if r := fetch(t, n2, sc2.Kernel)["help"]; r == nil || !r.ran {
		t.Error("help token did not activate the help command")
	}
}

// TestGroupForwardsTheArgsPayload pins that a group hands its child the
// same payload type it received, globals included. There is no second
// form to unwrap any more, and a group that dropped the globals would
// make -v invisible to every command under it.
func TestGroupForwardsTheArgsPayload(t *testing.T) {
	// test.show / test.parent are registered by TestGroupActivation;
	// RegisterComponent panics on duplicates.
	root := &loong.Node{
		Type: "test.parent", ID: "root",
		Children: []*loong.Node{
			{Type: "loong.cli.group", ID: "config", Lazy: true, Children: []*loong.Node{
				{Type: "test.show", ID: "show", Lazy: true},
			}},
		},
	}
	k := loong.New()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	parent, err := Peel([]string{"-v", "show", "theme"}, map[string]FlagSpec{"verbose": {Short: "v", Kind: "bool"}})
	if err != nil {
		t.Fatal(err)
	}
	config := root.Children[0]
	g := &Group{}
	if err := g.Run(&loong.Scope{Kernel: k, Node: config, Args: parent}); err != nil {
		t.Fatalf("group run: %v", err)
	}
	show, _ := k.Component("show")
	leaf, ok := show.(*gotArgs).got.(*Args)
	if !ok {
		t.Fatalf("leaf payload = %T, want *cli.Args", show.(*gotArgs).got)
	}
	if got := leaf.Positional(); !reflect.DeepEqual(got, []string{"theme"}) {
		t.Errorf("leaf args = %#v, want [theme]", got)
	}
	if !leaf.Bool("verbose") {
		t.Error("the group dropped the peeled globals on the way down")
	}
}

// TestUsageRendering pins the two-column usage listing: node-level
// descriptions (Node.Desc) win over the component type's WithDesc,
// groups render their subcommands inline, and color is opt-in via the
// flag (bold names, gray descriptions).
func TestUsageRendering(t *testing.T) {
	nodes := []*loong.Node{
		{Type: "owlet.tui", ID: "tui", Desc: "Start the interactive TUI"},
		{Type: "loong.cli.group", ID: "daemon", Children: []*loong.Node{
			{Type: "test.rec", ID: "start", Lazy: true},
		}}, // no node desc -> falls back to the type's WithDesc
		{Type: "test.rec", ID: "bare"}, // no desc anywhere -> no second column
	}
	var buf bytes.Buffer
	writeCommands(&buf, nodes, false)
	out := buf.String()
	if !strings.Contains(out, "tui") || !strings.Contains(out, "Start the interactive TUI") {
		t.Errorf("missing tui line/desc: %s", out)
	}
	if !strings.Contains(out, "daemon <start>") {
		t.Errorf("group line not rendered inline: %s", out)
	}
	if !strings.Contains(out, "CLI command group container") {
		t.Errorf("type WithDesc fallback missing: %s", out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("plain mode emitted ANSI codes: %s", out)
	}
	// Aligned second column: every description starts at the same
	// column within its line (padding to the widest head).
	col := func(s string) int {
		i := strings.Index(out, s)
		return i - (strings.LastIndex(out[:i], "\n") + 1)
	}
	tui, fb := col("Start the interactive"), col("CLI command group container")
	if tui == -1 || fb == -1 || tui != fb {
		t.Errorf("descriptions not column-aligned (tui=%d fallback=%d): %s", tui, fb, out)
	}

	buf.Reset()
	writeCommands(&buf, nodes[:1], true)
	if !strings.Contains(buf.String(), ansiBold+"tui") || !strings.Contains(buf.String(), ansiGray+"Start") {
		t.Errorf("color mode missing bold/gray: %q", buf.String())
	}
}

// TestUsageRenderingKeepsMountedChildrenOutOfTheListing pins the one
// node shape that advertises subcommands. A group dispatches to a child
// by name, so the listing spells the children out; every other node's
// children are mounted components. A command that mounts a server under
// itself — the dashboard's web node — must not read as if `dashboard
// <dashweb>` were invocable, because `dashboard dashweb` activates
// nothing.
func TestUsageRenderingKeepsMountedChildrenOutOfTheListing(t *testing.T) {
	nodes := []*loong.Node{
		{Type: "test.rec", ID: "dashboard", Desc: "Open the web dashboard",
			Children: []*loong.Node{
				{Type: "loong.web", ID: "dashweb", Lazy: true, Children: []*loong.Node{
					{Type: "test.rec", ID: "dash"},
				}},
			}},
	}
	var buf bytes.Buffer
	writeCommands(&buf, nodes, false)
	out := buf.String()
	if strings.Contains(out, "<") {
		t.Errorf("mounted children rendered as subcommands: %s", out)
	}
	if !strings.Contains(out, "Open the web dashboard") {
		t.Errorf("missing node desc: %s", out)
	}
}

// TestWriteUsageLayout pins the help-screen layout: the master's Desc
// becomes the program header, blank lines separate the sections, and
// a master without a Desc skips the header entirely.
func TestWriteUsageLayout(t *testing.T) {
	master := &loong.Node{Type: "loong.cli", ID: "cli", Desc: "Owlet - AI harness toolset",
		Children: []*loong.Node{
			{Type: "test.rec", ID: "globals", Lazy: true},
			{Type: "test.rec", ID: "tui", Lazy: true, Desc: "Start the interactive TUI"},
		}}
	var buf bytes.Buffer
	if err := writeUsage(&buf, master, "globals", false); err != nil {
		t.Fatal(err)
	}
	want := "Owlet - AI harness toolset\n" +
		"\n" +
		"usage: <command>\n" +
		"\n" +
		"  tui Start the interactive TUI\n" +
		"\n"
	if buf.String() != want {
		t.Errorf("layout mismatch:\ngot  %q\nwant %q", buf.String(), want)
	}

	bare := &loong.Node{Type: "loong.cli", ID: "cli",
		Children: []*loong.Node{{Type: "test.rec", ID: "tui", Lazy: true}}}
	buf.Reset()
	if err := writeUsage(&buf, bare, "", false); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(buf.String(), "usage:") || strings.Contains(buf.String(), "Owlet") {
		t.Errorf("bare layout wrong: %q", buf.String())
	}
}
