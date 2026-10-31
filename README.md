# unreal-review

**Deprecation.** unreal-review has moved into [0x7067/unreal-agent](https://github.com/0x7067/unreal-agent) (the `unreal-review` command). Use that repository. The fork selects the model provider with `UNREAL_HARNESS_LLM_PROVIDER`, which defaults to `openai`. Anything that relies on OpenRouter must set `UNREAL_HARNESS_LLM_PROVIDER=openrouter`. This repository will be archived.

A local CLI that reviews a git diff with the embedded [unreal-agent](https://github.com/unreallabsai/unreal-agent) harness and OpenRouter, then writes a portable findings file. GitHub inline comments are one renderer of that file.

## Install

```sh
go install ./cmd/unreal-review
```

Set `OPENROUTER_API_KEY` and `UNREAL_HARNESS_LLM_MODEL` (an OpenRouter model id).

Install [golangci-lint](https://golangci-lint.run/) v2 for `make fmt` and `make lint`.

```sh
make install-lint
make fmt
make check
```

Linux x64: install Bend with the same pin as CI:

```sh
sh tools/install-bend.sh
```

`make check` includes `make prove` (`tools/prove.sh`: checks `PROOF.bend` with Bend and enforces `spec/unsafe-allow.txt`). Product invariants live in `LAWS.bend`. Other platforms: https://bend-lang.com (`bend version` 2.0.27 is the local toolchain).

`make canary` builds `bin/unreal-review` from this checkout and checks it the way a user drives it. The recipes live in [.agents/skills/verify-unreal-review](.agents/skills/verify-unreal-review/SKILL.md). The canary asserts those offline steps, then runs `run --pr` and `render github` against a local GitHub stand-in. It needs no `OPENROUTER_API_KEY` secret and no `GH_TOKEN`, and it does not call openrouter.ai. A dummy key stays on the `run` steps whose recipe expects `401`; `UNREAL_REVIEW_OPENROUTER_API` points those calls at a local stand-in that returns that status. The release binary honors `UNREAL_REVIEW_OPENROUTER_API` and `UNREAL_REVIEW_GITHUB_API` only when the URL host is loopback (`127.0.0.1`, `::1`, or `localhost`). Any other value is an error. CI runs `check` and `canary` on pull requests, on pushes to `main`, and on `workflow_dispatch`. Those two jobs are the pair that must pass before merge. Turning them on as required status checks is a GitHub branch-protection setting. The [Review workflow](.github/workflows/review.yml) is live dogfood: it downloads the `unreal-review` binary from a [0x7067/unreal-agent](https://github.com/0x7067/unreal-agent) release and posts a review of a pull request. The release tag is the fixed `UNREAL_AGENT_VERSION` in [.github/actions/unreal-review/install.sh](.github/actions/unreal-review/install.sh). The workflow loads that installer from the pull request's base commit, so a tag change takes effect after it merges. `eval` stays outside `make check` and `make canary`. `make canary-live` is a separate live smoke of one temporary one-line commit through `https://openrouter.ai/api/v1`. It requires `OPENROUTER_API_KEY` and `UNREAL_HARNESS_LLM_MODEL` (documented model `openai/gpt-6-luna-pro`) and is not a CI job.

## Review a local change

From a git repository:

```sh
unreal-review run --out findings.jsonl
```

With no range flags, `run` reviews staged, unstaged, and untracked changes against `HEAD`. `run` takes a git checkout (`--workspace`, default `.`). `--from` and `--to` select `git diff --merge-base` of those refs (branch, tag, or SHA); omit `--to` to include the working tree. `--commit` reviews one commit against its first parent. `--branch` reviews a branch since it diverged from `main` or `master`. Pathspecs limit the range; `--exclude` omits globs. It writes [findings.jsonl](schema/findings-v1.md).

`--thinking-level` is `low`, `medium`, `high` (default), `xhigh`, or `max`.

`--agent-log` writes the harness session JSONL.

```sh
unreal-review run --from origin/main --to HEAD --out findings.jsonl
unreal-review run --branch feature --out findings.jsonl
unreal-review run --commit abc1234 --out findings.jsonl
unreal-review run --from abc1234 --to def5678 --exclude '*.lock' -- cmd/
```

`--out` names the checkpoint file; see [schema/findings-v1.md](schema/findings-v1.md) for the `run` record, resume rules, and `--fresh`.

`--strategy single` is the default. For a higher-cost recall experiment,
`--strategy focused` runs four independent discovery passes (correctness,
failure paths, security/data, and contracts/tests), then a fresh verification
and same-issue consolidation pass. Only verified findings reach the public
checkpoint, including evidence-backed minor notes. Line overlap alone does not
merge findings. The findings-v1 schema, severity rubric, and renderers are unchanged.

```sh
unreal-review run --strategy focused --out findings.jsonl
unreal-review eval --strategy focused --model openai/gpt-6-luna-pro
```

Focused stage state stays inside the agent adapter under
`~/.local/state/unreal-agent/sessions/focused/`. Resume with the same strategy,
model, thinking level, source, and output file. Completed stages are reused and
their durably recorded cost is reconciled against the root checkpoint.
For working-tree reviews, keep `--out` outside the reviewed workspace or ignore
`<out>`, `<out>.work`, `<out>.context`, `<out>.context.*`, `.<out>.tmp-*`, and `.<out>.work.tmp-*` in `--out`'s directory. Otherwise generated untracked
artifacts change the source fingerprint and correctly prevent continuation.
Planted eval fixtures ignore their generated checkpoint files automatically.
Changed settings or missing stage state require `--fresh`. `--timeout` covers
the whole review invocation, not each pass. More discovery may find more real
defects or more noise, so focused is not the default until repeated benchmark
measurements establish its tradeoff. Notes can be posted inline on GitHub.

Diffs above 200,000 bytes automatically use a bounded aggregate plan instead
of one unbounded prompt. `--decompose` exercises the same path for a smaller
diff:

```sh
unreal-review run --decompose --from origin/main --to HEAD --out findings.jsonl
```

The planner packs deterministic locality groups together up to the prompt
budget, assigns every byte of the selected unified diff to exactly one local
task, and splits oversized hunks at whole-line boundaries while preserving
old/new coordinates, and adds boundary tasks for cross-scope contracts. Every
task prompt is at most 120,000 serialized bytes. Task candidate output stays
private. Bounded independent verification and consolidation must process the
output from every local and boundary task before the single parent checkpoint
can become complete.
Interrupted runs retain completed artifacts and cumulative cost. Changed source,
model, strategy, plan, or missing adapter state refuses continuation instead of
publishing partial coverage as clean. An indivisible line, source context, or
whole-plan scope manifest that cannot fit is rejected explicitly, never
truncated.

`group` remains a deterministic planning/debugging command. It prints related
pathspec groups and a separate `run` command for each, but those independent
outputs are not the aggregate reviewed-head receipt:

```sh
unreal-review group --from origin/main --to HEAD
```

The findings-v1 public schema and renderers are unchanged. Aggregate task state
lives under `~/.local/state/unreal-agent/sessions/planned/`. See the
[DAG research and pilot note](docs/research/martian-improvements.md) for design
evidence and measurement limitations.

```sh
unreal-review render markdown findings.jsonl
```

## Post inline comments on a GitHub pull request

```sh
unreal-review render github --pr owner/repo#12 findings.jsonl
```

`--dry-run` prints the GitHub review payload and does not post. The renderer maps `anchor: new` to the right side of the diff and drops findings whose lines are not in the pull request patch. `run --pr` narrows the range to commits pushed since the newest reviewed commit (tracked by check runs), names already-posted findings in the prompt, and the renderer suppresses duplicates before the 50-comment cap, tags each comment and each finding listed as outside the patch with a marker, and keeps a status comment on the PR holding the run count and cumulative cost. A review is posted only when it says something new; the status comment still updates.

GitHub Actions runs the two commands in separate jobs. `run` reviews with a read-only token. `render github` runs in a second job with the write permissions needed to post the review, status comment, and check run. That job checks out the base commit's installer and does not check out the pull request. Both jobs install `unreal-review` from that base checkout of [.github/actions/unreal-review](.github/actions/unreal-review/action.yml) (`UNREAL_HARNESS_LLM_PROVIDER=openrouter` on the run step). This repository reviews its own pull requests with [.github/workflows/review.yml](.github/workflows/review.yml). [examples/github-actions/review.yml](examples/github-actions/review.yml) is a self-contained workflow for another repository: set `UNREAL_AGENT_VERSION` at the top, then the install step downloads the release tarball and `SHA256SUMS` from `0x7067/unreal-agent`, checks them with `sha256sum`, and puts `unreal-review` on `PATH`.

## Evaluate a model

```sh
unreal-review eval --model openai/gpt-6-luna-pro
unreal-review eval --json --model openai/gpt-6-luna-pro > eval.json
```

`eval` runs a planted-issue corpus of eleven cases — races and a
self-deadlock, nil dereferences and swallowed errors and a broken
error chain, a removed bounds guard, SQL injection, a leaked
goroutine, an unclosed response body, a poisoned `sync.Once`, and a
clean control — through the same pipeline as `run`, then
scores each findings file against the planted issues. `recall` is the
share of planted issues found (same file, overlapping lines);
`precision` is the share of findings that match something planted;
`severity` is the share of planted issues found and graded with the
expected severity; `status` is the review checkpoint. Each case gets a
fresh git workspace and its own findings.jsonl under `--out`. Eval calls
the model and costs money; it is not part of `make check`.

Baseline for `openai/gpt-6-luna-pro`, measured 2026-09-27: 11 of 11
cases complete, recall 10/10, precision 10/11, severity 9/10, total cost
USD 0.0219 over 32 requests. The miss on severity is `swallowed-error`; the
extra finding is in `once-failure`.

### Martian Code Review Bench

```sh
unreal-review eval --corpus martian --model openai/gpt-6-luna-pro
unreal-review eval --corpus martian --profile strict --parallel 4 --cases grafana-79265,sentry-67876 --model openai/gpt-6-luna-pro
```

`--corpus martian` reviews the 50 pull requests of the Martian Code Review
Bench offline set (cal.com, discourse, grafana, keycloak, sentry; 173 golden
comments). The golden data is vendored in `internal/eval/martian.json` from
[withmartian/code-review-benchmark](https://github.com/withmartian/code-review-benchmark)
at commit `e616e849755441da38f18bf3adba2c9583b03803` (`offline/golden_comments`,
MIT license), with the GitHub merge base and head SHA of each pull request
pinned beside it. Each case does a shallow fetch of those two commits into a
fresh repository under `--out` and reviews merge base to head.

`--profile` picks the golden categories: `core` (default, Martian's default;
158 comments: bug, security, concurrency, data, api, perf, test_gap,
doc_defect), `strict` (139: the first five), or `all` (173: adds style and
speculative). Golden comments have no line numbers, so an LLM judge
(`--judge-model`, default `anthropic/claude-sonnet-5.5`, through OpenRouter)
asks Martian's question, whether a finding identifies the same underlying
issue as a golden comment, in one call per case; each finding matches at most
one golden comment. Martian severity maps Critical and High to `error`,
Medium to `warning`, and Low to `note`. The report gives recall and severity
agreement overall and per Martian severity, and `extra` counts findings that
match no golden comment. The judge always sees every golden comment, and the
report also gives a built-in direct-match precision, recall, and F1 estimate
for each of `strict`, `core`, and `all`, summed over cases: every extra counts
against precision, and a match on a golden comment outside a profile counts as
neither a hit nor an extra in that profile. These profile counts follow
accounting, but comparable benchmark scores must be exported through Martian's
pinned step 2/2.5/3 runner, which also extracts and deduplicates candidates. The
JSON output carries the built-in judge's 0-based `pairs` and per-profile
`tp`/`fp`/`fn` for each case. The golden set is sparse on minor issues, so
extras are not all false positives. `--parallel` (default 8) runs cases at once.
The planted corpus keeps its line-overlap matcher and stays the default. A
severity with no golden comment in the selected cases prints `-` instead of a
rate. A three-attempt full baseline was measured 2026-09-29 with
`openai/gpt-6-luna-pro`. Findings were exported through Martian's runner at the
pinned commit above, including steps 2, 2.5, and 3 with
`anthropic/claude-sonnet-4.5` through OpenRouter. Mean precision/recall/F1 was
35.2/34.5/34.8 for strict, 37.1/32.9/34.8 for core, and 39.3/32.9/35.8 for
all. The attempts produced 93, 96, and 84 findings and cost $4.320 total for
reviews. In that historical build each attempt completed 49 cases;
`sentry-greptile-5` exceeded the then-current 200,000-byte direct-review limit
and was exported as an empty review. Automatic bounded decomposition now covers
that size class, but it still requires a new measured benchmark run. Martian
runner scoring cost is not recorded by the runner.

### PR #38 labeled regression

```sh
OPENROUTER_API_KEY=... UNREAL_HARNESS_LLM_MODEL=openai/gpt-6-luna-pro unreal-review eval --corpus pr38 --runs 3 --out pr38-baseline
OPENROUTER_API_KEY=... UNREAL_HARNESS_LLM_MODEL=openai/gpt-6-luna-pro unreal-review eval --corpus pr38 --runs 3 --json --out pr38-baseline
```

`--corpus pr38` checks out pull request #38 (`942c78c3`..`107a3230`) with the same shallow fetch and synthetic head as a Martian case, reviews that range, and scores the findings against eight labels taken from paid runs of this repository on that range. Five labels are `real` (recall is how many of those five a run hits): dropping `Fresh: true`, treating an unborn `.git/HEAD` as initialized, a root prompt the verifier uses without PR text or already-reported findings, the findings exclude file growing on retry, and a reused fixture directory resetting back to a stale base. Three are `weak`: workspace revalidation of `HEAD`, receipt or manifest save failure versus the cost ledger, and the `strategy.go` timeout shuffle, which is dead because the planned runner applies the deadline on the direct path too.

The text report prints this scoring rule under the table. Each finding is assigned to at most one label, the one with the largest line overlap; ties go to a weak label, then to the earlier label. Recall is distinct real labels hit over the five real labels. Precision is those hits over hits plus findings assigned to a weak label, and is n/a when a run has no real hit and no weak finding. A second finding on a real label already hit does not change the numerator. Unlabeled findings match no label and are outside precision, neither a hit nor a false positive, but they still count in the finding total and in error share; known-weak F4 at review.go:75-76 is intentionally unlabeled, and other unlabeled clusters sit there, so unlabeled is not a clean precision signal. Findings anchored to removed lines (anchor=old) use base coordinates and are scored as unlabeled. Means use completed runs only. A completed run with null precision is excluded from the precision mean. F1 is one hit if either Fresh site is chosen. A finding only at eval.go:169 is F6.

The report also gives the share of findings rated `error`, the finding count, and cost in USD, requests, and wall time. `--runs N` repeats the review and prints each run, the mean over completed runs, the total cost, and the union of real labels hit. Defaults are `--strategy single` and `--thinking-level high` unless those flags are passed. `--out <dir>` keeps each run's checkout and findings. A rerun with the same `--out` starts fresh. A failed run is printed and the command exits non-zero; it is left out of the mean. On `openai/gpt-6-luna-pro` a completed run costs about $0.45 to $0.55. The labels are judgments of what those runs reported, not a complete list of defects in the diff.

`--corpus labeled` reviews pr38 together with three more ranges of this repository (`pr31` `89539063`..`99151334`, `pr25` `123592e8`..`dd9e2114`, `pr3` `8ad9a39d`..`6addc09f`) and prints one table per case plus a pooled table. Each finding is assigned to at most one label, the one with the largest line overlap; ties go to a weak label, then to the earlier label. Recall is distinct real labels hit over that case's real labels. Precision is those hits over hits plus findings assigned to a weak label, and is n/a when a run has no real hit and no weak finding. A second finding on a real label already hit does not change the numerator. Unlabeled findings match no label and are outside precision, but they still count in the finding total and in error share. Findings anchored to removed lines (anchor=old) use base coordinates and are scored as unlabeled. Means use completed runs only. A completed run with null precision is excluded from the precision mean. The pooled row sums hits, weak findings, cost, and real labels across the selected cases and omits a repetition missing a case. A real hit moves precision by increasing the numerator, and a weak finding adds to the denominator. Unlabeled count and error share are the noise signals. Spent cost sums every score that exists, including a failed review and a case that finished before a later setup failed. Spent wall time is that review time and excludes checkout, including a failed setup. Only a review failure is a failed row, and only when every selected case has a score. A setup error renders the aggregate report, with every selected case slot and the pooled row, and then returns the error. `--cases` selects a subset. A rerun with the same `--out` starts fresh. `--corpus pr38` is unchanged. The pr31 `find` escapes (symlink, escaped newline, tilde, command substitution) are unfixed; from bot review comments; confirmed by code reading.

```sh
OPENROUTER_API_KEY=... UNREAL_HARNESS_LLM_MODEL=openai/gpt-6-luna-pro unreal-review eval --corpus labeled --runs 3 --out labeled-baseline
```

One run is the four reviews in sequence. On `openai/gpt-6-luna-pro`, expect about $0.90 to $1.30 and 30 to 45 minutes per run, and 1.5 to 2 hours for `--runs 3`.

## Findings file

The JSONL schema is the product. Renderers turn it into GitHub comments, markdown, or another display. See [schema/findings-v1.md](schema/findings-v1.md).
