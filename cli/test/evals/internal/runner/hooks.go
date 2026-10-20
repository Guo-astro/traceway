package runner

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/tracewayapp/traceway/cli/test/evals/internal/catalog"
	"github.com/tracewayapp/traceway/cli/test/evals/internal/env"
	"github.com/tracewayapp/traceway/cli/test/evals/internal/grade"
	"github.com/tracewayapp/traceway/cli/test/evals/internal/record"
	"github.com/tracewayapp/traceway/cli/test/evals/internal/seed"
)

// hookRunner fires case hooks after matching agent calls.
type hookRunner struct {
	hooks  []catalog.Hook
	counts []int
	inst   *env.Instance
	pid    string
	sink   *seed.LiveSink
	mu     sync.Mutex
}

func newHookRunner(c *catalog.Case, inst *env.Instance, primary env.Project, sink *seed.LiveSink) *hookRunner {
	return &hookRunner{hooks: c.Hooks, counts: make([]int, len(c.Hooks)), inst: inst, pid: primary.ID, sink: sink}
}

func (h *hookRunner) after(ctx context.Context, cl *record.Call) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, hk := range h.hooks {
		times := hk.Times
		if times == 0 {
			times = 1
		}
		if h.counts[i] >= times {
			continue
		}
		hit := false
		for k := range cl.Ops {
			if cl.Ops[k].OK && grade.Matches(&cl.Ops[k], hk.After) {
				hit = true
			}
		}
		if !hit {
			continue
		}
		h.counts[i]++
		h.run(ctx, hk)
	}
}

func (h *hookRunner) run(ctx context.Context, hk catalog.Hook) {
	api := h.inst.Backend.API().WithBearer(h.inst.Admin.JWT)
	arg := func(k string) string { return fmt.Sprint(hk.Args[k]) }
	switch hk.Action {
	case "edit_post_mortem":
		// A teammate edits the document between the agent's read and write.
		// The PUT replaces the whole document (title required), so the
		// title, tags and incident link are carried over from a fresh read.
		body := map[string]any{"contentMd": arg("content")}
		if cur, err := api.Do(ctx, http.MethodGet, "/api/post-mortems/"+arg("id"), env.ProjectQuery(h.pid), nil); err == nil && cur.OK() {
			m := cur.Map()
			body["title"], body["tags"], body["incidentId"] = m["title"], m["tags"], m["incidentId"]
		}
		_, _ = api.Do(ctx, http.MethodPut, "/api/post-mortems/"+arg("id"), env.ProjectQuery(h.pid), body)
	case "stub_healthy":
		h.inst.Stub.SetHealthy(true)
	case "stub_unhealthy":
		h.inst.Stub.SetHealthy(false)
	case "sleep":
		n, _ := strconv.Atoi(arg("seconds"))
		time.Sleep(time.Duration(n) * time.Second)
	case "approve_setup_draft":
		_, _ = env.ApprovePendingDraft(ctx, h.inst.Backend, h.inst.Admin)
	}
}

// startDraftApprover approves pending setup drafts while the run lasts, as
// the user does in the dashboard after the agent submits a plan.
func startDraftApprover(ctx context.Context, c *catalog.Case, inst *env.Instance) func() {
	if c.Setup == nil || !c.Setup.ApproveDrafts {
		return func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				_, _ = env.ApprovePendingDraft(ctx, inst.Backend, inst.Admin)
			}
		}
	}()
	return cancel
}
