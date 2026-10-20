package runner

import (
	"context"
	"path/filepath"

	"github.com/tracewayapp/traceway/cli/test/evals/internal/catalog"
	"github.com/tracewayapp/traceway/cli/test/evals/internal/env"
	"github.com/tracewayapp/traceway/cli/test/evals/internal/tools"
)

// prepareSurface mints the arm's credential and connects its surface: the
// CLI logged in with a read PAT (XDG dirs outside the workspace).
func prepareSurface(ctx context.Context, opt Options, c *catalog.Case, arm string, inst *env.Instance, primary env.Project, dir string) (surface, error) {
	s := surface{submit: tools.SubmitPerf}
	if arm == catalog.ArmCLISkill {
		s.skills = []string{"traceway"}
	}
	pat, err := env.CreatePAT(ctx, inst.Backend, inst.Admin.JWT, "eval agent", "read", 0)
	if err != nil {
		return s, err
	}
	s.cliName, s.cliBin = "traceway", opt.Bins.CLI
	home, err := env.NewCLIHome(filepath.Join(dir, "cli"), s.cliBin)
	if err != nil {
		return s, err
	}
	if err := home.Login(ctx, inst.Backend.URL, pat, primary.ID); err != nil {
		return s, err
	}
	s.cliHome = home
	return s, nil
}
