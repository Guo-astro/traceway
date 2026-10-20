// Package grade scores one run. Deterministic graders gate the result:
// answer, trajectory, grounding and the report checks.
package grade

import (
	"context"
	"fmt"
	"strings"

	"github.com/tracewayapp/traceway/cli/test/evals/internal/catalog"
	"github.com/tracewayapp/traceway/cli/test/evals/internal/record"
)

// Verdict is one grader's result.
type Verdict struct {
	Grader   string   `json:"grader"`
	Pass     bool     `json:"pass"`
	Gating   bool     `json:"gating"`
	Skipped  bool     `json:"skipped,omitempty"`
	Score    *float64 `json:"score,omitempty"`
	Failures []string `json:"failures,omitempty"`
	Notes    []string `json:"notes,omitempty"`
}

func (v *Verdict) fail(format string, a ...any) {
	v.Pass = false
	v.Failures = append(v.Failures, fmt.Sprintf(format, a...))
}

func (v *Verdict) note(format string, a ...any) { v.Notes = append(v.Notes, fmt.Sprintf(format, a...)) }

func newVerdict(name string, gating bool) *Verdict {
	return &Verdict{Grader: name, Pass: true, Gating: gating}
}

// Input is everything the graders see.
type Input struct {
	Case *catalog.Case // templates resolved
	Arm  string
	Run  *record.Run
	Seed map[string]any
}

// All runs every applicable grader in a fixed order.
func All(ctx context.Context, in *Input) []*Verdict {
	var out []*Verdict
	if in.Case.Expect.Answer != nil {
		out = append(out, Answer(in))
	}
	if in.Case.Expect.Calls != nil {
		out = append(out, Trajectory(in.Run, in.Case.Expect.Calls))
	}
	if in.Case.Suite == "perf" || len(in.Case.Expect.Grounding) > 0 {
		out = append(out, Grounding(in))
	}
	if in.Case.Expect.Report != nil {
		out = append(out, Report(in))
	}
	return out
}

// Passed reports whether every gating grader passed.
func Passed(vs []*Verdict) bool {
	for _, v := range vs {
		if v.Gating && !v.Skipped && !v.Pass {
			return false
		}
	}
	return true
}

// FinalText is the agent's report: the submission's report/summary field,
// else its last message.
func FinalText(r *record.Run) string {
	if r.Submission != nil {
		for _, k := range []string{"report", "summary"} {
			if s, ok := r.Submission[k].(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	return r.FinalText
}
