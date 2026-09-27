# Follow-up: pull request review loop

Open work from the session that moved the agent onto the embedded unreal-agent harness. Do these in order. Delete this file once all three are done.

The user wants no fallbacks: one source per value, and a clear error when it is missing.

## 1. Build the reviewer from the base commit in CI

`.github/workflows/review.yml` builds `unreal-review` from the PR checkout with `go install ./cmd/unreal-review`. It then runs that binary with `OPENROUTER_API_KEY` and a `pull-requests: write` `GH_TOKEN`. A same-repository PR can edit `internal/agent` to send either value anywhere.

Change:

- Keep checking out the PR head as the workspace; the diff and file reads need it.
- Build the binary from `github.event.pull_request.base.sha` in a separate worktree, for example `git worktree add "$RUNNER_TEMP/base" "$BASE_SHA"`, then `go install ./cmd/unreal-review` from there.
- Pass the SHA through `env:` as `BASE_SHA`. Do not interpolate `${{ }}` into the shell; see commit `7c81b2c`.
- Apply the same change to `examples/github-actions/review.yml`.

Done when a PR that edits `cmd/unreal-review` is still reviewed by the base binary. Check the Actions log for the worktree build.

## 2. Stop one dropped finding from blocking the review receipt

A finding whose lines are outside the PR patch turns incremental review back into full-range review, and a new review is posted on every push:

- `render.GitHub` (`internal/render/github.go`) puts dropped findings only in the review body, with no marker.
- `postedFingerprints` reads markers from inline comments only, so a dropped finding is never deduplicated.
- `renderGitHub` (`cmd/unreal-review/render.go`) creates the `unreal-review` check run only when `len(result.Dropped) == 0`, so the receipt never lands and `run --pr` keeps reviewing from the older receipt.
- `GitHubResult.PostReview()` stays true, so each push posts another review listing the same drop.

Decide the rule first. `spec/github.bend` `postNew` and the law `post_new_true_dropped` say a drop forces a review to be posted; the receipt rule is not modeled at all. Changing either is a product rule change: add or update the law and its proof in `LAWS.bend`/`PROOF.bend`, and mirror it in `spec/`, in the same change. One option: a drop caused by lines outside the patch still records the receipt, and dropped findings carry a marker in the review body so later runs deduplicate them. Keep the existing rule that the 50-comment cap blocks the receipt, since those findings are postable.

Done when two consecutive `render github` calls on the same head, with one out-of-patch finding, post one review and create the check run.

## 3. Run a live multi-push test PR

Nothing has exercised the current loop on a real PR with several pushes. Use a disposable repository or branch, not this repo's own PRs:

1. Push 1: a planted bug. Expect one inline comment and a check run on the head.
2. Push 2: an unrelated change. Expect the range to start at push 1's head, the old finding listed as already reported, and no repeated comment.
3. Push 3: fix the push 1 bug. Record what happens to the old comment; nothing resolves it today.
4. Push 4: a finding on a line outside the patch. Confirms item 2.

Record per push: the reviewed range from the status comment, new/duplicate/dropped counts, cost, and the summary. The summary must follow the contract in `schema/findings-v1.md#summary`.

## Known gaps found along the way

These came out of the review-loop assessment. They are not scheduled yet.

- Dedup hashes the finding body (`findings.Fingerprint`), so a reworded or line-shifted finding gets a new ID and can be posted again.
- A fixed finding is never resolved or answered on the PR.
- The agent never sees the PR title or description (`github.GetPullRequest` fetches `Title`, but `review.Pull` does not carry it).
- `reviewPrompt` cuts the diff at 200 KB and still marks the review `complete`.
- No `eval` baseline is recorded for the target model.

Fallbacks still in code this branch did not touch:

- `resolveToken` (`cmd/unreal-review/pull.go`): `GH_TOKEN`, then `GITHUB_TOKEN`, then `gh auth token`.
- `pullFullBase` (`internal/review/git.go`): `origin/<base ref>`, then the base SHA.
- `resolvePR` (`cmd/unreal-review/render.go`): `--pr`, then the Actions event file through `actionsPR`.
