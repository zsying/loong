package cli

import (
	"reflect"
	"strings"
	"testing"

	"github.com/zsying/loong"
)

// tailChildren builds two groups that each mount a `show` subcommand
// under a path-style id (config.show / cache.show) — the case that
// motivates tail matching: node ids stay globally unique while both
// groups expose a `show` command.
func tailChildren() []*loong.Node {
	return []*loong.Node{
		{Type: GroupType, ID: "config", Lazy: true, Children: []*loong.Node{
			{Type: "test.rec", ID: "config.show", Lazy: true},
			{Type: "test.rec", ID: "config.set", Lazy: true},
		}},
		{Type: GroupType, ID: "cache", Lazy: true, Children: []*loong.Node{
			{Type: "test.rec", ID: "cache.show", Lazy: true},
			{Type: "test.rec", ID: "cache.clear", Lazy: true},
		}},
	}
}

func recAt(t *testing.T, k *loong.Kernel, id string) *recorder {
	t.Helper()
	comp, err := k.Component(id)
	if err != nil {
		t.Fatalf("component %q: %v", id, err)
	}
	rec, ok := comp.(*recorder)
	if !ok {
		t.Fatalf("component %q = %T, want *recorder", id, comp)
	}
	return rec
}

// TestTailCommandMatching pins that an argv token resolves through the
// master and every group by exact id first, then by id tail — so
// `demo config show` activates config.show, not cache.show, and both
// groups can expose the same command name.
func TestTailCommandMatching(t *testing.T) {
	n, sc := cliNode(t, "", tailChildren()...)
	c := buildMaster(t, n, sc)

	if err := c.masterRun(sc, []string{"config", "show", "extra"}); err != nil {
		t.Fatalf("config show: %v", err)
	}
	rec := recAt(t, sc.Kernel, "config.show")
	if got := recArgs(t, rec).Positional(); !rec.ran || !reflect.DeepEqual(got, []string{"extra"}) {
		t.Fatalf("config.show ran=%v args=%v, want [extra]", rec.ran, got)
	}

	if err := c.masterRun(sc, []string{"cache", "show"}); err != nil {
		t.Fatalf("cache show: %v", err)
	}
	if !recAt(t, sc.Kernel, "cache.show").ran {
		t.Error("cache.show must run for `cache show`")
	}

	// A token may always spell the child's full id at the level that
	// owns it. Fresh kernel: groups run once per process, and the
	// config group already dispatched above.
	n2, sc2 := cliNode(t, "", tailChildren()...)
	c2 := buildMaster(t, n2, sc2)
	if err := c2.masterRun(sc2, []string{"config", "config.set", "k=v"}); err != nil {
		t.Fatalf("config.set by full id: %v", err)
	}
	if got := recArgs(t, recAt(t, sc2.Kernel, "config.set")).Positional(); !reflect.DeepEqual(got, []string{"k=v"}) {
		t.Fatalf("config.set args = %v, want [k=v]", got)
	}

	// An unknown subcommand names the token and the group's commands
	// (cache group, unused in this kernel so far).
	err := c2.masterRun(sc2, []string{"cache", "nope"})
	if err == nil || !strings.Contains(err.Error(), `"nope"`) || !strings.Contains(err.Error(), "clear") {
		t.Fatalf("unknown subcommand err = %v, want token and command list", err)
	}
}

// TestCommandTokenCollisions pins the Build-time ambiguity check: two
// siblings must not answer the same token, whether by shared tail or
// by one child's id equalling another's tail.
func TestCommandTokenCollisions(t *testing.T) {
	cases := []struct {
		name     string
		children []*loong.Node
	}{
		{
			name: "same tail under one group",
			children: []*loong.Node{
				{Type: "test.rec", ID: "config.show", Lazy: true},
				{Type: "test.rec", ID: "cache.show", Lazy: true},
			},
		},
		{
			name: "tail swallowed by a sibling id",
			children: []*loong.Node{
				{Type: "test.rec", ID: "show", Lazy: true},
				{Type: "test.rec", ID: "config.show", Lazy: true},
			},
		},
	}
	for _, tc := range cases {
		group := &loong.Node{Type: GroupType, ID: "config", Lazy: true, Children: tc.children}
		if err := checkCommandTokens(group); err == nil {
			t.Errorf("%s: checkCommandTokens = nil, want collision error", tc.name)
		}
	}

	// The master runs the same check at assembly time.
	_, sc := cliNode(t, "", []*loong.Node{
		{Type: "test.rec", ID: "db.migrate", Lazy: true},
		{Type: "test.rec", ID: "migrate", Lazy: true},
	}...)
	c := &Component{}
	if err := c.Build(sc); err == nil {
		t.Error("master Build = nil, want token collision error")
	}
}
