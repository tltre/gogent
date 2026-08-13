package provider

import (
	"errors"
	"testing"
)

func TestRegisterAndCreateEngine(t *testing.T) {
	// Engine names must be unique across the test run; use a dedicated name.
	const engine = "test-engine-register"
	unregisterTestEngine(t, engine)

	if err := RegisterEngine(engine, func() (IProvider, error) {
		return &mockProvider{modelInfo: ModelInfo{Provider: engine}}, nil
	}); err != nil {
		t.Fatalf("RegisterEngine() = %v", err)
	}
	t.Cleanup(func() { unregisterTestEngine(t, engine) })

	if !IsEngineRegistered(engine) {
		t.Fatalf("IsEngineRegistered(%q) = false, want true", engine)
	}

	impl, err := CreateEngine(engine)
	if err != nil {
		t.Fatalf("CreateEngine() = %v", err)
	}
	if impl == nil {
		t.Fatal("CreateEngine() = nil, want provider")
	}
	if info := impl.ModelInfo(); info.Provider != engine {
		t.Errorf("ModelInfo().Provider = %q, want %q", info.Provider, engine)
	}
}

func TestRegisterEngineDuplicate(t *testing.T) {
	const engine = "test-engine-duplicate"
	unregisterTestEngine(t, engine)

	f := func() (IProvider, error) { return &mockProvider{}, nil }
	if err := RegisterEngine(engine, f); err != nil {
		t.Fatalf("RegisterEngine() = %v", err)
	}
	t.Cleanup(func() { unregisterTestEngine(t, engine) })

	err := RegisterEngine(engine, f)
	if !errors.Is(err, ErrEngineAlreadyRegistered) {
		t.Fatalf("RegisterEngine(duplicate) = %v, want ErrEngineAlreadyRegistered", err)
	}
}

func TestCreateEngineUnknown(t *testing.T) {
	impl, err := CreateEngine("no-such-engine")
	if !errors.Is(err, ErrUnknownEngine) {
		t.Fatalf("CreateEngine(unknown) err = %v, want ErrUnknownEngine", err)
	}
	if impl != nil {
		t.Fatalf("CreateEngine(unknown) = %v, want nil", impl)
	}
}

func TestRegisteredEnginesSorted(t *testing.T) {
	names := []string{"zz-engine", "aa-engine", "mm-engine"}
	for _, n := range names {
		unregisterTestEngine(t, n)
		if err := RegisterEngine(n, func() (IProvider, error) { return &mockProvider{}, nil }); err != nil {
			t.Fatalf("RegisterEngine(%q) = %v", n, err)
		}
		t.Cleanup(func() { unregisterTestEngine(t, n) })
	}

	got := RegisteredEngines()
	if len(got) < len(names) {
		t.Fatalf("RegisteredEngines() len = %d, want >= %d", len(got), len(names))
	}
	// Verify the registered subset is sorted (they're near the start of the sorted list
	// only if alphabetically smallest — instead check sortedness of the returned slice).
	for i := 1; i < len(got); i++ {
		if got[i-1] > got[i] {
			t.Fatalf("RegisteredEngines() not sorted: %v", got)
		}
	}
}

// unregisterTestEngine removes a test engine if present.
func unregisterTestEngine(t *testing.T, name string) {
	t.Helper()
	UnregisterEngine(name)
}
