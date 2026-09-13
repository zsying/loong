package loong

import (
	"reflect"
	"strings"
	"testing"
)

// toolKind and otherKind are two contribution kinds. The kind is a Go
// type, exactly as the service key is, so declarations in different
// kinds never mix and a typo cannot pass for one.
type toolKind struct{}
type otherKind struct{}

// contNode contributes its own id to toolKind. It has no behavior: a
// contribution is a declaring fact about the node, not something a
// component has to run to produce.
type contNode struct{ Base }

// otherContNode contributes its own id to otherKind instead.
type otherContNode struct{ Base }

// dynNode contributes names at Build time: the case a declaration
// cannot express, because the names do not exist until the component
// has run (an MCP server's tool list, say).
type dynNode struct {
	Base
}

// dynNames is what the next dynNode built will provide. A package
// variable keeps the test tree free of config plumbing.
var dynNames []string

func (d *dynNode) Build(ctx *Scope) error {
	d.Base.Build(ctx)
	for _, name := range dynNames {
		if err := ctx.Provide[toolKind](name); err != nil {
			return err
		}
	}
	return nil
}

func regContributionTypes() {
	registerForTest("test.cont", func() Component { return &contNode{} },
		WithContributes[toolKind]())
	registerForTest("test.cont2", func() Component { return &otherContNode{} },
		WithContributes[otherKind]())
	registerForTest("test.dyn", func() Component { return &dynNode{} })
}

// A contribution is structure, not runtime: the tree knows which names
// it holds before anything is activated, so asking is free and does not
// depend on the order the tree happens to be built in. Every node here
// is lazy, so anything that activated one would show up as a component
// instance the query had no business creating.
func TestContributionsListsNamesInTreeOrder(t *testing.T) {
	regContributionTypes()
	root := &Node{
		Type: "loong.base", ID: "root",
		Children: []*Node{
			{Type: "test.cont", ID: "first", Lazy: true, Children: []*Node{
				{Type: "test.cont", ID: "nested", Lazy: true},
			}},
			{Type: "test.cont2", ID: "other", Lazy: true},
			{Type: "loong.base", ID: "plain", Lazy: true},
		},
	}
	k := New()
	defer func() { _ = k.Shutdown() }()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}

	if got, want := k.Contributions[toolKind](), []string{"first", "nested"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Contributions[toolKind] = %v, want %v", got, want)
	}
	// The kind is part of the declaration: otherKind's node is not a
	// tool, however tool-shaped it looks.
	if got, want := k.Contributions[otherKind](), []string{"other"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Contributions[otherKind] = %v, want %v", got, want)
	}
	// A kind nothing declares is an empty answer, not an error.
	if got := k.Contributions[*unusedSvc](); len(got) != 0 {
		t.Errorf("Contributions for an undeclared kind = %v, want none", got)
	}
	for _, id := range []string{"first", "nested", "other"} {
		if k.idIndex[id].component != nil {
			t.Errorf("Contributions activated node %q; the query must stay structural", id)
		}
	}
}

// The mounted tree is the declaration, so the public view has to carry
// it: a reader that has NodeInfo should see what a node offers without
// a second query per node.
func TestNodeInfoCarriesContributedNames(t *testing.T) {
	regContributionTypes()
	root := &Node{
		Type: "loong.base", ID: "root",
		Children: []*Node{
			{Type: "test.cont", ID: "a"},
			{Type: "loong.base", ID: "plain"},
		},
	}
	k := New()
	defer func() { _ = k.Shutdown() }()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}

	info, err := k.Node("a")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := info.Contributes, []string{"a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Contributes = %v, want %v (the name is the node's own id)", got, want)
	}
	plain, err := k.Node("plain")
	if err != nil {
		t.Fatal(err)
	}
	if len(plain.Contributes) != 0 {
		t.Errorf("plain Contributes = %v, want none: its type declares no kind", plain.Contributes)
	}
}

// Names that only exist at runtime join the same list, after the
// structural ones, in the order they were provided.
func TestProvidedNamesJoinTheContributions(t *testing.T) {
	regContributionTypes()
	dynNames = []string{"weather:lookup", "weather:forecast"}
	t.Cleanup(func() { dynNames = nil })

	root := &Node{
		Type: "loong.base", ID: "root",
		Children: []*Node{
			{Type: "test.cont", ID: "static"},
			{Type: "test.dyn", ID: "mcp"},
		},
	}
	k := New()
	defer func() { _ = k.Shutdown() }()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}

	want := []string{"static", "weather:lookup", "weather:forecast"}
	if got := k.Contributions[toolKind](); !reflect.DeepEqual(got, want) {
		t.Errorf("Contributions[toolKind] = %v, want %v", got, want)
	}
	info, err := k.Node("mcp")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := info.Contributes, []string{"weather:forecast", "weather:lookup"}; !reflect.DeepEqual(got, want) {
		t.Errorf("mcp Contributes = %v, want %v", got, want)
	}
}

// One name, one owner. Two nodes answering to the same name would make
// a reference ambiguous, which is the same mistake a duplicate node id
// is — and it has to fail where it happens, naming both sides.
func TestProvideRejectsAConflictingName(t *testing.T) {
	regContributionTypes()
	dynNames = []string{"read"}
	t.Cleanup(func() { dynNames = nil })

	root := &Node{
		Type: "loong.base", ID: "root",
		Children: []*Node{
			{Type: "test.cont", ID: "read"},
			{Type: "test.dyn", ID: "mcp"},
		},
	}
	k := New()
	defer func() { _ = k.Shutdown() }()
	err := k.Assemble(root)
	if err == nil {
		t.Fatal("assemble succeeded; two nodes now answer to \"read\"")
	}
	if !strings.Contains(err.Error(), `"read"`) || !strings.Contains(err.Error(), `"mcp"`) {
		t.Errorf("error = %v, want it to name the contribution and the node that provided it", err)
	}
}

// Re-providing a name the same node already contributes is a no-op: a
// component that re-affirms its own name is not an error, and the name
// still appears once.
func TestProvideIsIdempotentPerNode(t *testing.T) {
	regContributionTypes()
	dynNames = []string{"dup", "dup"}
	t.Cleanup(func() { dynNames = nil })

	root := &Node{
		Type:     "loong.base",
		ID:       "root",
		Children: []*Node{{Type: "test.dyn", ID: "mcp"}},
	}
	k := New()
	defer func() { _ = k.Shutdown() }()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	if got, want := k.Contributions[toolKind](), []string{"dup"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Contributions[toolKind] = %v, want %v", got, want)
	}
}

// A node may hold a name its own id already gives it: the structural
// declaration and the runtime one agree, and nothing is reported twice.
func TestProvideOfTheNodesOwnNameIsANoOp(t *testing.T) {
	regContributionTypes()
	registerForTest("test.contdyn", func() Component { return &dynNode{} },
		WithContributes[toolKind]())
	dynNames = []string{"self"}
	t.Cleanup(func() { dynNames = nil })

	root := &Node{
		Type:     "loong.base",
		ID:       "root",
		Children: []*Node{{Type: "test.contdyn", ID: "self"}},
	}
	k := New()
	defer func() { _ = k.Shutdown() }()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	if got, want := k.Contributions[toolKind](), []string{"self"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Contributions[toolKind] = %v, want %v", got, want)
	}
}
