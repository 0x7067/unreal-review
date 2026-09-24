# findings.jsonl v1

Canonical review output. One JSON object per line. `v` is `1`.

## `run`

Present on every `unreal-review run`. `cost` is required.

```json
{"v":1,"type":"run","id":"…","created_at":"2026-09-23T12:00:00Z","model":"anthropic/claude-sonnet-4.5","source":{"kind":"git","base":"main","head":"","base_sha":"abc","head_sha":"def"},"cost":{"amount_usd":0.0123,"currency":"USD","input_tokens":12000,"output_tokens":800,"reasoning_tokens":400,"cached_input_tokens":1000,"requests":3}}
```

`cost.amount_usd` is the amount charged for the review. Token fields are additive across model turns.

## `finding`

Every finding has `severity`. Allowed values: `error`, `warning`, `note`.

```json
{"v":1,"type":"finding","id":"a1b2c3d4e5f60708","path":"src/foo.go","start_line":12,"end_line":14,"anchor":"new","severity":"warning","body":"This map write races with the reader."}
```

| field | meaning |
| --- | --- |
| `path` | Repository-relative path |
| `start_line`, `end_line` | Inclusive 1-based lines |
| `anchor` | `new` (post-change file) or `old` (deleted lines) |
| `severity` | `error`, `warning`, or `note` |
| `body` | Markdown |

## `summary`

```json
{"v":1,"type":"summary","body":"Two races in the cache; the rest looks sound."}
```

## Renderers

A renderer reads this file and produces a host-specific display. GitHub inline comments map `anchor=new` to `RIGHT` and `anchor=old` to `LEFT`, and drop findings whose lines are not in the pull request diff.
