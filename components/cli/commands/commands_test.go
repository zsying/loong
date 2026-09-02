package commands

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/zsying/loong"
)

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
		{Type: "web", Service: true, ServiceTypes: []string{"*web.Router"}, Emits: []string{"biz.*"}},
		{Type: "app"},
		{Type: "log", ConfigFields: []loong.ConfigField{{Name: "level", Type: "string"}}},
	}
	out := renderListText(infos)
	if !strings.HasPrefix(out, "TYPE") {
		t.Fatalf("missing header:\n%s", out)
	}
	for _, want := range []string{"level", "*web.Router", "biz.*"} {
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
		{Type: "web", Service: true, ServiceTypes: []string{"*web.Router"}, Desc: "a <b>web</b>"},
	})
	for _, want := range []string{"<!doctype html>", "<table", "<td>web</td>", "*web.Router", "&lt;b&gt;web&lt;/b&gt;"} {
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
		"web": {Type: "web", Service: true},
	}
	out := renderTree(root, meta)
	for _, want := range []string{"app", "name=hello", "web (main)", "[service]", "listen=:8080"} {
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
