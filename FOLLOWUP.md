# Known gaps

Live multi-push review was validated in closed PR #23.

These came out of the review-loop assessment and are not scheduled yet.

- Dedup matches by `id` alone. A reworded issue that moved lines is caught only if the agent records it with the prior finding's `id`; location overlap alone is intentionally not enough because two defects can share a line. The live test did not exercise this path.
- Review history trusts markers from any author. A finding marker in any inline comment (as on `main`), or a dropped marker in any review body, counts as already reported, so anyone who can comment on the PR can suppress a finding by forging its id. Filtering history to the posting identity needs that identity resolved under `GITHUB_TOKEN`, where `GET /user` is not available.
- A fixed finding is never resolved or answered on the PR. Push 3 confirmed it. The prototype on `t3code/work-on-followup` was not ported because its resolved threads still suppressed a later recurrence by id or line overlap. A redesign needs separate open-thread context for resolution and all-history context for audit, while allowing a resolved issue to be reported again if it regresses. Replies and thread resolution must remain idempotent.
