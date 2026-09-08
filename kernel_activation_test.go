package loong

import (
	"testing"
)

// The activation tests share one registration of actProv/actConsumer
// (RegisterComponent panics on duplicate type names, mirroring
// database/sql.Register), with per-test counters reset before each run.
var actCounters struct{ builds, runs, stops int }

func init() {
	RegisterComponent("test.actprov", func() Component {
		return &actProv{builds: &actCounters.builds, runs: &actCounters.runs, stops: &actCounters.stops}
	}, WithService(func(c Component) *actSvc { return c.(*actProv).v }))
	RegisterComponent("test.actconsumer", func() Component {
		return &actConsumer{}
	})
}

func resetActCounters() {
	actCounters.builds, actCounters.runs, actCounters.stops = 0, 0, 0
}

// actSvc is the service value exposed by actProv. Its own type keeps it
// from colliding with any service type declared by other tests.
type actSvc struct{ n int }

// actProv is a non-lazy service component. It shares build/run counters
// through pointers so a double-activation (factory called twice, yielding
// a second instance) still increments the SAME counters — that is what
// lets the test detect the bug instead of being fooled by the pointer
// being reassigned to a fresh instance.
type actProv struct {
	Base
	builds *int
	runs   *int
	stops  *int
	v      *actSvc
}

func (p *actProv) Build(*Scope) error {
	*p.builds++
	p.v = &actSvc{n: 7}
	return nil
}

func (p *actProv) Run(*Scope) error {
	*p.runs++
	return nil
}

func (p *actProv) Stop(*Scope) error {
	*p.stops++
	return nil
}

// actConsumer is a non-lazy component whose Build pulls in actSvc via
// TryGet. The provider is declared AFTER it in the tree, so at startup
// the kernel lazily activates the provider inside this Build. A correct
// kernel then leaves that provider alone for the rest of assembly; a
// buggy kernel rebuilds and re-runs it when the assembly walk reaches it.
type actConsumer struct {
	Base
	got    *actSvc
	gotErr error
}

func (c *actConsumer) Build(ctx *Scope) error {
	c.got, c.gotErr = ctx.TryGet[*actSvc]()
	return nil
}

// TestNonLazyProviderActivatedOnce reproduces the double-activation bug:
// a non-lazy node's Build triggers on-demand activation of a sibling
// non-lazy provider declared later in the tree. The provider must be
// Built and Run exactly once across the whole assembly.
func TestNonLazyProviderActivatedOnce(t *testing.T) {
	resetActCounters()
	provBuilds, provRuns, provStops := &actCounters.builds, &actCounters.runs, &actCounters.stops

	root := &Node{
		Type: "base", ID: "root",
		Children: []*Node{
			{Type: "test.actconsumer", ID: "consumer"},
			{Type: "test.actprov", ID: "provider"},
		},
	}
	k := New()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}

	// The consumer's Build must have resolved the service: the provider
	// was activated on demand inside that Build.
	cons := k.idIndex["consumer"].component.(*actConsumer)
	if cons.got == nil || cons.gotErr != nil {
		t.Fatalf("consumer did not resolve service: got=%+v err=%v", cons.got, cons.gotErr)
	}

	// The provider must be Built and Run exactly once. With the bug these
	// counters are 2 (a second instance is created and wired/started).
	if *provBuilds != 1 {
		t.Fatalf("provider Build ran %d times, want exactly 1", provBuilds)
	}
	if *provRuns != 1 {
		t.Fatalf("provider Run ran %d times, want exactly 1", provRuns)
	}

	// Shutdown must stop the single provider instance (no leak, no panic).
	if err := k.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if *provStops != 1 {
		t.Fatalf("provider Stop ran %d times, want exactly 1", provStops)
	}
}

// TestNonLazyProviderDeclaredBeforeConsumer is the inverse ordering: the
// provider is declared BEFORE the consumer. No on-demand activation
// happens during the consumer's Build (the provider is already active),
// so this sanity-checks that the fix does not over-skip nodes.
func TestNonLazyProviderDeclaredBeforeConsumer(t *testing.T) {
	resetActCounters()
	provBuilds, provRuns, provStops := &actCounters.builds, &actCounters.runs, &actCounters.stops

	root := &Node{
		Type: "base", ID: "root",
		Children: []*Node{
			{Type: "test.actprov", ID: "provider"},
			{Type: "test.actconsumer", ID: "consumer"},
		},
	}
	k := New()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	cons := k.idIndex["consumer"].component.(*actConsumer)
	if cons.got == nil || cons.gotErr != nil {
		t.Fatalf("consumer did not resolve service: got=%+v err=%v", cons.got, cons.gotErr)
	}
	if *provBuilds != 1 || *provRuns != 1 {
		t.Fatalf("provider lifecycles: builds=%d runs=%d, want 1/1", provBuilds, provRuns)
	}
	if err := k.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if *provStops != 1 {
		t.Fatalf("provider Stop ran %d times, want exactly 1", provStops)
	}
}
