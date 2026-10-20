package report

import (
	"math"
	"testing"

	"github.com/tracewayapp/traceway/cli/test/evals/internal/runner"
)

func res(c, arm, model string, idx int, status string, cost float64) *runner.Result {
	suite := c[:len("perf")]
	if c[:5] == "debug" {
		suite = "debug"
	}
	return &runner.Result{Case: c, Suite: suite, Arm: arm, Model: model, RunIdx: idx, Status: status, Metrics: runner.Metrics{CostUSD: cost, ToolCalls: 10}}
}

func TestAggregate(t *testing.T) {
	runs := []*runner.Result{
		res("perf-a", "cli+skill", "opus", 1, "pass", 0.5),
		res("perf-a", "cli-noskill", "opus", 1, "fail", 0.4),
		res("perf-b", "cli+skill", "opus", 1, "pass", 0.7),
		res("perf-b", "cli-noskill", "opus", 1, "pass", 0.6),
		res("perf-c", "cli+skill", "opus", 1, "infra_error", 0),
	}
	a := Aggregate(runs)
	if r := a.Suites["perf|opus|cli+skill"]; r.Pass != 2 || r.Total != 2 {
		t.Fatalf("infra errors must not count: %+v", r)
	}
	if len(a.SkillUplift) != 1 || math.Abs(a.SkillUplift[0].Delta-50) > 1e-9 {
		t.Fatalf("uplift %+v", a.SkillUplift)
	}
	if c := a.Cost["opus"]; c.PassingRuns != 3 || math.Abs(c.AvgCostUSD-0.6) > 1e-9 {
		t.Fatalf("cost %+v", c)
	}
}
