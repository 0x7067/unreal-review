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

`make check` includes `make prove` (`bend PROOF.bend --check-only`). Product invariants live in `LAWS.bend`. Install Bend from https://bend-lang.com (`bend version` 2.0.27 is the local toolchain).

## Review a local change

From a git repository:

```sh
unreal-review run --out findings.jsonl
```

With no range flags, `run` reviews staged, unstaged, and untracked changes against `HEAD`. `run` takes a git checkout (`--workspace`, default `.`). `--from` and `--to` select `git diff --merge-base` of those refs (branch, tag, or SHA); omit `--to` to include the working tree. `--commit` reviews one commit against its first parent. `--branch` reviews a branch since it diverged from `main` or `master`. Pathspecs limit the range; `--exclude` omits globs. The selected range is sent in full to the agent with cwd at the checkout. It writes [findings.jsonl](schema/findings-v1.md). Every run records cost. Every finding has a severity: `error`, `warning`, or `note`.

```sh
unreal-review run --from origin/main --to HEAD --out findings.jsonl
unreal-review run --branch feature --out findings.jsonl
unreal-review run --commit abc1234 --out findings.jsonl
unreal-review run --from abc1234 --to def5678 --exclude '*.lock' -- cmd/
```

`--out` is the checkpoint. The `run` record stores completeness (`status`) and the reviewed commit SHAs plus a hash of the exact diff. Interrupt the process to pause. The same command continues that review if the diff is unchanged. `--fresh` starts over. GitHub rendering refuses a file that is not `complete`. The checkpoint is the findings file; it does not name an agent backend.

To review a large change in pieces, run `group` on the same git range. It prints related file groups and a `run` command for each:

```sh
unreal-review group --from origin/main --to HEAD
```

Files in one directory stay together. Files that share a stem in the same directory (`Button.tsx` with `Button.module.css`, `foo.go` with `foo_linux.go`) share a group. A test file that names an implementation (`foo_test.go`, `foo.test.ts`, `test_foo.py`, `FooTest.java`) joins that implementation's group when the paths are in different directories. Locale variants (`messages_en.properties` with `messages_zh.properties`, `locales/en/auth.json` with `locales/zh/auth.json`, or `docs/en/guide.md` with `docs/zh/guide.md`) share a group. A header and its source (`foo.h` with `foo.c`) do too. `go.mod` stays with `go.sum`. Files that import each other across directories join when the import matches exactly one changed path (Go, JavaScript/TypeScript, Python, Java, Kotlin, Rust, PHP, C#, Swift, Vue). A directory group larger than 25 files or 2000 changed lines splits on the next path component, then into size-capped chunks. Each `run` writes its own findings file and `diff_sha`.

```sh
unreal-review render markdown findings.jsonl
```

## Post inline comments on a GitHub pull request

```sh
unreal-review render github --pr owner/repo#12 findings.jsonl
```

`--dry-run` prints the GitHub review payload and does not post. The renderer maps `anchor: new` to the right side of the diff and drops findings whose lines are not in the pull request patch.

GitHub Actions is the same two commands. This repository reviews its own pull requests with [.github/workflows/review.yml](.github/workflows/review.yml); [examples/github-actions/review.yml](examples/github-actions/review.yml) is the shape to copy into another repository.

## Evaluate a model

```sh
unreal-review eval --model openai/gpt-6-luna-pro
unreal-review eval --json --model openai/gpt-6-luna-pro > eval.json
```

`eval` runs a planted-issue corpus — a data race, a nil dereference, and a
clean control — through the same pipeline as `run`, then scores each
findings file against the planted issues. `recall` is the share of planted
issues found (same file, overlapping lines); `precision` is the share of
findings that match something planted; `status` is the review checkpoint.
Each case gets a fresh git workspace and its own findings.jsonl under
`--out`. Eval calls the model and costs money; it is not part of `make check`.

## Findings file

The JSONL schema is the product. Renderers turn it into GitHub comments, markdown, or another display. See [schema/findings-v1.md](schema/findings-v1.md).
