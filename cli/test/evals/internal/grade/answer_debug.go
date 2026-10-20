package grade

import (
	"path/filepath"
	"strings"

	"github.com/tracewayapp/traceway/cli/test/evals/internal/catalog"
)

// answerDebug grades a submit_debug_diagnosis payload.
func answerDebug(v *Verdict, exp *catalog.Answer, sub map[string]any) {
	str := func(m map[string]any, k string) string { s, _ := m[k].(string); return strings.TrimSpace(s) }
	if exp.Category != "" && str(sub, "category") != exp.Category {
		v.fail("category %q, want %q", str(sub, "category"), exp.Category)
	}
	if exp.IssueHash != "" && !strings.EqualFold(str(sub, "issue_hash"), exp.IssueHash) {
		v.fail("issue_hash %q, want %q", str(sub, "issue_hash"), exp.IssueHash)
	}
	if exp.OccurrenceID != "" && !strings.EqualFold(str(sub, "occurrence_id"), exp.OccurrenceID) {
		v.fail("occurrence_id %q, want %q", str(sub, "occurrence_id"), exp.OccurrenceID)
	}
	if exp.Culprit != nil {
		c, _ := sub["culprit"].(map[string]any)
		if c == nil {
			c = map[string]any{}
		}
		if exp.Culprit.File != "" && !samePath(str(c, "file"), exp.Culprit.File) {
			v.fail("culprit.file %q, want %q", str(c, "file"), exp.Culprit.File)
		}
		if exp.Culprit.Symbol != "" && !sameSymbol(str(c, "symbol"), exp.Culprit.Symbol) {
			v.fail("culprit.symbol %q, want %q", str(c, "symbol"), exp.Culprit.Symbol)
		}
		if exp.Culprit.Line > 0 {
			tol := exp.Culprit.LineTolerance
			if tol == 0 {
				tol = 3
			}
			line, _ := toFloat(c["line"])
			if d := int(line) - exp.Culprit.Line; d < -tol || d > tol {
				v.fail("culprit.line %d, want %d±%d", int(line), exp.Culprit.Line, tol)
			}
		}
	}
	if exp.NeedsInstrumentation != nil {
		got, _ := sub["needs_instrumentation"].(bool)
		if got != *exp.NeedsInstrumentation {
			v.fail("needs_instrumentation %v, want %v", got, *exp.NeedsInstrumentation)
		}
	}
}

func samePath(got, want string) bool {
	g := filepath.ToSlash(filepath.Clean(strings.TrimPrefix(strings.TrimSpace(got), "./")))
	w := filepath.ToSlash(filepath.Clean(want))
	return g == w || strings.HasSuffix(g, "/"+w) || strings.HasSuffix(w, "/"+g) && strings.Contains(g, "/")
}

// sameSymbol accepts "(*OrderHandler).List", "OrderHandler.List" or "List"
// for an expected "List" (and the reverse).
func sameSymbol(got, want string) bool {
	last := func(s string) string {
		s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "()"))
		// Swift and Kotlin accessor frames: chatURL.getter, value.<get-x>.
		for _, suf := range []string{".getter", ".setter", ".modify", ".init", ".deinit"} {
			s = strings.TrimSuffix(s, suf)
		}
		if i := strings.LastIndexAny(s, ".:#"); i >= 0 {
			s = s[i+1:]
		}
		return strings.ToLower(strings.Trim(s, "()*"))
	}
	return last(got) != "" && last(got) == last(want)
}
