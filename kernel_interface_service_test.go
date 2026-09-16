package loong

import (
	"strings"
	"testing"
)

// A service type may be an interface, and the whole owlet tool redesign
// stands on that: "this node is a tool" has to be answerable by whether
// the node's component *type* implements a capability, because there is
// one component type per tool and a lookup has to reach the right one.
//
// The guarantee pinned here is narrow and mechanical:
//
//   - WithService keys the declaration by T itself (the interface type),
//     not by whatever concrete type the accessor happens to return;
//   - Providers[T] answers from the skeleton, so a lazy node that has
//     never built is still listed;
//   - a lookup through the interface yields the component, so calling a
//     method on it reaches the implementation;
//   - the consumer trace records the interface, like any other service.
//
// Each of those is a way a generic table keyed by concrete types would
// have failed instead.

// testSpeaker is the capability: an interface, declared by one component
// type and not by another.
type testSpeaker interface{ Speak() string }

type speakerImpl struct {
	Base
	message string
}

func (c *speakerImpl) Speak() string { return c.message }

// silentImpl is a component with no service declaration at all: the
// shape the interface lookup has to tell apart from a real provider.
type silentImpl struct{ Base }

// speakerConsumer asks for the capability both ways a consumer asks: by
// node id (what a resolver does) and by the parent chain (what a plain
// dependency does). It records the outcome instead of failing, so the
// test can report which of the two shapes broke.
type speakerConsumer struct {
	Base
	byID       testSpeaker
	byIDErr    error
	byChain    testSpeaker
	byChainErr error
}

func (c *speakerConsumer) Build(ctx *Scope) error {
	c.Base.Build(ctx)
	c.byID, c.byIDErr = ctx.TryGetFrom[testSpeaker]("a")
	c.byChain, c.byChainErr = ctx.TryGet[testSpeaker]()
	return nil
}

func registerSpeakerTypes() {
	registerForTest("test.speaker", func() Component { return &speakerImpl{message: "hello"} },
		WithService(func(c Component) testSpeaker { return c.(*speakerImpl) }),
		WithDesc("declares the speaker capability"))
	registerForTest("test.silent", func() Component { return &silentImpl{} })
	registerForTest("test.speaker.consumer", func() Component { return &speakerConsumer{} })
}

// TestInterfaceServiceTypeIsDeclaredByTypeAndReadFromTheSkeleton pins
// the two halves a resolver depends on: the declaration is keyed by the
// interface, and it is visible before anything has built.
func TestInterfaceServiceTypeIsDeclaredByTypeAndReadFromTheSkeleton(t *testing.T) {
	registerSpeakerTypes()

	root, err := Parse([]byte(`
type: test.silent
id: root
children:
  - type: test.speaker
    id: a
    lazy: true
  - type: test.silent
    id: quiet
`))
	if err != nil {
		t.Fatalf("parse tree: %v", err)
	}
	k := New()
	if err := k.Assemble(root); err != nil {
		t.Fatalf("assemble: %v", err)
	}
	defer k.Shutdown()

	got := k.Providers[testSpeaker]()
	if len(got) != 1 || got[0] != "a" {
		t.Errorf("Providers[testSpeaker]() = %v, want [a]: the declaring type is the interface, and the node is listed whether or not it built", got)
	}

	// The concrete type the accessor returns is not what the service is
	// keyed by — a table keyed that way would list nothing here.
	if n := len(k.Providers[*speakerImpl]()); n != 0 {
		t.Errorf("Providers[*speakerImpl]() lists %d node(s), want none: the declaration is made with T, not with the value's concrete type", n)
	}

	info, err := k.Node("a")
	if err != nil {
		t.Fatalf("node a: %v", err)
	}
	if len(info.Services) != 1 || info.Services[0] != "loong.testSpeaker" {
		t.Errorf("node a declares services %v, want [loong.testSpeaker]: the tree view spells a service type as <pkg>.<Type>, which is what a structural reader compares against", info.Services)
	}

	// A node with no declaration is not a provider of the interface, so
	// "offers nothing" is expressible without a value to inspect.
	for _, id := range k.Providers[testSpeaker]() {
		if id == "quiet" {
			t.Error("Providers[testSpeaker]() lists the node whose type declares nothing")
		}
	}
}

// TestLookupThroughAnInterfaceServiceReachesTheImplementation pins the
// call side: the value a consumer gets is the component, so a method
// called on it runs the implementation.
func TestLookupThroughAnInterfaceServiceReachesTheImplementation(t *testing.T) {
	registerSpeakerTypes()

	root, err := Parse([]byte(`
type: test.silent
id: root
children:
  - type: test.speaker
    id: a
  - type: test.speaker.consumer
    id: consumer
`))
	if err != nil {
		t.Fatalf("parse tree: %v", err)
	}
	k := New()
	if err := k.Assemble(root); err != nil {
		t.Fatalf("assemble: %v", err)
	}
	defer k.Shutdown()

	comp, err := k.Component("consumer")
	if err != nil {
		t.Fatalf("component consumer: %v", err)
	}
	c, ok := comp.(*speakerConsumer)
	if !ok {
		t.Fatalf("component consumer is %T", comp)
	}

	if c.byIDErr != nil {
		t.Errorf("TryGetFrom[testSpeaker](%q) failed: %v", "a", c.byIDErr)
	} else if got := c.byID.Speak(); got != "hello" {
		t.Errorf("the value resolved by id answers %q, want the implementation's %q: a lookup must yield the component itself", got, "hello")
	}
	if c.byChainErr != nil {
		t.Errorf("TryGet[testSpeaker]() failed: %v", c.byChainErr)
	} else if got := c.byChain.Speak(); got != "hello" {
		t.Errorf("the value resolved by the parent chain answers %q, want %q", got, "hello")
	}

	// The runtime trace keys consumers by the same type the provider
	// declared, so "who uses this capability" works for an interface.
	consumers := k.Consumers[testSpeaker]()
	if len(consumers) == 0 || consumers[0] != "consumer" {
		t.Errorf("Consumers[testSpeaker]() = %v, want it to name the consumer node", consumers)
	}
}

// TestLookupOfAnInterfaceServiceAbsentFromTheNodeFails pins the error
// path a resolver relies on: asking a node that declares nothing must
// fail by naming the interface, not by returning an empty value.
func TestLookupOfAnInterfaceServiceAbsentFromTheNodeFails(t *testing.T) {
	registerSpeakerTypes()

	root, err := Parse([]byte(`
type: test.silent
id: root
children:
  - type: test.silent
    id: quiet
  - type: test.speaker.consumer
    id: consumer
`))
	if err != nil {
		t.Fatalf("parse tree: %v", err)
	}
	k := New()
	if err := k.Assemble(root); err != nil {
		t.Fatalf("assemble: %v", err)
	}
	defer k.Shutdown()

	comp, err := k.Component("consumer")
	if err != nil {
		t.Fatalf("component consumer: %v", err)
	}
	c := comp.(*speakerConsumer)

	if c.byIDErr == nil {
		t.Error("TryGetFrom[testSpeaker](\"a\") succeeded for an unknown node id, want an error")
	}
	// The message must name the *interface*, spelled as the tree view
	// spells it. A component type name cannot satisfy this — the
	// registered type is test.speaker, and the mounted-typed list in
	// this message is the other half of the sentence.
	if c.byChainErr == nil {
		t.Error("TryGet[testSpeaker]() succeeded with no provider in the tree, want an error naming the interface")
	} else if !strings.Contains(c.byChainErr.Error(), "loong.testSpeaker") {
		t.Errorf("error = %v, want it to name the interface type loong.testSpeaker", c.byChainErr)
	}
}
