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
unreal-review run --base main --out findings.jsonl
```

`run` takes a git checkout (`--workspace`, default `.`), diffs `--base` against the working tree (or `--head`), and sends that unified diff to the agent with cwd at the checkout. It writes [findings.jsonl](schema/findings-v1.md). Every run records cost. Every finding has a severity: `error`, `warning`, or `note`.

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
