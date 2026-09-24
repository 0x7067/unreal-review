# AGENTS.md

unreal-review is a pipeline of replaceable pieces around one product: [schema/findings-v1.md](schema/findings-v1.md).

## Product

`findings.jsonl` is the review and the checkpoint. The JSON shape is [schema/findings-v1.md](schema/findings-v1.md). Product invariants are [LAWS.bend](LAWS.bend); [PROOF.bend](PROOF.bend) is the certificate. Do not restate those laws here.

## Pieces

| Piece | Owns | Seam |
| --- | --- | --- |
| Source | The diff under review | git workspace (staged + unstaged + untracked vs HEAD), `--from`/`--to` (merge-base; omit `--to` for the working tree), `--commit` (parent..commit), `--branch` (merge-base of main/master), pathspecs, `--exclude`; `unreal-review group` prints pathspec groups from that range for separate reviews; inside `internal/review` |
| Agent | Prompt + workspace → findings JSONL and cost | `review.Agent` |
| Review | Checkpoint, SHA binding, prompt, status | `internal/review` |
| Renderer | Display a report | functions on `findings.Report` (markdown, GitHub) |

Wire a replacement at `cmd/unreal-review`. `AgentRequest.ReviewID` is the review id; any continuation mapping stays inside the adapter (`internal/agent` for unreal-agent-runner).

A new backend, source, or renderer should plug in without changing the findings schema.

## Checks

`make check` (fmt, lint, vet, test, prove). `make prove` is `bend PROOF.bend --check-only`. `hooks/prove-stop.sh` runs the same gate when a turn ends and refuses the stop while the proof is red, for up to three consecutive failures before it stands down. It is registered project-level in `.claude/`, `.cursor/`, `.codex/` and `.grok/`; OpenCode (`.opencode/plugin/`) and Pi (`.pi/`) can only nudge, not block. For a one-shot verdict run `hooks/prove-stop.sh --check`.

Five of the six gate project-local hooks behind one-time trust, so a fresh clone is ungated until it is granted: Claude Code's workspace trust dialog, Codex `[hooks.state]` in `~/.codex/config.toml`, Cursor `--trust`, Pi `--approve`, Grok's `trusted_folders.toml`. An untrusted hook does not error, it just never runs.

`spec/` is a hand-written Bend model of the Go in `internal/`, and nothing checks that the two agree. A change to logic a law describes has to be mirrored in `spec/` in the same change, or the proof stays green while describing a program that no longer exists.

Do not add `bunfig.toml`. A new product rule is a `law` in `LAWS.bend` plus a proof in `PROOF.bend` in the same change. Do not edit `LAWS.bend` unless the user changes a product rule.
