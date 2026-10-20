package guard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEvalsWorkflowIsDispatchOnly is the manual-only trigger test: the
// repository's cli-evals.yml has workflow_dispatch as its only trigger and no
// other workflow references it or starts a paid run.
func TestEvalsWorkflowIsDispatchOnly(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "..", "..", ".github", "workflows")
	if err := CheckWorkflows(dir); err != nil {
		t.Fatal(err)
	}
}

func TestCheckWorkflowsRejectsAutomation(t *testing.T) {
	good := `name: CLI Evals
on:
  workflow_dispatch:
    inputs:
      max_cost_usd: {type: string, required: true}
permissions:
  contents: read
jobs:
  preflight:
    runs-on: ubuntu-latest
    steps:
      - run: go run ./cmd/twevals plan --max-cost-usd 1
  evals:
    environment: evals
    runs-on: ubuntu-latest
    env:
      TWEVALS_APPROVED: "1"
    steps:
      - run: go run ./cmd/twevals run --yes --max-cost-usd 1
`
	cases := map[string]struct {
		evals  string
		others map[string]string
		want   string
	}{
		"good":             {evals: good},
		"push trigger":     {evals: strings.Replace(good, "on:\n", "on:\n  push:\n", 1), want: "exactly one trigger"},
		"schedule":         {evals: strings.Replace(good, "on:\n", "on:\n  schedule:\n    - cron: '0 0 * * *'\n", 1), want: "exactly one trigger"},
		"workflow_call":    {evals: strings.Replace(good, "on:\n", "on:\n  workflow_call:\n", 1), want: "exactly one trigger"},
		"list of triggers": {evals: strings.Replace(good, "on:\n  workflow_dispatch:\n    inputs:\n      max_cost_usd: {type: string, required: true}\n", "on: [workflow_dispatch, push]\n", 1), want: "exactly [workflow_dispatch]"},
		"ungated paid job": {evals: strings.Replace(good, "    environment: evals\n", "", 1), want: "without environment: evals"},
		"caller":           {evals: good, others: map[string]string{"ci.yml": "jobs:\n  x:\n    uses: ./.github/workflows/cli-evals.yml\n"}, want: "references cli-evals.yml"},
		"dispatcher":       {evals: good, others: map[string]string{"nightly.yml": "on: schedule\njobs:\n  x:\n    steps:\n      - run: gh workflow run cli-evals.yml\n"}, want: "references cli-evals.yml"},
		"other paid run":   {evals: good, others: map[string]string{"cli.yml": "jobs:\n  x:\n    steps:\n      - run: cd cli/test/evals && go run ./cmd/twevals run --yes\n"}, want: "starts a paid eval run"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "cli-evals.yml"), []byte(c.evals), 0o644); err != nil {
				t.Fatal(err)
			}
			for f, body := range c.others {
				if err := os.WriteFile(filepath.Join(dir, f), []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			err := CheckWorkflows(dir)
			if c.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want error containing %q, got %v", c.want, err)
			}
		})
	}
}
