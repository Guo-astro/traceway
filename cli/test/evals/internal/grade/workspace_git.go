package grade

import (
	"os"
	"os/exec"
	"path/filepath"
)

// workspaceGit runs git in a workspace the agent has written to, trusting no
// git config found there: the setup suite's container shell can write
// .git/config, and core.fsmonitor or core.hooksPath would then run a program
// on the host as soon as git reads the index. The repository is pinned to the
// workspace's own .git, and only built-in defaults apply otherwise.
func workspaceGit(work string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", append([]string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null"}, args...)...)
	cmd.Dir = work
	cmd.Env = []string{"HOME=" + work, "PATH=" + os.Getenv("PATH"), "GIT_DIR=" + filepath.Join(work, ".git"), "GIT_WORK_TREE=" + work,
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_OPTIONAL_LOCKS=0"}
	return cmd
}
