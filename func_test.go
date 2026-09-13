package loong

import (
	"errors"
	"testing"
)

// Func must satisfy the Component interface.
var _ Component = Func(func(*Scope) error { return nil })

func TestFuncLifecycle(t *testing.T) {
	var ran bool
	f := Func(func(*Scope) error {
		ran = true
		return nil
	})
	if err := f.Build(&Scope{}); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := f.Run(&Scope{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !ran {
		t.Error("Run did not invoke the function")
	}
	if err := f.Stop(&Scope{}); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestFuncRunErrorPropagates(t *testing.T) {
	want := errors.New("boom")
	f := Func(func(*Scope) error { return want })
	if err := f.Run(&Scope{}); !errors.Is(err, want) {
		t.Fatalf("Run error = %v, want %v", err, want)
	}
}

func TestRegisterFuncRegistersType(t *testing.T) {
	RegisterFunc("loong.test.funcdescribe", func(*Scope) error { return nil },
		WithDesc("desc probe"))
	if got := Describe("loong.test.funcdescribe"); got != "desc probe" {
		t.Fatalf("Describe = %q, want %q", got, "desc probe")
	}
}

// TestFuncRunsViaKernel proves the kernel instantiates a RegisterFunc
// type and executes its Run: Assemble activates the root, which runs the
// whole active subtree including the Func child.
func TestFuncRunsViaKernel(t *testing.T) {
	ran := false
	RegisterFunc("loong.test.funcrun", func(*Scope) error {
		ran = true
		return nil
	}, WithDesc("kernel run probe"))
	k := New()
	root, err := Parse([]byte("type: base\nchildren:\n  - type: loong.test.funcrun\n    id: f\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := k.Assemble(root); err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if !ran {
		t.Fatal("registered Func did not run via kernel")
	}
}

// TestFuncRunErrorSurfacesViaKernel proves an error from Run propagates
// out of Assemble rather than being swallowed.
func TestFuncRunErrorSurfacesViaKernel(t *testing.T) {
	want := errors.New("boom")
	RegisterFunc("loong.test.funcerr", func(*Scope) error { return want })
	k := New()
	root, err := Parse([]byte("type: base\nchildren:\n  - type: loong.test.funcerr\n    id: f\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := k.Assemble(root); err == nil {
		t.Fatal("Assemble returned nil, want error from Func.Run")
	}
}
