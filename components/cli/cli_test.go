package cli

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/zsying/loong"
)

func TestArgsHelpers(t *testing.T) {
	args := []string{"--html", "--o=out", "name", "-x"}
	if !HasFlag(args, "html") {
		t.Error("HasFlag(html) = false")
	}
	if HasFlag(args, "json") {
		t.Error("HasFlag(json) = true")
	}
	if v, ok := Flag(args, "o"); !ok || v != "out" {
		t.Errorf("Flag(o) = %q,%v", v, ok)
	}
	if v, ok := Flag(args, "x"); ok {
		t.Errorf("Flag(x) = %q, want absent", v)
	}
	pos := Positional(args)
	if len(pos) != 1 || pos[0] != "name" {
		t.Errorf("Positional = %v, want [name]", pos)
	}
}

// gotArgs records the arguments a test command received, mirroring a
// leaf command reading ctx.Args.
type gotArgs struct {
	loong.Base
	got any
}

func (c *gotArgs) Run(ctx *loong.Scope) error {
	c.got = ctx.Args
	return nil
}

// quietRoot is a no-op parent for tests — the real cli master reads
// os.Args in Run, which does not apply under go test.
type quietRoot struct {
	loong.Base
}

// TestGroupActivation drives the group behavior: a cli.group node
// receives ["show", "theme"], activates its "show" child and passes
// the remaining arguments down. This is the parent-defined activation
// chain — no dispatch code outside the components.
func TestGroupActivation(t *testing.T) {
	loong.RegisterComponent("test.show", func() loong.Component { return &gotArgs{} })
	loong.RegisterComponent("test.parent", func() loong.Component { return &quietRoot{} })
	root := &loong.Node{
		Type: "test.parent", ID: "root",
		Children: []*loong.Node{
			{Type: "cli.group", ID: "config", Lazy: true, Children: []*loong.Node{
				{Type: "test.show", ID: "show", Lazy: true},
			}},
		},
	}
	k := loong.New()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}

	config := root.Children[0]
	g := &Group{}
	if err := g.Run(&loong.Scope{Kernel: k, Node: config, Args: []string{"show", "theme"}}); err != nil {
		t.Fatalf("group run: %v", err)
	}
	show, err := k.Component("show")
	if err != nil {
		t.Fatal(err)
	}
	if got := show.(*gotArgs).got; !reflect.DeepEqual(got, []string{"theme"}) {
		t.Errorf("leaf args = %v, want [theme]", got)
	}

	// A group invoked without a subcommand is a usage error.
	err = g.Run(&loong.Scope{Kernel: k, Node: config})
	if !errors.Is(err, ErrUsage) {
		t.Errorf("empty group = %v, want ErrUsage", err)
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
	root := &loong.Node{
		Type: "test.parent", ID: "root",
		Children: []*loong.Node{
			{Type: "cli.group", ID: "config", Lazy: true, Children: []*loong.Node{
				{Type: "test.show", ID: "show", Lazy: true},
			}},
		},
	}
	k := loong.New()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	info, err := k.Node("config")
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != "config" || info.Type != "cli.group" || !info.Lazy {
		t.Errorf("info = %+v", info)
	}
	if len(info.Children) != 1 || info.Children[0].ID != "show" {
		t.Errorf("children = %+v, want [show]", info.Children)
	}
	if _, err := k.Node("missing"); err == nil {
		t.Error("expected error for unknown node")
	}
	rootInfo, err := k.Root()
	if err != nil || rootInfo.ID != "root" {
		t.Errorf("Root = %+v, err %v", rootInfo, err)
	}
}
