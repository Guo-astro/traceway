# Traceway skill and MCP evals

**Manual only, and free.** Nothing here runs automatically: no workflow, no
`just check`/`test`/`default` recipe. This harness plays every case with a
fake model on loopback and makes no paid model calls; its Chat Completions
client refuses any non-loopback URL.

This module measures how well AI agents use Traceway through its skills. Every
run gets its own hermetic Traceway instance, the standalone backend in SQLite
mode, seeded through the real ingest endpoints. The agent works through real
`traceway` commands in a sandboxed shell, and deterministic graders score the
result.

| Suite | Measures | Arms |
|---|---|---|
| perf | Diagnosing latency against the bottleneck checklist in `performance.md` | `cli+skill`, `cli-noskill` |

`cli+skill` installs the `traceway` skill; `cli-noskill` is the same CLI with
no skill, the baseline that skill uplift is measured against.

## Running it

From `cli/`:

```
just eval-harness
```

runs `go test ./...` and then every case's oracle on every arm it lists, with
the fake model. From `cli/test/evals`, `go run ./cmd/twevals help` lists the
commands:

- `twevals list [--preset P] [--cases GLOB] [--arms A]` prints the case × arm
  combinations a selection picks.
- `twevals oracle [--all | <case-id>... | --suites S] [--arms A] [-j N]
  [--keep] [--out DIR]` plays the oracles and writes `results.json` and
  `summary.md` to `--out` (default `.cache/oracle/<timestamp>/`). It exits `1`
  when an oracle fails.

Presets (`presets.yaml`): `smoke` is the cases tagged `smoke` on the primary
arm, `core` every case on the primary arm, `full` every arm a case lists.

Requirements: Go (the `go`/`toolchain` lines of `go.mod`) and git. The
harness builds the backend and both CLI editions from this checkout into
`.cache/` (gitignored) and reuses them until the sources change.

## Reading the results

A run is `pass`, `fail`, `budget_exhausted` (counted as a fail) or
`infra_error` (excluded from pass rates).

Each run directory, `runs/<case>/<arm>/<model>/r<N>/` (with `+` in the arm
written as `_`, so `cli_skill`), holds:

- `transcript.jsonl`: every model response, every tool call with its id, op,
  argv, exit code and output;
- `run.json` and `result.json`;
- `seed.json`: the ids the seed exposed. The agent never sees it;
- the workspace, when the run failed or `--keep` was set.

Graders:

| Grader | Gates | Checks |
|---|---|---|
| `answer` | yes | The structured answer: checklist row (and not the distractor), locus, onset |
| `trajectory` | yes | Required, alternative, forbidden and ordered operations, and thrash limits, on surface-neutral operations (`internal/ops`) |
| `grounding` | yes | Cited calls exist, succeeded and were not truncated. Their output contains the deciding signal. No id in the answer was invented |
| `report` | yes | Regexps the final report must or must not match, for cases that set `expect.report` |

## Adding a case

A case is three things:

- a seed function in `internal/scenarios/<suite>/`, registered by name;
- a YAML file `cases/<suite>/<id>.yaml`;
- fixtures under `testdata/`.

`internal/suite` tests the catalog as a whole: references exist, checklist ids
are valid, and the leakage lint is clean. Done means:

1. The seed builds data with exactly one correct answer and one named
   distractor (`distractor:` in the YAML).
2. `twevals oracle <id>` passes on every arm the case lists.
3. `go test ./internal/suite/` passes.

`perf-db-n-plus-1` is the exemplar.

### The seed DSL (`internal/seed`)

```go
func init() { scenarios.Register("perf/db-n-plus-1", seedNPlusOne) }

func seedNPlusOne(s *seed.Scenario) error {
	p := s.Project("storefront", seed.OTel)                 // a real project, created through the API
	api := p.Service("orders-api", seed.Go("nethttp", "sql")).Versions("2.14.3")
	orders := api.Endpoint("GET", "/api/orders")
	orders.Traffic(seed.Daily(160), seed.Diurnal(), seed.Over(29*24*time.Hour))
	orders.Handler(func(r *seed.Req) {
		n := int(r.Lerp(6, 72) * (0.35 + 0.95*r.Float())) // grows over the window
		r.DB("SELECT * FROM orders WHERE user_id = $1 ...", r.Ms(3, 6), seed.Rows(n))
		for range n {
			r.DB("SELECT id, sku, qty, unit_price FROM order_items WHERE order_id = $1", r.Ms(6, 14))
		}
		r.Compute("encode response", r.Ms(2, 6))
	})
	s.Background(api, seed.TypicalTraffic(10)) // healthy endpoints, a benign 400, host metrics
	api.Host().CPU(seed.Flat(0.22, 0.31))
	s.Expose("slowRequest", orders.Slowest(24*time.Hour)) // for the oracle and graders, never the agent
	return nil
}
```

- **Timing is consistent by construction.** Work is laid out on a cursor.
  `r.DB`, `r.HTTP`, `r.Cache`, `r.Compute` and `r.Call(otherEndpoint, rtt)`
  each add a child span. `r.Gap(d)` is waiting time with no span. `r.Span`
  nests, and `r.Parallel` runs branches side by side. A parent's duration is
  where its cursor stops.
- **Releases** are `Versions(initial, seed.Deploy{At, Version})`, which sets
  the `service.version` resource attribute. `r.Version()` lets the handler
  change behaviour after a deploy.
- **Multiple instances** are `Instances("node-a", ...)`, with `r.Host()` and
  `HostNamed(h).CPU(...)`.
- **Traffic shapes** are `Daily`, `Diurnal`, `GrowRate`, `Burst`, `RateFunc`
  and `Over`. Use `Statuses` or `r.Status` to fail requests.
- **Logs and errors:**
  - `r.Log(sev, body, attrs...)` writes a trace-correlated OTLP log.
  - `svc.Log(at, ...)` writes a log outside any request.
  - `r.Error(seed.Exc{Type, Message, Stack})` records an exception event in
    the stack format of the service's language.
  - Every detailed request also gets an access-log line,
    `GET /path 200 1243ms`, with its trace id. That is how an agent finds a
    slow request's id, because `endpoints list` has none.
- **Metrics** come from `Host().CPU/Memory/Disk/Filesystem` (OTel
  hostmetrics names, `system.cpu.utilization` split by `state`) and from
  `svc.Gauge/Counter/Histogram(name, unit, attrs, series)`. Histograms arrive
  as `<name>.avg` and `<name>.count`. Series helpers: `Flat`, `Step`, `Ramp`,
  `Periodic`, `Sawtooth`, `Plus`, `Scale`, `Clamp`.
- **Browser and mobile SDK data** goes through `p.Crash(seed.Crash{...})`, with
  network and navigation `Actions` and console `Logs`, and `p.Session(...)`.
  It travels the native `/api/report` protocol, and covers only what those
  SDKs really send: no frame timings, no mobile metrics. `p.GroupMatching`
  reads an exception group's hash back after ingest; put work that needs the
  ingested data in `s.AfterFlush`.
- **Volume.** Requests outside the detail sample are sent as their root span
  (and any downstream server spans), so percentiles and counts stay complete
  while a scenario stays under about 20k spans. Requests with logs, errors or
  an `Expose` always keep their full tree.

Backend facts that bite (each one cost a case author an afternoon):

- Successful `GET`/`HEAD` requests to health-check routes (`/health`,
  `/healthz`, `/ping`, `/ready`, `/status`, `/up`, ...; the list is in
  `backend/app/services/healthcheck.service.go`) are dropped at ingest, which
  is the projects' default. An endpoint seeded on one never appears and
  `Verify` fails with "endpoints never became visible"; a failing one (status
  400 or more) is kept.
- `endpoints list --order-by p95|lastSeen` does not sort: the CLI sends
  `p95`/`lastSeen`, the SQLite/DuckDB repositories only know
  `p95_duration`/`last_seen`, and the list falls back to impact order. Keep
  a case's endpoint count at ten or fewer so the slow one is on the first page
  whatever the order. This is a product bug, not a harness one; the evals
  should catch the skill's reliance on it.
- A log body search over more than 24 hours answers 422 unless another filter
  (service, severity, trace) narrows it.
- Rare events confined to a short window can come out empty at some hours of
  the day (the arrival process follows the wall-clock hour). Use `Burst`, or
  seed the case at every hour of the day into `seed.NewDrySink()` and check
  the event is there before relying on it.

- Numeric OTel attributes must be integers, which the DSL does for you.
- Projects with a browser framework never get endpoints from OTLP.
- An exception's message does not split groups: vary the type or the frames
  to get distinct issues.
- Session recordings are stamped with the ingest time, so keep crashes that
  carry actions within the last ~12 hours.
- The unscoped `metrics query --name system.cpu.utilization --aggregation max`
  returns the idle state. That is a real trap the skill should steer around.

### The case YAML

See `perf-db-n-plus-1.yaml` for the full shape. The keys:

- `arms` and `invoke`: `slash` preloads SKILL.md the way a `/traceway`
  command does, and `natural` makes the agent load the skill itself. A slash
  case's prompt starts with `/traceway`, and only a slash case's does.
- `workspace.repo`: a fixture under `testdata/repos`. Fixtures can carry
  history: a `history.yaml` whose commits are overlays under `.history/`
  (only the files that differ from the fixture, plus an `absent:` list of
  files the commit lacks), committed with ages relative to the run so
  `git log` lines up with the seeded deploys.
- `prompt`, `budget`, `user`: scripted `ask_user` answers, first match wins.
- `expect.answer`, `expect.grounding`, `expect.calls`, `expect.report`.
- `judge.rubric` and `judge.expected`, the rubric for report quality. The
  harness does not score it.

Templates are resolved after seeding:

| Template | Value |
|---|---|
| `{{seed.<key>}}` | What the seed exposed |
| `{{env.url}}` | The instance URL |
| `{{env.project}}` | The primary project's id |
| `{{env.projects.<name>.id}}` | Another project's id |
| `{{env.stub}}` | The switchable stub's URL |
| `{{env.now}}` | The scenario's "now" |

A scalar that is exactly one template keeps its type.

The `oracle` is the reference trajectory the fake model plays:

- **One step is one tool call**, so steps are cited as `c01`, `c02`, ... in
  the submitted evidence.
- **`shell:` steps** run CLI commands. Use `jq` to keep cited outputs under
  the 30k-character cap: a truncated citation fails grounding.
- **`{{last.<json.path>}}` and `{{calls.c03.<path>}}`** read earlier tool
  results at play time.
- **`load_skill:` steps** play as a harmless `list_files .` on arms without
  skills (`cli-noskill`), so call ids stay the same on every arm.
- **The oracle ends** with `submit:`.

While developing a case, `TWEVALS_ONLY=<ids>` limits loading to those cases,
so an unfinished YAML file elsewhere cannot break your run.

### Leakage

The seeded strings (span names, attribute values, log bodies, stack traces,
object names) and the prompt must not name the answer. `internal/grade/leakage.go`
holds the denylist, which covers phrases like `n+1`, `stampede`,
`missing index`, `pool exhaust`, `gc pause` and `noisy neighbor`. Use real
library messages, such as SQLAlchemy's `QueuePool limit of size 5 overflow 10
reached` or Postgres's `deadlock detected`, and neutral names.

## Layout

```
models.yaml presets.yaml
cmd/twevals/           list | oracle
internal/catalog/      the case schema, presets, models.yaml
internal/checklist/    the bottleneck rows of performance.md, the perf answer enum
internal/env/          backend subprocess, registration, PATs, CLI logins, the stub
internal/seed/         the DSL, OTLP and /api/report writers, read-back verification
internal/scenarios/    one seed function per case
internal/agent/        the Chat Completions client (loopback only), the loop, prompt caching, transcripts, the fake model
internal/tools/        the sandboxed shell (mvdan.cc/sh, in-process coreutils, gojq, goawk), files, skills, ask_user, submit
internal/ops/          CLI → operation mapping, cross-checked against tool-map.md
internal/grade/        answer, trajectory, grounding, report, leakage
internal/record/       the run record: calls, operations, usage
internal/runner/       one run end to end
internal/report/       results.json and summary.md
internal/suite/        catalog-wide tests
cases/ testdata/       cases, fixture repos, oracle files
```

The agent's shell is an interpreter, never the host shell. It has the
`traceway` binary (logged in with a read PAT whose config lives outside the
workspace), `jq`, `awk`, read-only text utilities confined to the workspace,
and read-only `git`. No tool writes under a `.git` directory, git always
uses the workspace's repository, and git refuses arguments that name a file
outside the workspace or options that write files or run programs. Tool subprocesses get a scrubbed environment (`HOME`,
`XDG_*`, `PATH`, `TZ`, `LANG`), so secrets in the harness's environment
never reach them.

## Credits

The first performance scenarios were contributed by Liyu in PR #405
("feat(skills): add performance skill evals"). They were ported to this
module as `perf-db-n-plus-1`, `perf-db-missing-index`, `perf-net-sequential`,
`perf-db-pool-exhaustion`, `perf-app-sync-background`, `perf-net-large-payload`,
`perf-cache-no-cache`, `perf-infra-cpu-saturation`, `perf-cache-stampede`,
`perf-xs-ios-watchdog`, `perf-xs-android-timeouts`, `debug-android-npe` and
`debug-flutter-null-check`. The two debug cases sit in `cases/debug/` and are
not loaded yet: the harness runs the perf suite only.
