# unreal-review

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

`make check` includes `make prove` (`tools/prove.sh`: checks `PROOF.bend` with Bend and enforces `spec/unsafe-allow.txt`). Product invariants live in `LAWS.bend`. Install Bend from https://bend-lang.com (`bend version` 2.0.27 is the local toolchain).

`make canary` builds `bin/unreal-review` from this checkout and checks it the way a user drives it. The recipes live in [.agents/skills/verify-unreal-review](.agents/skills/verify-unreal-review/SKILL.md). The canary asserts those offline steps, then runs `run --pr` and `render github` against a local GitHub stand-in. It needs no `OPENROUTER_API_KEY` secret and no `GH_TOKEN`, and it does not call openrouter.ai. A dummy key stays on the `run` steps whose recipe expects `401`; `UNREAL_REVIEW_OPENROUTER_API` points those calls at a local stand-in that returns that status. The release binary honors `UNREAL_REVIEW_OPENROUTER_API` and `UNREAL_REVIEW_GITHUB_API` only when the URL host is loopback (`127.0.0.1`, `::1`, or `localhost`). Any other value is an error. CI runs `check` and `canary` on pull requests, on pushes to `main`, and on `workflow_dispatch`. Those two jobs are the pair that must pass before merge. Turning them on as required status checks is a GitHub branch-protection setting. The [Review workflow](.github/workflows/review.yml) is live dogfood: it posts a review of a pull request using the base commit's binary. `eval` stays outside `make check` and `make canary`.

## Review a local change

From a git repository:

```sh
unreal-review run --out findings.jsonl
```

With no range flags, `run` reviews staged, unstaged, and untracked changes against `HEAD`. `run` takes a git checkout (`--workspace`, default `.`). `--from` and `--to` select `git diff --merge-base` of those refs (branch, tag, or SHA); omit `--to` to include the working tree. `--commit` reviews one commit against its first parent. `--branch` reviews a branch since it diverged from `main` or `master`. Pathspecs limit the range; `--exclude` omits globs. The selected range is sent in full to the agent with cwd at the checkout. It writes [findings.jsonl](schema/findings-v1.md).

```sh
unreal-review run --from origin/main --to HEAD --out findings.jsonl
unreal-review run --branch feature --out findings.jsonl
unreal-review run --commit abc1234 --out findings.jsonl
unreal-review run --from abc1234 --to def5678 --exclude '*.lock' -- cmd/
```

`--out` names the checkpoint file; see [schema/findings-v1.md](schema/findings-v1.md) for the `run` record, resume rules, and `--fresh`.

`run` refuses a diff over 200,000 bytes rather than reviewing only part of it. To review a large change in pieces, run `group` on the same git range. It prints related file groups and a `run` command for each (`unreal-review group -h` for range flags):

```sh
unreal-review group --from origin/main --to HEAD
```

Each `run` writes its own findings file and `diff_sha`.

```sh
unreal-review render markdown findings.jsonl
```

## Post inline comments on a GitHub pull request

```sh
unreal-review render github --pr owner/repo#12 findings.jsonl
```

`--dry-run` prints the GitHub review payload and does not post. The renderer maps `anchor: new` to the right side of the diff and drops findings whose lines are not in the pull request patch. `run --pr` narrows the range to commits pushed since the newest reviewed commit (tracked by check runs), names already-posted findings in the prompt, and the renderer suppresses duplicates before the 50-comment cap, tags each comment and each finding listed as outside the patch with a marker, and keeps a status comment on the PR holding the run count and cumulative cost. A review is posted only when it says something new; the status comment still updates.

GitHub Actions runs the two commands in separate jobs. `run` builds from the base commit and reviews with a read-only token; `render github` runs in a second job that never checks out the pull request, with only the write permissions needed to post the review, status comment, and check run. This repository reviews its own pull requests with [.github/workflows/review.yml](.github/workflows/review.yml); [examples/github-actions/review.yml](examples/github-actions/review.yml) is the shape to copy into another repository.

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
match no golden comment. The golden set is sparse on minor issues, so extras
are not all false positives. `--parallel` (default 8) runs cases at once.
The planted corpus keeps its line-overlap matcher and stays the default.

## Findings file

The JSONL schema is the product. Renderers turn it into GitHub comments, markdown, or another display. See [schema/findings-v1.md](schema/findings-v1.md).
