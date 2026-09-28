# Follow-up: pull request review loop

Open work that remains after consolidating the review-loop branches. Delete this file once all remaining items are done.

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

These came out of the review-loop assessment and are not scheduled yet.

- Dedup matches by `id` alone. A reworded issue that moved lines is caught only if the agent records it with the prior finding's `id`; location overlap alone is intentionally not enough because two defects can share a line. The live test did not exercise this path.
- Review history trusts markers from any author. A finding marker in any inline comment (as on `main`), or a dropped marker in any review body, counts as already reported, so anyone who can comment on the PR can suppress a finding by forging its id. Filtering history to the posting identity needs that identity resolved under `GITHUB_TOKEN`, where `GET /user` is not available.
- A fixed finding is never resolved or answered on the PR. Push 3 confirmed it. The prototype on `t3code/work-on-followup` was not ported because its resolved threads still suppressed a later recurrence by id or line overlap. A redesign needs separate open-thread context for resolution and all-history context for audit, while allowing a resolved issue to be reported again if it regresses. Replies and thread resolution must remain idempotent.
