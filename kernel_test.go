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

// svcValue is a service value exposed by svcProvider.
type svcValue struct{ n int }

// svcProvider is a service component: it provides svcValue through
// ServiceProvider and is registered with AsService[*svcValue]().
type svcProvider struct {
	Base
	v *svcValue
}

func (s *svcProvider) Provide() any { return s.v }

func (s *svcProvider) Build(ctx *Scope) error {
	s.v = &svcValue{n: 42}
	return nil
}

func TestServiceLazyActivation(t *testing.T) {
	RegisterComponent("test.svc", func() Component { return &svcProvider{} }, AsService[*svcValue]())
	RegisterComponent("test.spy", func() Component { return &spy{r: &recorder{}} })
	root := &Node{
		Type: "test.spy", ID: "root",
		Children: []*Node{{Type: "test.svc", ID: "users"}},
	}
	k := New()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	// The service node must stay inactive after assembly: no instance,
	// so no SQLite open / network connect happens at startup.
	if n := k.idIndex["users"]; n.component != nil {
		t.Fatal("service node should not be activated during assembly")
	}
	// First Get activates the node on demand and returns its value.
	if v := k.Get[*svcValue](); v == nil || v.n != 42 {
		t.Fatalf("Get[*svcValue]() = %+v, want &{n:42}", v)
	}
	if n := k.idIndex["users"]; n.component == nil {
		t.Fatal("service node should be activated by Get")
	}
	// Subsequent gets hit the cache; no double activation.
	v1 := k.Get[*svcValue]()
	v2 := k.Get[*svcValue]()
	if v1 != v2 {
		t.Fatal("service should be a singleton across Get calls")
	}
	if err := k.Shutdown(); err != nil {
		t.Fatal(err)
	}
}

// svcUser is a regular component whose Build looks up a lazy service,
// mirroring how the web channel consumes the user service.
type svcUser struct {
	Base
	got *svcValue
}

func (u *svcUser) Build(ctx *Scope) error {
	u.got = ctx.Kernel.Get[*svcValue]()
	return nil
}

func TestServiceActivatedDuringParentBuild(t *testing.T) {
	RegisterComponent("test.svc", func() Component { return &svcProvider{} }, AsService[*svcValue]())
	RegisterComponent("test.svcuser", func() Component { return &svcUser{} })
	root := &Node{
		Type: "test.svcuser", ID: "root",
		Children: []*Node{{Type: "test.svc", ID: "users"}},
	}
	k := New()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	u := k.idIndex["root"].component.(*svcUser)
	if u.got == nil || u.got.n != 42 {
		t.Fatalf("parent Build did not resolve lazy service, got %+v", u.got)
	}
}

func TestServiceUniqueness(t *testing.T) {
	RegisterComponent("test.svc", func() Component { return &svcProvider{} }, AsService[*svcValue]())
	root := &Node{
		Type: "test.spy", ID: "root",
		Children: []*Node{
			{Type: "test.svc", ID: "a"},
			{Type: "test.svc", ID: "b"},
		},
	}
	k := New()
	if err := k.Assemble(root); err == nil {
		t.Fatal("expected error for duplicate service nodes")
	}
}

// lazyComp counts its Build invocations to verify on-demand activation.
type lazyComp struct {
	Base
	builds *int
}

func (l *lazyComp) Build(*Scope) error { *l.builds++; return nil }

func TestLazyNodeActivatedOnDemand(t *testing.T) {
	var builds int
	RegisterComponent("test.lazy", func() Component { return &lazyComp{builds: &builds} })
	RegisterComponent("test.spy", func() Component { return &spy{r: &recorder{}} })
	root := &Node{
		Type: "test.spy", ID: "root",
		Children: []*Node{{Type: "test.lazy", ID: "opt", Lazy: true}},
	}
	k := New()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	if builds != 0 {
		t.Fatalf("lazy node built during assembly: %d builds", builds)
	}
	if err := k.Activate("opt"); err != nil {
		t.Fatal(err)
	}
	if builds != 1 {
		t.Fatalf("after Activate: %d builds, want 1", builds)
	}
	// Activate is idempotent.
	if err := k.Activate("opt"); err != nil {
		t.Fatal(err)
	}
	if builds != 1 {
		t.Fatalf("after second Activate: %d builds, want 1", builds)
	}
	// Unknown id is an error; Shutdown skips never-activated nodes.
	if err := k.Activate("missing"); err == nil {
		t.Fatal("expected error for unknown node id")
	}
	if err := k.Shutdown(); err != nil {
		t.Fatal(err)
	}
}

// svcAlpha is the declared service type of alphaProvider; svcBeta is
// what alphaProvider actually returns, so activation must fail fast.
type svcAlpha struct{ n int }
type svcBeta struct{}

// alphaProvider declares AsService[*svcAlpha]() but Provide()s a
// *svcBeta — a contract violation surfaced at activation time.
type alphaProvider struct {
	Base
}

func (a *alphaProvider) Provide() any       { return &svcBeta{} }
func (a *alphaProvider) Build(*Scope) error { return nil }

func TestServiceTypeMismatch(t *testing.T) {
	RegisterComponent("test.alpha", func() Component { return &alphaProvider{} }, AsService[*svcAlpha]())
	RegisterComponent("test.spy", func() Component { return &spy{r: &recorder{}} })
	root := &Node{
		Type: "test.spy", ID: "root",
		Children: []*Node{{Type: "test.alpha", ID: "alpha"}},
	}
	k := New()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	// The mismatch must fail the on-demand activation of the service
	// node, not silently register a wrong-typed service.
	if _, err := k.TryGet[*svcAlpha](); err == nil {
		t.Fatal("expected error when Provide() type mismatches AsService declaration")
	}
	// Repeated lookups report the cached failure, not re-activate.
	if _, err := k.TryGet[*svcAlpha](); err == nil {
		t.Fatal("expected cached activation error on second lookup")
	}
}

// unknownSvc is never registered; lookups must return zero/error.
type unknownSvc struct{}

func TestGetUnknownService(t *testing.T) {
	k := New()
	if v := k.Get[*unknownSvc](); v != nil {
		t.Fatalf("Get[*unknownSvc]() = %v, want nil", v)
	}
	if _, err := k.TryGet[*unknownSvc](); err == nil {
		t.Fatal("TryGet for unknown service should return an error")
	}
}

// TestServiceConcurrentActivation races the first Get from many
// goroutines: the per-node activation mutex must yield exactly one
// instance that all callers share.
func TestServiceConcurrentActivation(t *testing.T) {
	RegisterComponent("test.svc", func() Component { return &svcProvider{} }, AsService[*svcValue]())
	RegisterComponent("test.spy", func() Component { return &spy{r: &recorder{}} })
	root := &Node{
		Type: "test.spy", ID: "root",
		Children: []*Node{{Type: "test.svc", ID: "users"}},
	}
	k := New()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	const n = 16
	vals := make([]*svcValue, n)
	var wg sync.WaitGroup
	for i := range vals {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			vals[i] = k.Get[*svcValue]()
		}(i)
	}
	wg.Wait()
	for i, v := range vals {
		if v == nil || v != vals[0] {
			t.Fatalf("vals[%d] = %v, want the same singleton %v", i, v, vals[0])
		}
	}
}
