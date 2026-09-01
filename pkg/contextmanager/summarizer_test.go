package contextmanager

import (
	"strings"
	"testing"
)

func TestBuildCompactionPrompt(t *testing.T) {
	msgs := []ContextMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}
	// No prior summary → no merge instructions, contains template.
	p := buildCompactionPrompt(msgs, nil)
	if !strings.Contains(p, "<conversation>") || !strings.Contains(p, "hello") {
		t.Error("prompt missing conversation for first compaction")
	}
	if strings.Contains(p, "<prior-summary>") {
		t.Error("first compaction prompt must not contain prior-summary")
	}
	if !strings.Contains(p, "## Topic") {
		t.Error("generic summary template missing")
	}

	// With prior summary → merge instructions + prior-summary block.
	prior := &Summary{Content: "PRIOR"}
	p2 := buildCompactionPrompt(msgs, prior)
	if !strings.Contains(p2, "<prior-summary>") || !strings.Contains(p2, "PRIOR") {
		t.Error("merge prompt missing prior-summary")
	}
	if !strings.Contains(p2, "discarded after this") {
		t.Error("merge prompt missing combine instructions")
	}
}

func TestRenderSummary(t *testing.T) {
	s := renderSummary("S1")
	if !strings.Contains(s, "<conversation-checkpoint>") || !strings.Contains(s, "S1") {
		t.Errorf("renderSummary = %q, want checkpoint wrapper", s)
	}
}
