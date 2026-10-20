package guard

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// paidRunRe matches a command line that starts a paid eval run.
var paidRunRe = regexp.MustCompile(`twevals(\s+|\S*\s+)(run|resume)\b|eval-local`)

// CheckWorkflows enforces the dispatch-only rule over a .github/workflows
// directory: cli-evals.yml exists, its `on:` is exactly workflow_dispatch, the
// approval flag is set only by a job behind the evals environment, and no
// other workflow references it or starts a paid run.
func CheckWorkflows(dir string) error {
	evalsPath := filepath.Join(dir, filepath.Base(WorkflowFile))
	raw, err := os.ReadFile(evalsPath)
	if err != nil {
		return fmt.Errorf("%s must exist: %w", WorkflowFile, err)
	}
	if err := checkEvalsWorkflow(raw); err != nil {
		return fmt.Errorf("%s: %w", WorkflowFile, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || name == filepath.Base(WorkflowFile) || !(strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")) {
			continue
		}
		other, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		text := string(other)
		if strings.Contains(text, "cli-evals.yml") || strings.Contains(text, "cli-evals.yaml") {
			return fmt.Errorf("%s references cli-evals.yml; no workflow may call or dispatch the evals", name)
		}
		if paidRunRe.MatchString(text) || strings.Contains(text, "TWEVALS_APPROVED") {
			return fmt.Errorf("%s starts a paid eval run; only %s may, and only when dispatched by hand", name, WorkflowFile)
		}
	}
	return nil
}

func checkEvalsWorkflow(raw []byte) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return err
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("not a YAML mapping")
	}
	root := doc.Content[0]
	on := mapValue(root, "on")
	if on == nil {
		// YAML 1.1 readers turn a bare `on` key into true; accept that
		// spelling too so the check cannot be dodged by quoting.
		on = mapValue(root, "true")
	}
	if on == nil {
		return fmt.Errorf("has no on: block")
	}
	switch on.Kind {
	case yaml.ScalarNode:
		if on.Value != "workflow_dispatch" {
			return fmt.Errorf("on: must be exactly workflow_dispatch, got %q", on.Value)
		}
	case yaml.SequenceNode:
		if len(on.Content) != 1 || on.Content[0].Value != "workflow_dispatch" {
			return fmt.Errorf("on: must be exactly [workflow_dispatch]")
		}
	case yaml.MappingNode:
		var keys []string
		for i := 0; i < len(on.Content); i += 2 {
			keys = append(keys, on.Content[i].Value)
		}
		if len(keys) != 1 || keys[0] != "workflow_dispatch" {
			return fmt.Errorf("on: must have exactly one trigger, workflow_dispatch; found %v", keys)
		}
	default:
		return fmt.Errorf("on: has an unexpected shape")
	}

	jobs := mapValue(root, "jobs")
	if jobs == nil || jobs.Kind != yaml.MappingNode {
		return fmt.Errorf("has no jobs")
	}
	gated := 0
	for i := 0; i < len(jobs.Content); i += 2 {
		name, job := jobs.Content[i].Value, jobs.Content[i+1]
		var buf strings.Builder
		enc := yaml.NewEncoder(&buf)
		_ = enc.Encode(job)
		body := buf.String()
		approves := strings.Contains(body, "TWEVALS_APPROVED")
		runsPaid := paidRunRe.MatchString(body)
		if !approves && !runsPaid {
			continue
		}
		envNode := mapValue(job, "environment")
		envName := ""
		if envNode != nil {
			if envNode.Kind == yaml.ScalarNode {
				envName = envNode.Value
			} else if n := mapValue(envNode, "name"); n != nil {
				envName = n.Value
			}
		}
		if envName != "evals" {
			return fmt.Errorf("job %q starts a paid run or sets TWEVALS_APPROVED without environment: evals", name)
		}
		gated++
	}
	if gated == 0 {
		return fmt.Errorf("no job runs the evals behind environment: evals")
	}
	return nil
}

func mapValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}
