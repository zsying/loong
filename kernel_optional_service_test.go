package loong

import (
	"strings"
	"testing"
)

// optSvc is the service an optional provider may or may not have.
type optSvc struct{ tag string }

// optNil declares *optSvc optionally and never has one: the shape of a
// capability that depends on something outside the tree (a configured
// backend, say) being there.
type optNil struct{ Base }

// optSome declares the same service optionally and always has one.
type optSome struct {
	Base
	v *optSvc
}

func (o *optSome) Build(ctx *Scope) error {
	o.v = &optSvc{tag: ctx.Node.ID}
	return nil
}

// optUser consumes *optSvc and records how it went instead of failing,
// which is what a consumer of an optional service has to do.
type optUser struct {
	Base
	got *optSvc
	err error
}

func (u *optUser) Build(ctx *Scope) error {
	u.got, u.err = ctx.TryGet[*optSvc]()
	return nil
}

func regOptionalTypes() {
	registerForTest("test.optnil", func() Component { return &optNil{} },
		WithOptionalService(func(Component) *optSvc { return nil }))
	registerForTest("test.optsome", func() Component { return &optSome{} },
		WithOptionalService(func(c Component) *optSvc { return c.(*optSome).v }))
	registerForTest("test.optuser", func() Component { return &optUser{} })
}

// An optional service that turns out empty must not fail the build. A
// capability the tree offers only sometimes is a case, not a mistake —
// making it an error is how "no backend configured" grows a sentinel
// struct and an error field that every consumer has to unpack.
func TestOptionalServiceAbsenceIsNotAFailure(t *testing.T) {
	regOptionalTypes()
	root := &Node{
		Type: "loong.base", ID: "root",
		Children: []*Node{
			{Type: "test.optnil", ID: "none"},
			{Type: "test.optuser", ID: "biz"},
		},
	}
	k := New()
	defer func() { _ = k.Shutdown() }()
	if err := k.Assemble(root); err != nil {
		t.Fatalf("assemble: %v", err)
	}

	biz := k.idIndex["biz"].component.(*optUser)
	if biz.got != nil {
		t.Errorf("biz resolved %+v, want nothing", biz.got)
	}
	if biz.err == nil {
		t.Error("biz got no error either; the absence was reported as a value")
	}
}

// A declaration that served nothing is not a provider: the tree still
// shows who *could* have served it, and a lookup has to answer with the
// same words it uses for a tree that mounted nobody.
func TestOptionalServiceThatServedNothingIsNotAProvider(t *testing.T) {
	regOptionalTypes()
	root := &Node{
		Type: "loong.base", ID: "root",
		Children: []*Node{
			{Type: "test.optnil", ID: "none"},
			{Type: "test.optuser", ID: "biz"},
		},
	}
	k := New()
	defer func() { _ = k.Shutdown() }()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}

	if got := k.Providers[*optSvc](); len(got) != 0 {
		t.Errorf("Providers = %v, want none: the only declaring node served nothing", got)
	}
	biz := k.idIndex["biz"].component.(*optUser)
	if biz.err == nil || !strings.Contains(biz.err.Error(), "no node provides") {
		t.Errorf("biz error = %v, want the \"no node provides\" wording", biz.err)
	}
	// The declaring types are still named, so the message says what
	// could have been mounted.
	if biz.err != nil && !strings.Contains(biz.err.Error(), "test.optsome") {
		t.Errorf("biz error = %v, want it to name the types that could provide it", biz.err)
	}
}

// With one candidate serving nothing and another serving, the empty one
// must not shadow the real provider — whoever is nearest.
func TestOptionalServiceIsSkippedAmongOtherProviders(t *testing.T) {
	regOptionalTypes()
	root := &Node{
		Type: "loong.base", ID: "root",
		Children: []*Node{
			{Type: "test.optnil", ID: "none"},
			{Type: "test.optsome", ID: "some"},
			{Type: "test.optuser", ID: "biz"},
		},
	}
	k := New()
	defer func() { _ = k.Shutdown() }()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}

	if got, want := k.Providers[*optSvc](), []string{"some"}; len(got) != len(want) || got[0] != want[0] {
		t.Errorf("Providers = %v, want %v", got, want)
	}
	biz := k.idIndex["biz"].component.(*optUser)
	if biz.got == nil || biz.got.tag != "some" {
		t.Errorf("biz resolved %+v, want the node that has one", biz.got)
	}
}

// A nil accessor on a *required* service still fails the activation: the
// check is what tells a component that returned too early, and the
// optional variant is an opt-in, not a relaxation of every service.
func TestRequiredServiceStillFailsOnNil(t *testing.T) {
	registerForTest("test.reqnil", func() Component { return &optNil{} },
		WithService(func(Component) *optSvc { return nil }))
	root := &Node{
		Type:     "loong.base",
		ID:       "root",
		Children: []*Node{{Type: "test.reqnil", ID: "broken"}},
	}
	k := New()
	defer func() { _ = k.Shutdown() }()
	err := k.Assemble(root)
	if err == nil {
		t.Fatal("assemble succeeded with a nil required service")
	}
	if !strings.Contains(err.Error(), "nil service") {
		t.Errorf("error = %v, want the nil-service wording", err)
	}
}
