---
name: verify-unreal-review
description: Drive the unreal-review CLI the way a user does — build the binary, review a git range into findings.jsonl, render markdown or a GitHub dry-run payload, and keep stdout/stderr/exit/files as evidence. Use when proving CLI behavior, verifying a change to run/render/checkpoint, or when asked to /verify-unreal-review.
---

# Verify unreal-review

unreal-review is a short-lived CLI. There is no server. Launch builds `bin/unreal-review`; each drive is a fresh process against an isolated git fixture or a findings file.

Read [features/README.md](features/README.md) before driving. Use that map as the recipe source. After a product change, `/maintain-verification-skill` is the upkeep path.

## Launch

From the repo root, or via the scripts below. Scripts resolve the repo from their own path.

```bash
export VERIFY_ROOT=${VERIFY_ROOT:-/tmp/verify-unreal-review}
.grok/skills/verify-unreal-review/scripts/launch.sh
.grok/skills/verify-unreal-review/scripts/doctor.sh
```

`launch.sh` runs `make build`, writes `bin/unreal-review`, creates `$VERIFY_ROOT/evidence/$VERIFY_RUN_ID` and `$VERIFY_ROOT/scratch/$VERIFY_RUN_ID`, and records the run id in `$VERIFY_ROOT/current-run`. Ready when it prints `ready: <path-to-bin>` and `doctor.sh` exits 0.

Export `VERIFY_RUN_ID` before launch to name the run. Concurrent runs must use distinct `VERIFY_ROOT` or `VERIFY_RUN_ID` values. Later scripts in the same run read `current-run` when `VERIFY_RUN_ID` is unset.

Teardown is [Cleanup](#cleanup). The CLI does not stay running.

## Doctor

Read-only. Run after launch, at the start of every fresh session, and after unexpected output.

```bash
.grok/skills/verify-unreal-review/scripts/doctor.sh
```

It must report `doctor: ok` and confirm all of:

- `VERIFY_BIN` is `$REPO/bin/unreal-review` and executable
- `unreal-review help` exits 0 and names `run`
- the binary is not older than `cmd/unreal-review/*.go` or `internal/*/*.go`
- `go` and `git` are on `PATH`
- evidence and scratch live under `$VERIFY_ROOT`, outside the repo

It also notes (without failing) whether `OPENROUTER_API_KEY`, `UNREAL_HARNESS_LLM_MODEL`, `unreal-agent-runner`, and a GitHub token/`gh auth` are present. Live `run` of a non-empty diff needs the key, a model, and the runner. GitHub posting needs `--token`, `GH_TOKEN`, `GITHUB_TOKEN`, or `gh auth token`. `--dry-run` calls the API only when `--token` or those env vars are set; it does not consult `gh`.

Refuse to drive any other `unreal-review` on `PATH`.

## Drive

Run every CLI invocation through `scripts/cli.sh` so command, stdout, stderr, and exit land under evidence. `cli.sh` exits 0 after capture; the CLI's exit code is `exit.txt`. Pass `--` with no following args to invoke the binary with no command.

```bash
.grok/skills/verify-unreal-review/scripts/cli.sh --name <step> -- <unreal-review args>
.grok/skills/verify-unreal-review/scripts/cli.sh --name <step> --no-github-auth -- <args>
.grok/skills/verify-unreal-review/scripts/cli.sh --name <step> --stdin <file> -- render markdown -
```

`--no-github-auth` unsets `GH_TOKEN`/`GITHUB_TOKEN` and puts `gh` off `PATH`. That keeps `--dry-run` off the network unless the command also passes `--token`. Posting without `--no-github-auth` may still use `gh auth token`.

For `run`, create a disposable repo first:

```bash
.grok/skills/verify-unreal-review/scripts/fixture-repo.sh
```

That prints `VERIFY_FIXTURE`, `BASE_SHA`, and `HEAD_SHA` and writes `$VERIFY_SCRATCH/fixture.env` (source it). Pass `--workspace "$VERIFY_FIXTURE"`. Do not use the product checkout as `--workspace` unless the recipe is reviewing this repo's own range.

`run` requires `--model` or `UNREAL_HARNESS_LLM_MODEL`, and `OPENROUTER_API_KEY`. Dummy values are enough for paths that never start the agent (empty diff, complete-file refuse, SHA mismatch, missing `--from`, bad revision). A non-empty diff execs `--runner` (default `unreal-agent-runner`). An absolute missing runner fails at `fork/exec`; a relative missing runner fails at `LookPath` with `install github.com/unreallabsai/unreal-agent/cmd/unreal-agent-runner`.

Live review of a non-empty diff spends OpenRouter credit. Drive it only when the feature file's live sub-feature is in scope and `UNREAL_HARNESS_LLM_MODEL` is set. Otherwise record the unmet precondition and continue.

Do not post a GitHub review unless the user names a disposable pull request. Default GitHub proof is `--dry-run`.

Stable handles: subcommands `run`, `render markdown`, `render github`; flags `--from`, `--to`, `--exclude`, `--out`, `--fresh`, `--workspace`, `--dry-run`, `--pr`, `--token`; positional pathspecs; `examples/findings.jsonl`.

## Evidence

Root: `$VERIFY_ROOT/evidence/$VERIFY_RUN_ID/` (also printed by launch). Each `cli.sh --name <step>` writes `$VERIFY_EVIDENCE/<step>/{cmd.txt,stdout.txt,stderr.txt,exit.txt}`. Copy `--out` files into that step directory after the command.

Proof is the user-visible action plus the resulting state:

- Exit code and the `unreal-review: …` stderr line for failures
- For `run`, the findings JSONL (`type=run` / `finding` / `summary`), including `status`, `source.base`, `source.head`, `source.*_sha`, `diff_sha`, and `cost`
- Empty unified diff fingerprints as SHA-256 of the empty string: `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`
- For markdown, the rendered text (cost line, summary, `## \`path\``, severity bullets)
- For GitHub `--dry-run`, the JSON payload on stdout (`event: COMMENT`, `comments[].side` `RIGHT` for `anchor=new` and `LEFT` for `anchor=old`)

`--dry-run` skips `CreateReview`. With `--token`, `GH_TOKEN`, or `GITHUB_TOKEN` it still `GET`s the pull request and files. Without those, it prints the payload and does not call the API, even if `gh auth token` would succeed. Observe which of those happened; do not trust the flag name.

Do not treat `make check`, `go test`, or internal setters as a user-path proof.

## Cleanup

```bash
.grok/skills/verify-unreal-review/scripts/cleanup.sh
```

Stops only PIDs listed in `$VERIFY_SCRATCH/pids` (the default recipes are synchronous and write none). Deletes `$VERIFY_SCRATCH` for this run id. Leaves `$VERIFY_EVIDENCE` in place. After cleanup, `ls "$VERIFY_EVIDENCE"` must still list the step directories.

On a failed attempt, run cleanup for that run id before starting another.

## Helpers

All under `.grok/skills/verify-unreal-review/scripts/`. Executable. `VERIFY_RUN_ID` from launch/`current-run`.

| Script | Invocation |
| --- | --- |
| `launch.sh` | `.grok/skills/verify-unreal-review/scripts/launch.sh` |
| `doctor.sh` | `.grok/skills/verify-unreal-review/scripts/doctor.sh` |
| `cli.sh` | `…/cli.sh --name <step> [--no-github-auth] [--stdin file] -- <args>` |
| `fixture-repo.sh` | `…/fixture-repo.sh [name]` (default `review`) |
| `sample-findings.sh` | `…/sample-findings.sh complete\|running\|failed\|legacy\|empty <path>` |
| `cleanup.sh` | `.grok/skills/verify-unreal-review/scripts/cleanup.sh` |
