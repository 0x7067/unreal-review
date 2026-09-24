# Group related files

`unreal-review group` reads the same git range as `run` and prints related file groups with a copy-paste `run` command for each. It does not start the agent and does not need a model or API key.

## Sub-features

- `group-empty` prints `no changes to group` for an empty `--from HEAD --to HEAD` range.
- `group-dirs` puts files that share a directory into one group and uses that directory as the pathspec.
- `group-tests` attaches `test_foo.py` to `foo.py` across directories and leaves an unmatched test in its own group.
- `group-locales` puts `messages_en.properties` with `messages_zh.properties`, and `locales/en/auth.json` with `locales/zh/auth.json`.
- `group-exclude` omits `--exclude` globs.
- `group-run-line` prints `unreal-review run` with `--from`, `--to`, `--out findings-<slug>.jsonl`, and the group's pathspecs.
- `group-missing-from` errors when no default branch name exists.

## How to get to it (user POV)

- `unreal-review group` from a git checkout (working tree vs merge-base of main/master).
- `unreal-review group --from <rev> --to <rev>`.
- `unreal-review group --exclude '*.lock' -- cmd/`.

## Driving it with verify-unreal-review

Preconditions:

- Launch and doctor have succeeded.
- `scripts/fixture-repo.sh` has printed `VERIFY_FIXTURE`, `BASE_SHA`, and `HEAD_SHA`.
- This recipe does not set `OPENROUTER_API_KEY` or `--model`.

- **Empty range.** Run `scripts/cli.sh --name group-empty -- group --workspace "$VERIFY_FIXTURE" --from HEAD --to HEAD`. Exit code `0`. `stdout.txt` is `no changes to group`.
- **Directory groups.** Run `scripts/cli.sh --name group-dirs -- group --workspace "$VERIFY_FIXTURE" --from "$BASE_SHA" --to HEAD`. Exit code `0`. `stdout.txt` contains a `cmd` group listing `cmd/main.go` and a `run` line with `-- cmd`. `hello.txt` is its own group (repository root). `skip.lock` is listed.
- **Exclude.** Run `scripts/cli.sh --name group-exclude -- group --workspace "$VERIFY_FIXTURE" --from "$BASE_SHA" --to HEAD --exclude '*.lock'`. Exit code `0`. `stdout.txt` does not contain `skip.lock`. The `run` line contains `--exclude *.lock`.
- **Cross-directory tests.** Create `$VERIFY_SCRATCH/repos/paired` on `main` with `pkg/a/a.py`, `pkg/b/b.py`, `tests/test_a.py`, `tests/test_b.py`, and `tests/test_c.py` committed after an empty base. Run `scripts/cli.sh --name group-tests -- group --workspace "$VERIFY_SCRATCH/repos/paired" --from HEAD~1 --to HEAD`. Exit code `0`. `stdout.txt` lists `pkg/a/a.py` with `tests/test_a.py`, `pkg/b/b.py` with `tests/test_b.py`, and `tests/test_c.py` alone. The first `run` line ends with `-- pkg/a/a.py tests/test_a.py`.
- **Locale families.** Create `$VERIFY_SCRATCH/repos/i18n` on `main` with `messages_en.properties`, `messages_zh.properties`, `locales/en/auth.json`, `locales/zh/auth.json`, and `README.md` committed after an empty base. Run `scripts/cli.sh --name group-locales -- group --workspace "$VERIFY_SCRATCH/repos/i18n" --from HEAD~1 --to HEAD`. Exit code `0`. `stdout.txt` lists the two properties files together, the two `auth.json` files together, and `README.md` alone.
- **Missing default branch.** Create a second repo on `develop` with no `main`/`master`. Run `scripts/cli.sh --name group-missing-from -- group --workspace "$VERIFY_SCRATCH/repos/develop"`. Exit code `1`. `stderr.txt` contains `set --from; could not find main or master`.
- **Proof.** Keep `group-dirs/stdout.txt` and `group-tests/stdout.txt`. Both exit `0`. The paired fixture shows two implementation groups plus the unmatched test.

## Gotchas

- `group` does not need a model, API key, or runner. A dummy key is not part of this recipe.
- `--to HEAD` is the commit, not the dirty tree. Omit `--to` to include uncommitted edits.
- Pathspecs and `--exclude` match `run`. Size does not omit files.
- Root-level files such as `hello.txt` do not use `.` as a pathspec. The printed `run` command lists those files.
- A leftover file in a directory that also has files in other groups is printed as a file pathspec. `tests/test_c.py` is not `-- tests`.
