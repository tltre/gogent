package app

import (
	"testing"

	"github.com/tltre/gogent/pkg/contextmanager"
)

func TestCompressionConfigFromYAML(t *testing.T) {
	cfg := compressionConfigFromYAML(map[string]any{
		"contextSize": 256000,
		"compression": map[string]any{
			"enabled":     false,
			"softRatio":   0.5,
			"hardRatio":   0.8,
			"minTokens":   1000,
			"reserved":    5000,
			"model":       "summary-model",
			"maxFailures": 5,
		},
	})
	if cfg.Enabled {
		t.Error("Enabled = true, want false")
	}
	if cfg.SoftRatio != 0.5 || cfg.HardRatio != 0.8 {
		t.Errorf("ratios = %v/%v, want 0.5/0.8", cfg.SoftRatio, cfg.HardRatio)
	}
	if cfg.MinTokens != 1000 || cfg.Reserved != 5000 {
		t.Errorf("MinTokens/Reserved = %d/%d, want 1000/5000", cfg.MinTokens, cfg.Reserved)
	}
	if cfg.Model != "summary-model" {
		t.Errorf("Model = %q, want summary-model", cfg.Model)
	}
	if cfg.MaxFailures != 5 {
		t.Errorf("MaxFailures = %d, want 5", cfg.MaxFailures)
	}
	if cfg.ContextSizeOverride != 256000 {
		t.Errorf("ContextSizeOverride = %d, want 256000", cfg.ContextSizeOverride)
	}
}

func TestCompressionConfigFromYAMLDefaults(t *testing.T) {
	def := contextmanager.DefaultCompressionConfig()
	got := compressionConfigFromYAML(nil)
	if got.Enabled != def.Enabled || got.SoftRatio != def.SoftRatio || got.HardRatio != def.HardRatio {
		t.Errorf("defaults not preserved: %+v vs %+v", got, def)
	}
}
