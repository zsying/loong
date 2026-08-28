package loong

import (
	"errors"
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
	RegisterComponent("test.spy", func() Component { return &spy{r: &r} })
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

func (p *eventParent) Build(ctx *Scope) error {
	ctx.On("x.hello", func(e Event) error {
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
	RegisterComponent("test.parent", func() Component { return &eventParent{got: &got} })
	RegisterComponent("test.child", func() Component { return &eventChild{} })
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

// typedLog collects payloads received via OnTyped.
var typedLog []string

// typedParent subscribes with OnTyped[string] and records the payload.
type typedParent struct {
	Base
}

func (p *typedParent) Build(ctx *Scope) error {
	ctx.OnTyped("x.typed", func(s string) error {
		typedLog = append(typedLog, s)
		return nil
	})
	return nil
}

// typedChild emits an arbitrary payload for "x.typed".
type typedChild struct {
	Base
	payload any
}

func (c *typedChild) Run(ctx *Scope) error {
	return ctx.Emit("x.typed", c.payload)
}

func TestOnTyped(t *testing.T) {
	RegisterComponent("test.tparent", func() Component { return &typedParent{} })
	RegisterComponent("test.tchild", func() Component { return &typedChild{payload: "hi"} })

	// Matching payload type: handler receives the typed value.
	k := New()
	root := &Node{
		Type: "test.tparent", ID: "p",
		Children: []*Node{{Type: "test.tchild", ID: "c"}},
	}
	if err := k.Assemble(root); err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if len(typedLog) != 1 || typedLog[0] != "hi" {
		t.Errorf("typed payload = %v, want [hi]", typedLog)
	}

	// Mismatched payload type: the wrapped handler error surfaces
	// through Emit during Run.
	RegisterComponent("test.tbad", func() Component { return &typedChild{payload: 42} })
	k2 := New()
	root2 := &Node{
		Type: "test.tparent", ID: "p2",
		Children: []*Node{{Type: "test.tbad", ID: "c2"}},
	}
	if err := k2.Assemble(root2); err == nil {
		t.Error("expected error for mismatched typed payload")
	}
}

func TestUnknownComponentType(t *testing.T) {
	k := New()
	root := &Node{Type: "test.missing", ID: "x"}
	if err := k.Assemble(root); err == nil {
		t.Fatal("expected error for unknown component type")
	}
}

// buildFail fails during Build.
type buildFail struct {
	Base
}

func (b *buildFail) Build(*Scope) error { return errors.New("boom") }

func TestAssembleFailureCleanup(t *testing.T) {
	var r recorder
	RegisterComponent("test.failcleanup", func() Component { return &spy{r: &r} })
	RegisterComponent("test.failboom", func() Component { return &buildFail{} })
	root := &Node{
		Type: "test.failcleanup", ID: "p",
		Children: []*Node{
			{Type: "test.failcleanup", ID: "c1"},
			{Type: "test.failboom", ID: "c2"},
		},
	}
	k := New()
	if err := k.Assemble(root); err == nil {
		t.Fatal("expected assembly to fail")
	}
	// c1 was built before c2 failed; it must be stopped during cleanup.
	found := false
	for _, s := range r.order {
		if s == "stop:c1" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("built components not cleaned up on failure; order = %v", r.order)
	}
	// Shutdown after a failed assembly must not panic (root may be nil).
	k2 := New()
	if err := k2.Shutdown(); err != nil {
		t.Fatalf("shutdown before assemble: %v", err)
	}
}
