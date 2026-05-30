package tool

import "context"

// execThink is a pass-through tool. It returns the input thought as-is.
// The LLM uses this for "self-talk" reasoning — no actual processing needed.
func execThink(_ context.Context, params map[string]any) (Result, error) {
	thought, _ := params["thought"].(string)
	return Result{
		Output:  thought,
		IsError: false,
	}, nil
}
