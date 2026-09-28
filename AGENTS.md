# AGENTS.md

unreal-review is a pipeline of replaceable pieces around one product: [schema/findings-v1.md](schema/findings-v1.md).

## Product

`findings.jsonl` is the review and the checkpoint. The JSON shape is [schema/findings-v1.md](schema/findings-v1.md). Product invariants are [LAWS.bend](LAWS.bend); [PROOF.bend](PROOF.bend) is the certificate. Do not restate those laws here.

## Pieces

| Piece | Owns | Seam |
| --- | --- | --- |
| Source | The diff under review | git workspace (staged + unstaged + untracked vs HEAD), `--from`/`--to` (merge-base; omit `--to` for the working tree), `--commit` (parent..commit), `--branch` (merge-base of main/master), `--pr` (commits since the newest commit carrying an `unreal-review` check run; full base range when none exists), pathspecs, `--exclude`; `unreal-review group` prints pathspec groups from that range for separate reviews; inside `internal/review` |
| Agent | Prompt + workspace → findings JSONL and cost | `review.Agent` |
| Review | Checkpoint, SHA binding, prompt, status | `internal/review` |
| Renderer | Display a report | functions on `findings.Report` (markdown, GitHub) |

Wire a replacement at `cmd/unreal-review`. `AgentRequest.ReviewID` is the review id; any continuation mapping stays inside the adapter (`internal/agent` for the unreal-agent harness).

A new backend, source, or renderer should plug in without changing the findings schema.

## Skills

Project skills live only under `.agents/skills/`. The verification skill is [verify-unreal-review](.agents/skills/verify-unreal-review/SKILL.md). `.claude/skills`, `.grok/skills`, and `.cursor/skills` are relative symlinks to `../.agents/skills`. `.claude/settings.json` and the other tool hook and config files stay real files in their tool directories. Do not add `CLAUDE.md`, do not copy these instructions under a tool directory, and do not copy the skill tree back as regular files.

A checkout with `core.symlinks` false writes those three links as text files. Run the scripts from `.agents/skills/verify-unreal-review/scripts/`.

## Checks

`make check` (fmt, lint, vet, test, prove). `make prove` runs `tools/prove.sh`: Bend checks `PROOF.bend` and the script enforces `spec/unsafe-allow.txt` for laws that lean on `@unsafe` code (running `bend PROOF.bend --check-only` alone is not enough). `hooks/prove-stop.sh` runs the same gate when a turn ends and refuses the stop while the proof is red, for up to three consecutive failures before it stands down. It is registered project-level in `.claude/`, `.cursor/`, `.codex/` and `.grok/`; OpenCode (`.opencode/plugin/`) and Pi (`.pi/`) can only nudge, not block. For a one-shot verdict run `hooks/prove-stop.sh --check`.

`make canary` is the CLI regression run. It builds `bin/unreal-review` from the checkout under test and asserts the offline recipes in [.grok/skills/verify-unreal-review](.grok/skills/verify-unreal-review/SKILL.md), then execs that binary against a local GitHub stand-in for `run --pr` and `render github`. The canary needs no API secret and does not call openrouter.ai. A dummy `OPENROUTER_API_KEY` stays on the `run` steps whose recipe expects `401`, and `UNREAL_REVIEW_OPENROUTER_API` points those calls at a local stand-in that returns that status. The release binary honors `UNREAL_REVIEW_OPENROUTER_API` and `UNREAL_REVIEW_GITHUB_API` only when the URL host is loopback (`127.0.0.1`, `::1`, or `localhost`). Any other value is an error. CI jobs `check` and `canary` are the pair that must pass on pull requests and on pushes to `main` (both also run on `workflow_dispatch`). Marking those two checks required is a GitHub branch-protection setting. Workflow `Review` (`.github/workflows/review.yml`) is live dogfood: it reviews a same-repo pull request with the base commit's binary and posts to GitHub. `eval` stays outside both jobs.

Five of the six gate project-local hooks behind one-time trust, so a fresh clone is ungated until it is granted: Claude Code's workspace trust dialog, Codex `[hooks.state]` in `~/.codex/config.toml`, Cursor `--trust`, Pi `--approve`, Grok's `trusted_folders.toml`. An untrusted hook does not error, it just never runs.

`spec/` is a hand-written Bend model of the Go in `internal/`, and nothing checks that the two agree. A change to logic a law describes has to be mirrored in `spec/` in the same change, or the proof stays green while describing a program that no longer exists.

Do not add `bunfig.toml`. A new product rule is a `law` in `LAWS.bend` plus a proof in `PROOF.bend` in the same change. Do not edit `LAWS.bend` unless the user changes a product rule.
