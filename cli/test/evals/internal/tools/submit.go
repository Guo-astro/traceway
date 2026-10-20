package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/tracewayapp/traceway/cli/test/evals/internal/checklist"
	"github.com/tracewayapp/traceway/cli/test/evals/internal/ops"
	"github.com/tracewayapp/traceway/cli/test/evals/internal/record"
)

// SubmitPerf is the perf suite's final-answer tool.
const SubmitPerf = "submit_perf_diagnosis"

// IsSubmit reports whether a tool is a final-answer tool.
func IsSubmit(name string) bool { return name == SubmitPerf }

// Submit is a final-answer tool. It validates the payload against its
// schema and the call ids it cites; a failure goes back to the model for
// repair, up to MaxRepairs times, after which the last payload is accepted
// as-is for grading.
type Submit struct {
	Kind       string // the tool's name: SubmitPerf
	CallExists func(id string) (ok, exists bool)
	MaxRepairs int

	mu      sync.Mutex
	repairs int
}

func (t *Submit) Spec() Spec {
	var rows []string
	for _, r := range checklist.Rows() {
		rows = append(rows, fmt.Sprintf("%s (%s)", r.ID, r.Title))
	}
	ids := checklist.IDs()
	item := map[string]any{"type": "string", "enum": ids}
	return Spec{Name: SubmitPerf, Description: "Submit your final performance diagnosis. This ends the task. " +
		"checklist_item is the bottleneck checklist row that explains the latency, or none_found when the latency is within expectations, or insufficient_evidence when the telemetry cannot tell. Rows: " + strings.Join(rows, "; ") + ". " +
		"Every evidence item cites the id of an earlier tool call (the [call cNN] line at the top of its result).",
		Parameters: obj(map[string]any{
			"checklist_item": item,
			"category":       strProp("database, application, network, caching, infrastructure, or none"),
			"locus": obj(map[string]any{
				"endpoint": strProp("Endpoint name, e.g. GET /api/orders"),
				"span":     strProp("The dominating span or query"),
				"service":  strProp("Service name"),
				"host":     strProp("Host or instance, when the cause is host-specific"),
			}),
			"onset":      map[string]any{"type": []string{"string", "null"}, "description": "RFC 3339 time the slowness started, or null"},
			"evidence":   evidenceSchema(),
			"ruled_out":  map[string]any{"type": "array", "items": obj(map[string]any{"checklist_item": item, "call_id": strProp("Call id"), "reason": strProp("Why it is ruled out")}, "checklist_item", "reason")},
			"fix":        strProp("The concrete fix"),
			"confidence": map[string]any{"type": "string", "enum": []string{"low", "medium", "high"}},
			"report":     strProp("Your report to the user, in Markdown"),
		}, "checklist_item", "locus", "evidence", "fix", "confidence", "report")}
}

func evidenceSchema() map[string]any {
	return map[string]any{"type": "array", "minItems": 1, "items": obj(map[string]any{
		"call_id": strProp("Id of the tool call, e.g. c04"),
		"claim":   strProp("What that call showed"),
	}, "call_id", "claim")}
}

func (t *Submit) Call(_ context.Context, raw json.RawMessage) Result {
	var payload map[string]any
	if err := decodeArgs(raw, &payload); err != nil {
		return t.repair(nil, []string{err.Error()})
	}
	problems := t.validate(payload)
	if len(problems) > 0 {
		return t.repair(payload, problems)
	}
	return t.accept(payload, "")
}

func (t *Submit) repair(payload map[string]any, problems []string) Result {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.repairs++
	if t.repairs > t.MaxRepairs && payload != nil {
		return t.accept(payload, strings.Join(problems, "; "))
	}
	return Result{Output: "Your submission was not accepted. Fix these problems and call " + t.Kind + " again:\n- " + strings.Join(problems, "\n- "), OK: false,
		Ops: []record.OpRecord{{Op: ops.Op{Op: ops.OpSubmit, Args: map[string]any{"tool": t.Kind, "accepted": false}}}}}
}

func (t *Submit) accept(payload map[string]any, problems string) Result {
	msg := "Submission received."
	if problems != "" {
		payload["_validation_problems"] = problems
	}
	return Result{Output: msg, OK: true, Final: true, Submission: payload,
		Ops: []record.OpRecord{{Op: ops.Op{Op: ops.OpSubmit, Args: map[string]any{"tool": t.Kind, "accepted": true}}, OK: true}}}
}

func (t *Submit) validate(p map[string]any) []string {
	var probs []string
	str := func(k string) string { s, _ := p[k].(string); return s }
	need := func(keys ...string) {
		for _, k := range keys {
			if _, ok := p[k]; !ok {
				probs = append(probs, k+" is required")
			}
		}
	}
	checkEvidence := func() {
		ev, ok := p["evidence"].([]any)
		if !ok || len(ev) == 0 {
			probs = append(probs, "evidence must cite at least one tool call")
			return
		}
		for i, e := range ev {
			m, _ := e.(map[string]any)
			id, _ := m["call_id"].(string)
			if id == "" {
				probs = append(probs, fmt.Sprintf("evidence[%d].call_id is required", i))
				continue
			}
			if t.CallExists != nil {
				if _, exists := t.CallExists(id); !exists {
					probs = append(probs, fmt.Sprintf("evidence[%d].call_id %q is not a call in this session (ids look like c04)", i, id))
				}
			}
		}
	}
	need("checklist_item", "locus", "evidence", "fix", "confidence", "report")
	if v := str("checklist_item"); v != "" && !checklist.IsID(v) {
		probs = append(probs, fmt.Sprintf("checklist_item %q is not one of the allowed values", v))
	}
	if rs, ok := p["ruled_out"].([]any); ok {
		for i, r := range rs {
			m, _ := r.(map[string]any)
			if v, _ := m["checklist_item"].(string); v != "" && !checklist.IsID(v) {
				probs = append(probs, fmt.Sprintf("ruled_out[%d].checklist_item %q is not an allowed value", i, v))
			}
		}
	}
	checkEvidence()
	if c := str("confidence"); c != "" && !slices.Contains([]string{"low", "medium", "high"}, c) {
		probs = append(probs, "confidence must be low, medium or high")
	}
	return probs
}
