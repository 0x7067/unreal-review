# Follow-up: pull request review loop

Open work from the session that moved the agent onto the embedded unreal-agent harness. Do these in order. Delete this file once all remaining items are done.

The user wants no fallbacks: one source per value, and a clear error when it is missing.

## 1. Live multi-push test PR (done)

Draft PR #23 (closed) against `receipt-out-of-patch-drops`, reviewed by the Review workflow with `openai/gpt-6-luna-pro`. Each run posted the `unreal-review` check run on its head, and every summary followed the contract.

| Push | Change | Reviewed | New / dup / not postable | Cost USD | Outcome |
| --- | --- | --- | --- | --- | --- |
| 1 `d32f3ce` | `livecache` with unlocked `Len`/`Delete` | base..d32f3ce | 1 / 0 / 0 | 0.003528 | One error inline at L27-32 |
| 2 `b676493` | Locked `Has` above `Get`, shifting lines | d32f3ce..b676493 | 0 / 0 / 0 | 0.001272 | LGTM review; old comment moved to L34-39, still current |
| 3 `c5247ea` | Lock `Len` and `Delete` | b676493..c5247ea | 0 / 0 / 0 | 0.001370 | LGTM review; old comment stays current and unresolved, widened to L27-43 |
| 4 `c7823a5` | `Reset` sets `items` to nil | c5247ea..c7823a5 | 1 / 0 / 0 | 0.003429 | New error inline on `reset.go:6` |

Every range started at the previous reviewed head, and no push repeated a comment. Push 4's finding landed inside the patch, so the out-of-patch path was not exercised live. The offline canary covers it.

An earlier run on a throwaway repository posted a second race comment after `Count`/`Remove` were added. That was not a dedup miss: the push added new unlocked copies while `Len`/`Delete` stayed, so the second comment was a distinct issue on different lines.

## Known gaps found along the way

These came out of the review-loop assessment. They are not scheduled yet.

- Dedup matches by `id`, by the agent's `duplicate_of`, and by line overlap with findings still mapped onto the head. An issue that moved lines and whose comment went outdated is caught only if the agent sets `duplicate_of`. In the live test the comment never went outdated, so this path stayed unexercised.
- A fixed finding is never resolved or answered on the PR. Push 3 confirmed it: after the fix, the comment stays current and GitHub widens its range over the edited lines, so line-overlap dedup now covers a wider span than the original finding.
- The agent never sees the PR title or description (`github.GetPullRequest` fetches `Title`, but `review.Pull` does not carry it).
- `reviewPrompt` cuts the diff at 200 KB and still marks the review `complete`.
- No `eval` baseline is recorded for the target model.

Fallbacks still in code this branch did not touch:

- `resolveToken` (`cmd/unreal-review/pull.go`): `GH_TOKEN`, then `GITHUB_TOKEN`, then `gh auth token`.
- `pullFullBase` (`internal/review/git.go`): `origin/<base ref>`, then the base SHA.
- `resolvePR` (`cmd/unreal-review/render.go`): `--pr`, then the Actions event file through `actionsPR`.
