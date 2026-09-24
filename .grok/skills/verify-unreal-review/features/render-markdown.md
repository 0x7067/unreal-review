# Render markdown

`unreal-review render markdown` reads a findings file (or stdin) and prints a markdown report: cost, summary, per-path headings, and severity bullets. Incomplete runs get a `Status:` line first. Empty input prints `No findings.`

## Sub-features

- `md-example` renders `examples/findings.jsonl` to stdout.
- `md-out` writes the same report to `--out`.
- `md-stdin` reads `-` from stdin.
- `md-empty` prints `No findings.` for an empty file.
- `md-missing` errors when the path does not exist.
- `md-running` prefixes `Status: running` for a running checkpoint.

## How to get to it (user POV)

- `unreal-review render markdown findings.jsonl`
- `unreal-review render markdown --out report.md findings.jsonl`
- `unreal-review render markdown - < findings.jsonl`
- `unreal-review render markdown` with stdin as the default file (`-`)

## Driving it with verify-unreal-review

Preconditions:

- Launch and doctor have succeeded.
- `$VERIFY_EXAMPLE` is `$VERIFY_REPO/examples/findings.jsonl`.
- `scripts/sample-findings.sh` can write running and empty files into scratch.

- **Example file.** Run `scripts/cli.sh --name md-example -- render markdown "$VERIFY_EXAMPLE"`. Exit code `0`. `stdout.txt` is exactly:

```
Cost: USD 0.004200 (1280 input, 320 output, 2 requests)

One data race and one unit mismatch in the cache.

## `internal/cache/cache.go`

- **error** L40–L42 (new): Concurrent map writes on the shared cache are not synchronized.
- **warning** L88 (new): The TTL is compared in milliseconds against a value stored in seconds.
```

- **--out file.** Run `scripts/cli.sh --name md-out -- render markdown --out "$VERIFY_SCRATCH/report.md" "$VERIFY_EXAMPLE"`. Exit code `0`. `stdout.txt` is empty. `$VERIFY_SCRATCH/report.md` matches the example stdout. Copy `report.md` into the step directory.
- **Stdin.** Run `scripts/cli.sh --name md-stdin --stdin "$VERIFY_EXAMPLE" -- render markdown -`. Exit code `0`. `stdout.txt` matches `md-example`.
- **Empty file.** Run `scripts/sample-findings.sh empty "$VERIFY_SCRATCH/empty.jsonl"` then `scripts/cli.sh --name md-empty -- render markdown "$VERIFY_SCRATCH/empty.jsonl"`. Exit code `0`. `stdout.txt` is `No findings.` plus a newline.
- **Missing file.** Run `scripts/cli.sh --name md-missing -- render markdown "$VERIFY_SCRATCH/does-not-exist.jsonl"`. Exit code `1`. `stderr.txt` contains `unreal-review: open` and `does-not-exist.jsonl`.
- **Running status.** Run `scripts/sample-findings.sh running "$VERIFY_SCRATCH/running.jsonl"` then `scripts/cli.sh --name md-running -- render markdown "$VERIFY_SCRATCH/running.jsonl"`. Exit code `0`. `stdout.txt` starts with `Status: running` then a cost line, then `## \`internal/cache/cache.go\``.
- **Proof.** Keep `md-example/stdout.txt` and the copied `md-out/report.md`. They match each other and name `internal/cache/cache.go`.

## Gotchas

- Default `--out` is `-` (stdout). A successful `--out file` leaves stdout empty; the proof is the file.
- A file with no records prints `No findings.` A summary-only report prints the summary and no heading.
- The en-dash in `L40–L42` is Unicode `–`, not ASCII `-`.
- Markdown render does not require GitHub auth or an API key.
