package catalog

import (
	"strings"
	"testing"
)

// TestInvokeMatchesPrompt: the runner preloads the skill for invoke: slash,
// so a case's invoke and its prompt must agree.
func TestInvokeMatchesPrompt(t *testing.T) {
	for _, c := range []struct {
		invoke, prompt string
		ok             bool
	}{
		{"slash", "/traceway perf GET /api/orders", true},
		{"natural", "Checkout got slow this week.", true},
		{"", "Checkout got slow this week.", true},
		{"slash", "Checkout got slow this week.", false},
		{"natural", "/traceway perf GET /api/orders", false},
	} {
		cs := &Case{ID: "perf-x", Suite: "perf", Seed: "perf/x", Arms: []string{ArmCLISkill}, Invoke: c.invoke, Prompt: c.prompt,
			Oracle: []Step{{Shell: "true"}}, Expect: Expect{Answer: &Answer{ChecklistItem: "none_found"}}, Distractor: "x"}
		err := cs.Validate()
		if got := err == nil || !strings.Contains(err.Error(), "invoke: slash"); got != c.ok {
			t.Errorf("invoke %q, prompt %q: %v", c.invoke, c.prompt, err)
		}
	}
}
