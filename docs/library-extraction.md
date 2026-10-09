# Extract the review logic as a reusable library

Status: done as of `v0.1.0`. `findings`, `diffmap`, `review`, and `agent`
are top-level packages under `module github.com/0x7067/unreal-review`; a
scratch consumer `go get`s the tag and completes `review.Run` against a
local git range with its own `review.Agent`.

## Why

`go.mod` declares `module unreal-review`. That is not a resolvable module
path, so another module cannot `require` it:

```
go get github.com/0x7067/unreal-review@<commit>
  parsing go.mod:
    module declares its path as: unreal-review
    but was required as: github.com/0x7067/unreal-review
```

Even with the path fixed, every package lives under `internal/` or `cmd/`,
and the Go toolchain forbids an outside module from importing `internal/`.
A git submodule does not help: the import restriction is on the source
tree, not on how it is vendored.

The goal is a library other Go programs can depend on: importable path,
versioned, and usable without shelling out to the CLI.

## What is already library-shaped

The code is cleanly layered. Only one package pulls in the network, and
only one pulls in the CLI. Import edges as they stand:

```
findings   (no internal deps, no external deps)
  ^  diffmap
  ^  review        (shells to git; no network)
       ^  agent    (unreal-agent harness; the agent strategy layer)
       ^  eval     (evaluation harness and corpora)
render  -> diffmap, findings, github     (presentation, CLI-facing)
github  -> network posting               (CLI-facing)
```

### Library material

- **`internal/findings`** — the output contract: `Report`, `Run`,
  `Finding`, `Source`, `Cost`, severity/anchor/status vocabularies, plus
  `Parse`, `Normalize`, `Fingerprint`, `AppendFinding`, `CheckSummary`,
  `AppendSummary`, `ReadFile`, `WriteFile`. Zero internal and zero
  external dependencies. This is the natural root package and should be
  importable first.
- **`internal/diffmap`** — `Map`, `New`, `ParseGitDiff`, `MergePatch`.
  Depends only on `findings`.
- **`internal/review`** — the engine core: `Run`, `Options`, `Result`,
  `Spec`, `Pull`, `PullResolver`, the `Agent` seam (`AgentRequest`,
  `AgentResult`), plan types (`ReviewPlan`, `PlanTask`, `DiffSpan`,
  `PlanCoverage`, `ValidatePlan`, `ValidateCoverage`), and `Groups` for
  change clustering. Depends only on `findings`; shells out to `git` for
  ranges.
- **`internal/agent`** — the discovery loop (planned, focused,
  observer, bounded bash, loopback client) over the `unreal-agent`
  harness. Depends on `findings`, `review`, and the harness. Extract only
  if consumers are expected to run the agent strategy themselves; it can
  also ship as a separate module or stay alongside the core.

### Keep out of the library

- `cmd/unreal-review` — flag parsing, secret loading, process wiring.
- `internal/render` and `internal/github` — Markdown/GitHub
  presentation and posting. Network- and host-facing; a CLI concern.
- `internal/eval` — evaluation harness and Martian corpora. Useful to
  reproduce the eval, but not needed to review a diff. Publish separately
  if at all.
- `secrets.go`, `hooks/`, `tools/` — CLI and local tooling.

## Proposed shape

Keep one repository, two layers. Minimal change, no behavior movement:

1. **Fix the module path.** `module github.com/0x7067/unreal-review`.
   Update every `internal/...` import to the new path in the same commit.
2. **Publish the library packages out of `internal/`.** Move
   `internal/findings`, `internal/diffmap`, `internal/review` (and
   `internal/agent` if desired) to top-level importable packages, for
   example `findings/`, `diffmap/`, `review/`, `agent/`. `cmd/` keeps its
   own `internal/` for CLI-only helpers if wanted.
3. **Expose a small public surface.** The library entry point is already
   `review.Run(ctx, review.Options{Workspace, Spec, Paths, Exclude, Out,
   Fresh, Model, Agent})`. Its dependency on the concrete agent strategy
   is already inverted through the `review.Agent` interface, so a consumer
   can supply its own model adapter. Publish the interfaces, keep the
   internals unexported.
4. **Tag releases.** `v0.1.0` once it builds and vets as an importable
   module, then SemVer from there.

Alternative if the CLI must not change: a nested module (for example
`library/` with its own `go.mod`) containing the library packages, with
the root module requiring it via a `replace`. One module is simpler and
preferred unless the CLI needs independence.

## Definition of done

- `go build ./...` and `go vet ./...` pass on the module.
- A throwaway module can `go get github.com/0x7067/unreal-review@<tag>`
  and call `review.Run` against a local git range with no `internal/`
  import error and no submodule.
- The CLI still works end to end from the same repository.
- `findings` remains dependency-free, so consumers can use the output
  schema without pulling the agent or the network stack.

## Non-goals

- No behavior change to review logic, prompts, or output schema.
- No new runtime dependencies; the library must stay pure Go over git and
  the model API, with no CLI, secret loading, or GitHub posting.
- No compatibility shims for the old bare module path. Nothing can have
  been importing it.

## First commit

Rename the module path and update imports. That alone unblocks
`require`; moving packages out of `internal/` is the second commit. Verify
between them with a scratch consumer module:

```sh
mkdir /tmp/consumer && cd /tmp/consumer
go mod init consumer
go get github.com/0x7067/unreal-review@<commit>
# a file that imports the review package must compile
```
