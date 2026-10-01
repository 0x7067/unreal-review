# Eval corpus runs

`unreal-review eval` runs a review corpus through the same pipeline as `run` and scores the findings file. `--corpus planted` (default) reviews synthetic fixtures against planted issues; `--corpus martian` reviews Martian Code Review Bench pull requests and matches findings to golden comments. Both call the model and spend credit.

## Sub-features

- `eval-model` refuses without `--model` or `UNREAL_HARNESS_LLM_MODEL`.
- `eval-key` refuses without `OPENROUTER_API_KEY`.
- `eval-strategy` rejects a strategy other than `single` or `focused`.
- `eval-corpus` rejects a corpus other than `planted` or `martian`.
- `eval-cases` rejects unknown or duplicate names in the selected corpus.
- `eval-planted` (skippable) reviews the planted fixtures and prints a score table or `--json` totals.
- `eval-martian` (skippable) reviews martian pull requests under `--profile`, `--parallel`, and `--judge-model`.

## How to get to it (user POV)

- `unreal-review eval --model <id> --out <dir>`
- `unreal-review eval --cases race,clean --json --out <dir>`
- `unreal-review eval --strategy focused --decompose --cases wrap-break --out <dir>`
- `unreal-review eval --corpus martian --profile strict --parallel 4 --judge-model <id>`

## Driving it with verify-unreal-review

Preconditions:

- Launch and doctor have succeeded.
- Dummy `OPENROUTER_API_KEY=dummy` and `--model x` cover every gate below; none of them start a case.
- Live runs need a real key and an explicit model and spend credit per case. Do not run them just to test flags.

- **Missing model.** Run `env -u UNREAL_HARNESS_LLM_MODEL scripts/cli.sh --name eval-model -- eval --out "$VERIFY_SCRATCH/e"`. Exit `1`. `stderr.txt` contains `set --model or UNREAL_HARNESS_LLM_MODEL`.
- **Missing key.** Run `env -u OPENROUTER_API_KEY scripts/cli.sh --name eval-key -- eval --model x --out "$VERIFY_SCRATCH/e"`. Exit `1`. `stderr.txt` contains `set OPENROUTER_API_KEY`.
- **Bad strategy.** Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name eval-strategy -- eval --strategy nope --model x --out "$VERIFY_SCRATCH/e"`. Exit `1`. `stderr.txt` contains `strategy "nope": want single or focused`. `make canary` also drives this gate as `focused-eval-invalid`.
- **Unknown corpus.** Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name eval-corpus -- eval --model x --corpus bogus --out "$VERIFY_SCRATCH/e"`. Exit `1`. `stderr.txt` contains `unknown corpus "bogus": use planted or martian`.
- **Unknown or duplicate case.** Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name eval-cases -- eval --model x --cases nosuch --out "$VERIFY_SCRATCH/e"`. Exit `1`. `stderr.txt` contains `unknown planted case "nosuch"`. `--cases race,race` prints `duplicate planted case "race"`.
- **Live planted.** If the model precondition is unmet, record `eval-planted` unreachable and stop this bullet. Otherwise run `scripts/cli.sh --name eval-planted -- eval --model "$UNREAL_HARNESS_LLM_MODEL" --cases race,clean --json --out "$VERIFY_SCRATCH/eval-planted"`. Inspect `total.completed`, per-case `status`/`err`, matched and extra findings, cost, requests, and elapsed; keep the stdout JSON and the per-case artifacts under `--out`. A single pass is a pilot, not a variance estimate.
- **Live martian.** Same unreachable rule. Run `scripts/cli.sh --name eval-martian -- eval --corpus martian --model "$UNREAL_HARNESS_LLM_MODEL" --judge-model <judge> --cases <name> --json --out "$VERIFY_SCRATCH/eval-martian"` and inspect the matched/golden accounting rather than treating exit 0 as a benchmark score.
- **Proof.** Keep `eval-corpus/stderr.txt` and `eval-cases/stderr.txt`. For a live run keep the stdout JSON and the `--out` directory.

## Gotchas

- `--cases` filters by name within the selected corpus; unknown and duplicate names fail before any case starts. Compare both strategies on the same selected cases.
- Exit 0 does not mean every case completed. Read `status`, `err`, and `total.completed` in the output.
- `--timeout` bounds each case, not the whole invocation.
- `--out` is created if missing. Use a fresh directory per comparison or stale case artifacts contaminate it.
- Planted scores and martian scores measure different things; a planted pilot is not a martian rank. Martian's built-in accounting is a direct matcher — the printed note explains how to get comparable benchmark scores.
