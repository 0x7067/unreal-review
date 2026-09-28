# Follow-up: pull request review loop

Open work from the session that moved the agent onto the embedded unreal-agent harness. Do these in order. Delete this file once all remaining items are done.

The user wants no fallbacks: one source per value, and a clear error when it is missing.

## 1. Run a live multi-push test PR

Nothing has exercised the current loop on a real PR with several pushes. Use a disposable repository or branch, not this repo's own PRs:

1. Push 1: a planted bug. Expect one inline comment and a check run on the head.
2. Push 2: an unrelated change. Expect the range to start at push 1's head, the old finding listed as already reported, and no repeated comment.
3. Push 3: fix the push 1 bug. Record what happens to the old comment; nothing resolves it today.
4. Push 4: a finding on a line outside the patch. Confirms that an out-of-patch finding posts once and still gets the check run.

Record per push: the reviewed range from the status comment, new/duplicate/dropped counts, cost, and the summary. The summary must follow the contract in `schema/findings-v1.md#summary`.

## Known gaps found along the way

These came out of the review-loop assessment. They are not scheduled yet.

- Dedup matches by `id`, by the agent's `duplicate_of`, and by line overlap with findings still mapped onto the head. An issue that moved lines and whose comment went outdated is caught only if the agent sets `duplicate_of`. Push 2 and push 3 of the live test should show whether it does.
- A fixed finding is never resolved or answered on the PR.
- The agent never sees the PR title or description (`github.GetPullRequest` fetches `Title`, but `review.Pull` does not carry it).
- `reviewPrompt` cuts the diff at 200 KB and still marks the review `complete`.
- No `eval` baseline is recorded for the target model.

Fallbacks still in code this branch did not touch:

- `resolveToken` (`cmd/unreal-review/pull.go`): `GH_TOKEN`, then `GITHUB_TOKEN`, then `gh auth token`.
- `pullFullBase` (`internal/review/git.go`): `origin/<base ref>`, then the base SHA.
- `resolvePR` (`cmd/unreal-review/render.go`): `--pr`, then the Actions event file through `actionsPR`.
