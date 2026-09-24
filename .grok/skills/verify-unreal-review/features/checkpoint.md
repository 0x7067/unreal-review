# Checkpoint and resume

`--out` is the checkpoint. A complete file refuses another `run` unless `--fresh`. A running or failed file refuses a different diff. The same command continues when the hashes match. An empty diff writes a new complete file and does not apply those refuses.

## Sub-features

- `ckpt-complete-refuse` errors when `--out` is already a complete review of a non-empty current diff.
- `ckpt-fresh` starts over on a complete file when `--fresh` is passed.
- `ckpt-sha-mismatch` errors when a non-complete checkpoint's `base_sha`/`head_sha`/`diff_sha` do not match the workspace.
- `ckpt-failed-resume` retries a failed checkpoint against the same diff (agent starts again).
- `ckpt-empty-overwrite` rewrites `--out` on an empty current diff even when the file is already complete.

## How to get to it (user POV)

- Re-run `unreal-review run` with the same `--out findings.jsonl`.
- Interrupt a review and run the same command to continue.
- Pass `--fresh` to replace a complete file.
- Change the git range or working tree and reuse `--out`.

## Driving it with verify-unreal-review

Preconditions:

- Launch and doctor have succeeded.
- `scripts/fixture-repo.sh` has printed `VERIFY_FIXTURE`, `BASE_SHA`, and `HEAD_SHA`.
- `OPENROUTER_API_KEY=dummy`, `--model x`, and `--runner /tmp/no-such-runner` on every command here.

- **Complete empty file.** Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name ckpt-seed-empty -- run --model x --runner /tmp/no-such-runner --workspace "$VERIFY_FIXTURE" --from HEAD --to HEAD --out "$VERIFY_SCRATCH/empty.jsonl"`. Exit code `0`. `empty.jsonl` is complete with `$VERIFY_EMPTY_DIFF_SHA`.
- **Empty overwrite.** Run the same command again as `OPENROUTER_API_KEY=dummy scripts/cli.sh --name ckpt-empty-overwrite -- run --model x --runner /tmp/no-such-runner --workspace "$VERIFY_FIXTURE" --from HEAD --to HEAD --out "$VERIFY_SCRATCH/empty.jsonl"`. Exit code `0`. The `run.id` in `empty.jsonl` changed. This is the empty-diff overwrite, not a refuse.
- **Complete refuse.** Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name ckpt-complete-refuse -- run --model x --runner /tmp/no-such-runner --workspace "$VERIFY_FIXTURE" --from "$BASE_SHA" --to HEAD --out "$VERIFY_SCRATCH/empty.jsonl"`. Exit code `1`. `stderr.txt` contains `is a complete review of from` and `pass --fresh to start over`. `empty.jsonl` is still the previous complete empty review.
- **Fresh.** Run the same range with `--fresh` as `OPENROUTER_API_KEY=dummy scripts/cli.sh --name ckpt-fresh -- run --fresh --model x --runner /tmp/no-such-runner --workspace "$VERIFY_FIXTURE" --from "$BASE_SHA" --to HEAD --out "$VERIFY_SCRATCH/empty.jsonl"`. Exit code `1` (`fork/exec /tmp/no-such-runner`). `empty.jsonl` now has `"status":"failed"`, `source.base` equal to `$BASE_SHA`, and a non-empty `diff_sha`.
- **Failed resume, same diff.** Run without `--fresh` as `OPENROUTER_API_KEY=dummy scripts/cli.sh --name ckpt-failed-resume -- run --model x --runner /tmp/no-such-runner --workspace "$VERIFY_FIXTURE" --from "$BASE_SHA" --to HEAD --out "$VERIFY_SCRATCH/empty.jsonl"`. Exit code `1` with `fork/exec` again. The `run.id` is unchanged from `ckpt-fresh`.
- **SHA mismatch.** Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name ckpt-working-failed -- run --model x --runner /tmp/no-such-runner --workspace "$VERIFY_FIXTURE" --from main --out "$VERIFY_SCRATCH/dirty.jsonl"` (dirty working tree, failed). Then `OPENROUTER_API_KEY=dummy scripts/cli.sh --name ckpt-sha-mismatch -- run --model x --runner /tmp/no-such-runner --workspace "$VERIFY_FIXTURE" --from "$BASE_SHA" --to HEAD --out "$VERIFY_SCRATCH/dirty.jsonl"`. Exit code `1`. `stderr.txt` matches `is a review of from <12-char sha> to <12-char sha> diff <12-char sha>; workspace is from <12-char sha> to <12-char sha> diff <12-char sha>`. `dirty.jsonl` is unchanged.
- **Proof.** Keep `ckpt-complete-refuse/stderr.txt` (`pass --fresh`) and `ckpt-sha-mismatch/stderr.txt` (`workspace is`). Keep copies of `empty.jsonl` after overwrite and after `--fresh`.

## Gotchas

- Empty current diffs skip checkpoint comparison and always persist a new complete `run`. Prove refuse against a non-empty range.
- Completeness is `status=complete`. Missing `status` is treated as complete. `running` and `failed` are resumable when hashes match.
- `--fresh` on a non-empty range starts the agent. Keep `--runner /tmp/no-such-runner` so this recipe cannot bill a model.
- Cost on a resumed run is additive. This recipe never records a non-zero cost.
