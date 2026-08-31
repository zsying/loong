package loong

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"gopkg.in/yaml.v3"
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

func TestParse(t *testing.T) {
	root, err := Parse([]byte("type: test.spy\nid: root\nchildren:\n  - type: test.spy\n    id: c\n    lazy: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if root.Type != "test.spy" || root.ID != "root" {
		t.Fatalf("root = %+v", root)
	}
	if len(root.Children) != 1 || !root.Children[0].Lazy || root.Children[0].ID != "c" {
		t.Fatalf("child = %+v", root.Children[0])
	}
	if _, err := Parse([]byte("type: [broken")); err == nil {
		t.Fatal("expected parse error for invalid yaml")
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

// scopeAt builds a Scope rooted at the node with the given id, the way
// a component's Build would see it. It lets tests drive Scope.Get /
// Scope.GetFrom against an assembled kernel.
func scopeAt(k *Kernel, id string) *Scope {
	n := k.idIndex[id]
	if n == nil {
		panic("unknown node " + id)
	}
	return &Scope{Kernel: k, Node: n}
}

// svcValue is a service value exposed by svcProvider.
type svcValue struct{ n int }

// svcProvider is a lazy service component: it declares *svcValue via
// WithService and is activated on first lookup.
type svcProvider struct {
	Base
	v *svcValue
}

func (s *svcProvider) Build(ctx *Scope) error {
	s.v = &svcValue{n: 42}
	return nil
}

func TestServiceLazyActivation(t *testing.T) {
	RegisterComponent("test.svc", func() Component { return &svcProvider{} },
		WithService(func(c Component) *svcValue { return c.(*svcProvider).v }))
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
	if v := scopeAt(k, "users").Get[*svcValue](); v == nil || v.n != 42 {
		t.Fatalf("Get[*svcValue]() = %+v, want &{n:42}", v)
	}
	if n := k.idIndex["users"]; n.component == nil {
		t.Fatal("service node should be activated by Get")
	}
	// Subsequent gets hit the cache; no double activation.
	s := scopeAt(k, "users")
	v1 := s.Get[*svcValue]()
	v2 := s.Get[*svcValue]()
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
	u.got = ctx.Get[*svcValue]()
	return nil
}

func TestServiceActivatedDuringParentBuild(t *testing.T) {
	RegisterComponent("test.svc", func() Component { return &svcProvider{} },
		WithService(func(c Component) *svcValue { return c.(*svcProvider).v }))
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

// nilSvc is the declared service type of nilProvider only, so it does
// not collide with other tests' registrations.
type nilSvc struct{}

// nilProvider declares a service whose accessor returns nil — the
// accessor ran before the component was ready, so activation must fail.
type nilProvider struct {
	Base
}

func (n *nilProvider) Build(*Scope) error { return nil }

func TestNilService(t *testing.T) {
	RegisterComponent("test.nil", func() Component { return &nilProvider{} },
		WithService(func(c Component) *nilSvc { return nil }))
	RegisterComponent("test.spy", func() Component { return &spy{r: &recorder{}} })
	root := &Node{
		Type: "test.spy", ID: "root",
		Children: []*Node{{Type: "test.nil", ID: "n"}},
	}
	k := New()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	if _, err := scopeAt(k, "root").TryGet[*nilSvc](); err == nil {
		t.Fatal("expected error when a service accessor returns nil")
	}
}

// TestLoadTreeLazyYAML verifies the full yaml -> register -> activate
// path for a `lazy: true` node: it parses, stays inactive after
// assembly, and comes alive via Activate.
func TestLoadTreeLazyYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "loong.yaml")
	content := `type: test.spy
id: root
children:
  - type: test.spy
    id: eager
  - type: test.spy
    id: opt
    lazy: true
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := LoadTree(path)
	if err != nil {
		t.Fatal(err)
	}
	if !root.Children[1].Lazy {
		t.Fatal("lazy: true was not parsed from yaml")
	}

	var r recorder
	RegisterComponent("test.spy", func() Component { return &spy{r: &r} })
	k := New()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	if n := k.idIndex["opt"]; n.component != nil {
		t.Fatal("lazy node should stay inactive after assembly")
	}
	if err := k.Activate("opt"); err != nil {
		t.Fatal(err)
	}
	if n := k.idIndex["opt"]; n.component == nil {
		t.Fatal("lazy node should be activated by Activate")
	}
	if err := k.Shutdown(); err != nil {
		t.Fatal(err)
	}
}

// unknownSvc is never registered; lookups must return zero/error.
type unknownSvc struct{}

func TestGetUnknownService(t *testing.T) {
	k := New()
	s := &Scope{Kernel: k} // no node context
	if v := s.Get[*unknownSvc](); v != nil {
		t.Fatalf("Get[*unknownSvc]() = %v, want nil", v)
	}
	if _, err := s.TryGet[*unknownSvc](); err == nil {
		t.Fatal("TryGet for unknown service should return an error")
	}
}

// TestServiceConcurrentActivation races the first Get from many
// goroutines: the per-node activation mutex must yield exactly one
// instance that all callers share.
func TestServiceConcurrentActivation(t *testing.T) {
	RegisterComponent("test.svc", func() Component { return &svcProvider{} },
		WithService(func(c Component) *svcValue { return c.(*svcProvider).v }))
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
			vals[i] = scopeAt(k, "users").Get[*svcValue]()
		}(i)
	}
	wg.Wait()
	for i, v := range vals {
		if v == nil || v != vals[0] {
			t.Fatalf("vals[%d] = %v, want the same singleton %v", i, v, vals[0])
		}
	}
}

// routerVal is a per-instance service value; its tag identifies which
// provider instance produced it.
type routerVal struct{ tag string }

// routerProv models a startup channel like web: it declares a service
// and is registered with Eager() so it activates during assembly.
type routerProv struct {
	Base
	v *routerVal
}

func (r *routerProv) Build(ctx *Scope) error {
	r.v = &routerVal{tag: ctx.Node.ID}
	return nil
}

// routerUser models a business component mounted under a provider; its
// Build resolves the service and records which provider instance won.
type routerUser struct {
	Base
	got *routerVal
}

func (u *routerUser) Build(ctx *Scope) error {
	u.got = ctx.Get[*routerVal]()
	return nil
}

func regWebPair() {
	RegisterComponent("test.web", func() Component { return &routerProv{} },
		WithService(func(c Component) *routerVal { return c.(*routerProv).v }),
		Eager())
	RegisterComponent("test.ruser", func() Component { return &routerUser{} })
}

// TestMultiInstanceTreePriority mounts two provider instances (main /
// admin), each with a business child. Each child must resolve the
// service of its own parent chain — the tree-scoped lookup.
func TestMultiInstanceTreePriority(t *testing.T) {
	regWebPair()
	root := &Node{
		Type: "test.spy", ID: "root",
		Children: []*Node{
			{Type: "test.web", ID: "main", Children: []*Node{
				{Type: "test.ruser", ID: "bizA"},
			}},
			{Type: "test.web", ID: "admin", Children: []*Node{
				{Type: "test.ruser", ID: "bizB"},
			}},
		},
	}
	k := New()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	bizA := k.idIndex["bizA"].component.(*routerUser)
	bizB := k.idIndex["bizB"].component.(*routerUser)
	if bizA.got == nil || bizA.got.tag != "main" {
		t.Fatalf("bizA resolved %+v, want the main provider", bizA.got)
	}
	if bizB.got == nil || bizB.got.tag != "admin" {
		t.Fatalf("bizB resolved %+v, want the admin provider", bizB.got)
	}
}

// TestAmbiguousService mounts two providers with no business child on
// either chain: a lookup from the root has no matching ancestor and
// must report an error suggesting GetFrom.
func TestAmbiguousService(t *testing.T) {
	regWebPair()
	root := &Node{
		Type: "test.spy", ID: "root",
		Children: []*Node{
			{Type: "test.web", ID: "a"},
			{Type: "test.web", ID: "b"},
		},
	}
	k := New()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	if _, err := scopeAt(k, "root").TryGet[*routerVal](); err == nil {
		t.Fatal("expected ambiguity error from the root lookup")
	}
}

// TestGetFrom picks a specific provider instance by node id, even when
// the caller's chain contains another provider.
func TestGetFrom(t *testing.T) {
	regWebPair()
	root := &Node{
		Type: "test.spy", ID: "root",
		Children: []*Node{
			{Type: "test.web", ID: "main", Children: []*Node{
				{Type: "test.ruser", ID: "bizA"},
			}},
			{Type: "test.web", ID: "admin"},
		},
	}
	k := New()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	v := scopeAt(k, "bizA").GetFrom[*routerVal]("admin")
	if v == nil || v.tag != "admin" {
		t.Fatalf("GetFrom(admin) = %+v, want the admin provider", v)
	}
	if _, err := scopeAt(k, "bizA").TryGetFrom[*routerVal]("missing"); err == nil {
		t.Fatal("expected error for unknown node id")
	}
}

// multiProv exposes two services from one component type.
type svcX struct{ n int }
type svcY struct{ s string }

type multiProv struct {
	Base
	x *svcX
	y *svcY
}

func (m *multiProv) Build(*Scope) error {
	m.x = &svcX{n: 1}
	m.y = &svcY{s: "y"}
	return nil
}

func TestMultipleServices(t *testing.T) {
	RegisterComponent("test.multi", func() Component { return &multiProv{} },
		WithService(func(c Component) *svcX { return c.(*multiProv).x }),
		WithService(func(c Component) *svcY { return c.(*multiProv).y }))
	RegisterComponent("test.spy", func() Component { return &spy{r: &recorder{}} })
	root := &Node{
		Type: "test.spy", ID: "root",
		Children: []*Node{{Type: "test.multi", ID: "m"}},
	}
	k := New()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	s := scopeAt(k, "root")
	vx := s.Get[*svcX]()
	vy := s.Get[*svcY]()
	if vx == nil || vx.n != 1 || vy == nil || vy.s != "y" {
		t.Fatalf("multiple services not both resolved: x=%+v y=%+v", vx, vy)
	}
}

// catSvc / catProv are used only by TestComponentsCatalog, so their
// service declaration does not collide with other tests.
type catSvc struct{}

type catProv struct {
	Base
}

func (c *catProv) Build(*Scope) error { return nil }

type catConfig struct {
	Mode string `yaml:"mode"`
	Size int    `yaml:"size,omitempty"`
}

// TestComponentsCatalog verifies the discovery metadata exposes type
// names, service flags, eager mode, events, descriptions and config
// keys.
func TestComponentsCatalog(t *testing.T) {
	RegisterComponent("test.cat", func() Component { return &catProv{} },
		WithConfig[catConfig](),
		WithService(func(c Component) *catSvc { return &catSvc{} }),
		WithEvents("test.one", "test.*"),
		WithDesc("a catalog entry"))
	infos := Components()
	var found *ComponentMeta
	for i := range infos {
		if infos[i].Type == "test.cat" {
			found = &infos[i]
			break
		}
	}
	if found == nil {
		t.Fatal("test.cat not listed in Components()")
	}
	if !found.Service || found.Eager {
		t.Errorf("meta = %+v, want Service=true Eager=false", found)
	}
	if len(found.Emits) != 2 || found.Emits[1] != "test.*" {
		t.Errorf("emits = %v, want [test.one test.*]", found.Emits)
	}
	if found.Desc != "a catalog entry" {
		t.Errorf("desc = %q", found.Desc)
	}
	if found.ConfigType != "loong.catConfig" {
		t.Errorf("config type = %q, want loong.catConfig", found.ConfigType)
	}
	wantFields := []ConfigField{
		{Name: "mode", Type: "string", Optional: false},
		{Name: "size", Type: "int", Optional: true},
	}
	if !reflect.DeepEqual(found.ConfigFields, wantFields) {
		t.Errorf("config fields = %+v, want %+v", found.ConfigFields, wantFields)
	}
}

// testCfg drives TestScopeConfig: decoding, strict unknown-field
// checking, and the empty-config zero value.
type testCfg struct {
	Name string `yaml:"name"`
	Port int    `yaml:"port,omitempty"`
}

func parseConfig(t *testing.T, yamlText string) yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(yamlText), &doc); err != nil {
		t.Fatal(err)
	}
	// doc is a DocumentNode; the mapping lives in Content[0], the same
	// shape LoadTree stores into Node.Config.
	return *doc.Content[0]
}

func TestScopeConfig(t *testing.T) {
	// Decoding a well-formed block.
	s := &Scope{Kernel: New(), Node: &Node{ID: "x"}, Raw: parseConfig(t, "name: hello\nport: 8080\n")}
	cfg, err := s.Config[testCfg]()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cfg.Name != "hello" || cfg.Port != 8080 {
		t.Fatalf("cfg = %+v, want name=hello port=8080", cfg)
	}

	// Unknown fields are rejected (strict mode).
	bad := &Scope{Kernel: New(), Node: &Node{ID: "y"}, Raw: parseConfig(t, "name: hi\ntypo_field: 1\n")}
	if _, err := bad.Config[testCfg](); err == nil || !strings.Contains(err.Error(), "typo_field") {
		t.Fatalf("expected unknown-field error mentioning typo_field, got %v", err)
	}

	// An absent config block yields the zero value.
	empty := &Scope{Kernel: New(), Node: &Node{ID: "z"}}
	z, err := empty.Config[testCfg]()
	if err != nil || z != (testCfg{}) {
		t.Fatalf("empty config = %+v, err %v, want zero value", z, err)
	}
}
