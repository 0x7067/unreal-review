# CLI usage

The CLI prints usage on stdout for `help` and on stderr when a command is missing or unknown. `run` refuses to start without a model, an API key, or a valid `--thinking-level`. Compaction stays silent unless `--compaction` or `UNREAL_REVIEW_COMPACTION` is set; an explicit value prints `compaction off` or the cutoff, or exits with an error, before the agent starts.

## Sub-features

- `usage-help` prints usage on stdout and exits 0.
- `usage-required` prints usage on stderr and exits 1 when no command is given.
- `usage-unknown` names the unknown command and exits 1.
- `run-model` refuses `run` without `--model` or `UNREAL_HARNESS_LLM_MODEL`.
- `run-key` refuses `run` without `OPENROUTER_API_KEY`.
- `run-thinking` rejects a thinking level other than `low`, `medium`, `high`, `xhigh`, or `max`.
- `run-exclude-empty` rejects an empty `--exclude`.
- `render-unknown` rejects a render target other than `github` or `markdown`.
- `render-required` rejects bare `render` with no target.
- `compaction-default` prints nothing about compaction when `--compaction` and `UNREAL_REVIEW_COMPACTION` are unset.
- `compaction-off` prints `compaction off` for `--compaction off` and `UNREAL_REVIEW_COMPACTION=off`.
- `compaction-zero` rejects `0`.
- `compaction-floor` rejects `149999`.
- `compaction-window` rejects a percent when the context window is unknown.
- `compaction-tokens` names the cutoff for `150000`.
- `compaction-percent` names the cutoff for `75%` of a `1000000` token window.

## How to get to it (user POV)

- Run `unreal-review` with no arguments.
- Run `unreal-review help`, `-h`, or `--help`.
- Run `unreal-review run -h` or `unreal-review render help`.
- Run `unreal-review run` without model or key.
- Run `unreal-review render` with no target.
- Run `unreal-review render html`.
- Run `unreal-review run` with compaction unset, `--compaction off`, `--compaction 0`, `--compaction 149999`, `--compaction 150000`, or `--compaction 75%` with `--context-window 1000000`.
- Set `UNREAL_REVIEW_COMPACTION` and `UNREAL_REVIEW_CONTEXT_WINDOW` in place of those flags.

## Driving it with verify-unreal-review

Preconditions:

- Launch and doctor have succeeded.
- This recipe unsets `UNREAL_HARNESS_LLM_MODEL` where noted so the model gate is visible.
- Unset `UNREAL_REVIEW_COMPACTION` and `UNREAL_REVIEW_CONTEXT_WINDOW` except where a step sets them.
- Accepted compaction values use `$VERIFY_FIXTURE` with `--from HEAD --to HEAD` so the agent does not start. A dummy `OPENROUTER_API_KEY` and `--model x` are enough. Rejected values stop at the gate.

- **Help.** Run `unreal-review help`. Run `scripts/cli.sh --name usage-help -- help`. Exit code `0`. `stdout.txt` contains `Usage:`, `unreal-review run`, and `unreal-review group`.
- **Missing command.** Run `unreal-review` with no args. Run `scripts/cli.sh --name usage-required --`. `exit.txt` is `1`. `stderr.txt` ends with `unreal-review: command required`.
- **Unknown command.** Run `scripts/cli.sh --name usage-unknown -- frob`. Exit code `1`. `stderr.txt` contains `unreal-review: unknown command "frob"`.
- **Missing model.** Run `env -u UNREAL_HARNESS_LLM_MODEL scripts/cli.sh --name run-model -- run --workspace "$VERIFY_SCRATCH" --out "$VERIFY_SCRATCH/unused.jsonl"`. Exit code `1`. `stderr.txt` contains `unreal-review: set --model or UNREAL_HARNESS_LLM_MODEL`.
- **Missing key.** Run `env -u OPENROUTER_API_KEY scripts/cli.sh --name run-key -- run --model x --workspace "$VERIFY_SCRATCH" --out "$VERIFY_SCRATCH/unused.jsonl"`. Exit code `1`. `stderr.txt` contains `unreal-review: set OPENROUTER_API_KEY`.
- **Bad thinking level.** Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name run-thinking -- run --model x --thinking-level nope --workspace "$VERIFY_SCRATCH" --out "$VERIFY_SCRATCH/unused.jsonl"`. Exit code `1`. `stderr.txt` contains `thinking level "nope": want low, medium, high, xhigh, or max`.
- **Empty exclude.** Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name run-exclude-empty -- run --model x --exclude '' --workspace "$VERIFY_SCRATCH" --out "$VERIFY_SCRATCH/unused.jsonl"`. Exit code `1`. `stderr.txt` contains `empty --exclude`.
- **Unknown render target.** Run `scripts/cli.sh --name render-unknown -- render html`. Exit code `1`. `stderr.txt` contains `unreal-review: unknown render target "html"`.
- **Missing render target.** Run `scripts/cli.sh --name render-required -- render`. Exit code `1`. `stderr.txt` contains `unreal-review: render target required: github or markdown`.
- **Compaction default.** Run `env -u UNREAL_REVIEW_COMPACTION -u UNREAL_REVIEW_CONTEXT_WINDOW OPENROUTER_API_KEY=dummy scripts/cli.sh --name compaction-default -- run --model x --workspace "$VERIFY_FIXTURE" --from HEAD --to HEAD --out "$VERIFY_SCRATCH/compaction-default.jsonl"`. Exit code `0`. `stderr.txt` contains `cost: USD 0.000000` and does not contain `compaction`.
- **Compaction off.** Run `env -u UNREAL_REVIEW_CONTEXT_WINDOW OPENROUTER_API_KEY=dummy scripts/cli.sh --name compaction-off -- run --model x --compaction off --workspace "$VERIFY_FIXTURE" --from HEAD --to HEAD --out "$VERIFY_SCRATCH/compaction-off.jsonl"`. Exit code `0`. `stderr.txt` contains `unreal-review: compaction off`.
- **Compaction off via env.** Run `env -u UNREAL_REVIEW_CONTEXT_WINDOW UNREAL_REVIEW_COMPACTION=off OPENROUTER_API_KEY=dummy scripts/cli.sh --name compaction-env-off -- run --model x --workspace "$VERIFY_FIXTURE" --from HEAD --to HEAD --out "$VERIFY_SCRATCH/compaction-env-off.jsonl"`. Exit code `0`. `stderr.txt` contains `unreal-review: compaction off`.
- **Compaction zero.** Run `env -u UNREAL_REVIEW_CONTEXT_WINDOW OPENROUTER_API_KEY=dummy scripts/cli.sh --name compaction-zero -- run --model x --compaction 0 --workspace "$VERIFY_SCRATCH" --out "$VERIFY_SCRATCH/unused.jsonl"`. Exit code `1`. `stderr.txt` contains `unreal-review: compaction "0": want off, a positive token count, or a percent from 1% to 100%`.
- **Compaction zero via env.** Run `env -u UNREAL_REVIEW_CONTEXT_WINDOW UNREAL_REVIEW_COMPACTION=0 OPENROUTER_API_KEY=dummy scripts/cli.sh --name compaction-env-zero -- run --model x --workspace "$VERIFY_SCRATCH" --out "$VERIFY_SCRATCH/unused.jsonl"`. Exit code `1`. `stderr.txt` contains `unreal-review: compaction "0": want off, a positive token count, or a percent from 1% to 100%`.
- **Compaction floor.** Run `env -u UNREAL_REVIEW_CONTEXT_WINDOW OPENROUTER_API_KEY=dummy scripts/cli.sh --name compaction-floor -- run --model x --compaction 149999 --workspace "$VERIFY_SCRATCH" --out "$VERIFY_SCRATCH/unused.jsonl"`. Exit code `1`. `stderr.txt` contains `unreal-review: compaction cutoff 149999 tokens is below 150000`.
- **Compaction floor via env.** Run `env -u UNREAL_REVIEW_CONTEXT_WINDOW UNREAL_REVIEW_COMPACTION=149999 OPENROUTER_API_KEY=dummy scripts/cli.sh --name compaction-env-floor -- run --model x --workspace "$VERIFY_SCRATCH" --out "$VERIFY_SCRATCH/unused.jsonl"`. Exit code `1`. `stderr.txt` contains `unreal-review: compaction cutoff 149999 tokens is below 150000`.
- **Unknown window.** Run `env -u UNREAL_REVIEW_COMPACTION -u UNREAL_REVIEW_CONTEXT_WINDOW OPENROUTER_API_KEY=dummy scripts/cli.sh --name compaction-window -- run --model x --compaction 75% --workspace "$VERIFY_SCRATCH" --out "$VERIFY_SCRATCH/unused.jsonl"`. Exit code `1`. `stderr.txt` contains `unreal-review: compaction "75%": context window unknown for "x"`.
- **Unknown window via env.** Run `env -u UNREAL_REVIEW_CONTEXT_WINDOW UNREAL_REVIEW_COMPACTION=75% OPENROUTER_API_KEY=dummy scripts/cli.sh --name compaction-env-window -- run --model x --workspace "$VERIFY_SCRATCH" --out "$VERIFY_SCRATCH/unused.jsonl"`. Exit code `1`. `stderr.txt` contains `unreal-review: compaction "75%": context window unknown for "x"`.
- **Token cutoff.** Run `env -u UNREAL_REVIEW_CONTEXT_WINDOW OPENROUTER_API_KEY=dummy scripts/cli.sh --name compaction-tokens -- run --model x --compaction 150000 --workspace "$VERIFY_FIXTURE" --from HEAD --to HEAD --out "$VERIFY_SCRATCH/compaction-tokens.jsonl"`. Exit code `0`. `stderr.txt` contains `unreal-review: compaction cutoff 150000 tokens`.
- **Token cutoff via env.** Run `env -u UNREAL_REVIEW_CONTEXT_WINDOW UNREAL_REVIEW_COMPACTION=150000 OPENROUTER_API_KEY=dummy scripts/cli.sh --name compaction-env-tokens -- run --model x --workspace "$VERIFY_FIXTURE" --from HEAD --to HEAD --out "$VERIFY_SCRATCH/compaction-env-tokens.jsonl"`. Exit code `0`. `stderr.txt` contains `unreal-review: compaction cutoff 150000 tokens`.
- **Percent cutoff.** Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name compaction-percent -- run --model x --compaction 75% --context-window 1000000 --workspace "$VERIFY_FIXTURE" --from HEAD --to HEAD --out "$VERIFY_SCRATCH/compaction-percent.jsonl"`. Exit code `0`. `stderr.txt` contains `unreal-review: compaction cutoff 750000 tokens`.
- **Percent cutoff via env.** Run `env UNREAL_REVIEW_COMPACTION=75% UNREAL_REVIEW_CONTEXT_WINDOW=1000000 OPENROUTER_API_KEY=dummy scripts/cli.sh --name compaction-env-percent -- run --model x --workspace "$VERIFY_FIXTURE" --from HEAD --to HEAD --out "$VERIFY_SCRATCH/compaction-env-percent.jsonl"`. Exit code `0`. `stderr.txt` contains `unreal-review: compaction cutoff 750000 tokens`.
- **Proof.** `usage-help/exit.txt` is `0` and `run-model/stderr.txt` contains the model error. Copy those four files under the evidence root; do not rely on the terminal scrollback.

## Gotchas

- `cli.sh --name usage-required --` invokes the binary with no command. `cli.sh` still exits 0; the CLI status is `exit.txt`.
- `run -h` prints flag usage on stderr and exits 1 (`unreal-review: flag: help requested`). Flag parsing still runs before the model check, so no model error appears even when `UNREAL_HARNESS_LLM_MODEL` is unset. Top-level `help`/`-h`/`--help` and the `render` help aliases (`render help`, `render -h`, `render --help`) exit 0.
- Flag parse errors (empty `--exclude`) print Go `flag` usage on stderr in addition to `unreal-review: …`.
