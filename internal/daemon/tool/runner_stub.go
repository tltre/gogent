package tool

import "context"

// StubRunner returns a clear "not yet supported" message for drivers
// that haven't been implemented yet. No mutable state — no lock needed.
type StubRunner struct {
	driverName string
}

// NewStubRunner creates a StubRunner for the given driver type.
func NewStubRunner(driverName string) *StubRunner {
	return &StubRunner{driverName: driverName}
}

// Execute returns a driver-not-supported error.
func (r *StubRunner) Execute(_ context.Context, def *ToolDefinition, _ map[string]any) (Result, error) {
	return Result{
		IsError:  true,
		ErrorMsg: def.Name + " (" + r.driverName + "): driver not yet supported in v0.12.3",
	}, nil
}
