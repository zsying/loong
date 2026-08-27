package loong

import (
	"reflect"
	"sync"
	"testing"
)

// recorder collects lifecycle call order for assertions.
type recorder struct {
	mu    sync.Mutex
	order []string
}

func (r *recorder) record(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.order = append(r.order, s)
}

// spy records each lifecycle phase it goes through.
type spy struct {
	Base
	r *recorder
}

func (s *spy) Build(ctx *Scope) error { s.r.record("build:" + ctx.Node.ID); return nil }
func (s *spy) Run(ctx *Scope) error   { s.r.record("run:" + ctx.Node.ID); return nil }
func (s *spy) Stop(ctx *Scope) error  { s.r.record("stop:" + ctx.Node.ID); return nil }

func TestLifecycleOrder(t *testing.T) {
	var r recorder
	Register("test.spy", func() Component { return &spy{r: &r} })
	root := &Node{
		Type: "test.spy", ID: "root",
		Children: []*Node{
			{Type: "test.spy", ID: "child1"},
			{Type: "test.spy", ID: "child2", Children: []*Node{
				{Type: "test.spy", ID: "grand"},
			}},
		},
	}
	k := New()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	if err := k.Shutdown(); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"build:root", "build:child1", "build:child2", "build:grand",
		"run:root", "run:child1", "run:child2", "run:grand",
		"stop:grand", "stop:child2", "stop:child1", "stop:root",
	}
	if !reflect.DeepEqual(r.order, want) {
		t.Errorf("lifecycle order mismatch:\ngot  %v\nwant %v", r.order, want)
	}
}

// eventParent subscribes to "x.hello" and records the event source.
type eventParent struct {
	Base
	got *string
}

func (p *eventParent) Register(reg *Registry) error {
	reg.On("x.hello", func(e Event) error {
		*p.got = e.Source
		return nil
	})
	return nil
}

// eventChild emits "x.hello" during Run.
type eventChild struct {
	Base
}

func (c *eventChild) Run(ctx *Scope) error {
	return ctx.Emit("x.hello", "hi")
}

func TestEventRouting(t *testing.T) {
	var got string
	Register("test.parent", func() Component { return &eventParent{got: &got} })
	Register("test.child", func() Component { return &eventChild{} })
	root := &Node{
		Type: "test.parent", ID: "parent",
		Children: []*Node{{Type: "test.child", ID: "child"}},
	}
	k := New()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	if got != "child" {
		t.Errorf("event source = %q, want %q", got, "child")
	}
}

func TestUnknownComponentType(t *testing.T) {
	k := New()
	root := &Node{Type: "test.missing", ID: "x"}
	if err := k.Assemble(root); err == nil {
		t.Fatal("expected error for unknown component type")
	}
}
