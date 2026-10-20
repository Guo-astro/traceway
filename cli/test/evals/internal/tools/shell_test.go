package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newShell(t *testing.T) (*Shell, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "work", ".skills", "traceway"), 0o755); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(root, "work")
	os.WriteFile(filepath.Join(work, ".skills", "traceway", "SKILL.md"), []byte("---\nname: traceway\ndescription: 'x'\n---\nbody"), 0o644)
	os.WriteFile(filepath.Join(work, "notes.txt"), []byte("alpha 3\nbeta 1\ngamma 2\nbeta 1\n"), 0o644)
	// A stand-in for the traceway binary: prints its args and environment.
	bin := filepath.Join(root, "bin", "traceway")
	os.MkdirAll(filepath.Dir(bin), 0o755)
	os.WriteFile(bin, []byte("#!/bin/sh\nif [ \"$1\" = env ]; then env; exit 0; fi\nif [ \"$1\" = fail ]; then echo '{\"error\":\"not_found\"}' >&2; exit 5; fi\necho '{\"data\":[{\"endpoint\":\"GET /a\",\"p95Duration\":1500000000},{\"endpoint\":\"GET /b\",\"p95Duration\":20000000}]}'\n"), 0o755)
	sb := &Sandbox{Root: work, ReadOnly: []string{".skills"}}
	return &Shell{Sandbox: sb, Bins: map[string]string{"traceway": bin}, BinEnv: []string{"HOME=" + root, "PATH=/usr/bin:/bin", "TZ=UTC"}}, work
}

func run(t *testing.T, s *Shell, cmd string) Result {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{"command": cmd})
	return s.Call(context.Background(), raw)
}

func TestShellUtilities(t *testing.T) {
	s, _ := newShell(t)
	cases := []struct{ cmd, want string }{
		{`echo hi | grep h`, "hi"},
		{`sort -k2 -n notes.txt | head -1`, "beta 1"},
		{`sort notes.txt | uniq -c | sort -rn | head -1 | tr -s ' '`, "2 beta 1"},
		{`cut -d' ' -f1 notes.txt | tail -n 1`, "beta"},
		{`awk '{s+=$2} END {print s}' notes.txt`, "7"},
		{`echo '{"a":[1,2,3]}' | jq '.a | length'`, "3"},
		{`echo '{"a":"x"}' | jq -r .a`, "x"},
		{`grep -c beta notes.txt`, "2"},
		{`wc -l < notes.txt`, "4"},
		{`X=$(echo nested); echo "got $X"`, "got nested"},
	}
	for _, c := range cases {
		r := run(t, s, c.cmd)
		if !r.OK || strings.TrimSpace(r.Output) != c.want {
			t.Errorf("%s: got ok=%v %q, want %q", c.cmd, r.OK, r.Output, c.want)
		}
	}
}

func TestShellConfinement(t *testing.T) {
	s, work := newShell(t)
	for _, cmd := range []string{`cat /etc/passwd`, `cat ../../etc/passwd`, `ls /`, `grep root /etc/passwd`, `cd / && cat etc/hostname`} {
		if r := run(t, s, cmd); r.OK || !strings.Contains(r.Output, "outside the workspace") && !strings.Contains(r.Output, "exit status") {
			t.Errorf("%s escaped the sandbox: %q", cmd, r.Output)
		}
	}
	for _, cmd := range []string{`curl https://example.com`, `python3 -c 1`, `bash -c id`, `sh -c id`, `node -e 1`, `go version`} {
		if r := run(t, s, cmd); r.OK || !strings.Contains(r.Output, "not found") {
			t.Errorf("%s should be unavailable: %q", cmd, r.Output)
		}
	}
	if r := run(t, s, `echo hacked > .skills/traceway/SKILL.md`); r.OK {
		t.Errorf("wrote into .skills: %q", r.Output)
	}
	if r := run(t, s, `echo kept > out.txt && cat out.txt`); !r.OK || strings.TrimSpace(r.Output) != "kept" {
		t.Errorf("workspace write failed: %q", r.Output)
	}
	if _, err := os.Stat(filepath.Join(work, "out.txt")); err != nil {
		t.Error(err)
	}
}

func TestShellRecordsTracewayInPipelines(t *testing.T) {
	s, _ := newShell(t)
	r := run(t, s, `traceway endpoints list --since 24h --order-by p95 --output json | jq -r '.data[0].endpoint'`)
	if !r.OK || strings.TrimSpace(r.Output) != "GET /a" {
		t.Fatalf("pipeline output %q", r.Output)
	}
	if len(r.Ops) != 1 || r.Ops[0].Op.Op != "endpoints.list" || r.Ops[0].Args["since"] != "24h" || r.Ops[0].Args["order_by"] != "p95" {
		t.Fatalf("ops %+v", r.Ops)
	}
	if !strings.Contains(r.Ops[0].Output, "p95Duration") {
		t.Error("the raw output before the pipe must be kept for grounding")
	}
	r = run(t, s, `traceway fail x; traceway exceptions show abc`)
	if len(r.Ops) != 2 || r.Ops[0].OK || r.Ops[0].Exit != 5 || !r.Ops[1].OK || r.Ops[1].Args["hash"] != "abc" {
		t.Fatalf("ops %+v", r.Ops)
	}
}

// TestToolEnvIsScrubbed asserts plan §3.5: secrets in the harness's own
// environment never reach tool subprocesses.
func TestToolEnvIsScrubbed(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-or-secret")
	t.Setenv("GITHUB_TOKEN", "ghp_secret")
	t.Setenv("JWT_SECRET", "jwt-secret-value-that-is-long-enough")
	s, _ := newShell(t)
	r := run(t, s, `traceway env`)
	for _, leak := range []string{"sk-or-secret", "ghp_secret", "jwt-secret-value", "OPENROUTER", "GITHUB_TOKEN", "JWT_SECRET"} {
		if strings.Contains(r.Output, leak) {
			t.Errorf("tool environment leaked %s:\n%s", leak, r.Output)
		}
	}
	r = run(t, s, `echo "$OPENROUTER_API_KEY$GITHUB_TOKEN$JWT_SECRET"`)
	if strings.TrimSpace(r.Output) != "" {
		t.Errorf("interpreter environment leaked: %q", r.Output)
	}
}

func TestEditAndReadFile(t *testing.T) {
	s, _ := newShell(t)
	ed := &EditFile{Sandbox: s.Sandbox}
	res := ed.Call(context.Background(), json.RawMessage(`{"path":"notes.txt","old_string":"gamma 2","new_string":"gamma 9"}`))
	if !res.OK {
		t.Fatal(res.Output)
	}
	rd := &ReadFile{Sandbox: s.Sandbox}
	res = rd.Call(context.Background(), json.RawMessage(`{"path":"notes.txt"}`))
	if !strings.Contains(res.Output, "gamma 9") {
		t.Fatal(res.Output)
	}
	res = ed.Call(context.Background(), json.RawMessage(`{"path":".skills/traceway/SKILL.md","old_string":"body","new_string":"x"}`))
	if res.OK || !strings.Contains(res.Output, "read-only") {
		t.Fatalf("edited .skills: %q", res.Output)
	}
	res = rd.Call(context.Background(), json.RawMessage(`{"path":"/etc/passwd"}`))
	if res.OK {
		t.Fatal("read outside the workspace")
	}
}

func TestAskUserRules(t *testing.T) {
	a := &AskUser{}
	if got := a.Answer("anything"); !strings.Contains(got, "anything to add") {
		t.Fatal(got)
	}
}
