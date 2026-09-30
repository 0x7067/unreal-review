# Martian benchmark: findings, corrections, and improvements

Research and implementation note, 2026-09-30. The baseline used
`openai/gpt-6-luna-pro`, thinking `high`, and three attempts scored with the
Martian runner at `e616e849755441da38f18bf3adba2c9583b03803` through OpenRouter
using `anthropic/claude-sonnet-4.5` for extraction, deduplication, and judging.
The published comparison data is that commit's
`offline/analysis/benchmark_dashboard.json`, not a claim about today's live
leaderboard. Baseline artifacts currently live in the ignored
`.worktrees/martian-baseline/.eval/martian/` directory, not in this document.

## Baseline placement

Core profile, Sonnet 4.5 judge, F1 ranking with our three-attempt mean inserted:

| rank | tool | precision % | recall % | F1 % |
|---:|---|---:|---:|---:|
| 1 | cubic-v2 | 56.5 | 60.8 | 58.5 |
| 2 | qodo-extended-v2 | 56.2 | 57.0 | 56.6 |
| 3 | augment | 52.4 | 61.4 | 56.6 |
| 17 | claude-code | 36.5 | 41.1 | 38.7 |
| 18 | claude | 38.3 | 36.1 | 37.1 |
| **19** | **unreal-review** | **37.1** | **32.9** | **34.8** |
| 20 | codeant-v2 | 32.4 | 34.8 | 33.5 |

This is a self-measured comparison, not a Martian-certified ranking.
It weights precision and recall equally (F1). The dashboard's default beta
is 2, which weights recall more heavily. Comparing our Sonnet scores against
other tools' Opus or GPT scores is a sensitivity illustration, not a valid
same-judge ranking. We have not yet scored our outputs with those two judges.

### What the baseline actually establishes

- **Repeated attempts disagree about individual goldens.** Of 173 goldens in
  the all profile, 101 were found in none of three attempts, 18 in one, 9 in
  two, and 45 in all three. Average all-profile recall is 32.95%, and the
  three-attempt union is 41.62%. Critical+High recall rises from an average
  43.94% to a union of 56.06%. A union does not establish precision or F1 for
  an actual merged review. Three attempts do not prove that the never-found
  issues are impossible to find by further sampling.
- **There is one infrastructure coverage failure.** `sentry-greptile-5` is
  252,701 unified-diff bytes, 106 changed files, and 3,293 added/deleted lines.
  All three attempts refused it before calling the agent. Its six goldens
  remained in the denominator. Only 49 of 50 cases completed per attempt.
- **File count did not correlate with lower recall in this sample.** Pooled
  all-profile recall was 29.3% for <=10 files, 29.9% for 11-30, and 47.9% for
  >30. These buckets mix languages, defect categories, and PR difficulty.
  They do not prove that large contexts are safe or that increasing the byte
  limit would preserve quality. An oversized PR is not evidence for the
  capabilities of runs we never executed.
- **Some reviews were consistently empty despite goldens.**
  `grafana-76186`, `sentry-80528`, and `calcom-22345` had empty findings and
  complete clean summaries in all attempts. This deserves tracing whether
  the model missed, rejected, or disagreed with each issue. We do not have
  evidence that the prompt's evidence rule caused those misses.
- **Low-severity and certain categories had lower measured recall.** Low
  recall was 18.1%, `test_gap` 0%, `perf` 16.7%, `style` 10%, whereas
  concurrency was 54.8% and API 51.3%. The sample contains only four
  `test_gap` and six `perf` goldens. Prompt-lens explanations are hypotheses,
  not controlled experimental results.
- **Volume needs three distinct measurements.** Raw finding counts were
  93/96/84. Martian extracted 170/168/155 candidates before profile
  accounting, an average of 3.29 candidates per PR. Core TP+FP counts were
  145/149/128 and are not literal candidate totals: TP counts matched goldens,
  profiles ignore some matches, and extraction can split one finding into
  several candidates. The original claim that our volume already matched
  the leaders used the wrong denominator and is withdrawn.

## 1. OpenRouter model identity

**Yes: an OpenRouter request routed to Anthropic's same snapshot uses the
same model.** A different public slug or results-directory name is not a
model mismatch. On 2026-09-30 the [public endpoint metadata][or-endpoints]
for `anthropic/claude-sonnet-4.5` lists the Anthropic endpoint as
`anthropic/claude-4.5-sonnet-20250929`, consistent with Martian's dated
Sonnet 4.5 snapshot. Moving to a different endpoint is not necessary merely
to rename the model. This corrects the earlier recommendation.

For future reproducibility, retain:

1. Runner commit, golden-data revision, all steps 2/2.5/3, profile, beta,
   full case denominator, source SHAs, extraction/dedup inputs and outputs.
2. The requested model slug, endpoint metadata at run time, actual provider
   reported by OpenRouter, temperature (runner uses 0), reasoning and output
   parameters. Today's metadata cannot prove which route historical calls
   used if those responses were not retained.
3. If Anthropic-only routing is desired, use the runner's OpenAI client's
   request `extra_body` to set:

   ```json
   {"provider":{"only":["anthropic"],"allow_fallbacks":false,"require_parameters":true}}
   ```

   These are OpenRouter's documented [provider-routing settings][or-routing].
   The current pinned runner does not expose this request body through an
   environment variable. Add explicit request configuration or use an
   account/key provider restriction, then retain that configuration.
4. Re-score our saved reviews if we want Opus/GPT judge comparisons, not
   because equivalent Sonnet names alone invalidate our current score.

Same model does not mean bit-identical output. Re-judging a sample measures
sensitivity and pipeline compatibility, but a one-point F1 tolerance on two
tools cannot establish statistical equivalence. No verified runner-scoring
cost estimate is available. Do not promise that 10k judge calls cost only a
few dollars.

## 2. We already have decomposition, not aggregate execution

`unreal-review group` selects the same git range and prints pathspec groups.
`internal/review/group.go` already combines directories, stem companions,
implementation/tests, lockfiles, locales, headers, and selected Go import
relationships. `group_import.go` supplies Go-specific dependency edges.

It does **not** execute group reviews, accumulate a parent report, or ensure
completion of all groups before issuing a reviewed-head receipt. Limits are
25 files and 2,000 changed lines per group with a singleton exemption.
Neither limit guarantees <=200,000 actual diff bytes. A single huge file
cannot be split by pathspec. The Bend model's `CappedTail` allows that
singleton, so claiming every group is hard-bounded would misstate the proof.

### Primary-source research

| source | technique | evidence and limits |
|---|---|---|
| [Anthropic public review plugin][cc-plugin] | independent reviewers, candidate validation and filtering | concrete open implementation of parallel review followed by validation, not a measured gain for our harness |
| [Anthropic production Code Review][claude-review] | parallel search, verification, deduplication, effort scaled by complexity | vendor-reported production observations, not an exposed dependency scheduler or proof that four reviewers are optimal |
| [ClusterChanges][clusterchanges] | related-change clustering by syntactic/semantic information | supports decomposing mixed-purpose changes, not arbitrary byte slicing |
| [ChgCutter][chgcutter] | program-dependence graph partitions | reports partition accuracy on a limited corpus, not AI-review precision/recall |
| [Controlled human study][human-study] | decomposed rather than bundled review tasks | fewer wrongly reported issues, no observed increase in defects found in that study |

### Recommended large-PR design: dependency groups plus boundary reviews

```mermaid
flowchart TD
    Source[Freeze base/head/full diff and changed-hunk manifest] --> Graph[Existing grouping plus dependency/contract edges]
    Graph --> SCC[Collapse dependency cycles into SCCs]
    SCC --> Units[Pack bounded review units]
    Units --> Local[Independent local review tasks]
    Units --> Boundary[Caller/callee, schema/config and API boundary tasks]
    Local --> Verify[Verify and consolidate candidates]
    Boundary --> Verify
    Verify --> Complete[Coverage barrier and one parent report]
    Complete --> Publish[One summary and reviewed-head receipt]
```

Key design choices:

- File-dependency edges are **context relationships**, not automatically
  scheduler dependencies. Read-only reviews over a frozen workspace can run
  concurrently. The execution DAG requires an edge when one task consumes
  another's artifact, such as verification after discovery.
- Collapse strongly connected components for planning, but do not merge every
  connected file into an unbounded prompt. A giant SCC still needs bounded
  units and explicit cross-unit contract tasks.
- Start with existing deterministic groups. Extend relationships
  incrementally beyond Go imports (caller/API boundaries, shared DTO/schema,
  migrations and configuration) instead of deploying an unvalidated graph
  parser for every language at once. Retain edge provenance and conservative
  fallbacks when relationship resolution is incomplete.
- Budget actual serialized prompts, including PR context, boundaries, and
  tool outputs, not just file counts or numstat lines. Token limits depend on
  the model. Oversized singleton files need hunk or syntax-aware units with
  old/new line maps, never silent truncation or arbitrary byte cuts.
- Every changed hunk has exactly one primary owner. Context can overlap.
  Tests belong with their implementation where possible. Track coverage of
  local units, inter-unit edges, verifier batches, and binary/deleted/renamed
  files, including explicitly unsupported scopes.
- Keep all tasks tied to the full source SHA plus deterministic scope/plan
  digests. Completed tasks survive cancellation and resume. Failed or
  unreviewed tasks must never become a clean complete parent review.
- Verification batches can run per unit/candidate, followed by a consolidation
  task for duplicates and a final boundary check. One giant verifier prompt
  is not a scalable large-PR solution.
- Publish only the parent. Rendering individual groups can incorrectly mark
  the whole head reviewed after a subset finishes and skip subsequent work.

`oversized_diff_refused` remains unchanged in this implementation. Automatic
big-PR execution is a separate source/planner/checkpoint change that needs
coverage modeling plus corresponding laws and proofs. The user's request
establishes big-PR support as a desired outcome. The implementation should
change the product rule deliberately, not evade it by silently excluding
paths. Raising the cap or switching to `-U0` is not the researched solution:
`-U0` saves bytes but removes initially supplied context, and a larger cap
has no established quality guarantee.

## 3. Implemented recall experiment

The single-review prompt now encourages systematic coverage, investigating
uncertain premises before abandoning them, comparison of refactors with the
old implementation, and evidence-backed minor notes. It explicitly forbids
publishing unsupported allegations at any severity. We did not weaken the
public evidence standard.

The opt-in `--strategy focused` agent implements this execution DAG:

```mermaid
flowchart LR
    Diff[Source-bound review input] --> Correctness[Correctness and state]
    Diff --> Failures[Failure paths and lifetimes]
    Diff --> Security[Security and data]
    Diff --> Contracts[Contracts, tests and documentation]
    Correctness --> Union[Private candidate union]
    Failures --> Union
    Security --> Union
    Contracts --> Union
    Union --> Verifier[Fresh verification and same-issue consolidation]
    Verifier --> Report[Verified findings and one summary]
```

Each discovery node has a separate session and work file. Candidates are
private hypotheses, not published findings. The verifier must inspect code,
reject unconfirmed premises, retain candidate identities, and consolidate
only the same underlying issue. Line overlap alone is not deduplication.
No verifier-generated finding outside the candidate identity/location set is
accepted. A concrete small issue can be `note`, but severity does not excuse
missing evidence. **Notes are currently posted by the GitHub renderer**,
so allowing more notes is not noise-free. No severity filter was added.

The DAG lives behind `review.Agent`. Its stage manifest and continuation
mapping remain adapter-owned, leaving findings-v1 unchanged. Resume validates
configuration and dependencies, skips completed stages, and reconciles staged
cost against `AgentRequest.PriorCost` in the parent checkpoint. Missing
adapter state or changed settings requires a fresh review. Timeout covers
the whole focused invocation, and shared logs serialize writes.

Use:

```sh
unreal-review run --strategy focused --model openai/gpt-6-luna-pro --out findings.jsonl
unreal-review eval --strategy focused --model openai/gpt-6-luna-pro \
  --cases race,wrap-break,once-failure,clean
unreal-review eval --corpus martian --strategy focused --parallel 2 \
  --judge-model anthropic/claude-sonnet-4.5 --model openai/gpt-6-luna-pro
```

`single` remains the default. Focused reviews spend more model requests and
can have longer latency. `--parallel` controls cases, so two focused cases
can run up to eight discovery agents concurrently. The focused verifier's
candidate budget is explicitly bounded rather than truncated. It does not
remove the 200KB root diff limit.

### Measurement before changing the default

1. Compare single and focused on the planted corpus, including the clean
   control. Record completion, precision/recall, cost, requests, and time.
2. Run repeated paired attempts on the full Martian set with fixed judge and
   runner pipeline. Keep all 50 cases in the denominator. Do not expose
   goldens to discovery or verification.
3. Separate ablations: broader single prompt, focused discovery without
   consolidation, and focused discovery with verification. The implementation
   ships the safe verified version, not an unverified publishing mode.
4. Report mean/spread, paired per-PR differences and per-golden hit frequency.
   Bootstrap at PR level, not independent comments or repeated attempts.
   Account for variation caused by extraction/judging as well as reviewing.
5. Audit unmatched findings and rejected candidates. Sparse goldens make
   benchmark "extra" different from a demonstrated false accusation.
6. Use unseen regressions and live dogfood to guard against tuning to the
   fixed 50 PRs. Promote focused to default only after observed quality and
   acceptable cost, not because multi-agent review is fashionable.

No improvement in full-benchmark scores is established by implementation or
mock-provider tests alone. `eval --attempts N`, a reusable repeated-run
analysis/plot tool, and aggregate large-PR execution remain follow-up work.
Official leaderboard inclusion is not a blocker for publishing self-measured
results with these qualifications.

[or-endpoints]: https://openrouter.ai/api/v1/models/anthropic/claude-sonnet-4.5/endpoints
[or-routing]: https://openrouter.ai/docs/features/provider-routing
[cc-plugin]: https://github.com/anthropics/claude-code/blob/main/plugins/code-review/commands/code-review.md
[claude-review]: https://claude.com/blog/code-review
[clusterchanges]: https://www.microsoft.com/en-us/research/publication/helping-developers-help-themselves-automatic-decomposition-of-code-review-changes/
[chgcutter]: https://link.springer.com/article/10.1007/s11390-019-1917-9
[human-study]: https://arxiv.org/abs/1805.10978
