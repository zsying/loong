package loong

import (
	"strings"
	"testing"
)

// Activating a node activates its subtree, which means a service lookup
// made during a Build can start a subtree — and that subtree can need
// something from the node whose Build is still on the stack. The edges
// below are that shape, and they are exactly owlet's: the coordinator
// needs the tool registry while it builds, and the tool registry's
// dispatch child needs the coordinator.
//
// Such a pair is not a cycle the kernel can resolve by trying harder: one
// of the two Builds has to finish first, and neither can. What it can do
// is refuse to build a node twice and say why. The tree breaks the cycle
// by declaring the provider above the consumer, and these two tests pin
// both halves — the refusal, and the order that avoids it.

// svcA and svcB are the two service types the cycle is made of.
type svcA struct{}
type svcB struct{}

// cycA provides *svcA and needs *svcB while it builds. onBuild counts
// instantiations, which is what "built twice" means from the outside.
type cycA struct {
	Base
	onBuild func()
}

func (c *cycA) Build(sc *Scope) error {
	if c.onBuild != nil {
		c.onBuild()
	}
	_, err := sc.TryGet[*svcB]()
	return err
}

// cycB provides *svcB and asks for nothing itself: the second edge lives
// in its subtree, which is where a real cycle comes from.
type cycB struct{ Base }

// askA needs *svcA while it builds and provides nothing.
type askA struct{ Base }

func (c *askA) Build(sc *Scope) error {
	_, err := sc.TryGet[*svcA]()
	return err
}

// TestABuildTimeCycleIsReportedInsteadOfBuiltTwice: with the consumer
// declared first, building it pulls in the provider's subtree, whose
// child needs the consumer back. The kernel must not quietly instantiate
// the consumer a second time — the second instance would be built while
// the first is still building, and a component that registers anything
// (a service, a route, a goroutine) would do it twice.
func TestABuildTimeCycleIsReportedInsteadOfBuiltTwice(t *testing.T) {
	var instantiated int
	registerForTest("test.cycA", func() Component {
		return &cycA{onBuild: func() { instantiated++ }}
	}, WithService(func(Component) *svcA { return &svcA{} }))
	registerForTest("test.cycB", func() Component { return &cycB{} },
		WithService(func(Component) *svcB { return &svcB{} }))
	registerForTest("test.askA", func() Component { return &askA{} })

	k := New()
	root := &Node{
		Type: "loong.base", ID: "root",
		Children: []*Node{
			{Type: "test.cycA", ID: "first"},
			{Type: "test.cycB", ID: "second", Children: []*Node{
				{Type: "test.askA", ID: "inner"},
			}},
		},
	}
	err := k.Assemble(root)
	defer func() { _ = k.Shutdown() }()

	if err == nil {
		t.Fatal("assemble succeeded, want the build-time cycle to be reported")
	}
	if !strings.Contains(err.Error(), `"first"`) {
		t.Errorf("error = %v, want it to name the node that was asked for while building", err)
	}
	if !strings.Contains(err.Error(), "already being built") {
		t.Errorf("error = %v, want it to say the node is already being built", err)
	}
	if instantiated != 1 {
		t.Errorf("the consumer was instantiated %d times, want 1: "+
			"a second Build would run with the first still on the stack", instantiated)
	}
}

// TestDeclarationOrderBreaksABuildTimeCycle is the way out, and the
// reason the rule is worth stating: the same two edges are fine once the
// provider is declared above the consumer. The walk builds the provider
// first, so the consumer is there when the provider's subtree asks for
// it — and the consumer's own Build finds the provider already
// registered instead of having to start it.
func TestDeclarationOrderBreaksABuildTimeCycle(t *testing.T) {
	registerForTest("test.cycA", func() Component { return &cycA{} },
		WithService(func(Component) *svcA { return &svcA{} }))
	registerForTest("test.cycB", func() Component { return &cycB{} },
		WithService(func(Component) *svcB { return &svcB{} }))
	registerForTest("test.askA", func() Component { return &askA{} })

	k := New()
	root := &Node{
		Type: "loong.base", ID: "root",
		Children: []*Node{
			{Type: "test.cycB", ID: "second", Children: []*Node{
				{Type: "test.askA", ID: "inner"},
			}},
			{Type: "test.cycA", ID: "first"},
		},
	}
	if err := k.Assemble(root); err != nil {
		t.Fatalf("assemble with the provider declared first: %v", err)
	}
	defer func() { _ = k.Shutdown() }()

	for _, id := range []string{"second", "inner", "first"} {
		if _, err := k.Component(id); err != nil {
			t.Errorf("Component(%q): %v", id, err)
		}
	}
}
