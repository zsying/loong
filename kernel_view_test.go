package loong

import (
	"reflect"
	"strings"
	"testing"
)

// unusedSvc is declared by no component type.
type unusedSvc struct{}

// regTwoWebTypes registers a second component type that declares the
// same service type as regWebPair's test.web. Nothing in loong says a
// service type belongs to exactly one component type — providers are
// resolved per node — so a lookup that has to pick an inactive
// provider must choose among all of them, not among the ones that
// happen to share a declaring type.
func regTwoWebTypes() {
	regWebPair()
	registerForTest("test.web2", func() Component { return &routerProv{} },
		WithService(func(c Component) *routerVal { return c.(*routerProv).v }))
}

// Providers is the structural counterpart of GetFrom: it answers which
// node ids can serve a *routerVal, in tree declaration order, so a
// caller can address one instance without reading the config yaml.
//
// It describes the tree, not the runtime: lazy nodes are listed before
// anything activated them, and asking must not activate anything.
func TestProvidersListsDeclaringNodesInTreeOrder(t *testing.T) {
	regTwoWebTypes()
	root := &Node{
		Type: "base", ID: "root",
		Children: []*Node{
			{Type: "test.web2", ID: "second-type", Lazy: true},
			{Type: "test.web", ID: "first-type", Lazy: true, Children: []*Node{
				{Type: "test.web", ID: "nested", Lazy: true},
			}},
		},
	}
	k := New()
	defer func() { _ = k.Shutdown() }()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}

	got := k.Providers[*routerVal]()
	want := []string{"second-type", "first-type", "nested"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Providers = %v, want %v (declaration order, every declaring type)", got, want)
	}
	for _, id := range want {
		if k.idIndex[id].component != nil {
			t.Errorf("Providers activated node %q; the query must stay structural", id)
		}
	}

	// A service type no component declares has no providers — an empty
	// answer, not an error.
	if got := k.Providers[*unusedSvc](); len(got) != 0 {
		t.Errorf("Providers for an undeclared service = %v, want none", got)
	}
}

// Only the second declaring type has a node in the tree, and that node
// is lazy. Whichever declaring type the kernel looks at first, the
// lookup has to find it: the provider is there, and guessing a type is
// not a reason to miss it.
func TestLazyLookupFindsTheProviderWhateverDeclaresIt(t *testing.T) {
	regTwoWebTypes()
	root := &Node{
		Type:     "base",
		ID:       "root",
		Children: []*Node{{Type: "test.web2", ID: "only", Lazy: true}},
	}
	k := New()
	defer func() { _ = k.Shutdown() }()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}

	v, err := scopeAt(k, "root").TryGet[*routerVal]()
	if err != nil {
		t.Fatalf("TryGet: %v", err)
	}
	if v.tag != "only" {
		t.Errorf("provider tag = %q, want %q", v.tag, "only")
	}
}

// Two declaring types and two lazy nodes, one mounted inside the other:
// bringing up the outer node brings up the consumer under it, and the
// lookup that consumer makes resolves to the declaring node it is mounted
// under — not to the one that happens to come first in the tree.
//
// The assertion moved when `lazy` came to mean a subtree: the consumer is
// no longer built at startup while both providers are inactive, it is
// built by the activation of the node above it. The property is the same
// — tree order must not decide — so the check that the other provider is
// still dormant is what carries it now.
func TestLazyLookupPrefersTheNearestAncestorAcrossDeclaringTypes(t *testing.T) {
	regTwoWebTypes()
	root := &Node{
		Type: "base", ID: "root",
		Children: []*Node{
			{Type: "test.web2", ID: "other", Lazy: true},
			{Type: "test.web", ID: "mine", Lazy: true, Children: []*Node{
				{Type: "test.ruser", ID: "biz"},
			}},
		},
	}
	k := New()
	defer func() { _ = k.Shutdown() }()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}

	// biz resolves during its own Build, while both providers are still
	// inactive; the activation of its parent is what builds it.
	if err := k.Activate("mine"); err != nil {
		t.Fatal(err)
	}
	biz := k.idIndex["biz"].component.(*routerUser)
	if biz.got == nil || biz.got.tag != "mine" {
		t.Errorf("biz resolved %+v, want the provider it is mounted under", biz.got)
	}
	if k.idIndex["other"].actDone {
		t.Error("the first lazy provider was activated: tree order decided the lookup")
	}
}

// NodeInfo is the public view of a mounted node. A reader that has
// only it must not have to reach back into the internal *Node for the
// node's description, nor lose track of where the node sits.
func TestNodeInfoCarriesDescAndParent(t *testing.T) {
	root := &Node{
		Type: "base", ID: "root", Desc: "the root",
		Children: []*Node{
			{Type: "base", ID: "child", Desc: "a child", Children: []*Node{
				{Type: "base", ID: "grand"},
			}},
		},
	}
	k := New()
	defer func() { _ = k.Shutdown() }()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}

	info, err := k.Root()
	if err != nil {
		t.Fatal(err)
	}
	if info.Desc != "the root" {
		t.Errorf("root Desc = %q, want %q", info.Desc, "the root")
	}
	if info.ParentID != "" {
		t.Errorf("root ParentID = %q, want empty", info.ParentID)
	}

	child := info.Children[0]
	if child.Desc != "a child" || child.ParentID != "root" {
		t.Errorf("child = %+v, want desc %q with parent %q", child, "a child", "root")
	}
	grand := child.Children[0]
	if grand.ParentID != "child" {
		t.Errorf("grand ParentID = %q, want %q", grand.ParentID, "child")
	}
	// Desc is the node's own; the type-level fallback stays with the
	// reader (Describe), so an undescribed node reads as empty rather
	// than as a description the kernel silently invented.
	if grand.Desc != "" {
		t.Errorf("grand Desc = %q, want empty", grand.Desc)
	}
	if Describe(grand.Type) == "" {
		t.Error("Describe(base) is empty; the fallback readers rely on is gone")
	}

	one, err := k.Node("child")
	if err != nil {
		t.Fatal(err)
	}
	if one.Desc != "a child" || one.ParentID != "root" {
		t.Errorf("Node(child) = %+v, want the metadata Root reported for it", one)
	}
}

// A lookup that finds nothing has to say which nothing it means: no
// component type declares the service at all, or the types that do have
// no node in this tree.
func TestProviderLookupErrorsNameWhatIsMissing(t *testing.T) {
	regTwoWebTypes()
	root := &Node{Type: "base", ID: "root"}
	k := New()
	defer func() { _ = k.Shutdown() }()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	s := scopeAt(k, "root")

	_, err := s.TryGet[*routerVal]()
	if err == nil {
		t.Fatal("TryGet found a provider in a tree that mounts none")
	}
	if !strings.Contains(err.Error(), "test.web") {
		t.Errorf("error = %v, want it to name the component types that could provide it", err)
	}

	_, err = s.TryGet[*unusedSvc]()
	if err == nil {
		t.Fatal("TryGet found a service nothing declares")
	}
	if !strings.Contains(err.Error(), "no service of type") {
		t.Errorf("error = %v, want the undeclared-service wording", err)
	}
}

// Providers answers "which nodes can serve T", but nothing answered
// "what does this node serve" — a reader of the mounted tree had to
// look the node's type up in Components() to find out. The view carries
// the declaration itself, still structurally: it says what the type
// declares, and asking activates nothing.
func TestNodeInfoCarriesDeclaredServices(t *testing.T) {
	regWebPair()
	root := &Node{
		Type: "base", ID: "root",
		Children: []*Node{
			{Type: "test.web", ID: "web", Lazy: true},
			{Type: "test.ruser", ID: "biz", Lazy: true},
		},
	}
	k := New()
	defer func() { _ = k.Shutdown() }()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}

	info, err := k.Node("web")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"*loong.routerVal"}; !reflect.DeepEqual(info.Services, want) {
		t.Errorf("web Services = %v, want %v", info.Services, want)
	}
	biz, err := k.Node("biz")
	if err != nil {
		t.Fatal(err)
	}
	if len(biz.Services) != 0 {
		t.Errorf("biz Services = %v, want none", biz.Services)
	}
	if k.idIndex["web"].component != nil {
		t.Error("Node(id) activated the node; the view must stay structural")
	}
}

// Consumers is the other half of the dependency picture: Providers says
// who can serve T, Consumers says who went and got it. It is a trace of
// what happened, not a declaration: only a resolution that returned a
// value is recorded, and a node that never ran never asked.
func TestConsumersRecordsWhoResolvedAService(t *testing.T) {
	regWebPair()
	root := &Node{
		Type: "base", ID: "root",
		Children: []*Node{
			{Type: "test.web", ID: "web", Children: []*Node{
				{Type: "test.ruser", ID: "biz"},
			}},
			{Type: "test.ruser", ID: "idle", Lazy: true},
		},
	}
	k := New()
	defer func() { _ = k.Shutdown() }()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}

	if got, want := k.Consumers[*routerVal](), []string{"biz"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Consumers = %v, want %v", got, want)
	}
	// A lookup that found nothing is not a dependency: recording it
	// would list nodes that never got the service.
	s := scopeAt(k, "web")
	if _, err := s.TryGet[*unusedSvc](); err == nil {
		t.Fatal("TryGet found *unusedSvc")
	}
	if got := k.Consumers[*unusedSvc](); len(got) != 0 {
		t.Errorf("Consumers recorded a lookup that failed: %v", got)
	}
}
