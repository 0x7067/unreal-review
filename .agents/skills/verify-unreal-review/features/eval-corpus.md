# Eval corpus runs

`unreal-review eval` runs a review corpus through the same pipeline as `run` and scores the findings file. `--corpus planted` (default) reviews synthetic fixtures against planted issues; `--corpus martian` reviews Martian Code Review Bench pull requests and matches findings to golden comments; `--corpus pr38` reviews this repository's pull request #38 and scores labeled sites from earlier runs of that range; `--corpus labeled` reviews that range plus the other labeled pull requests and prints each case with a pooled score. Planted, martian, pr38, and labeled call the model and spend credit. `--runs` repeats pr38 and labeled.

## Sub-features

- `eval-model` refuses without `--model` or `UNREAL_HARNESS_LLM_MODEL`.
- `eval-key` refuses without `OPENROUTER_API_KEY`.
- `eval-strategy` rejects a strategy other than `single` or `focused`.
- `eval-corpus` rejects a corpus other than `planted`, `martian`, `pr38`, or `labeled`.
- `eval-runs` rejects `--runs` below 1.
- `eval-runs-corpus` rejects `--runs` other than 1 unless the corpus is `pr38` or `labeled`.
- `eval-runs-pr38` accepts `--runs` above 1 on the pr38 corpus and then requires a model.
- `eval-runs-labeled` accepts `--runs` above 1 on the labeled corpus and then requires a model.
- `eval-cases` rejects unknown or duplicate names in the selected corpus.
- `eval-planted` (skippable) reviews the planted fixtures and prints a score table or a `--json` summary with per-case `cases` and `total` fields.
- `eval-martian` (skippable) reviews martian pull requests under `--profile`, `--parallel`, and `--judge-model`.
- `eval-pr38` (skippable) reviews this repository's pull request #38 with `--runs` and prints each run, the mean, and the union of hits.

## How to get to it (user POV)

- `unreal-review eval --model <id> --out <dir>`
- `unreal-review eval --model <id> --cases race,clean --json --out <dir>`
- `unreal-review eval --model <id> --strategy focused --decompose --cases wrap-break --out <dir>`
- `unreal-review eval --corpus martian --model <id> --profile strict --parallel 4 --judge-model <id>`
- `unreal-review eval --corpus pr38 --runs 3 --out <dir>`
- `unreal-review eval --corpus labeled --runs 3 --out <dir>`

## Driving it with verify-unreal-review

Preconditions:

- Launch and doctor have succeeded.
- Dummy `OPENROUTER_API_KEY=dummy` and `--model x` cover every gate below; none of them start a case.
- Live runs need a real key and an explicit model and spend credit per case. Do not run them just to test flags.

- **Missing model.** Run `env -u UNREAL_HARNESS_LLM_MODEL scripts/cli.sh --name eval-model -- eval --out "$VERIFY_SCRATCH/e"`. Exit `1`. `stderr.txt` contains `set --model or UNREAL_HARNESS_LLM_MODEL`.
- **Missing key.** Run `env -u OPENROUTER_API_KEY scripts/cli.sh --name eval-key -- eval --model x --out "$VERIFY_SCRATCH/e"`. Exit `1`. `stderr.txt` contains `set OPENROUTER_API_KEY`.
- **Bad strategy.** Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name eval-strategy -- eval --strategy nope --model x --out "$VERIFY_SCRATCH/e"`. Exit `1`. `stderr.txt` contains `strategy "nope": want single or focused`. `make canary` also drives this gate as `focused-eval-invalid`.
- **Unknown corpus.** Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name eval-corpus -- eval --model x --corpus bogus --out "$VERIFY_SCRATCH/e"`. Exit `1`. `stderr.txt` contains `unknown corpus "bogus": use planted, martian, pr38, or labeled`.
- **Runs below one.** Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name eval-runs -- eval --model x --corpus pr38 --runs 0 --out "$VERIFY_SCRATCH/e"`. Exit `1`. `stderr.txt` contains `--runs must be at least 1`.
- **Runs on another corpus.** Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name eval-runs-corpus -- eval --model x --corpus planted --runs 3 --out "$VERIFY_SCRATCH/e"`. Exit `1`. `stderr.txt` contains `--runs applies to the pr38 and labeled corpora`.
- **pr38 runs above one.** Run `env -u UNREAL_HARNESS_LLM_MODEL scripts/cli.sh --name eval-runs-pr38 -- eval --corpus pr38 --runs 2 --out "$VERIFY_SCRATCH/e"`. Exit `1`. `stderr.txt` contains `set --model or UNREAL_HARNESS_LLM_MODEL`. It does not contain `--runs applies to the pr38 and labeled corpora`.
- **labeled runs above one.** Run `env -u UNREAL_HARNESS_LLM_MODEL scripts/cli.sh --name eval-runs-labeled -- eval --corpus labeled --runs 2 --out "$VERIFY_SCRATCH/e"`. Exit `1`. `stderr.txt` contains `set --model or UNREAL_HARNESS_LLM_MODEL`. It does not contain `--runs applies to the pr38 and labeled corpora`.
- **Unknown or duplicate case.** Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name eval-cases -- eval --model x --cases nosuch --out "$VERIFY_SCRATCH/e"`. Exit `1`. `stderr.txt` contains `unknown planted case "nosuch"`. `--cases race,race` prints `duplicate planted case "race"`.
- **Live planted.** If the model precondition is unmet, record `eval-planted` unreachable and stop this bullet. Otherwise run `scripts/cli.sh --name eval-planted -- eval --model "$UNREAL_HARNESS_LLM_MODEL" --cases race,clean --json --out "$VERIFY_SCRATCH/eval-planted"`. Inspect `total.completed`, per-case `status`/`err`, matched and extra findings, cost, requests, and elapsed; keep the stdout JSON and the per-case artifacts under `--out`. A single pass is a pilot, not a variance estimate.
- **Live martian.** Same unreachable rule. Run `scripts/cli.sh --name eval-martian -- eval --corpus martian --model "$UNREAL_HARNESS_LLM_MODEL" --judge-model <judge> --cases <name> --json --out "$VERIFY_SCRATCH/eval-martian"` and inspect the matched/golden accounting rather than treating exit 0 as a benchmark score.
- **Live pr38.** Same unreachable rule. Run `scripts/cli.sh --name eval-pr38 -- eval --corpus pr38 --model "$UNREAL_HARNESS_LLM_MODEL" --runs 3 --out "$VERIFY_SCRATCH/eval-pr38"`. The table has one row per run plus a mean and a union; read recall, precision, and error share. Repeat with `--json` and check `completed`, `failed`, `mean`, and `cost_usd`.
- **Live labeled.** Same unreachable rule. Run `scripts/cli.sh --name eval-labeled -- eval --corpus labeled --model "$UNREAL_HARNESS_LLM_MODEL" --runs 3 --out "$VERIFY_SCRATCH/eval-labeled"`. The text has one table per case, a pooled table, and a `spent` line; read recall, precision, and error share. Repeat with `--json` and check `cases`, `pooled`, and `spent_cost_usd`.
- **Proof.** Keep `eval-corpus/stderr.txt`, `eval-runs/stderr.txt`, `eval-runs-corpus/stderr.txt`, `eval-runs-pr38/stderr.txt`, `eval-runs-labeled/stderr.txt`, and `eval-cases/stderr.txt`. For a live run keep the stdout JSON and the `--out` directory.

## Gotchas

- `--cases` filters by name within the selected corpus; unknown and duplicate names fail before any case starts. Compare both strategies on the same selected cases.
- Exit 0 does not mean every case completed. Read `status`, `err`, and `total.completed` in the output.
- `--timeout` bounds the agent review inside each case, not checkout, Martian judging, or the whole invocation — `checkoutMartian` runs on the outer context with no deadline.
- `--out` is created if missing. Use a fresh directory per comparison or stale case artifacts contaminate it. A pr38 or labeled rerun with the same `--out` starts fresh; planted and martian do not.
- Planted scores and martian scores measure different things; a planted pilot is not a martian rank. Martian's built-in accounting is a direct matcher — the printed note explains how to get comparable benchmark scores.
