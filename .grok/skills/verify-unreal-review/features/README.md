# unreal-review verification map

This directory is the maintained source for verifying the user-facing behavior of the unreal-review CLI. Read the index before driving, then use the matching feature file as the recipe.

## Baseline preconditions

- `scripts/launch.sh` has built `$VERIFY_REPO/bin/unreal-review`.
- `scripts/doctor.sh` printed `doctor: ok`.
- Evidence and scratch live under `$VERIFY_ROOT` (`/tmp/verify-unreal-review` by default).
- Paths named `scripts/…` are relative to `.grok/skills/verify-unreal-review/`.
- Drive every command through `scripts/cli.sh --name <step> -- …`. `cli.sh` exits 0 after capture; read `exit.txt` for the CLI status.
- `run` uses `--workspace` from `scripts/fixture-repo.sh`, not the product checkout.
- Dummy `OPENROUTER_API_KEY` and `--model x` are enough when the agent will not start.
- Live review of a non-empty diff needs a real key, `UNREAL_HARNESS_LLM_MODEL` or `--model`, and `unreal-agent-runner`. Skip that sub-feature when any of those is missing.
- Do not post a GitHub review unless the user names a disposable pull request.

## Driving conventions

- Start every recipe from the baseline unless its preconditions say otherwise.
- Treat every command as literal. Keep flag names and example paths unchanged.
- Isolate GitHub auth with `cli.sh --no-github-auth` when the recipe must not send `GH_TOKEN`/`GITHUB_TOKEN` or succeed at `gh auth token`. `--dry-run` still GETs if `--token` is passed.
- Restore nothing in the product repo. Fixture repos are disposable.
- Keep proof artifacts; `scripts/cleanup.sh` removes scratch only.

## Proof and skip reporting

- CLI proof is the command, stdout, stderr, exit code, and any `--out` file.
- Mutation proof is a second read of the findings JSONL or rendered file.
- Record the feature id and `--name` step with every artifact.
- Report an unreachable path with the attempted command and the unmet precondition.
- Do not report a skipped live review or GitHub post as verified through a dry-run or empty-diff path.

## Feature entry contract

Each feature file starts with an H1 title and one paragraph describing the user-visible behavior. It then uses exactly four H2 sections in this order.

1. `Sub-features` lists short IDs with one line for each behavior.
2. `How to get to it (user POV)` lists every user entry point.
3. `Driving it with verify-unreal-review` starts with `Preconditions:` and uses labeled bullets that pair each user action with an exact command and observable result.
4. `Gotchas` lists traps that can waste or invalidate a verification run.

Keep implementation details out of the map. Name only user paths, stable handles, required state, commands, and observable proof.

## Features

- [CLI usage](./cli-usage.md) covers help, unknown commands, and the env/flag gates on `run`.
- [Review a git range](./review-git-range.md) covers `--from`/`--to`, empty diffs, pathspecs, `--exclude`, and the skippable live agent path.
- [Checkpoint and resume](./checkpoint.md) covers complete-file refuse, `--fresh`, SHA mismatch, failed resume, and empty-diff overwrite.
- [Render markdown](./render-markdown.md) covers stdout, `--out`, stdin, empty input, missing files, and incomplete status.
- [Render GitHub](./render-github.md) covers `--dry-run` payloads, posting gates, PR specs, and what dry-run still fetches.
