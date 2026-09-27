# unreal-review

A local CLI that reviews a git diff with the embedded [unreal-agent](https://github.com/unreallabsai/unreal-agent) harness and OpenRouter, then writes a portable findings file. GitHub inline comments are one renderer of that file.

## Install

```sh
go install ./cmd/unreal-review
```

Set `OPENROUTER_API_KEY` and `UNREAL_HARNESS_LLM_MODEL` (an OpenRouter model id).

Install [golangci-lint](https://golangci-lint.run/) v2 for `make fmt` and `make lint`.

```sh
make install-lint
make fmt
make check
```

`make check` includes `make prove` (`tools/prove.sh`: checks `PROOF.bend` with Bend and enforces `spec/unsafe-allow.txt`). Product invariants live in `LAWS.bend`. Install Bend from https://bend-lang.com (`bend version` 2.0.27 is the local toolchain).

## Review a local change

From a git repository:

```sh
unreal-review run --out findings.jsonl
```

With no range flags, `run` reviews staged, unstaged, and untracked changes against `HEAD`. `run` takes a git checkout (`--workspace`, default `.`). `--from` and `--to` select `git diff --merge-base` of those refs (branch, tag, or SHA); omit `--to` to include the working tree. `--commit` reviews one commit against its first parent. `--branch` reviews a branch since it diverged from `main` or `master`. Pathspecs limit the range; `--exclude` omits globs. The selected range is sent in full to the agent with cwd at the checkout. It writes [findings.jsonl](schema/findings-v1.md).

```sh
unreal-review run --from origin/main --to HEAD --out findings.jsonl
unreal-review run --branch feature --out findings.jsonl
unreal-review run --commit abc1234 --out findings.jsonl
unreal-review run --from abc1234 --to def5678 --exclude '*.lock' -- cmd/
```

`--out` names the checkpoint file; see [schema/findings-v1.md](schema/findings-v1.md) for the `run` record, resume rules, and `--fresh`.

To review a large change in pieces, run `group` on the same git range. It prints related file groups and a `run` command for each (`unreal-review group -h` for range flags):

```sh
unreal-review group --from origin/main --to HEAD
```

Each `run` writes its own findings file and `diff_sha`.

```sh
unreal-review render markdown findings.jsonl
```

## Post inline comments on a GitHub pull request

```sh
unreal-review render github --pr owner/repo#12 findings.jsonl
```

`--dry-run` prints the GitHub review payload and does not post. The renderer maps `anchor: new` to the right side of the diff and drops findings whose lines are not in the pull request patch. `run --pr` narrows the range to commits pushed since the newest reviewed commit (tracked by check runs), names already-posted findings in the prompt, and the renderer suppresses duplicates before the 50-comment cap, tags each comment with a marker, and keeps a status comment on the PR holding the run count and cumulative cost. A review is posted only when it says something new; the status comment still updates.

GitHub Actions is the same two commands. This repository reviews its own pull requests with [.github/workflows/review.yml](.github/workflows/review.yml); [examples/github-actions/review.yml](examples/github-actions/review.yml) is the shape to copy into another repository.

## Evaluate a model

```sh
unreal-review eval --model openai/gpt-6-luna-pro
unreal-review eval --json --model openai/gpt-6-luna-pro > eval.json
```

`eval` runs a planted-issue corpus of eleven cases — races and a
self-deadlock, nil dereferences and swallowed errors and a broken
error chain, a removed bounds guard, SQL injection, a leaked
goroutine, an unclosed response body, a poisoned `sync.Once`, and a
clean control — through the same pipeline as `run`, then
scores each findings file against the planted issues. `recall` is the
share of planted issues found (same file, overlapping lines);
`precision` is the share of findings that match something planted;
`severity` is the share of planted issues found and graded with the
expected severity; `status` is the review checkpoint. Each case gets a
fresh git workspace and its own findings.jsonl under `--out`. Eval calls
the model and costs money; it is not part of `make check`.

## Findings file

The JSONL schema is the product. Renderers turn it into GitHub comments, markdown, or another display. See [schema/findings-v1.md](schema/findings-v1.md).
