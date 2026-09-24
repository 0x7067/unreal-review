# CLI usage

The CLI prints usage on stdout for `help` and on stderr when a command is missing or unknown. `run` refuses to start without a model, an API key, a resolvable runner, or a valid `--thinking-level`.

## Sub-features

- `usage-help` prints usage on stdout and exits 0.
- `usage-required` prints usage on stderr and exits 1 when no command is given.
- `usage-unknown` names the unknown command and exits 1.
- `run-model` refuses `run` without `--model` or `UNREAL_HARNESS_LLM_MODEL`.
- `run-key` refuses `run` without `OPENROUTER_API_KEY`.
- `run-thinking` rejects a thinking level other than `low`, `medium`, `high`, `xhigh`, or `max`.
- `run-exclude-empty` rejects an empty `--exclude`.
- `run-runner` refuses a relative runner that is not on `PATH`.
- `render-unknown` rejects a render target other than `github` or `markdown`.

## How to get to it (user POV)

- Run `unreal-review` with no arguments.
- Run `unreal-review help`, `-h`, or `--help`.
- Run `unreal-review run -h` or `unreal-review render help`.
- Run `unreal-review run` without model, key, or runner.
- Run `unreal-review render html`.

## Driving it with verify-unreal-review

Preconditions:

- Launch and doctor have succeeded.
- This recipe unsets `UNREAL_HARNESS_LLM_MODEL` where noted so the model gate is visible.

- **Help.** Run `unreal-review help`. Run `scripts/cli.sh --name usage-help -- help`. Exit code `0`. `stdout.txt` contains `Usage:`, `unreal-review run`, and `unreal-review group`.
- **Missing command.** Run `unreal-review` with no args. Run `scripts/cli.sh --name usage-required --`. `exit.txt` is `1`. `stderr.txt` ends with `unreal-review: command required`.
- **Unknown command.** Run `scripts/cli.sh --name usage-unknown -- frob`. Exit code `1`. `stderr.txt` contains `unreal-review: unknown command "frob"`.
- **Missing model.** Run `env -u UNREAL_HARNESS_LLM_MODEL scripts/cli.sh --name run-model -- run --workspace "$VERIFY_SCRATCH" --out "$VERIFY_SCRATCH/unused.jsonl"`. Exit code `1`. `stderr.txt` contains `unreal-review: set --model or UNREAL_HARNESS_LLM_MODEL`.
- **Missing key.** Run `env -u OPENROUTER_API_KEY -u UNREAL_HARNESS_LLM_API_KEY scripts/cli.sh --name run-key -- run --model x --workspace "$VERIFY_SCRATCH" --out "$VERIFY_SCRATCH/unused.jsonl"`. Exit code `1`. `stderr.txt` contains `unreal-review: set OPENROUTER_API_KEY`.
- **Bad thinking level.** Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name run-thinking -- run --model x --thinking-level nope --workspace "$VERIFY_SCRATCH" --out "$VERIFY_SCRATCH/unused.jsonl"`. Exit code `1`. `stderr.txt` contains `thinking level "nope": want low, medium, high, xhigh, or max`.
- **Empty exclude.** Run `OPENROUTER_API_KEY=dummy scripts/cli.sh --name run-exclude-empty -- run --model x --exclude '' --workspace "$VERIFY_SCRATCH" --out "$VERIFY_SCRATCH/unused.jsonl"`. Exit code `1`. `stderr.txt` contains `empty --exclude`.
- **Missing relative runner.** Run `scripts/fixture-repo.sh` then `OPENROUTER_API_KEY=dummy scripts/cli.sh --name run-runner -- run --model x --runner not-a-runner-xyz --workspace "$VERIFY_FIXTURE" --from HEAD --to HEAD --out "$VERIFY_SCRATCH/rel.jsonl"`. `exit.txt` is `1`. `stderr.txt` contains `find not-a-runner-xyz` and `install github.com/unreallabsai/unreal-agent/cmd/unreal-agent-runner`.
- **Unknown render target.** Run `scripts/cli.sh --name render-unknown -- render html`. Exit code `1`. `stderr.txt` contains `unreal-review: unknown render target "html"`.
- **Proof.** `usage-help/exit.txt` is `0` and `run-model/stderr.txt` contains the model error. Copy those four files under the evidence root; do not rely on the terminal scrollback.

## Gotchas

- `cli.sh --name usage-required --` invokes the binary with no command. `cli.sh` still exits 0; the CLI status is `exit.txt`.
- `run -h` exits 0 even when `UNREAL_HARNESS_LLM_MODEL` is unset; the model check runs after flag parse.
- An absolute `--runner` path is not checked with `LookPath`. A missing absolute runner is not this feature; it surfaces later as `fork/exec`.
- Flag parse errors (empty `--exclude`) print Go `flag` usage on stderr in addition to `unreal-review: …`.
