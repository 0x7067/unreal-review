# Review a git range

`unreal-review run` reviews `git diff --merge-base` of `--from` and `--to` in `--workspace` and writes `findings.jsonl`. An empty diff completes immediately with summary `No changes to review.` Pathspecs and `--exclude` omit files. A non-empty diff starts the agent.

## Sub-features

- `range-empty` completes an empty `--from HEAD --to HEAD` range without starting the agent.
- `range-detect-from` uses `main` (or `master` / `origin/main` / `origin/master`) when `--from` is omitted.
- `range-missing-from` errors when no default branch name exists.
- `range-bad-rev` errors on an unknown `--from` or `--to`.
- `range-working-tree` with omitted `--to` diffs the working tree; `source.head` is empty and `source.head_sha` is `HEAD`.
- `range-pathspec` limits the diff to positional pathspecs.
- `range-exclude` omits `--exclude` globs; excluding every changed path yields the empty-diff completion.
- `range-live` (skippable) reviews a non-empty range with the real runner and model.

## How to get to it (user POV)

- `unreal-review run --out findings.jsonl` from a git checkout (working tree vs merge-base of main/master).
- `unreal-review run --from <rev> --to <rev> --out findings.jsonl`.
- `unreal-review run --exclude '*.lock' -- cmd/`.
- GitHub Actions: `--from origin/${{ github.base_ref }} --to HEAD`.

## Driving it with verify-unreal-review

Preconditions:

- Launch and doctor have succeeded.
- `scripts/fixture-repo.sh` has printed `VERIFY_FIXTURE`, `BASE_SHA`, and `HEAD_SHA`.
- Dummy `OPENROUTER_API_KEY=dummy` and `--model x` are set for every `run` in this recipe except `range-live`.
- `--runner /tmp/no-such-runner` is passed on every non-live `run` so a non-empty diff cannot start a real agent.

- **Empty range.** Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name range-empty -- run --model x --runner /tmp/no-such-runner --workspace "$VERIFY_FIXTURE" --from HEAD --to HEAD --out "$VERIFY_SCRATCH/empty.jsonl"`. Exit code `0`. `stderr.txt` contains `cost: USD 0.000000`. Copy `empty.jsonl` into the step directory. The `run` record has `status":"complete"`, `source.base` and `source.head` equal to `HEAD`, equal `base_sha`/`head_sha`, `diff_sha` equal to `$VERIFY_EMPTY_DIFF_SHA`, and a summary `No changes to review.`
- **Detect --from.** Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name range-detect -- run --model x --runner /tmp/no-such-runner --workspace "$VERIFY_FIXTURE" --to HEAD --out "$VERIFY_SCRATCH/detect.jsonl"`. Exit code `0`. The `run` record has `"base":"main"` and `diff_sha` equal to `$VERIFY_EMPTY_DIFF_SHA` (HEAD is the tip of main; `--to HEAD` does not include the dirty working tree).
- **Missing default branch.** Create a second repo on `develop` with no `main`/`master`. Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name range-missing-from -- run --model x --runner /tmp/no-such-runner --workspace "$VERIFY_SCRATCH/repos/develop" --out "$VERIFY_SCRATCH/develop.jsonl"`. Exit code `1`. `stderr.txt` contains `set --from; could not find main or master`.
- **Bad revision.** Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name range-bad-rev -- run --model x --runner /tmp/no-such-runner --workspace "$VERIFY_FIXTURE" --from nosuchrev --to HEAD --out "$VERIFY_SCRATCH/badfrom.jsonl"`. Exit code `1`. `stderr.txt` contains `git rev-parse nosuchrev` and `unknown revision`.
- **Working tree.** Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name range-working-tree -- run --model x --runner /tmp/no-such-runner --workspace "$VERIFY_FIXTURE" --from main --out "$VERIFY_SCRATCH/dirty.jsonl"`. Exit code `1` because the dirty `hello.txt` makes a non-empty diff and `/tmp/no-such-runner` does not exist. `stderr.txt` contains `status: failed` and `fork/exec /tmp/no-such-runner`. `dirty.jsonl` has `"status":"failed"`, no `source.head` field, `head_sha` equal to `HEAD_SHA`, and a `diff_sha` other than `$VERIFY_EMPTY_DIFF_SHA`.
- **Pathspec.** Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name range-pathspec -- run --model x --runner /tmp/no-such-runner --workspace "$VERIFY_FIXTURE" --from "$BASE_SHA" --to HEAD --out "$VERIFY_SCRATCH/path-lock.jsonl" -- skip.lock`. Exit code `1` (non-empty lockfile diff). `path-lock.jsonl` has `"status":"failed"` and a `diff_sha` other than `$VERIFY_EMPTY_DIFF_SHA`.
- **Exclude everything changed.** Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name range-exclude -- run --model x --runner /tmp/no-such-runner --workspace "$VERIFY_FIXTURE" --from "$BASE_SHA" --to HEAD --exclude '*.lock' --exclude 'hello.txt' --exclude 'cmd/*' --out "$VERIFY_SCRATCH/excl.jsonl"`. Exit code `0`. `excl.jsonl` is complete with summary `No changes to review.` and `diff_sha` equal to `$VERIFY_EMPTY_DIFF_SHA`. `source.base` is `$BASE_SHA` and `source.head` is `HEAD`.
- **Live review.** If `UNREAL_HARNESS_LLM_MODEL` is unset or `unreal-agent-runner` is missing, record `range-live` unreachable with that precondition and stop this bullet. Otherwise run `scripts/cli.sh --name range-live -- run --workspace "$VERIFY_FIXTURE" --from "$BASE_SHA" --to HEAD --out "$VERIFY_SCRATCH/live.jsonl" --exclude '*.lock'`. Exit code `0`. `live.jsonl` has `"status":"complete"`, a `cost.requests` greater than 0, and a summary line. `stderr.txt` contains `cost:`.
- **Proof.** Keep `range-empty/empty.jsonl` (copied) and `range-exclude/excl.jsonl`. Both complete with the empty `diff_sha`. Keep `range-working-tree` stderr showing `fork/exec` as proof the dirty tree was non-empty.

## Gotchas

- `--to HEAD` is the commit, not the dirty tree. Omit `--to` to include uncommitted edits.
- Empty diffs never start the agent. A missing runner still exits 0 for `range-empty`.
- `--from`/`--to` accept a branch, tag, or SHA. The findings `source.base`/`source.head` store those strings; `*_sha` store the resolved objects.
- Pathspecs are positional after `--`. `--exclude` is repeatable. Size does not omit files.
- `range-live` spends OpenRouter credit. Dummy keys and `/tmp/no-such-runner` are required on every other `run` in this file so a mistake cannot bill a model.
