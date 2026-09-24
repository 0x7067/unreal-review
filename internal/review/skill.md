---
name: pr-review
description: Review a git diff and write findings.jsonl using the unreal-review schema.
---

# Review the diff

Read `brief.md` first. The repository is at `repo/`.

Inspect only the changed code. Open files in `repo/` when you need surrounding context. Do not edit files.

Write `findings.jsonl` in the workspace root when you are done. Each line is one JSON object.

Schema version `v` is `1`.

A finding:

```json
{"v":1,"type":"finding","path":"src/foo.go","start_line":12,"end_line":14,"anchor":"new","severity":"warning","body":"This map write races with the reader on line 40."}
```

- `path` is relative to the repository root.
- `start_line` and `end_line` are inclusive 1-based lines.
- `anchor` is `new` for the post-change file, `old` for deleted lines.
- `severity` is required: `error`, `warning`, or `note`.
- `body` is markdown. State the problem and why it matters. Skip style nits unless they hide a bug.

A summary line:

```json
{"v":1,"type":"summary","body":"Two races in the cache; the rest looks sound."}
```

Rules:

- Prefer lines that appear in the diff in `brief.md`.
- One finding per issue. Merge duplicates. A finding without severity is invalid.
- If there is nothing material, write only a summary. Do not invent findings.
- Do not call git hosting APIs. Do not post comments. The file is the output.
