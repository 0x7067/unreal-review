# Render GitHub

`unreal-review render github` turns a findings file into a pull-request review payload. `--dry-run` prints the payload. Posting requires a complete review and a token. `anchor: new` maps to `RIGHT`; `anchor: old` maps to `LEFT`.

## Sub-features

- `gh-dry-run` prints a COMMENT payload without posting when `--pr owner/repo#n` is set and no token is available.
- `gh-pr-url` accepts a `https://github.com/owner/repo/pull/n` spec.
- `gh-pr-number` accepts `--pr n --repo owner/repo`.
- `gh-pr-required` errors when `--pr` is missing.
- `gh-incomplete` refuses to post a `running` or `failed` file.
- `gh-dry-incomplete` still prints a payload for an incomplete file.
- `gh-legacy-complete` treats a missing `status` as complete.
- `gh-no-token` errors on a real post when no token and no `gh` exist.
- `gh-dry-with-token` with `--token` or `GH_TOKEN`/`GITHUB_TOKEN` `GET`s the pull request on `--dry-run`. `gh auth token` is not consulted for dry-run.
- `gh-post` (skippable) posts only when the user names a disposable pull request.

## How to get to it (user POV)

- `unreal-review render github --dry-run --pr owner/repo#12 findings.jsonl`
- `unreal-review render github --dry-run --token <token> --pr owner/repo#12 findings.jsonl`
- `unreal-review render github --pr owner/repo#12 findings.jsonl`
- `unreal-review render github --pr https://github.com/owner/repo/pull/12 findings.jsonl`
- `unreal-review render github --pr 12 --repo owner/repo findings.jsonl`

## Driving it with verify-unreal-review

Preconditions:

- Launch and doctor have succeeded.
- `$VERIFY_EXAMPLE` is `$VERIFY_REPO/examples/findings.jsonl`.
- Use `scripts/cli.sh --no-github-auth` unless the step is `gh-post`.
- Do not run `gh-post` unless the user named a disposable `owner/repo#n`.

- **Dry-run payload.** Run `scripts/cli.sh --name gh-dry-run --no-github-auth -- render github --dry-run --pr owner/repo#12 "$VERIFY_EXAMPLE"`. Exit code `0`. `stdout.txt` is JSON with `"owner": "owner"`, `"repo": "repo"`, `"pull_number": 12`, `"event": "COMMENT"`, two comments on `internal/cache/cache.go` with `"side": "RIGHT"`, the error comment `"start_line": 40` / `"line": 42`, the warning comment `"line": 88` and no `start_line`, and a body that contains `One data race and one unit mismatch in the cache.` and `Cost: USD 0.004200`.
- **PR URL.** Run `scripts/cli.sh --name gh-pr-url --no-github-auth -- render github --dry-run --pr https://github.com/acme/widgets/pull/9 "$VERIFY_EXAMPLE"`. Exit code `0`. JSON has `"owner": "acme"`, `"repo": "widgets"`, `"pull_number": 9`.
- **PR number.** Run `scripts/cli.sh --name gh-pr-number --no-github-auth -- render github --dry-run --pr 9 --repo acme/widgets "$VERIFY_EXAMPLE"`. Exit code `0`. Same owner/repo/number as the URL step. Run `scripts/cli.sh --name gh-pr-number-bare --no-github-auth -- render github --dry-run --pr 9 "$VERIFY_EXAMPLE"`. Exit code `1`. `stderr.txt` contains `pull request number 9 needs owner/repo`.
- **Missing --pr.** Run `scripts/cli.sh --name gh-pr-required --no-github-auth -- render github --dry-run "$VERIFY_EXAMPLE"`. Exit code `1`. `stderr.txt` contains `unreal-review: set --pr`.
- **Incomplete post.** Run `scripts/sample-findings.sh running "$VERIFY_SCRATCH/running.jsonl"` then `scripts/cli.sh --name gh-incomplete --no-github-auth -- render github --pr owner/repo#1 "$VERIFY_SCRATCH/running.jsonl"`. Exit code `1`. `stderr.txt` contains `findings are running; resume the review before posting`. Repeat with `sample-findings.sh failed` as `gh-incomplete-failed`. `stderr.txt` contains `findings are failed`.
- **Incomplete dry-run.** Run `scripts/cli.sh --name gh-dry-incomplete --no-github-auth -- render github --dry-run --pr owner/repo#1 "$VERIFY_SCRATCH/running.jsonl"`. Exit code `0`. JSON `"event": "COMMENT"` and a body that contains `Cost:`.
- **Legacy missing status.** Run `scripts/sample-findings.sh legacy "$VERIFY_SCRATCH/legacy.jsonl"` then `scripts/cli.sh --name gh-legacy --no-github-auth -- render github --dry-run --pr owner/repo#3 "$VERIFY_SCRATCH/legacy.jsonl"`. Exit code `0`. The comment has `"side": "LEFT"` and `"path": "a.go"`. The body contains `legacy`.
- **Post without token.** Run `scripts/cli.sh --name gh-no-token --no-github-auth -- render github --pr owner/repo#12 "$VERIFY_EXAMPLE"`. Exit code `1`. `stderr.txt` contains `set GH_TOKEN, GITHUB_TOKEN, or --token to post a review`.
- **Dry-run still GETs with a token.** Run `scripts/cli.sh --name gh-dry-with-token --no-github-auth -- render github --dry-run --token verify-unreal-review-dummy --pr owner/repo#12 "$VERIFY_EXAMPLE"`. Exit code `1`. `stderr.txt` contains `github GET /repos/owner/repo/pulls/12`. It does not contain `posted`. The GET is proof `--dry-run` still fetched the pull request when `--token` (or `GH_TOKEN`/`GITHUB_TOKEN`) is set. A dummy token typically yields `401`; a real token against `owner/repo` yields `404`.
- **Live post.** Unreachable unless the user named a disposable pull request. On that path, run `scripts/cli.sh --name gh-post -- render github --pr <that-pr> "$VERIFY_EXAMPLE"` and expect stderr `posted 2 inline comment(s)` only if those lines exist in that PR patch; otherwise comments may be dropped. Do not use `examples/findings.jsonl` against a real unrelated PR as success proof.
- **Proof.** Keep `gh-dry-run/stdout.txt` (RIGHT-side comments, no network), `gh-no-token/stderr.txt`, and `gh-dry-with-token/stderr.txt` (GET, no `posted`).

## Gotchas

- `--dry-run` reads `--token`, `GH_TOKEN`, and `GITHUB_TOKEN` only. It does not run `gh auth token`. Posting does, when those three are empty.
- `--no-github-auth` unsets env tokens and shadows `gh` with a failing shim. `--token` on the command line still causes a GET.
- `--dry-run` skips the completeness check and skips `CreateReview`. It does not skip `GetPullRequest` / `ListPullFiles` when a token exists.
- Findings whose lines are not in the PR patch are dropped only when those files were fetched (`HasLines`). A no-token dry-run places every finding.
- Completeness for posting: `status=complete`, or missing `status`. `running` and `failed` refuse to post.
- Do not treat a GET 401 or 404 as a successful post.
