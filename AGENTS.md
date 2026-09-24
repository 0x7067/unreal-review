# AGENTS.md

unreal-review is a pipeline of replaceable pieces around one product: [schema/findings-v1.md](schema/findings-v1.md).

## Product

`findings.jsonl` is the review. It is also the checkpoint: `run.id`, `run.status`, and `run.source` (`base_sha`, `head_sha`, `diff_sha`) bind findings to one diff. Completeness is `status=complete`. This file must not name an agent backend, a harness session, or a renderer.

The selected git range is reviewed in full. Pathspecs and `--exclude` omit files; size does not.

## Pieces

| Piece | Owns | Seam |
| --- | --- | --- |
| Source | The diff under review | git `--from`/`--to` (branch, tag, or SHA; both optional: working tree vs merge-base of main/master), pathspecs, `--exclude`; inside `internal/review` |
| Agent | Prompt + workspace → findings JSONL and cost | `review.Agent` |
| Review | Checkpoint, SHA binding, prompt, status | `internal/review` |
| Renderer | Display a report | functions on `findings.Report` (markdown, GitHub) |

Wire a replacement at `cmd/unreal-review`. Do not leak a backend's session, log, or wire types into `findings` or the checkpoint. `AgentRequest.ReviewID` is the review id; any continuation mapping stays inside the adapter (`internal/agent` for unreal-agent-runner).

A new backend, source, or renderer should plug in without changing the findings schema. If a change needs a field that only one backend understands, it does not belong on `run`.

## Checks

`make check` (fmt, lint, vet, test).
