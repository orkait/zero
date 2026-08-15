package tui

import (
	"strings"
	"testing"
)

// The model picker must title each row with the model NAME, not the models.dev
// marketing description. Regression: Ollama Cloud models resolved full-sentence
// descriptions from the remote catalog, so rows showed sentences and several
// models sharing one description looked like exact duplicates (issue: /model
// list rendered descriptions as titles).
func TestModelPickerDisplayNamePrefersModelName(t *testing.T) {
	sentence := "DeepSeek chat model for instruction following, coding, and analysis"
	cases := []struct {
		id          string
		description string
		want        string
	}{
		{"deepseek-v3.2", sentence, "Deepseek V3.2"},
		{"deepseek-v3.2-thinking", sentence, "Deepseek V3.2 Thinking"},
		{"glm-5.2", "GLM flagship model", "GLM 5.2"},
		{"qwen3-coder:480b", "Qwen coding agent model for repository tasks", "Qwen3 Coder 480b"},
		{"anthropic/claude-sonnet-4.6", "some blurb", "Claude Sonnet 4.6"},
	}
	for _, tc := range cases {
		if got := modelPickerDisplayName(tc.id, tc.description); got != tc.want {
			t.Errorf("modelPickerDisplayName(%q, %q) = %q, want %q", tc.id, tc.description, got, tc.want)
		}
	}
}

// Two distinct model ids that share the same catalog description must render as
// distinct rows (the "duplicate-looking rows" symptom), never as the shared
// sentence.
func TestModelPickerDisplayNameDistinctForSharedDescription(t *testing.T) {
	shared := "Mistral coding agent model for repository tasks and software engineering"
	a := modelPickerDisplayName("codestral-2", shared)
	b := modelPickerDisplayName("devstral-2", shared)
	if a == b {
		t.Fatalf("shared description collapsed rows to the same title: %q", a)
	}
	if a == shared || b == shared {
		t.Fatalf("row title used the description sentence: a=%q b=%q", a, b)
	}
}

// With no id to name the row, a non-generic description is still an acceptable
// fallback; a generic placeholder (or nothing) falls back to "model".
// Cline ships paid cline-pass/* and free vendor-prefixed twins that share a
// display name after the picker strips the namespace (e.g. cline-pass/deepseek-v4-flash
// vs deepseek/deepseek-v4-flash). Keep the catalog "(free)" marker so the rows
// stay distinct, matching openclaude's label.
func TestModelPickerDisplayNameKeepsClineFreeAndPaidTwinsDistinct(t *testing.T) {
	paid := modelPickerDisplayName("cline-pass/deepseek-v4-flash", "DeepSeek V4 Flash")
	free := modelPickerDisplayName("deepseek/deepseek-v4-flash", "DeepSeek V4 Flash (free)")
	if paid == free {
		t.Fatalf("paid and free Cline twins collapsed to %q", paid)
	}
	if !strings.Contains(strings.ToLower(free), "free") {
		t.Fatalf("free twin label %q is missing (free)", free)
	}
	if strings.Contains(strings.ToLower(paid), "free") {
		t.Fatalf("paid twin label %q was tagged free", paid)
	}
	already := modelPickerDisplayName("poolside/laguna-s-2.1:free", "Laguna S 2.1 (free)")
	if strings.Count(strings.ToLower(already), "free") != 1 {
		t.Fatalf("already-tagged free id double-labelled: %q", already)
	}
}

func TestModelPickerDisplayNameFallsBackToDescriptionOnlyWithoutID(t *testing.T) {
	if got := modelPickerDisplayName("", "Friendly Name"); got != "Friendly Name" {
		t.Fatalf("empty id should fall back to the description, got %q", got)
	}
	if got := modelPickerDisplayName("", "catalog default"); got != "model" {
		t.Fatalf("generic description with no id should yield %q, got %q", "model", got)
	}
	if got := modelPickerDisplayName("", ""); got != "model" {
		t.Fatalf("empty id and description should yield %q, got %q", "model", got)
	}
}
