# Review a git range

`unreal-review run` reviews a git diff in `--workspace` and writes `findings.jsonl`. With no range flags it reviews staged, unstaged, and untracked changes against `HEAD`. `--from`/`--to` is `git diff --merge-base`. `--commit` is that commit against its first parent. `--branch` is merge-base of `main` or `master`. An empty diff completes immediately with summary `No changes to review.` Pathspecs and `--exclude` omit files. A non-empty diff starts the agent.

## Sub-features

- `range-empty` completes an empty `--from HEAD --to HEAD` range without starting the agent.
- `range-workspace` with no range flags diffs the dirty tree and untracked files against `HEAD`.
- `range-detect-from` uses `main` (or `master` / `origin/main` / `origin/master`) with `--branch HEAD`.
- `range-missing-from` errors when `--branch` is set and no default branch name exists.
- `range-to-only` errors when `--to` is set without `--from`.
- `range-mixed` errors when `--commit` or `--branch` is combined with `--from`/`--to`.
- `range-bad-rev` errors on an unknown `--from` or `--to`.
- `range-working-tree` with `--from` and omitted `--to` diffs the working tree; `source.head` is empty and `source.head_sha` is `HEAD`.
- `range-commit` diffs one commit against its parent.
- `range-branch` diffs a branch since it diverged from `main`.
- `range-pathspec` limits the diff to positional pathspecs.
- `range-exclude` omits `--exclude` globs; excluding every changed path yields the empty-diff completion.
- `range-live` (skippable) reviews a non-empty range with the real runner and model.

## How to get to it (user POV)

- `unreal-review run --out findings.jsonl` from a git checkout (staged, unstaged, and untracked vs `HEAD`).
- `unreal-review run --from <rev> --to <rev> --out findings.jsonl`.
- `unreal-review run --from <rev> --out findings.jsonl` (working tree vs merge-base of that ref).
- `unreal-review run --commit <rev> --out findings.jsonl`.
- `unreal-review run --branch <name> --out findings.jsonl`.
- `unreal-review run --exclude '*.lock' -- cmd/`.
- GitHub Actions: `--from origin/${{ github.base_ref }} --to HEAD`.

## Driving it with verify-unreal-review

Preconditions:

- Launch and doctor have succeeded.
- `scripts/fixture-repo.sh` has printed `VERIFY_FIXTURE`, `BASE_SHA`, and `HEAD_SHA`.
- Dummy `OPENROUTER_API_KEY=dummy` and `--model x` are set for every `run` in this recipe except `range-live`.
- `--runner /tmp/no-such-runner` is passed on every non-live `run` so a non-empty diff cannot start a real agent.

- **Empty range.** Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name range-empty -- run --model x --runner /tmp/no-such-runner --workspace "$VERIFY_FIXTURE" --from HEAD --to HEAD --out "$VERIFY_SCRATCH/empty.jsonl"`. Exit code `0`. `stderr.txt` contains `cost: USD 0.000000`. Copy `empty.jsonl` into the step directory. The `run` record has `status":"complete"`, `source.base` and `source.head` equal to `HEAD`, equal `base_sha`/`head_sha`, `diff_sha` equal to `$VERIFY_EMPTY_DIFF_SHA`, and a summary `No changes to review.`
- **Workspace.** Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name range-workspace -- run --model x --runner /tmp/no-such-runner --workspace "$VERIFY_FIXTURE" --out "$VERIFY_SCRATCH/workspace.jsonl"`. Exit code `1` because dirty `hello.txt` and untracked `extra.txt` make a non-empty diff. `workspace.jsonl` has `"status":"failed"`, `"base":"HEAD"`, no `source.head` field, and a `diff_sha` other than `$VERIFY_EMPTY_DIFF_SHA`.
- **Detect default branch.** Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name range-detect -- run --model x --runner /tmp/no-such-runner --workspace "$VERIFY_FIXTURE" --branch HEAD --out "$VERIFY_SCRATCH/detect.jsonl"`. Exit code `0`. The `run` record has `"base":"main"`, `"head":"HEAD"`, and `diff_sha` equal to `$VERIFY_EMPTY_DIFF_SHA` (HEAD is the tip of main; `--branch` does not include the dirty working tree).
- **Missing default branch.** Create a second repo on `develop` with no `main`/`master`. Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name range-missing-from -- run --model x --runner /tmp/no-such-runner --workspace "$VERIFY_SCRATCH/repos/develop" --branch HEAD --out "$VERIFY_SCRATCH/develop.jsonl"`. Exit code `1`. `stderr.txt` contains `could not find main or master; set --from`.
- **`--to` without `--from`.** Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name range-to-only -- run --model x --runner /tmp/no-such-runner --workspace "$VERIFY_FIXTURE" --to HEAD --out "$VERIFY_SCRATCH/to-only.jsonl"`. Exit code `1`. `stderr.txt` contains `set --from or use --branch`.
- **Mixed flags.** Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name range-mixed -- run --model x --runner /tmp/no-such-runner --workspace "$VERIFY_FIXTURE" --from HEAD --commit HEAD --out "$VERIFY_SCRATCH/mixed.jsonl"`. Exit code `1`. `stderr.txt` contains `use only one of --from/--to, --commit, or --branch`.
- **Bad revision.** Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name range-bad-rev -- run --model x --runner /tmp/no-such-runner --workspace "$VERIFY_FIXTURE" --from nosuchrev --to HEAD --out "$VERIFY_SCRATCH/badfrom.jsonl"`. Exit code `1`. `stderr.txt` contains `git rev-parse nosuchrev` and `unknown revision`.
- **Working tree.** Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name range-working-tree -- run --model x --runner /tmp/no-such-runner --workspace "$VERIFY_FIXTURE" --from main --out "$VERIFY_SCRATCH/dirty.jsonl"`. Exit code `1` because the dirty `hello.txt` makes a non-empty diff and `/tmp/no-such-runner` does not exist. `stderr.txt` contains `status: failed` and `fork/exec /tmp/no-such-runner`. `dirty.jsonl` has `"status":"failed"`, no `source.head` field, `head_sha` equal to `HEAD_SHA`, and a `diff_sha` other than `$VERIFY_EMPTY_DIFF_SHA`.
- **Commit.** Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name range-commit -- run --model x --runner /tmp/no-such-runner --workspace "$VERIFY_FIXTURE" --commit HEAD --out "$VERIFY_SCRATCH/commit.jsonl"`. Exit code `1` (HEAD vs its parent is the fixture's second commit). `commit.jsonl` has `"head":"HEAD"` and `"base":"HEAD^"`.
- **Branch.** Same as detect: `--branch HEAD` on the fixture is empty. Keep `range-detect` as the proof. On a repo with a feature branch, `--branch feature` lists only files introduced on that branch.
- **Pathspec.** Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name range-pathspec -- run --model x --runner /tmp/no-such-runner --workspace "$VERIFY_FIXTURE" --from "$BASE_SHA" --to HEAD --out "$VERIFY_SCRATCH/path-lock.jsonl" -- skip.lock`. Exit code `1` (non-empty lockfile diff). `path-lock.jsonl` has `"status":"failed"` and a `diff_sha` other than `$VERIFY_EMPTY_DIFF_SHA`.
- **Exclude everything changed.** Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name range-exclude -- run --model x --runner /tmp/no-such-runner --workspace "$VERIFY_FIXTURE" --from "$BASE_SHA" --to HEAD --exclude '*.lock' --exclude 'hello.txt' --exclude 'cmd/*' --out "$VERIFY_SCRATCH/excl.jsonl"`. Exit code `0`. `excl.jsonl` is complete with summary `No changes to review.` and `diff_sha` equal to `$VERIFY_EMPTY_DIFF_SHA`. `source.base` is `$BASE_SHA` and `source.head` is `HEAD`.
- **Live review.** If `UNREAL_HARNESS_LLM_MODEL` is unset or `unreal-agent-runner` is missing, record `range-live` unreachable with that precondition and stop this bullet. Otherwise run `scripts/cli.sh --name range-live -- run --workspace "$VERIFY_FIXTURE" --from "$BASE_SHA" --to HEAD --out "$VERIFY_SCRATCH/live.jsonl" --exclude '*.lock'`. Exit code `0`. `live.jsonl` has `"status":"complete"`, a `cost.requests` greater than 0, and a summary line. `stderr.txt` contains `cost:`.
- **Proof.** Keep `range-empty/empty.jsonl` (copied) and `range-exclude/excl.jsonl`. Both complete with the empty `diff_sha`. Keep `range-workspace` and `range-working-tree` stderr showing `fork/exec` as proof those diffs were non-empty. Keep `range-to-only/stderr.txt` and `range-mixed/stderr.txt`.

## Gotchas

- No range flags is workspace mode (dirty tree plus untracked vs `HEAD`). That is a non-empty diff on the standard fixture.
- `--to HEAD` is the commit, not the dirty tree. `--from main` with omitted `--to` includes uncommitted edits. `--branch` is committed only.
- `--from`/`--to`, `--commit`, and `--branch` cannot be combined.
- Empty diffs never start the agent. A missing runner still exits 0 for `range-empty`.
- `--from`/`--to`/`--commit`/`--branch` accept a branch, tag, or SHA. The findings `source.base`/`source.head` store those strings; `*_sha` store the resolved objects.
- Pathspecs are positional after `--`. `--exclude` is repeatable. Size does not omit files.
- `range-live` spends OpenRouter credit. Dummy keys and `/tmp/no-such-runner` are required on every other `run` in this file so a mistake cannot bill a model.
