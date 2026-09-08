package loong

import (
	"reflect"
	"strings"
	"testing"
)

func newTestComponent() Component { return &Base{} }

// TestRegisterComponentDuplicatePanics pins the registration contract:
// registering two factories under the same type name must panic at
// init() time (mirroring database/sql.Register and flag), because a
// silent overwrite would make the config tree resolve to whichever
// init() happened to run last.
func TestRegisterComponentDuplicatePanics(t *testing.T) {
	const name = "loong.test.dupe"
	RegisterComponent(name, newTestComponent)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on duplicate component type registration, got none")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, name) {
			t.Errorf("panic should mention the duplicate type name %q, got: %v", name, r)
		}
	}()
	RegisterComponent(name, newTestComponent)
}

// TestRegisterComponentDistinctNamesOK proves the duplicate guard does
// not over-fire: different type names may freely register factories,
// even when they declare the same service type (the lookup resolves
// per node, and ambiguous lazy lookups report an error).
func TestRegisterComponentDistinctNamesOK(t *testing.T) {
	RegisterComponent("loong.test.svc.a", newTestComponent,
		WithService(func(c Component) *testService { return &testService{} }))
	RegisterComponent("loong.test.svc.b", newTestComponent,
		WithService(func(c Component) *testService { return &testService{} }))

	meta := map[string]bool{}
	for _, m := range Components() {
		meta[m.Type] = true
	}
	for _, want := range []string{"loong.test.svc.a", "loong.test.svc.b"} {
		if !meta[want] {
			t.Errorf("component type %q not registered", want)
		}
	}
}

type testService struct{ _ int }

var _ = reflect.TypeOf(&testService{})

// registerForTest re-registers a component type for tests, overwriting
// any previous entry. Production RegisterComponent panics on duplicate
// type names; tests legitimately rebind factory closures per test, so
// they bypass the guard with this helper.
func registerForTest(name string, fn func() Component, opts ...ComponentOption) {
	e := regEntry{factory: fn}
	for _, o := range opts {
		o(&e)
	}
	for _, sd := range e.services {
		serviceOwners[sd.typ] = name
	}
	factories[name] = e
}
