package grade

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

// TestWorkspaceGitIgnoresRepoConfig: a .git/config the agent wrote (the
// setup suite's container shell can) never runs a program on the host when
// the graders list the workspace's files.
func TestWorkspaceGitIgnoresRepoConfig(t *testing.T) {
	dir := t.TempDir()
	work := filepath.Join(dir, "work")
	os.MkdirAll(work, 0o755)
	os.WriteFile(filepath.Join(work, "main.go"), []byte("package main\n"), 0o644)
	for _, args := range [][]string{{"init", "-q"}, {"add", "main.go"}, {"-c", "user.email=x@y", "-c", "user.name=x", "commit", "-qm", "init"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = work
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git unavailable: %v %s", err, out)
		}
	}
	hooks := filepath.Join(work, "hooks")
	os.MkdirAll(hooks, 0o755)
	for _, h := range []string{"post-index-change", "reference-transaction"} {
		os.WriteFile(filepath.Join(hooks, h), []byte("#!/bin/sh\necho x > ../PWNED\n"), 0o755)
	}
	cfg, _ := os.ReadFile(filepath.Join(work, ".git", "config"))
	cfg = append(cfg, []byte("[core]\n\tfsmonitor = \"echo x > ../PWNED; false\"\n\thooksPath = hooks\n\tworktree = ..\n")...)
	os.WriteFile(filepath.Join(work, ".git", "config"), cfg, 0o644)
	os.WriteFile(filepath.Join(work, "new.go"), []byte("package main\n"), 0o644)
	os.WriteFile(filepath.Join(work, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644)

	files, ok := gitVisibleFiles(work)
	if !ok || !slices.Contains(files, "main.go") || !slices.Contains(files, "new.go") || slices.Contains(files, "work/main.go") {
		t.Errorf("gitVisibleFiles = %v, %v", files, ok)
	}
	changed, err := changedFiles(work)
	if err != nil || !slices.Contains(changed, "main.go") || !slices.Contains(changed, "new.go") {
		t.Errorf("changedFiles = %v, %v", changed, err)
	}
	for _, p := range []string{filepath.Join(dir, "PWNED"), filepath.Join(work, "PWNED")} {
		if _, err := os.Stat(p); err == nil {
			t.Fatalf("git ran a program from the workspace's config (%s)", p)
		}
	}
}
