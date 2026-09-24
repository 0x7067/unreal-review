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

`make check` (fmt, lint, vet, test, prove). `make prove` is `bend PROOF.bend --check-only`.

Do not add `bunfig.toml`. A new product rule is a `law` in `LAWS.bend` plus a proof in `PROOF.bend` in the same change. Do not edit `LAWS.bend` unless the user changes a product rule.
