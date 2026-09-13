package loong

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// A lazy node is a subsystem, not a node: `lazy: true` on a node turns
// off the whole subtree it is mounted over, and activating the node
// brings that subtree up in phase order — every Build, parent before
// child, before any Run. These tests pin both halves. They exist because
// the rule used to be per node, which left a tree readable two ways: a
// subsystem marked off whose children were nonetheless running.
//
// The shape they must tell apart is the one the tree is written in —
// containment — so every tree below nests the nodes it talks about.

// recComp records its own lifecycle in a shared log, tagged with its node
// id, so a test asserts *what* ran and *in what order*: the two things a
// lazy subtree has to get right.
type recComp struct {
	Base
	log *[]string
}

func (c *recComp) Build(sc *Scope) error {
	*c.log = append(*c.log, "build:"+sc.Node.ID)
	return nil
}

func (c *recComp) Run(sc *Scope) error {
	*c.log = append(*c.log, "run:"+sc.Node.ID)
	return nil
}

func (c *recComp) Stop(sc *Scope) error {
	*c.log = append(*c.log, "stop:"+sc.Node.ID)
	return nil
}

// recTree registers the recording component against one log and returns a
// kernel for it. Self-contained on purpose: these tests must not depend on
// another test file's registrations (see registerForTest).
func recTree(log *[]string) *Kernel {
	registerForTest("test.rec", func() Component { return &recComp{log: log} })
	return New()
}

// badComp fails its Build, for the failure path.
type badComp struct{ Base }

func (b *badComp) Build(*Scope) error { return errors.New("bad build") }

// argComp records the activation argument it was handed — the value a
// parent passes through Scope.Activate.
type argComp struct {
	Base
	got any
}

func (a *argComp) Build(sc *Scope) error {
	a.got = sc.Args
	return nil
}

// TestLazyAnchorKeepsItsSubtreeOffAtStartup is the first half of the
// rule: marking the anchor lazy means nothing under it is built at
// startup. A non-lazy child is not an escape hatch — if it were, the tree
// would report a subsystem as off while part of it is running, and the
// child's Build would run without the parent its wiring assumes.
func TestLazyAnchorKeepsItsSubtreeOffAtStartup(t *testing.T) {
	var log []string
	k := recTree(&log)
	root := &Node{
		Type: "base", ID: "root",
		Children: []*Node{
			{Type: "test.rec", ID: "off", Lazy: true, Children: []*Node{
				{Type: "test.rec", ID: "inner"},
			}},
			{Type: "test.rec", ID: "always"},
		},
	}
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = k.Shutdown() }()

	if want := []string{"build:always", "run:always"}; !reflect.DeepEqual(log, want) {
		t.Errorf("lifecycle = %v, want %v: the lazy anchor's subtree stayed off", log, want)
	}
}

// TestActivatingALazyAnchorActivatesItsSubtree closes the rule: the
// subtree comes up as one unit, and in the order the component model
// requires — parent before child, and every Build before any Run. A
// child wired to something its anchor builds (a router, a listener)
// depends on both halves of that.
func TestActivatingALazyAnchorActivatesItsSubtree(t *testing.T) {
	var log []string
	k := recTree(&log)
	root := &Node{
		Type: "base", ID: "root",
		Children: []*Node{
			{Type: "test.rec", ID: "off", Lazy: true, Children: []*Node{
				{Type: "test.rec", ID: "inner"},
			}},
			{Type: "test.rec", ID: "always"},
		},
	}
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = k.Shutdown() }()

	if err := k.Activate("off"); err != nil {
		t.Fatalf("Activate(off): %v", err)
	}

	want := []string{
		"build:always", "run:always",
		"build:off", "build:inner",
		"run:off", "run:inner",
	}
	if !reflect.DeepEqual(log, want) {
		t.Errorf("lifecycle = %v, want %v", log, want)
	}
}

// TestALazyDescendantStaysOffInsideAnActivatedSubtree draws the inner
// boundary: activation reaches the anchor's non-lazy descendants and
// stops at the ones the tree marked lazy again. A nested option (a
// debug server under a subsystem) is therefore still off until it is
// asked for, and asking for it does not re-run the anchor.
func TestALazyDescendantStaysOffInsideAnActivatedSubtree(t *testing.T) {
	var log []string
	k := recTree(&log)
	root := &Node{
		Type: "base", ID: "root",
		Children: []*Node{
			{Type: "test.rec", ID: "off", Lazy: true, Children: []*Node{
				{Type: "test.rec", ID: "inner"},
				{Type: "test.rec", ID: "deeper", Lazy: true, Children: []*Node{
					{Type: "test.rec", ID: "leaf"},
				}},
			}},
		},
	}
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = k.Shutdown() }()

	if err := k.Activate("off"); err != nil {
		t.Fatalf("Activate(off): %v", err)
	}
	want := []string{"build:off", "build:inner", "run:off", "run:inner"}
	if !reflect.DeepEqual(log, want) {
		t.Fatalf("after Activate(off) = %v, want %v: the nested lazy node stayed off", log, want)
	}

	if err := k.Activate("deeper"); err != nil {
		t.Fatalf("Activate(deeper): %v", err)
	}
	want = append(want, "build:deeper", "build:leaf", "run:deeper", "run:leaf")
	if !reflect.DeepEqual(log, want) {
		t.Errorf("after Activate(deeper) = %v, want %v", log, want)
	}
}

// TestActivationArgumentsBelongToTheAnchor: args are the parent's word to
// the node it activates, so the subtree below inherits the activation but
// not the argument. Handing a child an argument meant for its parent would
// make Scope.Args mean two things at once.
func TestActivationArgumentsBelongToTheAnchor(t *testing.T) {
	registerForTest("test.argrec", func() Component { return &argComp{} })
	k := New()
	root := &Node{
		Type: "base", ID: "root",
		Children: []*Node{
			{Type: "test.argrec", ID: "off", Lazy: true, Children: []*Node{
				{Type: "test.argrec", ID: "inner"},
			}},
		},
	}
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = k.Shutdown() }()

	if err := scopeAt(k, "root").Activate("off", "payload"); err != nil {
		t.Fatalf("Scope.Activate(off, payload): %v", err)
	}
	if got := k.idIndex["off"].component.(*argComp).got; got != "payload" {
		t.Errorf("anchor Args = %#v, want %q", got, "payload")
	}
	if got := k.idIndex["inner"].component.(*argComp).got; got != nil {
		t.Errorf("child Args = %#v, want nil: the argument belongs to the anchor", got)
	}

	// A Kernel-level activation has no parent to speak for it, so it
	// passes nothing — the subtree below is still brought up.
	k2 := New()
	root2 := &Node{
		Type: "base", ID: "root",
		Children: []*Node{
			{Type: "test.argrec", ID: "off", Lazy: true, Children: []*Node{
				{Type: "test.argrec", ID: "inner"},
			}},
		},
	}
	if err := k2.Assemble(root2); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = k2.Shutdown() }()

	if err := k2.Activate("off"); err != nil {
		t.Fatalf("k2.Activate(off): %v", err)
	}
	if got := k2.idIndex["off"].component.(*argComp).got; got != nil {
		t.Errorf("anchor Args = %#v, want nil: Kernel.Activate passes nothing", got)
	}
	if k2.idIndex["inner"].component == nil {
		t.Error("subtree was not activated")
	}
}

// TestAFailedBuildInsideTheSubtreeStopsWhatWasBuilt: a subtree that fails
// to come up must not leave half of it running. Everything the activation
// built is stopped, in reverse.
func TestAFailedBuildInsideTheSubtreeStopsWhatWasBuilt(t *testing.T) {
	var log []string
	registerForTest("test.bad", func() Component { return &badComp{} })
	k := recTree(&log)
	root := &Node{
		Type: "base", ID: "root",
		Children: []*Node{
			{Type: "test.rec", ID: "off", Lazy: true, Children: []*Node{
				{Type: "test.rec", ID: "ok"},
				{Type: "test.bad", ID: "bad"},
			}},
		},
	}
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = k.Shutdown() }()

	err := k.Activate("off")
	if err == nil {
		t.Fatal("Activate(off) succeeded, want the child's Build failure")
	}
	if !strings.Contains(err.Error(), `"bad"`) {
		t.Errorf("error = %v, want it to name the failing node", err)
	}

	want := []string{"build:off", "build:ok", "stop:ok", "stop:off"}
	if !reflect.DeepEqual(log, want) {
		t.Errorf("lifecycle = %v, want %v: the built part is stopped in reverse", log, want)
	}
}

// TestActivatingANodeAssemblyBuiltIsANoOp: assembly is an activation —
// the root's — so the same idempotence has to cover the nodes it brought
// up. It did not while assembly had its own build path: those nodes were
// never marked active, and asking for one of them built a second
// instance (silently, when the component provides no service).
func TestActivatingANodeAssemblyBuiltIsANoOp(t *testing.T) {
	var log []string
	k := recTree(&log)
	root := &Node{
		Type: "base", ID: "root",
		Children: []*Node{
			{Type: "test.rec", ID: "up"},
		},
	}
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = k.Shutdown() }()

	once := append([]string(nil), log...)
	if err := k.Activate("up"); err != nil {
		t.Fatalf("Activate(up): %v", err)
	}
	if !reflect.DeepEqual(log, once) {
		t.Errorf("Activate rebuilt an assembled node: %v -> %v", once, log)
	}
}

// TestActivatingALazyAnchorTwiceBuildsItOnce keeps the idempotence the
// per-node rule already had: the cascade must not turn a second
// activation into a second instance.
func TestActivatingALazyAnchorTwiceBuildsItOnce(t *testing.T) {
	var log []string
	k := recTree(&log)
	root := &Node{
		Type: "base", ID: "root",
		Children: []*Node{
			{Type: "test.rec", ID: "off", Lazy: true, Children: []*Node{
				{Type: "test.rec", ID: "inner"},
			}},
		},
	}
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = k.Shutdown() }()

	if err := k.Activate("off"); err != nil {
		t.Fatalf("Activate(off): %v", err)
	}
	once := append([]string(nil), log...)
	if err := k.Activate("off"); err != nil {
		t.Fatalf("Activate(off) again: %v", err)
	}
	if !reflect.DeepEqual(log, once) {
		t.Errorf("second activation changed the lifecycle: %v -> %v", once, log)
	}
}
