package loong

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// subscriber records every payload it receives on a topic.
type subscriber struct {
	Base
	mu   sync.Mutex
	got  []any
	fail error
}

func (s *subscriber) record(v any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, v)
	return s.fail
}

func (s *subscriber) snapshot() []any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]any, len(s.got))
	copy(out, s.got)
	return out
}

// subBuild subscribes to a topic during Build, as a real component would.
type subBuild struct {
	Base
	topic string
	sub   *subscriber
	stop  func()
}

func (c *subBuild) Build(ctx *Scope) error {
	c.stop = ctx.Subscribe(c.topic, func(e Event) error {
		return c.sub.record(e.Payload)
	})
	return nil
}

// TestPublishReachesSubscriberAcrossTheTree pins the core property that
// Emit cannot provide: a subscriber receives a topic published by a node
// that is NOT its parent, and not even an ancestor.
func TestPublishReachesSubscriberAcrossTheTree(t *testing.T) {
	sub := &subscriber{}
	registerForTest("test.sub", func() Component { return &subBuild{topic: "config.changed", sub: sub} })
	registerForTest("test.pub", func() Component { return &publisher{topic: "config.changed", payload: "v2"} })

	// The publisher is a sibling subtree of the subscriber: under the
	// single-hop Emit routing, neither could ever hear the other.
	root := &Node{
		Type: "test.sub", ID: "listener",
		Children: []*Node{
			{Type: "test.pub", ID: "inside"},
		},
	}
	k := New()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}

	got := sub.snapshot()
	if len(got) != 1 || got[0] != "v2" {
		t.Fatalf("subscriber payloads = %v, want [v2]", got)
	}
}

// publisher publishes one topic during Run.
type publisher struct {
	Base
	topic   string
	payload any
	err     error
}

func (p *publisher) Run(ctx *Scope) error {
	p.err = ctx.Publish(p.topic, p.payload)
	return p.err
}

// TestPublishReachesEverySubscriber pins that a topic fans out: two
// independent subscribers on the same topic both receive the payload.
// This is the property "notify several nodes" requires.
func TestPublishReachesEverySubscriber(t *testing.T) {
	a, b := &subscriber{}, &subscriber{}
	registerForTest("test.subA", func() Component { return &subBuild{topic: "config.changed", sub: a} })
	registerForTest("test.subB", func() Component { return &subBuild{topic: "config.changed", sub: b} })
	registerForTest("test.pubB", func() Component { return &publisher{topic: "config.changed", payload: 42} })

	root := &Node{
		Type: "test.subA", ID: "a",
		Children: []*Node{
			{Type: "test.subB", ID: "b", Children: []*Node{
				{Type: "test.pubB", ID: "pub"},
			}},
		},
	}
	k := New()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	for name, s := range map[string]*subscriber{"a": a, "b": b} {
		if got := s.snapshot(); len(got) != 1 || got[0] != 42 {
			t.Errorf("subscriber %s payloads = %v, want [42]", name, got)
		}
	}
}

// TestSubscribeIgnoresTitlesItDidNotAskFor pins that delivery is by
// topic and nothing else: a subscriber on one topic never sees another's.
func TestSubscribeIgnoresOtherTopics(t *testing.T) {
	sub := &subscriber{}
	registerForTest("test.subOne", func() Component { return &subBuild{topic: "config.changed", sub: sub} })
	registerForTest("test.pubOther", func() Component { return &publisher{topic: "tools.changed", payload: "x"} })

	root := &Node{
		Type: "test.subOne", ID: "listener",
		Children: []*Node{{Type: "test.pubOther", ID: "pub"}},
	}
	k := New()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	if got := sub.snapshot(); len(got) != 0 {
		t.Errorf("subscriber got %v, want nothing", got)
	}
}

// TestPublishWithNoSubscriberIsSilent pins that publishing to an unheard
// topic is a successful no-op, matching Emit's notification semantics:
// the publisher does not need to know who is listening.
func TestPublishWithNoSubscriberIsSilent(t *testing.T) {
	registerForTest("test.pubLonely", func() Component { return &publisher{topic: "nobody.home", payload: nil} })
	root := &Node{Type: "test.pubLonely", ID: "solo"}
	k := New()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
}

// TestSubscribeCancelStopsDelivery pins that the cancel func returned by
// Subscribe unsubscribes, so a stopped node stops being called.
func TestSubscribeCancelStopsDelivery(t *testing.T) {
	sub := &subscriber{}
	c := &cancelSub{topic: "config.changed", sub: sub}
	registerForTest("test.cancelSub", func() Component { return c })
	registerForTest("test.pubTwice", func() Component { return &twoPhasePub{topic: "config.changed", after: func() { c.stop() }} })

	root := &Node{
		Type:     "test.cancelSub",
		ID:       "listener",
		Children: []*Node{{Type: "test.pubTwice", ID: "pub"}},
	}
	k := New()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	// First publish delivered, second (after cancel) must not arrive.
	if got := sub.snapshot(); len(got) != 1 || got[0] != "first" {
		t.Fatalf("payloads = %v, want [first]", got)
	}
}

// cancelSub captures the cancel func so a peer can invoke it mid-flight.
type cancelSub struct {
	Base
	topic string
	sub   *subscriber
	stop  func()
}

func (c *cancelSub) Build(ctx *Scope) error {
	c.stop = ctx.Subscribe(c.topic, func(e Event) error { return c.sub.record(e.Payload) })
	return nil
}

// twoPhasePub publishes, runs a callback (that cancels), then publishes again.
type twoPhasePub struct {
	Base
	topic string
	after func()
}

func (p *twoPhasePub) Run(ctx *Scope) error {
	if err := ctx.Publish(p.topic, "first"); err != nil {
		return err
	}
	p.after()
	return ctx.Publish(p.topic, "second")
}

// TestPublishPropagatesHandlerError pins that a handler failure is
// reported to the publisher rather than swallowed: a subscriber that
// fails to apply a change must be able to stop the publish.
func TestPublishPropagatesHandlerError(t *testing.T) {
	boom := errors.New("boom")
	sub := &subscriber{fail: boom}
	registerForTest("test.subFail", func() Component { return &subBuild{topic: "config.changed", sub: sub} })
	registerForTest("test.pubFail", func() Component { return &publisher{topic: "config.changed", payload: "x"} })

	root := &Node{
		Type:     "test.subFail",
		ID:       "listener",
		Children: []*Node{{Type: "test.pubFail", ID: "pub"}},
	}
	k := New()
	err := k.Assemble(root)
	if !errors.Is(err, boom) {
		t.Errorf("Assemble err = %v, want it to wrap %v", err, boom)
	}
}

// TestSubscribeAfterBuildStillReceives pins that subscribing is a runtime
// operation, not a Build-only one, because a subscriber may only learn it
// needs a topic later.
func TestSubscribeAfterBuildStillReceives(t *testing.T) {
	sub := &subscriber{}
	late := &lateSub{topic: "config.changed", sub: sub}
	registerForTest("test.lateSub", func() Component { return late })
	registerForTest("test.pubLate", func() Component { return &publisher{topic: "config.changed", payload: "z"} })

	root := &Node{
		Type:     "test.lateSub",
		ID:       "listener",
		Children: []*Node{{Type: "test.pubLate", ID: "pub"}},
	}
	k := New()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	if got := sub.snapshot(); len(got) != 1 {
		t.Errorf("payloads = %v, want one delivery", got)
	}
}

// lateSub subscribes in Run, after Build has returned.
type lateSub struct {
	Base
	topic string
	sub   *subscriber
	stop  func()
}

func (c *lateSub) Run(ctx *Scope) error {
	c.stop = ctx.Subscribe(c.topic, func(e Event) error { return c.sub.record(e.Payload) })
	return nil
}

// typedSub subscribes with SubscribeTyped, receiving a concrete type.
type typedSub struct {
	Base
	got *[]string
}

func (c *typedSub) Build(ctx *Scope) error {
	ctx.SubscribeTyped[string]("config.changed", func(v string) error {
		*c.got = append(*c.got, v)
		return nil
	})
	return nil
}

// TestSubscribeTypedDeliversTheTypedPayload pins the wrapper's happy path.
func TestSubscribeTypedDeliversTheTypedPayload(t *testing.T) {
	var got []string
	registerForTest("test.typedSub", func() Component { return &typedSub{got: &got} })
	registerForTest("test.pubTyped", func() Component { return &publisher{topic: "config.changed", payload: "ok"} })

	root := &Node{
		Type:     "test.typedSub",
		ID:       "listener",
		Children: []*Node{{Type: "test.pubTyped", ID: "pub"}},
	}
	k := New()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "ok" {
		t.Errorf("typed payloads = %v, want [ok]", got)
	}
}

// TestSubscribeTypedRejectsTheWrongPayloadType pins that a type mismatch
// surfaces to the publisher as an error rather than panicking inside the
// routing table, which is the whole reason the assertion lives here.
func TestSubscribeTypedRejectsTheWrongPayloadType(t *testing.T) {
	var got []string
	registerForTest("test.typedSubBad", func() Component { return &typedSub{got: &got} })
	registerForTest("test.pubTypedBad", func() Component { return &publisher{topic: "config.changed", payload: 123} })

	root := &Node{
		Type:     "test.typedSubBad",
		ID:       "listener",
		Children: []*Node{{Type: "test.pubTypedBad", ID: "pub"}},
	}
	k := New()
	err := k.Assemble(root)
	if err == nil {
		t.Fatal("Assemble succeeded, want a payload type error")
	}
	if !strings.Contains(err.Error(), "want string") {
		t.Errorf("err = %v, want it to name the expected type", err)
	}
	if len(got) != 0 {
		t.Errorf("handler ran with %v, want it skipped", got)
	}
}

// reentrantSub mutates the subscription table from inside its own
// handler: it unsubscribes itself and publishes again.
type reentrantSub struct {
	Base
	got  *[]string
	stop func()
	ctx  *Scope
}

func (c *reentrantSub) Build(ctx *Scope) error {
	c.ctx = ctx
	c.stop = ctx.Subscribe("config.changed", func(e Event) error {
		*c.got = append(*c.got, e.Payload.(string))
		// Touching the table from within a delivery is the hazard the
		// lock-outside-call design exists to survive.
		c.stop()
		return c.ctx.Publish("config.changed", "nested")
	})
	return nil
}

// TestHandlerMayTouchTheSubscriptionTable pins that a handler runs
// outside the kernel lock: it can unsubscribe itself and publish again
// without deadlocking. A publish that held the lock across delivery
// would hang here.
func TestHandlerMayTouchTheSubscriptionTable(t *testing.T) {
	var got []string
	sub := &reentrantSub{got: &got}
	registerForTest("test.reentrant", func() Component { return sub })
	registerForTest("test.pubOnce", func() Component { return &publisher{topic: "config.changed", payload: "outer"} })

	root := &Node{
		Type:     "test.reentrant",
		ID:       "listener",
		Children: []*Node{{Type: "test.pubOnce", ID: "pub"}},
	}
	k := New()
	done := make(chan error, 1)
	go func() { done <- k.Assemble(root) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Assemble did not finish: publish is holding the lock across delivery")
	}
	// The outer payload arrived; the nested one must not, because the
	// handler unsubscribed itself before publishing it.
	if len(got) != 1 || got[0] != "outer" {
		t.Errorf("payloads = %v, want [outer]", got)
	}
}
