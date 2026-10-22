import Link from "next/link";
import type { Metadata } from "next";
import { ArrowRight } from "lucide-react";

import { SectionHead } from "@/components/section-head";
import { StatsStrip } from "@/components/stats-strip";
import { FinalCTA } from "@/components/final-cta";
import { AuroraBackground } from "@/components/aurora-background";
import { BenchmarkCaseMatrix } from "@/components/benchmark-case-matrix";
import {
  BenchmarkComparisonTable,
  type ComparisonRow,
  type ComparisonValue,
} from "@/components/benchmark-comparison-table";
import {
  BenchmarkIndexChart,
  type IndexView,
} from "@/components/benchmark-index-chart";
import {
  BenchmarkCostChart,
  type CostPoint,
} from "@/components/benchmark-cost-chart";
import { BENCHMARK } from "@/lib/benchmark-data";
import {
  MODEL_COLOR,
  formatPp,
  formatUsd,
  numberWord,
  rate,
} from "@/lib/benchmark-format";
import type { Model, ModelKey } from "@/lib/benchmarks";

const { run, models, suites, setups, comparisons, groups, knownIssues, cases } =
  BENCHMARK;

const modelByKey = Object.fromEntries(models.map((m) => [m.key, m])) as Record<
  ModelKey,
  Model
>;
const setupLabels = Object.fromEntries(setups.map((s) => [s.arm, s.label]));
const suiteLabel = Object.fromEntries(suites.map((s) => [s.id, s.label]));
const groupById = Object.fromEntries(groups.map((g) => [g.id, g]));
const capitalize = (s: string) => s[0].toUpperCase() + s.slice(1);
// A batch in prose: "debug ran on 2026-10-22 at commit 21bbe026", or for a
// batch narrowed to the setups run again after a fix, "the debug CLI + skill
// runs ran again on ... at commit ...".
const partText = (p: (typeof run.parts)[number], withCommit: boolean) => {
  const forModels = p.models ? ` for ${joinList(p.models.map((k) => modelByKey[k].short))}` : "";
  if (!p.arms) return `${joinList(p.suites)}${forModels} ${withCommit ? "ran " : ""}on ${p.date}${withCommit ? ` at commit ${p.commit}` : ""}`;
  const labels = p.arms.map((a) => setupLabels[a] ?? a);
  return `the ${joinList(p.suites)} ${joinList(labels)} runs${forModels} ${withCommit ? "ran again" : "again"} on ${p.date}${withCommit ? `, with the skill fix of commit ${p.commit}` : ""}`;
};
const regradedText = run.regraded
  ? ` Every saved transcript was scored on ${run.regraded} by the same graders: after a grader fix the transcripts are scored again, so no number here mixes two versions of a grader.`
  : "";
const runDates = [...new Set(run.parts.map((p) => p.date))].sort();
const totalCases = suites.reduce((n, s) => n + s.cases, 0);
const joinList = (items: string[]) =>
  items.length < 2
    ? items.join("")
    : `${items.slice(0, -1).join(", ")} and ${items[items.length - 1]}`;
const modelCount = numberWord(models.length);

export const metadata: Metadata = {
  title: "Traceway AI Benchmarks",
  description: `How well do AI agents debug production with Traceway? ${totalCases} seeded production failures, ${run.runsPerModel} runs per model, ${modelCount} models (${models.map((m) => m.label).join(", ")}), graded by deterministic code. Pass rates, costs and every case.`,
};

const CARD_STYLE = {
  background: "var(--ink-0)",
  border: "1px solid var(--hair-2)",
  boxShadow:
    "0 1px 2px rgba(10,14,24,0.04), 0 12px 30px -18px rgba(10,14,24,0.14)",
};

function totalsOf(key: ModelKey) {
  return modelByKey[key].totals;
}

function setupResult(suite: string, arm: string, key: ModelKey) {
  return setups.find((s) => s.suite === suite && s.arm === arm)!.results[key];
}

function pickBy(score: (m: Model) => number, best: "max" | "min") {
  return models.reduce((a, b) =>
    best === "max"
      ? score(b) > score(a)
        ? b
        : a
      : score(b) < score(a)
        ? b
        : a,
  );
}

const best = pickBy((m) => m.totals.pass, "max");
const costPerPass = (m: Model) => m.totals.costUsd / m.totals.pass;
const value = pickBy(costPerPass, "min");
// The next cheapest model per passing run, which is what the value pick is
// measured against.
const runnerUp = models
  .filter((m) => m.key !== value.key)
  .sort((a, b) => costPerPass(a) - costPerPass(b))[0];
const distinctSetups = new Set(setups.map((s) => s.arm)).size;
const hasSuite = (id: string) => suites.some((s) => s.id === id);
const upliftOf = (key: ModelKey, suite = "perf") =>
  comparisons.skillUplift.find((u) => u.suite === suite && u.model === key)!;
const upliftSuites = [...new Set(comparisons.skillUplift.map((u) => u.suite))];
// One row per (suite, CLI arm) pair the harness compares. A pair carried by a
// single case (debug-no-writes on the write arms) is kept in the data but not
// shown as a difference: one run is not a rate.
const cliMcpPairs = [
  ...new Map(comparisons.cliVsMcp.map((c) => [`${c.suite} ${c.cliArm}`, c])).values(),
].filter(
  (c) =>
    setups.find((s) => s.suite === c.suite && s.arm === c.cliArm)!.results[models[0].key].runs >= 3,
);
// The uplift badge goes to a model only when it leads on every suite that
// has a control arm; the finding below gives the per-suite numbers.
const upliftLeaders = upliftSuites.map((suite) =>
  pickBy((m) => upliftOf(m.key, suite).pp, "max"),
);
const biggestUplift =
  upliftLeaders.length && upliftLeaders.every((m) => m.key === upliftLeaders[0].key)
    ? upliftLeaders[0]
    : null;
const cheapest = pickBy((m) => m.totals.costUsd, "min");
const priciest = pickBy((m) => m.totals.costUsd, "max");

const BADGES: Partial<Record<ModelKey, string[]>> = {};
const badgeOf: [Model | null, string][] = [
  [best, "Highest pass rate"],
  [value, "Lowest cost per pass"],
  [biggestUplift, "Biggest skill uplift"],
];
for (const [m, label] of badgeOf) if (m) (BADGES[m.key] ??= []).push(label);

const controls = groupById["perf-ctl"];
const byControls = [...models].sort(
  (a, b) => controls.results[b.key].pass - controls.results[a.key].pass,
);
const mcpPerf = (key: ModelKey) =>
  comparisons.cliVsMcp.find((c) => c.suite === "perf" && c.model === key)!;
const mcpFavoured = models.filter((m) => mcpPerf(m.key).pp < 0);
const mcpGapText = (a: ModelKey, b: ModelKey) => {
  const gap = mcpPerf(b).mcp - mcpPerf(a).mcp;
  if (gap === 0) return `level with ${modelByKey[b].short}`;
  return `${Math.abs(gap)}pp ${gap > 0 ? "behind" : "ahead of"} ${modelByKey[b].short}`;
};
const upliftOrder = [...models].sort(
  (a, b) => upliftOf(b.key).pp - upliftOf(a.key).pp,
);
// The model with the lowest perf pass rate on the CLI without the skill.
const weakestWithoutSkill = pickBy(
  (m) => rate(setupResult("perf", "cli-noskill", m.key).pass, setupResult("perf", "cli-noskill", m.key).runs),
  "min",
);
const controlsClean = (m: Model) => controls.results[m.key].pass === controls.results[m.key].runs;

const suitePasses = (suiteId: string, key: ModelKey) =>
  setups
    .filter((s) => s.suite === suiteId)
    .reduce((n, s) => n + s.results[key].pass, 0);
const suiteRuns = (suiteId: string, key: ModelKey) =>
  setups
    .filter((s) => s.suite === suiteId)
    .reduce((n, s) => n + s.results[key].runs, 0);
const byDebug = hasSuite("debug")
  ? [...models].sort(
      (a, b) => suitePasses("debug", b.key) - suitePasses("debug", a.key),
    )
  : [];
const debugMargin =
  byDebug.length > 1
    ? suitePasses("debug", byDebug[0].key) - suitePasses("debug", byDebug[1].key)
    : 0;
const groupLeaders = (id: string) => {
  const g = groupById[id];
  const top = Math.max(...models.map((m) => g.results[m.key].pass));
  return models.filter((m) => g.results[m.key].pass === top);
};
const groupCounts = (id: string) =>
  joinList(
    models.map(
      (m) => `${m.short} ${groupById[id].results[m.key].pass}/${groupById[id].results[m.key].runs}`,
    ),
  );
const noWrites = cases.find((c) => c.id === "debug-no-writes");
const noWritesRuns = noWrites
  ? models.flatMap((m) =>
      Object.entries(noWrites.runs[m.key]).map(([arm, r]) => ({ model: m, arm, run: r })),
    )
  : [];
const noWritesPassed = noWritesRuns.filter((x) => x.run.status === "pass");
const noWritesTouched = noWritesRuns.filter((x) => x.run.graders.includes("state"));
const GRADER_NAME: Record<string, string> = {
  answer: "answer",
  grounding: "grounding",
  trajectory: "trajectory",
  report: "report",
  state: "state",
  hidden_tests: "hidden-test",
};
const noWritesFailedOn = [
  ...new Set(
    noWritesRuns
      .filter((x) => x.run.status !== "pass")
      .flatMap((x) => x.run.graders.map((g) => GRADER_NAME[g] ?? g)),
  ),
];
const whereRan = (xs: typeof noWritesRuns) =>
  joinList(xs.map((x) => `${x.model.short} on the ${(setupLabels[x.arm] ?? x.arm).replace(/^Write /, "write ")}`));

// A setup with one or two cases is a coin flip, not a rate: it stays in the
// tables and out of the charts.
const chartSetups = setups.filter((s) => s.results[models[0].key].runs >= 3);
const unchartedSetups = setups.length - chartSetups.length;

const costPoints: CostPoint[] = [
  ...chartSetups.flatMap((s) =>
    models.map((m) => ({
      id: `${s.suite}-${s.arm}-${m.key}`,
      model: m.key,
      modelLabel: m.label,
      modelShort: m.short,
      label: `${suiteLabel[s.suite]} · ${s.label}`,
      pass: s.results[m.key].pass,
      runs: s.results[m.key].runs,
      costPerRunUsd: s.results[m.key].costPerRunUsd,
      overall: false,
    })),
  ),
  ...models.map((m) => ({
    id: `all-${m.key}`,
    model: m.key,
    modelLabel: m.label,
    modelShort: m.short,
    label: `All ${m.totals.runs} runs`,
    pass: m.totals.pass,
    runs: m.totals.runs,
    costPerRunUsd: m.totals.costPerRunUsd,
    overall: true,
  })),
];

const maxCost = Math.max(...models.map((m) => m.totals.costUsd));
const issueRuns = (key: ModelKey) =>
  cases.flatMap((c) => Object.values(c.runs[key])).filter((r) => r.knownIssue).length;
const KIND_WORD: Record<string, string> = { case: "cases", skill: "skill", harness: "harness" };
const issueKinds = Object.keys(KIND_WORD)
  .filter((k) => knownIssues.some((i) => i.kind === k))
  .map((k) => KIND_WORD[k]);
const issueGroups = groups
  .filter((g) => knownIssues.some((i) => cases.find((c) => c.id === i.case)?.group === g.id))
  .map((g) => g.label);

const groupRightAnswer = (groupId: string, key: ModelKey) =>
  cases
    .filter((c) => c.group === groupId)
    .flatMap((c) => Object.values(c.runs[key]))
    .filter((r) => r.kind === "pass" || r.kind === "trajectory").length;

const indexViews: IndexView[] = [
  {
    id: "overall",
    tab: "All runs",
    title: `by model, all ${run.runsPerModel} runs`,
    bars: models.map((m) => ({
      id: m.key,
      model: m.key,
      weights: m.weights,
      label: m.label,
      detail: m.vendor,
      pass: m.totals.pass,
      rightAnswerEvidence: m.totals.rightAnswerEvidence,
      runs: m.totals.runs,
    })),
  },
  {
    id: "setup",
    tab: "By suite and setup",
    title: "by suite and setup",
    bars: chartSetups.flatMap((s) =>
      models.map((m) => ({
        id: `${s.suite}-${s.arm}-${m.key}`,
        model: m.key,
        weights: m.weights,
        label: m.short,
        detail: `${suiteLabel[s.suite]} · ${s.label}`,
        pass: s.results[m.key].pass,
        rightAnswerEvidence: s.results[m.key].rightAnswerEvidence,
        runs: s.results[m.key].runs,
      })),
    ),
  },
  {
    id: "group",
    tab: "By case group",
    title: "by case group",
    bars: groups.flatMap((g) =>
      models.map((m) => ({
        id: `${g.id}-${m.key}`,
        model: m.key,
        weights: m.weights,
        label: m.short,
        detail: g.label,
        pass: g.results[m.key].pass,
        rightAnswerEvidence: groupRightAnswer(g.id, m.key),
        runs: g.results[m.key].runs,
      })),
    ),
  },
];

const indexSubtitle = `Traceway agent evals, run on ${joinList(runDates)}. ${joinList(
  suites.map((s) => `${s.label} (${s.cases} cases)`),
)} on ${distinctSetups} setups, ${run.runsPerCase} run per case, graded by deterministic code.${
  unchartedSetups
    ? ` The ${numberWord(unchartedSetups)} setups with fewer than three cases are in the tables, not the charts.`
    : ""
}`;

const byPass = [...models].sort((a, b) => b.totals.pass - a.totals.pass);
const pctText = (pass: number, runs: number) =>
  `${((100 * pass) / runs).toFixed(1)}%`;
const passValue = (
  r: { pass: number; runs: number },
  note?: string,
): ComparisonValue => ({
  value: r.pass / r.runs,
  text: pctText(r.pass, r.runs),
  note,
  title: `${r.pass} of ${r.runs} runs`,
});
const perModelRow = (
  label: string,
  sub: string,
  f: (key: ModelKey) => ComparisonValue[],
  better: "higher" | "lower" = "higher",
): ComparisonRow => ({
  label,
  sub,
  better,
  values: Object.fromEntries(models.map((m) => [m.key, f(m.key)])) as Record<
    ModelKey,
    ComparisonValue[]
  >,
});
const suiteCases = (id: string) => suites.find((s) => s.id === id)!.cases;
const armCases = (suite: string, arm: string) =>
  setupResult(suite, arm, models[0].key).runs;
// "13 of 14 cases" when a setup does not run every case of its suite.
const casesText = (suite: string, arm: string) => {
  const n = armCases(suite, arm);
  const all = suiteCases(suite);
  return n === all ? `${n} cases` : `${n} of ${all} cases`;
};
const GROUP_ORDER = [
  "perf-ctl",
  "perf-db",
  "perf-app",
  "perf-net",
  "perf-cache",
  "perf-infra",
  "perf-xs",
  "debug-backend",
  "debug-mobile",
  "debug-ctl",
  "setup",
].filter((id) => groupById[id]);
const smallestGroup = Math.min(
  ...groups.map((g) => g.results[models[0].key].runs),
);
const comparisonRows: ComparisonRow[] = [
  perModelRow(
    "Performance diagnosis",
    `perf · ${suiteCases("perf")} cases, CLI`,
    (k) => [
      passValue(setupResult("perf", "cli+skill", k), "with the skill"),
      passValue(setupResult("perf", "cli-noskill", k), "without the skill"),
    ],
  ),
  perModelRow(
    "Performance diagnosis over MCP",
    `perf · ${suiteCases("perf")} cases, MCP server`,
    (k) => [passValue(setupResult("perf", "mcp", k))],
  ),
  ...(hasSuite("debug")
    ? [
        perModelRow(
          "Root-cause debugging",
          `debug · ${casesText("debug", "cli+skill")}, CLI`,
          (k) => [
            passValue(setupResult("debug", "cli+skill", k), "with the skill"),
            passValue(setupResult("debug", "cli-noskill", k), "without the skill"),
          ],
        ),
        perModelRow(
          "Root-cause debugging over MCP",
          `debug · ${casesText("debug", "mcp")}, MCP server`,
          (k) => [passValue(setupResult("debug", "mcp", k))],
        ),
      ]
    : []),
  perModelRow(
    "Safe write actions",
    `write · ${suiteCases("write")} cases`,
    (k) => [
      passValue(setupResult("write", "cli-write+skill", k), "write CLI + skill"),
      passValue(setupResult("write", "mcp-write", k), "write MCP server"),
    ],
  ),
  ...GROUP_ORDER.map((id) => {
    const g = groupById[id];
    return perModelRow(
      g.label,
      `${g.code} · ${g.cases} cases, ${g.results[models[0].key].runs} runs`,
      (k) => [passValue(g.results[k])],
    );
  }),
  perModelRow("Overall pass rate", `all ${run.runsPerModel} runs`, (k) => [
    passValue(totalsOf(k)),
  ]),
  perModelRow(
    "Right answer and evidence",
    "trajectory check not counted",
    (k) => [
      passValue({
        pass: totalsOf(k).rightAnswerEvidence,
        runs: totalsOf(k).runs,
      }),
    ],
  ),
  perModelRow(
    "Agent spend",
    `all ${run.runsPerModel} runs, lower is better`,
    (k) => [
      { value: totalsOf(k).costUsd, text: formatUsd(totalsOf(k).costUsd) },
    ],
    "lower",
  ),
  perModelRow(
    "Cost per passing run",
    "agent spend ÷ passed runs, lower is better",
    (k) => {
      const v = totalsOf(k).costUsd / totalsOf(k).pass;
      return [{ value: v, text: formatUsd(v) }];
    },
    "lower",
  ),
];

export default function BenchmarksPage() {
  return (
    <main className="relative">
      <section className="hero hero-product relative">
        <AuroraBackground variant="hero" />
        <div className="wrap relative z-10">
          <div className="flex flex-col items-center text-center">
            <h1 className="max-w-[24ch]">
              Traceway <em>AI Benchmarks</em>
            </h1>
            <p className="hero-sub text-pretty" style={{ maxWidth: 720 }}>
              We believe you should bring your own AI. Traceway isn&apos;t here
              to sell you an agent. It lets the model you already trust reach
              your production data, on your terms. These benchmarks show how
              well different models do on our eval suite, and help us make it
              better.
            </p>
            <div className="hero-cta-row justify-center">
              <Link
                href={run.methodologyUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="btn btn-ghost"
              >
                How we measure
                <ArrowRight className="h-4 w-4" />
              </Link>
            </div>
          </div>
          <StatsStrip
            className="mx-auto max-w-4xl"
            stats={[
              { num: `<em>${run.totalRuns}</em>`, label: "Graded runs" },
              {
                num: `<em>${suites.reduce((n, s) => n + s.cases, 0)}</em>`,
                label: "Cases",
              },
              { num: `<em>${distinctSetups}</em>`, label: "Setups" },
              { num: `<em>${models.length}</em>`, label: "Models" },
            ]}
          />
        </div>
      </section>

      <div className="band-light">
        <section id="results" className="wrap scroll-mt-20 pt-16 pb-10 md:pt-20">
          <SectionHead
            eyebrow="Headline"
            title={
              <>
                Pass rate, <em>model by model.</em>
              </>
            }
            description={`Every model ran the same ${run.runsPerModel} runs. A run passes when its answer is right, its evidence is grounded, its writes are safe and, where it fixed code, the hidden tests pass. Tool-call counts are reported but never gate.`}
          />
          <BenchmarkIndexChart views={indexViews} subtitle={indexSubtitle} />
          <div className="mt-8 grid gap-5 sm:grid-cols-2 lg:grid-cols-4">
            {byPass.map((m) => (
              <ModelCard key={m.key} model={m} badges={BADGES[m.key] ?? []} />
            ))}
          </div>
        </section>

        <section id="compare" className="wrap scroll-mt-20 py-16 md:py-20">
          <SectionHead
            eyebrow="At a glance"
            title={
              <>
                Every benchmark, <em>side by side.</em>
              </>
            }
            description="Pass rates per suite, setup and case group, then what the runs cost. The best result in each row is in bold, and a shaded cell means that model led every number in the row."
          />
          <BenchmarkComparisonTable
            models={byPass.map((m) => ({
              key: m.key,
              label: m.label,
              short: m.short,
              note: m.weights === "open" ? "open weights" : m.vendor,
            }))}
            rows={comparisonRows}
          />
          <p className="mt-4 text-[0.8125rem] leading-relaxed text-fg-3">
            One run per case and setup, so a single run moves the smallest
            groups ({smallestGroup} runs) by {Math.round(100 / smallestGroup)}{" "}
            points. Open a{" "}
            <Link
              href="https://github.com/tracewayapp/traceway/pulls"
              target="_blank"
              rel="noopener noreferrer"
              className="text-blue-bright hover:underline"
            >
              PR
            </Link>{" "}
            or email{" "}
            <a
              href="mailto:dusan@tracewayapp.com"
              className="text-blue-bright hover:underline"
            >
              dusan@tracewayapp.com
            </a>{" "}
            if you&apos;d like to see your model featured.
          </p>
        </section>

        <section id="findings" className="wrap scroll-mt-20 py-16 md:py-20">
          <SectionHead
            eyebrow="Key findings"
            title={
              <>
                What the runs say, <em>and what they don&apos;t.</em>
              </>
            }
          />
          <div className="grid gap-12 md:grid-cols-2">
            <Finding
              label="01 · Judgment"
              title={
                controlsClean(byControls[0])
                  ? `${joinList(byControls.filter(controlsClean).map((m) => m.short))} ${byControls.filter(controlsClean).length === 1 ? "never false-alarms" : "never false-alarm"}.`
                  : `${byControls[0].short} false-alarms least.`
              }
            >
              The control cases are traps: a healthy endpoint, endpoints marked
              slow on purpose, a misleading p95 metric, a pasted alert.{" "}
              {byControls[0].short} passed {controls.results[byControls[0].key].pass}{" "}
              of {controls.results[byControls[0].key].runs} control runs.{" "}
              {joinList(
                byControls
                  .slice(1)
                  .map((m) => `${m.short} passed ${controls.results[m.key].pass}`),
              )}
              . The gap between the models is in judgment, not in reading
              telemetry.
            </Finding>
            <Finding label="02 · Value" title={`${value.short} is the value pick.`}>
              {rate(value.totals.pass, value.totals.runs)}% of its runs passed for{" "}
              {formatUsd(value.totals.costUsd)} of agent spend,{" "}
              {formatUsd(costPerPass(value))} per passing run. {runnerUp.short} is
              next at {formatUsd(costPerPass(runnerUp))} per passing run:{" "}
              {rate(runnerUp.totals.pass, runnerUp.totals.runs)}% passed for{" "}
              {formatUsd(runnerUp.totals.costUsd)},{" "}
              {Math.abs(runnerUp.totals.pass - value.totals.pass)}{" "}
              {runnerUp.totals.pass >= value.totals.pass ? "more" : "fewer"} passing
              runs.
              {best.key !== value.key
                ? ` ${best.short} passed ${best.totals.pass - value.totals.pass} more runs than ${value.short}, for ${Math.round(best.totals.costUsd / value.totals.costUsd)}× its spend. On MCP perf, ${value.short} is ${mcpGapText(value.key, best.key)}.`
                : null}
            </Finding>
            <Finding
              label="03 · The skill"
              title={
                upliftOrder[0].key === weakestWithoutSkill.key
                  ? "The skill helps the weakest model most."
                  : `The skill helps ${upliftOrder[0].short} most.`
              }
            >
              On the perf suite, CLI + skill against the same CLI without it:{" "}
              {joinList(
                upliftOrder.map((m) => `${m.short} ${formatPp(upliftOf(m.key).pp)}`),
              )}
              .{" "}
              {upliftSuites.includes("debug")
                ? `On the debug suite: ${joinList(
                    upliftOrder.map(
                      (m) => `${m.short} ${formatPp(upliftOf(m.key, "debug").pp)}`,
                    ),
                  )}. `
                : null}
              {mcpFavoured.length === 1
                ? `${mcpFavoured[0].short} is the only model that does better on MCP perf (${mcpPerf(mcpFavoured[0].key).mcp}%) than on CLI + skill (${mcpPerf(mcpFavoured[0].key).cli}%).`
                : mcpFavoured.length > 1
                  ? `${joinList(mcpFavoured.map((m) => `${m.short} (${mcpPerf(m.key).mcp}% vs ${mcpPerf(m.key).cli}%)`))} do better on MCP perf than on CLI + skill.`
                  : null}
            </Finding>
            <Finding label="04 · Our bugs" title="Some failures are ours.">
              {capitalize(numberWord(knownIssues.length))} problems behind
              these failures are in our own {joinList(issueKinds)}, not in the
              models. They account for{" "}
              {joinList(
                models.map(
                  (m, i) =>
                    `${issueRuns(m.key)} of ${m.short}'s ${m.totals.runs - m.totals.pass}${i === 0 ? " failures" : ""}`,
                ),
              )}
              . We still count them as failures. Here they are.
            </Finding>
            {hasSuite("debug") ? (
              <Finding
                label="05 · Fixing code"
                title={`${byDebug[0].short} passes the most debug runs${debugMargin === 1 ? ", by one" : ""}.`}
              >
                On the debug suite the agent has to find the culprit from the
                telemetry and patch it, and hidden tests check the fix.{" "}
                {joinList(
                  byDebug.map(
                    (m) =>
                      `${m.short} passed ${suitePasses("debug", m.key)} of ${suiteRuns("debug", m.key)}`,
                  ),
                )}
                . On the backend cases, where the hidden tests run against the
                patch, {joinList(groupLeaders("debug-backend").map((m) => m.short))}{" "}
                {groupLeaders("debug-backend").length === 1 ? "leads" : "lead"}:{" "}
                {groupCounts("debug-backend")}. On the mobile cases, checked
                statically: {groupCounts("debug-mobile")}.{" "}
                {noWrites
                  ? `${
                      noWritesPassed.length
                        ? `The cleanup request made with a write credential passed only for ${whereRan(noWritesPassed)}`
                        : "No model passed the cleanup request made with a write credential"
                    }; ${
                      noWritesTouched.length
                        ? `${whereRan(noWritesTouched)} changed Traceway, which the task forbids`
                        : "no model changed anything in Traceway"
                    }, and the ${noWritesPassed.length ? "other " : ""}runs failed the ${joinList(noWritesFailedOn)} check${noWritesFailedOn.length === 1 ? "" : "s"}.`
                  : null}
              </Finding>
            ) : null}
          </div>

          <div className="mt-14 grid gap-5 md:grid-cols-2">
            {knownIssues.map((issue) => (
              <div key={issue.id} className="rounded-2xl p-6" style={CARD_STYLE}>
                <div className="flex flex-wrap items-center gap-2">
                  <span className="tag">
                    {issue.kind === "skill"
                      ? "Skill bug"
                      : issue.kind === "harness"
                        ? "Harness bug"
                        : "Case bug"}
                  </span>
                  <code>{issue.case}</code>
                </div>
                <h3 className="mt-4 text-[1.0625rem] font-semibold text-fg-0">
                  {issue.title}
                </h3>
                <p className="muted mt-2 text-[0.9375rem] text-pretty">
                  {issue.detail}
                </p>
                <p className="mt-4 font-mono text-[0.75rem] text-fg-2">
                  {issue.truncated
                    ? `It hit ${issue.affectedRuns} of the ${issue.caseRuns} runs it covers, ${issue.truncated === issue.affectedRuns ? "all" : issue.truncated} on truncated output. `
                    : ""}
                  Runs hit:{" "}
                  {models.map((m) => `${m.short} ${issue.byModel[m.key]}`).join(" · ")}
                </p>
              </div>
            ))}
          </div>
        </section>

        <section id="setups" className="wrap scroll-mt-20 py-16 md:py-20">
          <SectionHead
            eyebrow="By setup"
            title={
              <>
                Pass rate by suite <em>and setup.</em>
              </>
            }
            description={`${capitalize(
              joinList(
                suites
                  .filter((s) => s.setups.includes("cli-noskill"))
                  .map((s) =>
                    armCases(s.id, "cli-noskill") === s.cases
                      ? `${s.label.toLowerCase()} cases`
                      : `${armCases(s.id, "cli-noskill")} of the ${s.cases} ${s.label.toLowerCase()} cases`,
                  ),
              ),
            )} run with the CLI and skill, the CLI alone as a control group, and the MCP server. Write cases${
              noWrites ? ", and the debug case that hands the agent a write credential," : ""
            } run with the write CLI and skill, and the write MCP server.`}
          />
          <DataTable minWidth={190 + models.length * 160}>
            <thead>
              <tr>
                <Th>Setup</Th>
                {models.map((m) => (
                  <ModelTh key={m.key} model={m} />
                ))}
              </tr>
            </thead>
            <tbody>
              {suites.map((s) => (
                <SuiteRows key={s.id} suite={s} />
              ))}
              <tr style={{ borderTop: "1px solid var(--hair-2)" }}>
                <RowHead title="All runs" sub={`${run.runsPerModel} per model`} />
                {models.map((m) => (
                  <Td key={m.key}>
                    <PassCell
                      model={m.key}
                      pass={m.totals.pass}
                      runs={m.totals.runs}
                      rightAnswerEvidence={m.totals.rightAnswerEvidence}
                      strong
                    />
                  </Td>
                ))}
              </tr>
              <SubheadRow label="Differences" span={models.length + 1} />
              {upliftSuites.map((suite) => (
                <DeltaRow
                  key={suite}
                  title={`Skill uplift, ${suite}`}
                  sub="CLI + skill minus CLI without the skill"
                  cells={models.map((m) => {
                    const u = upliftOf(m.key, suite);
                    return { key: m.key, pp: u.pp, detail: `${u.with}% vs ${u.without}%` };
                  })}
                />
              ))}
              {cliMcpPairs.map((p) => (
                <DeltaRow
                  key={`${p.suite}-${p.cliArm}`}
                  title={`CLI vs MCP, ${p.suite}`}
                  sub={`${setupLabels[p.cliArm]} minus ${setupLabels[p.mcpArm]}`}
                  cells={models.map((m) => {
                    const c = comparisons.cliVsMcp.find(
                      (x) =>
                        x.suite === p.suite && x.cliArm === p.cliArm && x.model === m.key,
                    )!;
                    return { key: m.key, pp: c.pp, detail: `${c.cli}% vs ${c.mcp}%` };
                  })}
                />
              ))}
            </tbody>
          </DataTable>
          <p className="dim mt-4 text-[0.8125rem]">
            &ldquo;Right answer + evidence&rdquo; counts the passes plus the runs
            that failed only the trajectory check: their answer, evidence,
            report, writes and hidden tests all passed. The differences are in
            percentage points, computed before rounding.
          </p>
        </section>

        <section id="groups" className="wrap scroll-mt-20 py-16 md:py-20">
          <SectionHead
            eyebrow="By case group"
            title={
              <>
                Where each model <em>wins and loses.</em>
              </>
            }
            description={`Passes over every setup a case ran on. Groups follow the case names: perf-db-n-plus-1 is in Database, perf-ctl-healthy in Controls and traps${
              hasSuite("debug") ? ", and the debug groups follow the stack the fix is in" : ""
            }.`}
          />
          <DataTable minWidth={340 + models.length * 160}>
            <thead>
              <tr>
                <Th>Group</Th>
                {models.map((m) => (
                  <ModelTh key={m.key} model={m} />
                ))}
              </tr>
            </thead>
            <tbody>
              {groups.map((g) => {
                const top = Math.max(...models.map((m) => g.results[m.key].pass));
                return (
                  <tr key={g.id} style={{ borderTop: "1px solid var(--hair)" }}>
                    <RowHead
                      title={g.label}
                      code={g.code}
                      sub={`${g.cases} cases · ${g.results[models[0].key].runs} runs per model`}
                      description={g.description}
                    />
                    {models.map((m) => (
                      <Td key={m.key}>
                        <PassCell
                          model={m.key}
                          pass={g.results[m.key].pass}
                          runs={g.results[m.key].runs}
                          strong={g.results[m.key].pass === top}
                        />
                      </Td>
                    ))}
                  </tr>
                );
              })}
            </tbody>
          </DataTable>
          <p className="dim mt-4 text-[0.8125rem]">
            The group&apos;s best count is set in bold. Groups hold{" "}
            {Math.min(...groups.map((g) => g.cases))} to{" "}
            {Math.max(...groups.map((g) => g.cases))} cases, so a single run
            moves a small group a lot. {joinList(issueGroups)} include the
            case and skill problems listed under Key findings.
          </p>
        </section>

        <section id="cost" className="wrap scroll-mt-20 py-16 md:py-20">
          <SectionHead
            eyebrow="Cost and efficiency"
            title={
              <>
                What a diagnosis <em>costs.</em>
              </>
            }
            description={`Agent spend is the model cost the harness recorded for each run, through OpenRouter. The judge's spend is separate. ${priciest.short} cost ${Math.round(priciest.totals.costUsd / cheapest.totals.costUsd)}× as much as ${cheapest.short} for the same ${run.runsPerModel} runs.`}
          />

          <div className="cost-wrap" data-cost-mount>
            {models.map((m) => (
              <div key={m.key} className="cost-bars">
                <div className="cost-name">
                  <span
                    aria-hidden
                    className="inline-block size-2.5 shrink-0 rounded-full"
                    style={{ background: MODEL_COLOR[m.key] }}
                  />
                  {m.short}
                </div>
                <div className="cost-bar h-6">
                  <div
                    className="fill"
                    data-w={((100 * m.totals.costUsd) / maxCost).toFixed(2)}
                    style={{
                      width: `${((100 * m.totals.costUsd) / maxCost).toFixed(2)}%`,
                      minWidth: 3,
                      background: MODEL_COLOR[m.key],
                      borderRadius: "0 4px 4px 0",
                    }}
                  />
                </div>
                <div className="cost-total">
                  <em>{formatUsd(m.totals.costUsd)}</em>
                  <span className="block text-[0.75rem] text-fg-3">
                    {formatUsd(m.totals.costPerRunUsd)} per run
                  </span>
                </div>
              </div>
            ))}
          </div>

          <div className="mt-14 rounded-2xl p-5 sm:p-7" style={CARD_STYLE}>
            <h3 className="text-[1.0625rem] font-semibold text-fg-0">
              Cost per run against pass rate
            </h3>
            <p className="muted mt-1 text-[0.9375rem]">
              Up is better, left is cheaper. Hover or focus a dot for its
              numbers; every value is also in the tables on this page.
            </p>
            <div className="mt-5">
              <BenchmarkCostChart
                points={costPoints}
                models={models.map((m) => ({ key: m.key, label: m.label }))}
              />
            </div>
          </div>

          <div className="mt-10">
            <DataTable minWidth={190 + models.length * 130}>
              <thead>
                <tr>
                  <Th>All {run.runsPerModel} runs</Th>
                  {models.map((m) => (
                    <ModelTh key={m.key} model={m} />
                  ))}
                </tr>
              </thead>
              <tbody>
                <MetricRow label="Agent spend" values={models.map((m) => formatUsd(m.totals.costUsd))} />
                <MetricRow label="Average cost per run" values={models.map((m) => formatUsd(m.totals.costPerRunUsd))} />
                <MetricRow label="Cost per passing run" values={models.map((m) => formatUsd(m.totals.costUsd / m.totals.pass))} />
                <MetricRow label="Average tool calls per run" values={models.map((m) => m.totals.avgToolCalls.toFixed(1))} />
                <MetricRow label="Average wall time per run" values={models.map((m) => `${m.totals.avgWallS.toFixed(1)}s`)} />
                <MetricRow label="Cache hit rate (input tokens)" values={models.map((m) => `${Math.round(100 * m.totals.cacheHitRate)}%`)} />
                <MetricRow
                  label="Average judge score (0–1)"
                  values={models.map((m) =>
                    m.totals.avgJudge === null ? "–" : m.totals.avgJudge.toFixed(2),
                  )}
                />
                <MetricRow label="Judge spend" values={models.map((m) => formatUsd(m.totals.judgeCostUsd))} />
              </tbody>
            </DataTable>
            <p className="dim mt-4 text-[0.8125rem]">
              Runs went three at a time
              {run.parts.length > 1
                ? `, in ${numberWord(run.parts.length)} batches (${run.parts
                    .map((p) => partText(p, false))
                    .join("; ")}), each batch on one machine`
                : " on one machine"}
              , so wall time includes some contention.{" "}
              {models
                .filter((m) => m.totals.judged < m.totals.runs)
                .map(
                  (m) =>
                    `The judge scored ${m.totals.judged} of ${m.short}'s ${m.totals.runs} runs.`,
                )
                .join(" ")}
            </p>
          </div>
        </section>

        <section id="matrix" className="wrap scroll-mt-20 py-16 md:py-20">
          <SectionHead
            eyebrow="Every case"
            title={
              <>
                The full matrix: <em>every case, setup and model.</em>
              </>
            }
            description="One cell per run. Hover a cell for the first reason the graders gave, or click a row to read every failure of that case."
          />
          <BenchmarkCaseMatrix
            cases={cases}
            models={models.map((m) => ({ key: m.key, label: m.label, short: m.short }))}
            suites={suites.map((s) => ({ id: s.id, label: s.label, setups: s.setups }))}
            groups={groups.map((g) => ({ id: g.id, label: g.label }))}
            setupLabels={setupLabels}
            issues={knownIssues.map((i) => ({ id: i.id, kind: i.kind, title: i.title }))}
          />
        </section>
      </div>

      <section id="methodology" className="wrap scroll-mt-20 pt-20 pb-6">
        <SectionHead
          eyebrow="Methodology"
          title={
            <>
              How we <em>measure.</em>
            </>
          }
          description={`Each task is a fresh Traceway backend seeded with weeks of telemetry from a system broken in one way, plus a planted red herring. The model works with the real CLI or MCP server, and deterministic graders check its answer, its evidence and anything it changed. ${
            run.parts.length > 1
              ? capitalize(joinList(run.parts.map((p) => partText(p, true))))
              : `All runs on ${run.date}, at commit ${run.commit}`
          }; ${run.runsPerCase} run${run.runsPerCase === 1 ? "" : "s"} per case.${regradedText}`}
          cta={
            <div className="flex flex-wrap items-center gap-x-6 gap-y-3">
              <Link
                href={run.methodologyUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="btn btn-accent"
              >
                Methodology and every test case
                <ArrowRight className="h-4 w-4" />
              </Link>
              <Link
                href={run.harnessUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="font-mono text-[0.6875rem] uppercase tracking-[0.08em] text-fg-2 transition-colors hover:text-blue-bright"
              >
                The eval harness on GitHub →
              </Link>
            </div>
          }
        />
      </section>

      <FinalCTA
        title={
          <>
            Give your agent <em>production context.</em>
          </>
        }
        description="Install the skill or connect the MCP server, and let your agent take the next slow endpoint or the next crash."
        primary={{ label: "Explore Agent Skills", href: "/product/agent-skills" }}
        secondary={{ label: "See the MCP server", href: "/product/mcp" }}
      />
    </main>
  );
}

function ModelSwatch({ model, size = 10 }: { model: ModelKey; size?: number }) {
  return (
    <span
      aria-hidden
      className="inline-block shrink-0 rounded-full"
      style={{ width: size, height: size, background: MODEL_COLOR[model] }}
    />
  );
}

function ModelCard({ model, badges }: { model: Model; badges: string[] }) {
  const t = model.totals;
  const pct = rate(t.pass, t.runs);
  return (
    <div className="flex flex-col rounded-[20px] px-6 py-7" style={CARD_STYLE}>
      <div className="flex items-center gap-2.5">
        <ModelSwatch model={model.key} size={12} />
        <span className="text-[17px] font-semibold text-fg-0">{model.label}</span>
      </div>
      <p className="mt-1.5 font-mono text-[0.75rem] break-all text-fg-3">
        {model.id}
      </p>
      <div className="mt-6 flex items-baseline gap-2">
        <span className="text-[48px] leading-none font-semibold tracking-[-0.03em] text-fg-0">
          {pct}%
        </span>
        <span className="text-[0.875rem] text-fg-2">
          {t.pass} of {t.runs} runs passed
        </span>
      </div>
      <div
        className="mt-4 h-1.5 overflow-hidden rounded-full"
        style={{ background: "var(--ink-3)" }}
        role="img"
        aria-label={`${pct}% pass rate`}
      >
        <div
          className="h-full rounded-full"
          style={{ width: `${(100 * t.pass) / t.runs}%`, background: MODEL_COLOR[model.key] }}
        />
      </div>
      <dl className="mt-6 grid gap-3 text-[0.875rem]">
        <CardStat label="Right answer and evidence" value={`${t.rightAnswerEvidence}/${t.runs}`} />
        <CardStat label="Agent cost" value={formatUsd(t.costUsd)} />
        <CardStat label="Cost per run" value={formatUsd(t.costPerRunUsd)} />
      </dl>
      {badges.length ? (
        <div className="mt-6 flex flex-wrap gap-2">
          {badges.map((b) => (
            <span key={b} className="tag">
              {b}
            </span>
          ))}
        </div>
      ) : null}
    </div>
  );
}

function CardStat({ label, value }: { label: string; value: string }) {
  return (
    <div
      className="flex items-baseline justify-between gap-4 pt-3"
      style={{ borderTop: "1px solid var(--hair)" }}
    >
      <dt className="text-fg-2">{label}</dt>
      <dd className="m-0 font-mono text-fg-0 tabular-nums">{value}</dd>
    </div>
  );
}

function Finding({
  label,
  title,
  children,
}: {
  label: string;
  title: string;
  children: React.ReactNode;
}) {
  return (
    <div className="border-t border-hair pt-8">
      <p className="font-mono text-[0.6875rem] uppercase tracking-[0.14em] text-fg-3">
        {label}
      </p>
      <h3 className="mt-3">{title}</h3>
      <p className="muted mt-3 max-w-[520px] text-pretty">{children}</p>
    </div>
  );
}

function DataTable({
  minWidth,
  children,
}: {
  minWidth: number;
  children: React.ReactNode;
}) {
  return (
    <div className="overflow-x-auto rounded-2xl" style={CARD_STYLE}>
      <table className="w-full border-collapse text-left" style={{ minWidth }}>
        {children}
      </table>
    </div>
  );
}

function Th({ children }: { children: React.ReactNode }) {
  return (
    <th
      scope="col"
      className="sticky left-0 z-10 px-5 py-3.5 align-bottom font-mono text-[0.6875rem] font-medium uppercase tracking-[0.1em] text-fg-3"
      style={{ borderBottom: "1px solid var(--hair-2)", background: "var(--ink-0)" }}
    >
      {children}
    </th>
  );
}

function ModelTh({ model }: { model: Model }) {
  return (
    <th
      scope="col"
      className="px-5 py-3.5 align-bottom text-[0.8125rem] font-semibold text-fg-0"
      style={{ borderBottom: "1px solid var(--hair-2)" }}
    >
      <span className="inline-flex items-center gap-2 whitespace-nowrap">
        <ModelSwatch model={model.key} />
        {model.label}
      </span>
    </th>
  );
}

function Td({ children }: { children: React.ReactNode }) {
  return <td className="px-5 py-4 align-top">{children}</td>;
}

function RowHead({
  title,
  sub,
  code,
  description,
}: {
  title: string;
  sub?: string;
  code?: string;
  description?: string;
}) {
  return (
    <th
      scope="row"
      className="sticky left-0 z-10 min-w-[190px] px-5 py-4 align-top font-normal"
      style={{ background: "var(--ink-0)" }}
    >
      <span className="block text-[0.9375rem] font-semibold text-fg-0">
        {title}
      </span>
      {code ? (
        <span className="mt-0.5 block font-mono text-[0.75rem] text-fg-3">
          {code}
        </span>
      ) : null}
      {description ? (
        <span className="mt-1.5 block max-w-[300px] text-[0.8125rem] leading-snug text-fg-2">
          {description}
        </span>
      ) : null}
      {sub ? (
        <span className="mt-1 block font-mono text-[0.75rem] text-fg-3">{sub}</span>
      ) : null}
    </th>
  );
}

function SubheadRow({ label, span }: { label: string; span: number }) {
  return (
    <tr style={{ borderTop: "1px solid var(--hair-2)" }}>
      <th
        scope="colgroup"
        colSpan={span}
        className="px-5 pt-5 pb-1 font-mono text-[0.6875rem] font-medium uppercase tracking-[0.14em] text-fg-3"
      >
        <span className="sticky left-5 inline-block">{label}</span>
      </th>
    </tr>
  );
}

function SuiteRows({
  suite,
}: {
  suite: (typeof suites)[number];
}) {
  return (
    <>
      <SubheadRow
        label={`${suite.label} · ${suite.cases} cases`}
        span={models.length + 1}
      />
      {suite.setups.map((arm) => (
        <tr key={arm} style={{ borderTop: "1px solid var(--hair)" }}>
          <RowHead
            title={setupLabels[arm]}
            code={arm}
            sub={
              armCases(suite.id, arm) === suite.cases
                ? undefined
                : `${armCases(suite.id, arm)} of the ${suite.cases} cases`
            }
          />
          {models.map((m) => {
            const r = setupResult(suite.id, arm, m.key);
            return (
              <Td key={m.key}>
                <PassCell
                  model={m.key}
                  pass={r.pass}
                  runs={r.runs}
                  rightAnswerEvidence={r.rightAnswerEvidence}
                />
              </Td>
            );
          })}
        </tr>
      ))}
    </>
  );
}

function PassCell({
  model,
  pass,
  runs,
  rightAnswerEvidence,
  strong,
}: {
  model: ModelKey;
  pass: number;
  runs: number;
  rightAnswerEvidence?: number;
  strong?: boolean;
}) {
  return (
    <div className="min-w-[140px]">
      <div className="flex items-baseline justify-between gap-3 font-mono tabular-nums">
        <span
          className={
            strong
              ? "text-[0.9375rem] font-semibold text-fg-0"
              : "text-[0.9375rem] text-fg-1"
          }
        >
          {pass}/{runs}
        </span>
        <span className="text-[0.8125rem] text-fg-2">{rate(pass, runs)}%</span>
      </div>
      <div
        className="mt-2 h-1.5 overflow-hidden rounded-full"
        style={{ background: "var(--ink-3)" }}
      >
        <div
          className="h-full rounded-full"
          style={{ width: `${(100 * pass) / runs}%`, background: MODEL_COLOR[model] }}
        />
      </div>
      {rightAnswerEvidence !== undefined ? (
        <p className="mt-2 font-mono text-[0.6875rem] whitespace-nowrap text-fg-3 tabular-nums">
          {rightAnswerEvidence}/{runs} right answer + evidence
        </p>
      ) : null}
    </div>
  );
}

function DeltaRow({
  title,
  sub,
  cells,
}: {
  title: string;
  sub: string;
  cells: { key: ModelKey; pp: number; detail: string }[];
}) {
  return (
    <tr style={{ borderTop: "1px solid var(--hair)" }}>
      <RowHead title={title} sub={sub} />
      {cells.map((c) => (
        <Td key={c.key}>
          <span className="block font-mono text-[0.9375rem] font-semibold text-fg-0 tabular-nums">
            {formatPp(c.pp)}
          </span>
          <span className="mt-1 block font-mono text-[0.6875rem] text-fg-3 tabular-nums">
            {c.detail}
          </span>
        </Td>
      ))}
    </tr>
  );
}

function MetricRow({ label, values }: { label: string; values: string[] }) {
  return (
    <tr style={{ borderTop: "1px solid var(--hair)" }}>
      <th
        scope="row"
        className="sticky left-0 z-10 min-w-[190px] px-5 py-3.5 text-[0.9375rem] font-normal text-fg-1"
        style={{ background: "var(--ink-0)" }}
      >
        {label}
      </th>
      {values.map((v, i) => (
        <td
          key={models[i].key}
          className="px-5 py-3.5 font-mono text-[0.9375rem] text-fg-0 tabular-nums"
        >
          {v}
        </td>
      ))}
    </tr>
  );
}
