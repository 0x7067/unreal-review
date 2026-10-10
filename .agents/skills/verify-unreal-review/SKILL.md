---
name: verify-unreal-review
description: Drive the unreal-review CLI the way a user does — build the binary, review a git range into findings.jsonl, group related files, render markdown or a GitHub dry-run payload, and keep stdout/stderr/exit/files as evidence. Use when proving CLI behavior, verifying a change to run/group/render/checkpoint, or when asked to /verify-unreal-review.
---

# Verify unreal-review

unreal-review is a short-lived CLI. There is no server. Launch builds `bin/unreal-review`; each drive is a fresh process against an isolated git fixture or a findings file.

Read [features/README.md](features/README.md) before driving. Use that map as the recipe source. After a product change, update `features/*` in the same PR as the product change.

## Launch

From the repo root, or via the scripts below. Scripts resolve the repo from their own path.

```bash
export VERIFY_ROOT=${VERIFY_ROOT:-/tmp/verify-unreal-review}
.agents/skills/verify-unreal-review/scripts/launch.sh
.agents/skills/verify-unreal-review/scripts/doctor.sh
```

`launch.sh` runs `make build`, writes `bin/unreal-review`, creates `$VERIFY_ROOT/evidence/$VERIFY_RUN_ID` and `$VERIFY_ROOT/scratch/$VERIFY_RUN_ID`, and records the run id in `$VERIFY_ROOT/current-run`. Ready when it prints `ready: <path-to-bin>` and `doctor.sh` exits 0.

Export `VERIFY_RUN_ID` before launch to name the run. Concurrent runs must use distinct `VERIFY_ROOT` or `VERIFY_RUN_ID` values. Later scripts in the same run read `current-run` when `VERIFY_RUN_ID` is unset.

Teardown is [Cleanup](#cleanup). The CLI does not stay running.

## Doctor

Read-only. Run after launch, at the start of every fresh session, and after unexpected output.

```bash
.agents/skills/verify-unreal-review/scripts/doctor.sh
```

It must report `doctor: ok` and confirm all of:

- `VERIFY_BIN` is `$REPO/bin/unreal-review` and executable
- `unreal-review help` exits 0 and names `run` and `group`
- the binary is not older than `cmd/unreal-review/*.go`, `internal/*/*.go`, or the library packages (`findings`, `diffmap`, `review`, `agent`)
- `go` and `git` are on `PATH`
- evidence and scratch live under `$VERIFY_ROOT`, outside the repo

It also notes (without failing) whether `OPENROUTER_API_KEY`, `UNREAL_HARNESS_LLM_MODEL`, and `GH_TOKEN` are present. Live `run` of a non-empty diff needs the key and a model; the harness is built into the binary, no external runner to check. GitHub posting and `run --pr` need `GH_TOKEN`; nothing else is read. `--dry-run` calls the API only when `GH_TOKEN` is set.

Refuse to drive any other `unreal-review` on `PATH`.

## Drive

Run every CLI invocation through `scripts/cli.sh` so command, stdout, stderr, and exit land under evidence. `cli.sh` exits 0 after capture; the CLI's exit code is `exit.txt`. Pass `--` with no following args to invoke the binary with no command.

```bash
.agents/skills/verify-unreal-review/scripts/cli.sh --name <step> -- <unreal-review args>
.agents/skills/verify-unreal-review/scripts/cli.sh --name <step> --no-github-auth -- <args>
.agents/skills/verify-unreal-review/scripts/cli.sh --name <step> --stdin <file> -- render markdown -
```

`--no-github-auth` unsets `GH_TOKEN`, so posting cannot reach GitHub.

For `run`, create a disposable repo first:

```bash
.agents/skills/verify-unreal-review/scripts/fixture-repo.sh
```

That prints `VERIFY_FIXTURE`, `BASE_SHA`, and `HEAD_SHA` and writes `$VERIFY_SCRATCH/fixture.env` (source it). Pass `--workspace "$VERIFY_FIXTURE"`. Do not use the product checkout as `--workspace` unless the recipe is reviewing this repo's own range.

`run` requires `--model` or `UNREAL_HARNESS_LLM_MODEL`, and `OPENROUTER_API_KEY`. Dummy values are enough for paths that never start the agent (empty diff, complete-file refuse, SHA mismatch, mixed range flags, bad revision). A non-empty diff runs the embedded harness and calls the model directly; a dummy key reaches the model and fails on a `401`, so it never bills.

Live review of a non-empty diff spends OpenRouter credit. Drive it only when the feature file's live sub-feature is in scope and `UNREAL_HARNESS_LLM_MODEL` is set. Otherwise record the unmet precondition and continue.

Do not post a GitHub review unless the user names a disposable pull request. Default GitHub proof is `--dry-run`.

Stable handles: subcommands `run`, `group`, `eval`, `render markdown`, `render github`; flags `--from`, `--to`, `--commit`, `--branch`, `--exclude`, `--out`, `--fresh`, `--workspace`, `--dry-run`, `--pr`, `--repo`, `--model`, `--thinking-level`, `--strategy`, `--decompose`, `--timeout`, `--compaction`, `--context-window`, `--agent-log`, and the eval-only `--corpus`, `--cases`, `--json`, `--judge-model`, `--profile`, `--parallel`; positional pathspecs; `examples/findings.jsonl`.

## Evidence

Root: `$VERIFY_ROOT/evidence/$VERIFY_RUN_ID/` (also printed by launch). Each `cli.sh --name <step>` writes `$VERIFY_EVIDENCE/<step>/{cmd.txt,stdout.txt,stderr.txt,exit.txt}`. Copy `--out` files into that step directory after the command.

Proof is the user-visible action plus the resulting state:

- Exit code and the `unreal-review: …` stderr line for failures
- For `run`, the findings JSONL (`type=run` / `finding` / `summary`), including `status`, `source.base`, `source.head`, `source.*_sha`, `diff_sha`, and `cost`
- Empty unified diff fingerprints as SHA-256 of the empty string: `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`
- For markdown, the rendered text (cost line, summary, `## \`path\``, severity bullets)
- For GitHub `--dry-run`, the JSON payload on stdout (`event: COMMENT`, `comments[].side` `RIGHT` for `anchor=new` and `LEFT` for `anchor=old`)

`--dry-run` skips `CreateReview`. With `GH_TOKEN` it still `GET`s the pull request and files. Without it, it prints the payload and does not call the API. Observe which of those happened; do not trust the flag name.

Do not treat `make check`, `go test`, or internal setters as a user-path proof.

## CI

`make canary` runs the offline recipes in `features/` as assertions. It builds `bin/unreal-review` from this checkout, drives each command through `scripts/cli.sh`, and fails when the exit status, stdout, stderr, or JSONL disagree with the recipe. A second stage execs that same binary against a local GitHub stand-in for `run --pr` and `render github` (narrowing to the latest `unreal-review` check run, duplicate suppression, the status comment, and the check-run receipt).

The canary keeps a dummy `OPENROUTER_API_KEY` on the `run` steps whose recipe expects a `401`, and points `UNREAL_REVIEW_OPENROUTER_API` at a local stand-in that returns that status. Those steps do not call openrouter.ai. The release binary honors `UNREAL_REVIEW_OPENROUTER_API` and `UNREAL_REVIEW_GITHUB_API` only for a loopback host (`127.0.0.1`, `::1`, or `localhost`); any other value is an error. `range-live`, `gh-post`, and `gh-dry-with-token` stay outside the canary: the first two are the skippable live steps, and `gh-dry-with-token` calls `api.github.com`.

`make canary-live` is outside that job. It requires `OPENROUTER_API_KEY` and `UNREAL_HARNESS_LLM_MODEL` (documented model `openai/gpt-6-luna-pro`) and reviews one tiny commit on `https://openrouter.ai/api/v1`. It refuses a set `UNREAL_REVIEW_OPENROUTER_API`, so it cannot be pointed at the local stub.

CI jobs `check` and `canary` are the regression pair. Workflow `Review` is the live dogfood path.

## Cleanup

```bash
.agents/skills/verify-unreal-review/scripts/cleanup.sh
```

Deletes `$VERIFY_SCRATCH` for this run id. Leaves `$VERIFY_EVIDENCE` in place. After cleanup, `ls "$VERIFY_EVIDENCE"` must still list the step directories.

On a failed attempt, run cleanup for that run id before starting another.

## Helpers

All under `.agents/skills/verify-unreal-review/scripts/`. Executable. `VERIFY_RUN_ID` from launch/`current-run`.

| Script | Invocation |
| --- | --- |
| `launch.sh` | `.agents/skills/verify-unreal-review/scripts/launch.sh` |
| `doctor.sh` | `.agents/skills/verify-unreal-review/scripts/doctor.sh` |
| `cli.sh` | `…/cli.sh --name <step> [--no-github-auth] [--stdin file] -- <args>` |
| `fixture-repo.sh` | `…/fixture-repo.sh [name]` (default `review`) |
| `sample-findings.sh` | `…/sample-findings.sh complete\|running\|failed\|legacy\|empty <path>` |
| `cleanup.sh` | `.agents/skills/verify-unreal-review/scripts/cleanup.sh` |
