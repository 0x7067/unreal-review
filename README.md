# unreal-review

A local CLI that reviews a git diff with [unreal-agent-runner](https://github.com/unreallabsai/unreal-agent) and OpenRouter, then writes a portable findings file. GitHub inline comments are one renderer of that file.

## Install

```sh
go install github.com/unreallabsai/unreal-agent/cmd/unreal-agent-runner@v0.2.0
go install ./cmd/unreal-review
```

Set `OPENROUTER_API_KEY` and `UNREAL_HARNESS_LLM_MODEL` (an OpenRouter model id).

Install [golangci-lint](https://golangci-lint.run/) v2 for `make fmt` and `make lint`.

```sh
make install-lint
make fmt
make check
```

## Review a local change

From a git repository:

```sh
unreal-review run --out findings.jsonl
```

Omit `--from` and `--to` to review the working tree against the merge-base of `main` or `master`. `run` takes a git checkout (`--workspace`, default `.`). `--from` and `--to` accept a branch name, tag, or commit SHA when you want a pinned range. Pathspecs limit the range; `--exclude` omits globs. Binary files and diffs larger than 4000 lines are skipped. The remaining unified diff is sent to the agent with cwd at the checkout. It writes [findings.jsonl](schema/findings-v1.md). Every run records cost. Every finding has a severity: `error`, `warning`, or `note`.

```sh
unreal-review run --from origin/main --to HEAD --out findings.jsonl
unreal-review run --from abc1234 --to def5678 --exclude '*.lock' -- cmd/
```

`--out` is the checkpoint. The `run` record stores completeness (`status`) and the reviewed commit SHAs plus a hash of the exact diff. Interrupt the process to pause. The same command continues that review if the diff is unchanged. `--fresh` starts over. GitHub rendering refuses a file that is not `complete`. The checkpoint is the findings file; it does not name an agent backend.

```sh
unreal-review render markdown findings.jsonl
```

## Post inline comments on a GitHub pull request

```sh
unreal-review render github --pr owner/repo#12 findings.jsonl
```

`--dry-run` prints the GitHub review payload and does not post. The renderer maps `anchor: new` to the right side of the diff and drops findings whose lines are not in the pull request patch.

GitHub Actions is the same two commands. See [examples/github-actions/review.yml](examples/github-actions/review.yml).

## Findings file

The JSONL schema is the product. Renderers turn it into GitHub comments, markdown, or another display. See [schema/findings-v1.md](schema/findings-v1.md).
