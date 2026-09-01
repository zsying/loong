package cli

import (
	"bytes"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/zsying/loong"
)

func TestContextFlags(t *testing.T) {
	c := NewContext([]string{"--html", "--o=out", "name", "-x"}, &bytes.Buffer{}, &bytes.Buffer{})
	if !c.HasFlag("html") {
		t.Error("HasFlag(html) = false")
	}
	if c.HasFlag("json") {
		t.Error("HasFlag(json) = true")
	}
	if v, ok := c.Flag("o"); !ok || v != "out" {
		t.Errorf("Flag(o) = %q,%v", v, ok)
	}
	if v, ok := c.Flag("x"); ok {
		t.Errorf("Flag(x) = %q, want absent", v)
	}
	pos := c.Positional()
	if len(pos) != 1 || pos[0] != "name" {
		t.Errorf("Positional = %v, want [name]", pos)
	}
	c.SetExit(3)
	if c.ExitCode() != 3 {
		t.Errorf("ExitCode = %d, want 3", c.ExitCode())
	}
}

// test command components for dispatch.
type testCmd struct {
	loong.Base
}

func (c *testCmd) Run(*loong.Scope) error { return nil }

func regTestTree(t *testing.T) *loong.Kernel {
	t.Helper()
	loong.RegisterComponent("test.cmd", func() loong.Component { return &testCmd{} })
	loong.RegisterComponent("test.group", func() loong.Component { return &testCmd{} })
	// Root uses the real cli master registered by this package (eager),
	// so dispatch can hand leaf arguments to its Context.
	root := &loong.Node{
		Type: "cli", ID: "root",
		Children: []*loong.Node{
			{Type: "test.cmd", ID: "list", Lazy: true},
			{Type: "test.group", ID: "config", Lazy: true, Children: []*loong.Node{
				{Type: "test.cmd", ID: "show", Lazy: true},
				{Type: "test.cmd", ID: "set", Lazy: true},
			}},
		},
	}
	k := loong.New()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	return k
}

func TestDispatch(t *testing.T) {
	k := regTestTree(t)

	// Leaf command.
	if code := Dispatch(k, []string{"list"}); code != 0 {
		t.Errorf("dispatch(list) = %d, want 0", code)
	}
	// Arguments after the leaf command are injected into Context.
	if code := Dispatch(k, []string{"list", "--html"}); code != 0 {
		t.Errorf("dispatch(list --html) = %d, want 0", code)
	}
	master, err := k.Component("root")
	if err != nil {
		t.Fatal(err)
	}
	if got := master.(*Component).Ctx.Args; len(got) != 1 || got[0] != "--html" {
		t.Errorf("leaf args injected = %v, want [--html]", got)
	}
	// Multi-level command path.
	if code := Dispatch(k, []string{"config", "show"}); code != 0 {
		t.Errorf("dispatch(config show) = %d, want 0", code)
	}
	// Group invoked directly -> usage error.
	if code := Dispatch(k, []string{"config"}); code != 2 {
		t.Errorf("dispatch(config) = %d, want 2", code)
	}
	// Unknown command.
	if code := Dispatch(k, []string{"nope"}); code != 1 {
		t.Errorf("dispatch(nope) = %d, want 1", code)
	}
	// Arguments after a leaf command are the command's own (legal).
	if code := Dispatch(k, []string{"list", "extra"}); code != 0 {
		t.Errorf("dispatch(list extra) = %d, want 0 (extra is a command argument)", code)
	}
	// No arguments -> usage.
	if code := Dispatch(k, nil); code != 2 {
		t.Errorf("dispatch() = %d, want 2", code)
	}
}

func cfgNode(t *testing.T, s string) yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(s), &doc); err != nil {
		t.Fatal(err)
	}
	return *doc.Content[0]
}

func TestRenderListText(t *testing.T) {
	infos := []loong.ComponentMeta{
		{Type: "web", Service: true, ServiceTypes: []string{"*web.Router"}, Eager: true, Emits: []string{"biz.*"}},
		{Type: "app"},
		{Type: "log", ConfigFields: []loong.ConfigField{{Name: "level", Type: "string"}}},
	}
	out := renderListText(infos)
	if !strings.HasPrefix(out, "TYPE") {
		t.Fatalf("missing header:\n%s", out)
	}
	for _, want := range []string{"eager", "level", "*web.Router", "biz.*"} {
		if !strings.Contains(out, want) {
			t.Errorf("text missing %q:\n%s", want, out)
		}
	}
	appAt := strings.Index(out, "\napp")
	webAt := strings.Index(out, "\nweb")
	if appAt < 0 || webAt < 0 || appAt > webAt {
		t.Errorf("rows not sorted by type:\n%s", out)
	}
}

func TestRenderListHTML(t *testing.T) {
	out := renderListHTML([]loong.ComponentMeta{
		{Type: "web", Service: true, ServiceTypes: []string{"*web.Router"}, Eager: true, Desc: "a <b>web</b>"},
	})
	for _, want := range []string{"<!doctype html>", "<table", "<td>web</td>", "eager", "*web.Router", "&lt;b&gt;web&lt;/b&gt;"} {
		if !strings.Contains(out, want) {
			t.Errorf("html missing %q:\n%s", want, out)
		}
	}
}

func TestRenderTree(t *testing.T) {
	root := &loong.Node{Type: "app", Config: cfgNode(t, "name: hello\n")}
	web := &loong.Node{Type: "web", ID: "main", Config: cfgNode(t, "listen: :8080\n")}
	root.Children = []*loong.Node{web}
	meta := map[string]loong.ComponentMeta{
		"web": {Type: "web", Service: true, Eager: true},
	}
	out := renderTree(root, meta)
	for _, want := range []string{"app", "name=hello", "web (main)", "[service · eager]", "listen=:8080"} {
		if !strings.Contains(out, want) {
			t.Errorf("tree missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "app ()") {
		t.Errorf("root rendered empty parens:\n%s", out)
	}
}

func TestScaffoldComponent(t *testing.T) {
	src, err := scaffoldComponent("greet")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"type greet struct",
		"greetConfig",
		"ctx.Config[greetConfig]()",
		`RegisterComponent("biz.greet"`,
		"loong.WithConfig[greetConfig]()",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("scaffold missing %q:\n%s", want, src)
		}
	}
	if strings.Contains(src, "`") {
		t.Errorf("scaffold must not contain raw backticks:\n%s", src)
	}
}

func TestNodeQuery(t *testing.T) {
	k := regTestTree(t)
	info, err := k.Node("config")
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != "config" || info.Type != "test.group" || !info.Lazy {
		t.Errorf("info = %+v", info)
	}
	if len(info.Children) != 2 || info.Children[0].ID != "show" {
		t.Errorf("children = %+v, want [show set]", info.Children)
	}
	if _, err := k.Node("missing"); err == nil {
		t.Error("expected error for unknown node")
	}
	root, err := k.Root()
	if err != nil || root.ID != "root" {
		t.Errorf("Root = %+v, err %v", root, err)
	}
}
