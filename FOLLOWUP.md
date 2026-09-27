# Follow-up: pull request review loop

Open work from the session that moved the agent onto the embedded unreal-agent harness. Items 1 (base-commit build in CI) and 2 (out-of-diff drops keep the receipt) are done. Delete this file once item 3 is done.

The user wants no fallbacks: one source per value, and a clear error when it is missing.

## 3. Run a live multi-push test PR

Nothing has exercised the current loop on a real PR with several pushes. Use a disposable repository or branch, not this repo's own PRs:

1. Push 1: a planted bug. Expect one inline comment and a check run on the head.
2. Push 2: an unrelated change. Expect the range to start at push 1's head, the old finding listed as already reported, and no repeated comment.
3. Push 3: fix the push 1 bug. Record what happens to the old comment; nothing resolves it today.
4. Push 4: a finding on a line outside the patch. Expect it listed once in the review body, a check run on the head, and no repeat on push 5.

Check the Actions log on push 1 for the `git worktree add` base build. Also confirm an LGTM push posts one LGTM review; the LGTM check was inverted until item 2.

Record per push: the reviewed range from the status comment, new/duplicate/dropped counts, cost, and the summary. The summary must follow the contract in `schema/findings-v1.md#summary`.

## Known gaps found along the way

These came out of the review-loop assessment. They are not scheduled yet.

- Dedup hashes the finding body (`findings.Fingerprint`), so a reworded or line-shifted finding gets a new ID and can be posted again.
- A fixed finding is never resolved or answered on the PR.
- `ReportedFindings` reads inline comments only, so an out-of-diff finding listed in a review body is missing from the prompt's "already reported" list. The renderer still suppresses it.
- The agent never sees the PR title or description (`github.GetPullRequest` fetches `Title`, but `review.Pull` does not carry it).
- `reviewPrompt` cuts the diff at 200 KB and still marks the review `complete`.
- No `eval` baseline is recorded for the target model.

Fallbacks still in code this branch did not touch:

- `resolveToken` (`cmd/unreal-review/pull.go`): `GH_TOKEN`, then `GITHUB_TOKEN`, then `gh auth token`.
- `pullFullBase` (`internal/review/git.go`): `origin/<base ref>`, then the base SHA.
- `resolvePR` (`cmd/unreal-review/render.go`): `--pr`, then the Actions event file through `actionsPR`.
