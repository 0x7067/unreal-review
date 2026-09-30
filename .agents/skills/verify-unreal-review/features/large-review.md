# Aggregate large review

`run` automatically reviews a selected diff above 200,000 bytes through bounded local and cross-scope boundary tasks. It publishes one parent findings file only after every task completes and every emitted candidate passes independent verification. `--decompose` uses the same coverage-gated path for a smaller diff.

## Sub-features

- `large-auto` automatically plans an oversized multi-file diff without a manual `group` loop.
- `large-singleton` splits an oversized single-file hunk at whole-line boundaries while retaining original old/new anchors.
- `large-boundary` reviews cross-scope contracts in addition to each local scope.
- `large-private` keeps discovery candidates out of the public root checkpoint.
- `large-coverage` requires exact completed and verified coverage for every plan task before completion.
- `large-resume` reuses durable completed tasks and cumulative cost after interruption.
- `large-source-change` refuses completion if the originally resolved diff changes during execution.
- `large-unsupported` rejects indivisible source/context/manifest input that cannot fit a bounded prompt rather than truncating it.
- `large-explicit` uses `--decompose` below the automatic threshold.

## How to get to it (user POV)

- Select a diff larger than 200,000 unified-diff bytes with the ordinary `run` range flags.
- Run a smaller review with `--decompose`.
- Resume with the same output, source, model, thinking level and strategy after cancellation.
- Use `--fresh` to deliberately create a new root and plan state.

## Driving it with verify-unreal-review

Preconditions:

- Launch and doctor have succeeded.
- Offline compiled canaries require only their scripted loopback provider and must make no external API calls.
- Live execution requires a real `OPENROUTER_API_KEY`, explicitly selected model and sufficient time/credit. Do not post to GitHub.

- **Compiled automatic path.** Run `VERIFY_ROOT="$VERIFY_ROOT" make canary`. The tagged CLI tests construct an exact 252,701-byte multi-file diff, exercise the release binary against a loopback Responses provider, and require local plus boundary task prompts before verification. The final root must retain the full source fingerprint and cumulative cost.
- **Oversized singleton.** The same canary constructs a file with more than 200,000 diff bytes and hundreds of separated hunks. Inspect the test artifact and request log: markers from every hunk and both old/new anchors must appear in bounded local prompts, never a silently shortened root diff.
- **Privacy and coverage.** While the provider is serving staged calls, repeatedly read the parent `findings.jsonl`. It may be running, but must not expose hypotheses or a clean summary. Completion is valid only after every local and boundary task has a successful receipt and all emitted candidates have passed bounded independent verification.
- **Interrupt and resume.** Interrupt the aggregate run after at least one task completes, preserve the output and adapter state, then repeat the identical command. The root id and source digest stay unchanged, completed tasks are not called again, and cost is cumulative. `render github` without `--dry-run` must reject the running checkpoint before any GitHub request.
- **Binding failures.** Repeat with a changed model/strategy, modified source, deleted planned state, or a different plan. Each must fail rather than restart under old child sessions. Use `--fresh` for an intentional restart.
- **Explicit small decomposition.** Run `scripts/cli.sh --name large-explicit -- run --decompose --model "$UNREAL_HARNESS_LLM_MODEL" --workspace "$VERIFY_FIXTURE" --out "$VERIFY_SCRATCH/large-explicit.jsonl"`. Inspect a complete root and cumulative cost. A fake-provider canary proves pipeline mechanics; a live invocation is required to assess review quality.
- **Unsupported input.** Create one changed line larger than the task prompt budget, then run with `--decompose`. Exit is nonzero, no provider call occurs, and stderr identifies unsupported indivisible input. Do not accept a partial findings file as proof.

## Gotchas

- Automatic decomposition is not arbitrary byte slicing. Local ownership covers every original diff byte exactly once; repeated metadata and boundary excerpts are context only.
- The current dependency hints include existing grouping and Go import edges. Other languages use a conservative full changed-scope manifest; this is not a claim of complete semantic graph or SCC resolution.
- `single` remains the practical strategy for automatic large reviews. Combining `--strategy focused` with decomposition multiplies discovery work and has not shown a seeded-defect recall gain in the small pilot.
- The 120,000 value is a serialized prompt-byte bound, not a model token guarantee. The model context limit can impose a lower practical bound.
- Adapter manifests and task artifacts live under `~/.local/state/unreal-agent/sessions/planned/`. Missing state on a resumed checkpoint is an error.
- Unknown provider usage from a process killed while a request is still in flight cannot be recovered.
- `group` remains useful for inspecting path relationships, but separately rendering its outputs can incorrectly imply complete reviewed-head coverage.
