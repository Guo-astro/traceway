// Package tools implements what the agent under test can call: a sandboxed
// shell (an interpreter with an allowlist, never the host shell), confined
// file tools, skills, ask_user, and the structured submit tools. Every call
// returns the operations it performed so the graders can check the
// trajectory.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tracewayapp/traceway/cli/test/evals/internal/record"
)

// Spec is a tool's function definition as the model sees it.
type Spec struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// Result is what one call produced.
type Result struct {
	Output    string
	OK        bool
	Truncated bool
	Ops       []record.OpRecord
	// Final is set when a submit/finish payload validated: the run ends.
	Final      bool
	Submission map[string]any
}

// Tool is one callable tool.
type Tool interface {
	Spec() Spec
	Call(ctx context.Context, args json.RawMessage) Result
}

// MaxOutput caps what one shell or file call returns to the model.
const MaxOutput = 30000

// Cap truncates s to MaxOutput characters with a marker.
func Cap(s string) (string, bool) { return CapAt(s, MaxOutput) }

// CapAt truncates s to n characters with a marker.
func CapAt(s string, n int) (string, bool) {
	if len(s) <= n {
		return s, false
	}
	more := len(s) - n
	return s[:n] + fmt.Sprintf("\n[output truncated: %d more characters. Narrow the query (smaller window, page size, field selection) or filter the output.]", more), true
}

// Sandbox confines file access to the run's workspace.
type Sandbox struct {
	Root     string   // absolute workspace path
	ReadOnly []string // workspace-relative prefixes that cannot be written (.skills)
}

// ErrOutside is returned for any path that leaves the workspace.
var ErrOutside = errors.New("path is outside the workspace")

// Resolve maps p (absolute, or relative to base) to an absolute path inside
// the workspace. Symlinks pointing out of the workspace are refused.
func (s *Sandbox) Resolve(base, p string) (string, error) {
	if p == "/dev/null" {
		return p, nil
	}
	if base == "" {
		base = s.Root
	}
	var abs string
	if filepath.IsAbs(p) {
		abs = filepath.Clean(p)
	} else {
		abs = filepath.Clean(filepath.Join(base, p))
	}
	if !within(s.Root, abs) {
		return "", fmt.Errorf("%s: %w", p, ErrOutside)
	}
	// Resolve the longest existing prefix to catch symlink escapes.
	probe := abs
	for {
		real, err := filepath.EvalSymlinks(probe)
		if err == nil {
			rootReal, _ := filepath.EvalSymlinks(s.Root)
			if rootReal == "" {
				rootReal = s.Root
			}
			if !within(rootReal, real) {
				return "", fmt.Errorf("%s: %w", p, ErrOutside)
			}
			break
		}
		parent := filepath.Dir(probe)
		if parent == probe || !within(s.Root, parent) {
			break
		}
		probe = parent
	}
	return abs, nil
}

// Writable reports whether abs may be written.
func (s *Sandbox) Writable(abs string) error {
	if abs == "/dev/null" {
		return nil
	}
	rel, err := filepath.Rel(s.Root, abs)
	if err != nil {
		return err
	}
	for _, ro := range s.ReadOnly {
		if rel == ro || strings.HasPrefix(rel, ro+string(filepath.Separator)) {
			return fmt.Errorf("%s is read-only", filepath.ToSlash(rel))
		}
	}
	return nil
}

// Rel returns the workspace-relative form of abs.
func (s *Sandbox) Rel(abs string) string {
	rel, err := filepath.Rel(s.Root, abs)
	if err != nil {
		return abs
	}
	return filepath.ToSlash(rel)
}

func within(root, p string) bool {
	if p == root {
		return true
	}
	return strings.HasPrefix(p, strings.TrimSuffix(root, string(filepath.Separator))+string(filepath.Separator))
}

// decodeArgs unmarshals tool arguments, tolerating an empty payload.
func decodeArgs(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte("{}")
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("invalid arguments: %v", err)
	}
	return nil
}

func errResult(format string, a ...any) Result {
	return Result{Output: "error: " + fmt.Sprintf(format, a...), OK: false}
}

// readFileCapped reads a workspace file for a tool.
func readFileCapped(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// obj builds a JSON-schema object.
func obj(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func intProp(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}
