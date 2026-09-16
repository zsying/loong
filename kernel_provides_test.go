package loong

import "testing"

// A consumer that validates its own children asks about one node at a
// time — "may this child answer a lookup of T?" — and until now the only
// way to ask was to enumerate the whole tree with Providers[T] and test
// membership. Provides[T] is that question, and these tests pin the two
// things that make it a projection of Providers rather than a second
// opinion: it is answered by the same predicate (an id is listed by
// Providers exactly when Provides agrees), and it reads the same
// skeleton (asking activates nothing).
//
// The concrete consumer is owlet's tool container, which refuses a leaf
// under owlet.tools that serves no tools. It asks per child, which is
// what the question is, instead of reading the whole tree to answer it.

// ptCap is the capability: an interface, so the declaration is keyed by
// the interface rather than by what the accessor returns.
type ptCap interface{ Tag() string }

// ptImpl declares ptCap.
type ptImpl struct{ Base }

func (c *ptImpl) Tag() string { return "impl" }

// ptNothing declares nothing: the shape the answer has to tell apart
// from a provider, and the shape an unknown id has to look like.
type ptNothing struct{ Base }

// ptOptNil declares ptCap optionally and never has one — a declaration
// that turns out to serve nothing.
type ptOptNil struct{ Base }

func registerProvidesTypes() {
	registerForTest("test.pt.impl", func() Component { return &ptImpl{} },
		WithService(func(c Component) ptCap { return c.(*ptImpl) }),
		WithDesc("declares the capability"))
	registerForTest("test.pt.nothing", func() Component { return &ptNothing{} })
	registerForTest("test.pt.optnil", func() Component { return &ptOptNil{} },
		WithOptionalService(func(Component) ptCap { return nil }))
}

// TestProvidesAnswersAboutOneNodeFromTheSkeleton pins the answer for the
// three ids a caller can hand it — a provider, a node that declares
// nothing, and an id that names no node — and pins that asking is free
// of side effects, because the caller is an assembly-time check that
// must not bring up the node it is inspecting.
func TestProvidesAnswersAboutOneNodeFromTheSkeleton(t *testing.T) {
	registerProvidesTypes()

	root, err := Parse([]byte(`
type: test.pt.nothing
id: root
children:
  - type: test.pt.impl
    id: a
    lazy: true
  - type: test.pt.nothing
    id: quiet
`))
	if err != nil {
		t.Fatalf("parse tree: %v", err)
	}
	k := New()
	defer func() { _ = k.Shutdown() }()
	if err := k.Assemble(root); err != nil {
		t.Fatalf("assemble: %v", err)
	}

	if !k.Provides[ptCap]("a") {
		t.Error("Provides[ptCap](\"a\") = false for the node whose type declares it: the answer comes from the skeleton, not from what has built")
	}
	// The tree declared this node lazy, and the question must not undo
	// that: a per-node question that activates is the side effect the
	// whole-tree read was avoiding too.
	if _, err := k.Component("a"); err == nil {
		t.Error("asking about node a activated it: Provides must not build the node it is asked about")
	}

	if k.Provides[ptCap]("quiet") {
		t.Error("Provides[ptCap](\"quiet\") = true for a node whose component type declares no service")
	}
	// An id that names nothing cannot answer a lookup, so it is not a
	// provider — the same answer as for a node that declares nothing,
	// which is what makes the predicate total.
	if k.Provides[ptCap]("nosuchnode") {
		t.Error("Provides[ptCap](\"nosuchnode\") = true for an id that names no node")
	}
	// The declaration is made with T, not with the accessor's concrete
	// type, so the same node is not a provider of the implementation.
	if k.Provides[*ptImpl]("a") {
		t.Error("Provides[*ptImpl](\"a\") = true: the declaration is keyed by the interface, not by the value's concrete type")
	}

	// The two answers cannot disagree: Providers is the whole-tree
	// comprehension of this predicate, which is the reason a consumer may
	// use either one.
	ids := k.Providers[ptCap]()
	if len(ids) != 1 || ids[0] != "a" {
		t.Fatalf("Providers[ptCap]() = %v, want [a]", ids)
	}
	if !k.Provides[ptCap](ids[0]) {
		t.Errorf("Providers[ptCap]() lists %q while Provides[ptCap](%q) = false", ids[0], ids[0])
	}
}

// TestProvidesFollowsAnOptionalDeclarationThatServedNothing pins the
// half of the answer that is not structural: a node that declared T and
// turned out to serve nothing stops being a provider, while the tree
// view keeps showing the declaration. That gap is why a consumer must
// ask the kernel instead of reading NodeInfo.Services for itself.
func TestProvidesFollowsAnOptionalDeclarationThatServedNothing(t *testing.T) {
	registerProvidesTypes()

	root, err := Parse([]byte(`
type: test.pt.nothing
id: root
children:
  - type: test.pt.optnil
    id: maybe
    lazy: true
`))
	if err != nil {
		t.Fatalf("parse tree: %v", err)
	}
	k := New()
	defer func() { _ = k.Shutdown() }()
	if err := k.Assemble(root); err != nil {
		t.Fatalf("assemble: %v", err)
	}

	// Unbuilt: the declaration stands, which is all the skeleton knows.
	if !k.Provides[ptCap]("maybe") {
		t.Error("Provides[ptCap](\"maybe\") = false before the node built: a declaration that has not been evaluated yet is a candidate")
	}

	if err := k.Activate("maybe"); err != nil {
		t.Fatalf("activate maybe: %v", err)
	}

	if k.Provides[ptCap]("maybe") {
		t.Error("Provides[ptCap](\"maybe\") = true after the optional accessor returned nil: a node that serves nothing must not answer a lookup of T")
	}
	info, err := k.Node("maybe")
	if err != nil {
		t.Fatalf("node maybe: %v", err)
	}
	if len(info.Services) != 1 || info.Services[0] != "loong.ptCap" {
		t.Errorf("node maybe declares services %v, want [loong.ptCap]: the tree view reports the declaration, which is what makes it the wrong source for \"can this node serve T\"", info.Services)
	}
}
