package grade

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// HiddenTests verifies a code change: it records the diff, copies the case's
// hidden tests (testdata/hidden/<name>/) into the workspace only after the
// agent finished, runs the case command, and checks the diff touched the
// culprit. A real model's patch runs in a container with no network; it runs
// on the host only for oracle runs (our own code) or when AllowHostTests is
// set explicitly.
func HiddenTests(ctx context.Context, in *Input) *Verdict {
	spec := in.Case.Expect.Code
	v := newVerdict("hidden_tests", spec.Gating)
	changed, err := changedFiles(in.Workspace)
	if err != nil {
		v.fail("read the diff: %v", err)
		return v
	}
	v.note("changed files: %s", strings.Join(changed, ", "))
	if spec.TouchesCulprit && in.Case.Expect.Answer != nil && in.Case.Expect.Answer.Culprit != nil {
		want := in.Case.Expect.Answer.Culprit.File
		hit := false
		for _, c := range changed {
			if samePath(c, want) {
				hit = true
			}
		}
		if !hit {
			v.fail("the diff does not touch the culprit file %s", want)
		}
	}
	for _, g := range spec.MustChange {
		if !anyGlob(g, changed) {
			v.fail("the diff does not change %s", g)
		}
	}
	for _, g := range spec.MustNotChange {
		if anyGlob(g, changed) {
			v.fail("the diff must not change %s", g)
		}
	}
	if spec.Static || spec.Command == "" {
		return v
	}
	if spec.HiddenTests != "" {
		src := filepath.Join(in.EvalsRoot, "testdata", "hidden", spec.HiddenTests)
		if err := copyTree(src, in.Workspace); err != nil {
			v.fail("copy hidden tests: %v", err)
			return v
		}
	}
	timeout := spec.Timeout.D()
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var cmd *exec.Cmd
	switch {
	case in.FakeModel || in.AllowHostTests:
		cmd = exec.CommandContext(ctx, "sh", "-c", spec.Command)
		cmd.Dir = in.Workspace
		cmd.Env = hostTestEnv(in.Workspace)
	case in.SandboxImage != "" && dockerAvailable():
		cmd = exec.CommandContext(ctx, "docker", "run", "--rm", "--network", "none",
			"-v", in.Workspace+":/work", "-w", "/work", "-e", "HOME=/tmp", "-e", "GOFLAGS=-mod=mod", "-e", "GOCACHE=/tmp/gocache",
			in.SandboxImage, "sh", "-c", spec.Command)
	default:
		v.Skipped = true
		v.note("skipped: no container runtime for the model's patch (set TWEVALS_HOST_TESTS=1 to run it on the host)")
		return v
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err = cmd.Run()
	tail := out.String()
	if len(tail) > 3000 {
		tail = tail[len(tail)-3000:]
	}
	if err != nil {
		v.fail("%s failed: %v\n%s", spec.Command, err, tail)
		return v
	}
	v.note("%s passed", spec.Command)
	return v
}

func hostTestEnv(work string) []string {
	home := filepath.Join(work, ".twevals-home")
	_ = os.MkdirAll(home, 0o755)
	env := []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "TZ=UTC", "LANG=C.UTF-8", "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local"}
	for _, k := range []string{"GOCACHE", "GOMODCACHE", "GOPATH"} {
		if v := os.Getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	if os.Getenv("GOCACHE") == "" {
		if d, err := os.UserCacheDir(); err == nil {
			env = append(env, "GOCACHE="+filepath.Join(d, "go-build"))
		}
	}
	return env
}

func dockerAvailable() bool {
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}
	return exec.Command("docker", "info").Run() == nil
}

func changedFiles(work string) ([]string, error) {
	run := func(args ...string) ([]string, error) {
		out, err := workspaceGit(work, args...).Output()
		if err != nil {
			return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		var lines []string
		for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if l != "" && !strings.HasPrefix(l, ".twevals") {
				lines = append(lines, l)
			}
		}
		return lines, nil
	}
	mod, err := run("diff", "--name-only", "HEAD")
	if err != nil {
		return nil, err
	}
	untracked, err := run("ls-files", "-o", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	return append(mod, untracked...), nil
}

func anyGlob(g string, files []string) bool {
	for _, f := range files {
		if ok, _ := path.Match(g, f); ok || globAny(g, f) || f == g {
			return true
		}
	}
	return false
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, raw, 0o644)
	})
}
