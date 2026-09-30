# Martian Code Review Bench: where we land and what to change

Research note, 2026-09-30. Source data: the three-attempt baseline from
2026-09-29 (`openai/gpt-6-luna-pro`, thinking `high`) scored with Martian's
runner at `e616e849755441da38f18bf3adba2c9583b03803`, plus the per-tool
`overall_metrics` that the runner ships in
`offline/analysis/benchmark_dashboard.json`.

## Where we land today

Core profile, published Sonnet 4.5 judge column, 22 tools:

| rank | tool | P | R | F1 | candidates/PR |
|---:|---|---:|---:|---:|---:|
| 1 | cubic-v2 | 56.5 | 60.8 | 58.5 | 3.4 |
| 2 | qodo-extended-v2 | 56.2 | 57.0 | 56.6 | 3.2 |
| 3 | augment | 52.4 | 61.4 | 56.6 | 3.7 |
| 17 | claude-code | 36.5 | 41.1 | 38.7 | 3.6 |
| 18 | claude | 38.3 | 36.1 | 37.1 | 3.0 |
| **19** | **unreal-review** | **37.1** | **32.9** | **34.8** | **2.9** |
| 20 | codeant-v2 | 32.4 | 34.8 | 33.5 | 3.4 |

The rank is 19 or 20 under all three published judges. Our volume per PR
already matches the leaders. The gap is in hit rate, not volume: cubic
produces 3.4 candidates per PR at 56.5% precision, and we produce 2.9 at 37.1%.

## Insights from the baseline

1. **Most misses are not random.** Of 173 goldens, 101 were found in no
   attempt, 27 in one or two attempts, and 45 in all three. A 3-attempt union
   lifts recall from 32.9 to 41.6 (Critical+High goes from 43.9 to 56.1).
   Sampling more gets part of the way. The other 101 need a different review.
2. **Size is not the recall problem.** Pooled recall by PR size: 29.3% on PRs
   with 10 or fewer files, 29.9% on 11 to 30, and 47.9% on over 30. Large PRs are
   blocked only by the hard limit: `sentry-greptile-5` is 252,701 bytes, 106
   files, 3,293 changed lines, and 6 goldens, and we score it as empty.
3. **Confident clean verdicts on buggy PRs.** `grafana-76186` (two High bugs),
   `sentry-80528` (one High), and `calcom-22345` came back empty in every
   attempt with a summary saying what was checked. The system prompt tells the
   agent to drop any finding whose premise it cannot confirm. That rule
   protects precision, but here it also cost recall.
4. **The note channel is almost unused.** Each attempt recorded exactly one
   `note` (the rest were about 55% `error` and 45% `warning`), yet 46 of the 173
   goldens are Low. Recall is 18.1% on Low, 0% on `test_gap`, 16.7% on `perf`,
   and 10% on `style`.
5. **Strong where the prompt points.** Concurrency (54.8%) and api (51.3%) are
   well above our mean. Those are exactly the classes the severity rubric in
   `internal/review/review.go` names. Categories the rubric never mentions
   score low.

## 1. Exact judge parity

The runner reads `MARTIAN_MODEL`, `MARTIAN_BASE_URL`, and `MARTIAN_API_KEY`
and calls the judge at `temperature: 0.0` through an OpenAI-compatible
client. It writes results to `results/<model with / replaced by _>/`. Our
run used `anthropic/claude-sonnet-4.5` through OpenRouter, so it landed in
`anthropic_claude-sonnet-4.5/`, a different directory from the published
column.

Steps:

1. Point the runner at the dated model through Anthropic's
   OpenAI-compatible endpoint:
   `MARTIAN_BASE_URL=https://api.anthropic.com/v1/`,
   `MARTIAN_MODEL=anthropic/claude-sonnet-4-5-20250929` (strip the
   `anthropic/` prefix if the endpoint rejects it, and keep the results
   directory name `anthropic_claude-sonnet-4-5-20250929`). Run steps 2, 2.5,
   and 3 for `unreal-review-a{1,2,3}` only. They append to the published
   evaluations, so every tool shares one file and one judge.
2. Calibrate the route before trusting it. Re-judge two published tools
   (for example `cubic-v2` and `claude-code`) into a scratch copy and diff them
   against the shipped `evaluations.json`. If the F1 values match within about one point, the
   route is equivalent. If not, the judge drifted and needs reporting.
3. Repeat for `anthropic_claude-opus-4-5-20251101` and `openai_gpt-5.2`, and
   report the three-judge mean the way Martian does.
4. Add a `tools/martian-plot` script that reads
   `benchmark_dashboard.json` plus our evaluations and emits the
   precision-recall scatter per judge and profile. That makes the "where do we
   land" question a single command.

Cost: steps 2 and 3 of the runner take one judge call per golden and
candidate pair. For three attempts that is on the order of 10k calls per
judge, a few dollars on Sonnet.

## 2. Review big PRs

Constraint: `oversized_diff_refused` in `LAWS.bend` says a diff over the
limit is refused. Anything below changes that product rule, so it needs
your sign-off, and the law, proof, and `spec/` must be updated in the same
change.

Recommended design: **automatic fan-out inside one review**.

- Over the limit, `review.Run` calls the existing `Groups` clustering
  (directory, family, and Go import edges, capped at 25 files and 2,000 lines)
  instead of refusing.
- Each group runs one agent with its own diff hunks and the full PR file list
  (paths and line counts only), so cross-file premises can still be opened
  and checked. The workspace already holds the whole repository.
- Groups run in parallel and write into per-group work files. The
  checkpoint stays one `findings.jsonl` bound to the full-diff SHA, and
  resume skips groups that are already complete. The schema does not change:
  the agent still records ordinary findings.
- A final, cheap "seams" pass gets the list of group summaries and the diff
  of exported or changed signatures, and looks only for cross-group contract
  breaks. This covers the one thing splitting loses.
- The summary is written once over all group summaries, so the one-line
  summary contract holds.

Cheaper alternatives, measured on `sentry-greptile-5`:

- Excluding snapshots, fixtures, and lock files saves 1% (250,383 bytes).
  That is not enough.
- `-U0` hunks bring it to 187,991 bytes, under the limit, but they strip the
  context the agent reads first. Only worth it as a fallback.
- Raising the byte limit. 200 KB is roughly 55k tokens, well under the model
  window. Raising it to about 400 KB is a one-line change plus a law update.
  Data point 2 shows no recall drop on our largest PRs, so this is a
  reasonable first step while fan-out is built.

## 3. Variance and recall

Ranked by expected gain per cost:

1. **Lens passes instead of repeat sampling.** Run K parallel passes with
   different focus prompts (correctness and logic, error handling and nil
   paths, security and data, API contract and tests), then merge. Unlike
   random resampling, this targets the 101 never-found goldens. Insight 5
   suggests the model finds what it is pointed at. Review spend is about $0.03
   per PR per pass, so K=4 costs about $6 for the full bench.
2. **Verifier pass for precision.** Candidate volume will rise, so every
   merged finding gets re-checked by a fresh agent with just that finding
   and the repository, which keeps or drops it. This replaces the blanket
   "drop what you cannot confirm" rule in the reviewer prompt. Generation can
   then run with high recall and verification guards precision. That is the
   over-generate-and-filter pattern Martian's own methodology describes.
3. **Dedup across passes** by location overlap plus a same-issue check,
   mapped onto existing `id` semantics (`mergeFindings` already collapses by
   `id`).
4. **Use the note channel.** Loosen the prompt so real but minor issues are
   recorded as `note` instead of dropped. Renderers can keep notes out of
   inline GitHub comments, so the PR experience does not get noisier.
5. **Measure variance, don't guess.** `eval --corpus martian --attempts N`
   should report the mean and spread of P, R, and F1 and the per-golden hit
   frequency (always, sometimes, never). The analysis above then becomes a
   standing report.

Suggested experiment order, each scored with the pinned runner and parity
judge on the full set, three attempts:

| step | change | expected effect |
|---|---|---|
| A | parity judge + plot | trustworthy baseline |
| B | loosen drop rule, allow notes | R up, P down slightly |
| C | K=4 lens passes + merge | R up, toward the 3-union 41.6 and past it |
| D | verifier pass | P back up |
| E | fan-out over the limit (law change) | recovers `sentry-greptile-5` |

## 4. Leaderboard inclusion

Not a goal for now. Publishing needs about 600 to 1,000 public reviewed PRs
and bot-attributable reviews. The Review workflow already posts as a check
run, so reviews are attributable once there is public usage.
