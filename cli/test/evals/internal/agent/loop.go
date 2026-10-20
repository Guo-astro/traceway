package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tracewayapp/traceway/cli/test/evals/internal/catalog"
	"github.com/tracewayapp/traceway/cli/test/evals/internal/record"
	"github.com/tracewayapp/traceway/cli/test/evals/internal/tools"
)

// Config is one agent run.
type Config struct {
	Model      *catalog.Model
	Chat       Chatter
	Tools      []tools.Tool
	System     string
	Prompt     string
	SubmitTool string
	Budget     catalog.Budget
	Transcript *Transcript
	// AfterCall runs harness hooks after each executed call.
	AfterCall func(ctx context.Context, c *record.Call)
	// Questions returns the ask_user exchanges at the end.
	Questions func() []record.Question
	// SkillLoaded returns the skills loaded during the run.
	SkillLoaded func() []string
}

// Stop reasons.
const (
	StopSubmitted = "submitted"
	StopBudget    = "budget_exhausted"
	StopNoSubmit  = "no_submit"
	StopInfra     = "infra_error"
)

// Run drives the model until it submits, a budget runs out, or it stops
// without submitting (after one nudge).
func Run(ctx context.Context, cfg Config) *record.Run {
	budget := cfg.Budget.Defaults()
	ctx, cancel := context.WithTimeout(ctx, budget.Timeout.D())
	defer cancel()

	run := &record.Run{Prompt: cfg.Prompt, Started: time.Now().UTC()}
	defer func() { run.Elapsed = time.Since(run.Started) }()

	byName := map[string]tools.Tool{}
	defs := make([]ToolDef, 0, len(cfg.Tools))
	for _, t := range cfg.Tools {
		s := t.Spec()
		byName[s.Name] = t
		defs = append(defs, ToolDef{Type: "function", Function: FunctionDef{Name: s.Name, Description: s.Description, Parameters: s.Parameters}})
	}
	sort.SliceStable(defs, func(i, j int) bool { return defs[i].Function.Name < defs[j].Function.Name })

	msgs := []Message{{Role: "system", Content: cfg.System}, {Role: "user", Content: cfg.Prompt}}
	cfg.Transcript.Event("start", map[string]any{"model": cfg.Model.ID, "system": cfg.System, "prompt": cfg.Prompt, "tools": defs})

	nudged := false
	callN := 0
	outputTokens := 0
	providers := map[string]bool{}
	finish := func(reason, detail string) *record.Run {
		run.StopReason, run.StopDetail = reason, detail
		if cfg.Questions != nil {
			run.Questions = cfg.Questions()
		}
		if cfg.SkillLoaded != nil {
			run.SkillLoaded = cfg.SkillLoaded()
		}
		for p := range providers {
			run.Providers = append(run.Providers, p)
		}
		sort.Strings(run.Providers)
		cfg.Transcript.Event("stop", map[string]any{"reason": reason, "detail": detail, "usage": run.Usage, "turns": run.Turns})
		return run
	}

	for {
		if run.Turns >= budget.MaxTurns {
			return finish(StopBudget, fmt.Sprintf("max_turns %d reached", budget.MaxTurns))
		}
		run.Turns++
		req := &Request{
			Model:      cfg.Model.ID,
			Messages:   applyCaching(cfg.Model.Caching, msgs),
			Tools:      defs,
			ToolChoice: "auto",
			MaxTokens:  cfg.Model.MaxTokensPerTurn,
			Usage:      map[string]any{"include": true},
		}
		if cfg.Model.Temperature != nil {
			req.Temperature = cfg.Model.Temperature
		}
		if len(cfg.Model.Provider.Order) > 0 || cfg.Model.Provider.AllowFallbacks != nil {
			req.Provider = &ProviderPrefs{Order: cfg.Model.Provider.Order, AllowFallbacks: cfg.Model.Provider.AllowFallbacks, RequireParameters: true}
		}
		if cfg.Model.ReasoningEffort != "" {
			req.Reasoning = map[string]any{"effort": cfg.Model.ReasoningEffort}
		}
		resp, err := cfg.Chat.Chat(ctx, req)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return finish(StopBudget, "wall-clock timeout")
			}
			return finish(StopInfra, err.Error())
		}
		u := usageOf(resp)
		run.Usage.Add(u)
		outputTokens += u.OutputTokens - u.ReasoningTokens
		if resp.Provider != "" {
			providers[resp.Provider] = true
		}
		msg := resp.Choices[0].Message
		msg.Role = "assistant"
		cfg.Transcript.Event("response", map[string]any{"turn": run.Turns, "id": resp.ID, "provider": resp.Provider, "model": resp.Model,
			"finish_reason": resp.Choices[0].FinishReason, "content": msg.Content, "tool_calls": msg.ToolCalls, "usage": u})
		if text, ok := msg.Content.(string); ok && strings.TrimSpace(text) != "" {
			run.FinalText = text
		}
		// The assistant message, tool calls included, must precede the
		// tool results or Anthropic models reject the history.
		msgs = append(msgs, msg)

		if len(msg.ToolCalls) == 0 {
			if nudged {
				return finish(StopNoSubmit, "stopped without calling "+cfg.SubmitTool)
			}
			nudged = true
			msgs = append(msgs, Message{Role: "user", Content: fmt.Sprintf("You have not finished: call %s with your final answer now.", cfg.SubmitTool)})
			continue
		}
		if outputTokens > budget.MaxOutputTokens {
			return finish(StopBudget, fmt.Sprintf("max_output_tokens %d exceeded", budget.MaxOutputTokens))
		}

		final := false
		for _, tc := range msg.ToolCalls {
			callN++
			id := fmt.Sprintf("c%02d", callN)
			c := &record.Call{ID: id, Turn: run.Turns, Tool: tc.Function.Name, Args: json.RawMessage(nonEmpty(tc.Function.Arguments))}
			var res tools.Result
			switch {
			case final:
				res = tools.Result{Output: "not executed: the task was already submitted"}
				c.Rejected = true
			case callN > budget.MaxToolCalls && !isSubmit(tc.Function.Name):
				res = tools.Result{Output: fmt.Sprintf("not executed: the tool-call budget (%d) is used up. Call %s now.", budget.MaxToolCalls, cfg.SubmitTool)}
				c.Rejected = true
			default:
				t, ok := byName[tc.Function.Name]
				if !ok {
					res = tools.Result{Output: fmt.Sprintf("error: there is no tool named %q", tc.Function.Name)}
				} else {
					started := time.Now()
					res = t.Call(ctx, c.Args)
					c.DurationMs = time.Since(started).Milliseconds()
				}
			}
			c.Output, c.OK, c.Truncated, c.Ops = res.Output, res.OK, res.Truncated, res.Ops
			run.Calls = append(run.Calls, c)
			cfg.Transcript.Event("tool", map[string]any{"call": c})
			if !c.Rejected && cfg.AfterCall != nil {
				cfg.AfterCall(ctx, c)
			}
			if res.Final {
				final = true
				run.Submission = res.Submission
				run.SubmitTool = tc.Function.Name
				run.SubmitCall = id
			}
			content := fmt.Sprintf("[call %s]\n%s", id, res.Output)
			if !res.Final {
				content += fmt.Sprintf("\n[budget: %d tool calls and %d turns left]", max(0, budget.MaxToolCalls-callN), max(0, budget.MaxTurns-run.Turns))
			}
			msgs = append(msgs, Message{Role: "tool", ToolCallID: tc.ID, Content: content})
		}
		if final {
			return finish(StopSubmitted, "")
		}
		if ctx.Err() != nil {
			return finish(StopBudget, "wall-clock timeout")
		}
	}
}

func isSubmit(name string) bool {
	return name == tools.SubmitPerf || name == tools.SubmitDebug || name == tools.Finish
}

func nonEmpty(s string) string {
	if strings.TrimSpace(s) == "" {
		return "{}"
	}
	return s
}

func usageOf(r *Response) record.Usage {
	return record.Usage{
		InputTokens:      r.Usage.PromptTokens,
		CachedTokens:     r.Usage.PromptTokensDetails.CachedTokens,
		CacheWriteTokens: r.Usage.PromptTokensDetails.CacheWriteTokens,
		OutputTokens:     r.Usage.CompletionTokens,
		ReasoningTokens:  r.Usage.CompletionTokensDetails.ReasoningTokens,
		CostUSD:          r.Usage.Cost,
	}
}
