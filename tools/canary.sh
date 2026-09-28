#!/bin/bash
# Offline assertions for the verify-unreal-review recipes.
# Exit status, stdout, stderr, and JSONL are checked against features/*.md.
# Skipped on purpose: range-live, gh-post (live), gh-dry-with-token (calls api.github.com).
# Fake-GitHub coverage of run --pr and render github is the Go test make canary runs next.
# Non-empty diffs still expect a 401. That status comes from tools/openrouter-stub.py
# on 127.0.0.1, not from openrouter.ai.
set -euo pipefail

stub_stop() {
	if [ -n "${STUB_PID:-}" ] && kill -0 "$STUB_PID" 2>/dev/null; then
		kill "$STUB_PID" 2>/dev/null || true
		wait "$STUB_PID" 2>/dev/null || true
	fi
	STUB_PID=
}

trap 'status=$?; echo "canary: command failed at line $LINENO (exit $status)" >&2; echo "evidence: ${VERIFY_EVIDENCE:-unset}" >&2; stub_stop; exit $status' ERR
trap 'stub_stop' EXIT

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"

export LC_ALL=C.UTF-8
export VERIFY_ROOT=${VERIFY_ROOT:-$(mktemp -d "${TMPDIR:-/tmp}/unreal-review-canary.XXXXXX")}
export VERIFY_RUN_ID=${VERIFY_RUN_ID:-canary}
# VERIFY_RUN_ID is a single path segment under VERIFY_ROOT. Rejecting "." and
# ".." and anything outside [A-Za-z0-9._-] keeps evidence and scratch cleanup
# from walking out of that root.
case $VERIFY_RUN_ID in
'' | '.' | '..' | *[!A-Za-z0-9._-]*)
	echo "canary: VERIFY_RUN_ID must be one path segment (letters, digits, dot, underscore, hyphen)" >&2
	exit 1
	;;
esac
mkdir -p "$VERIFY_ROOT"
# Actions sets GITHUB_REPOSITORY, which render/run use as the default --repo,
# and GITHUB_EVENT_PATH, which supplies a pull request when --pr is empty.
# Drop both so a bare --pr number still fails closed, the same as off Actions.
unset GH_TOKEN GITHUB_TOKEN GITHUB_REPOSITORY GITHUB_EVENT_PATH UNREAL_HARNESS_LLM_MODEL OPENROUTER_API_KEY UNREAL_REVIEW_GITHUB_API UNREAL_REVIEW_OPENROUTER_API || true

# lib.sh keys off $0, so sourcing it from this script would point at tools/.
# Launch still sources lib.sh itself; these are the same paths it derives.
FEATURES="$ROOT/.grok/skills/verify-unreal-review/features"
SCRIPTS="$ROOT/.grok/skills/verify-unreal-review/scripts"
VERIFY_EVIDENCE="$VERIFY_ROOT/evidence/$VERIFY_RUN_ID"
VERIFY_SCRATCH="$VERIFY_ROOT/scratch/$VERIFY_RUN_ID"
VERIFY_EXAMPLE="$ROOT/examples/findings.jsonl"
EMPTY=e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855

say() { printf 'canary: %s\n' "$*"; }

start_openrouter_stub() {
	local url_file="$VERIFY_ROOT/openrouter-stub.url" origin i
	mkdir -p "$VERIFY_ROOT"
	# Drop a URL left by an earlier run in this root before the new stub
	# opens the file. Otherwise the wait below can read the stale URL.
	rm -f "$url_file"
	STUB_LOG="$VERIFY_ROOT/openrouter-stub.log"
	: >"$STUB_LOG"
	python3 "$ROOT/tools/openrouter-stub.py" "$STUB_LOG" >"$url_file" &
	STUB_PID=$!
	for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
		if [ -s "$url_file" ]; then
			break
		fi
		if ! kill -0 "$STUB_PID" 2>/dev/null; then
			miss "openrouter stub exited before binding"
		fi
		sleep 0.05
	done
	if [ ! -s "$url_file" ]; then
		miss "openrouter stub did not print a URL"
	fi
	origin=$(head -n 1 "$url_file")
	case "$origin" in
	http://127.0.0.1:*) ;;
	*) miss "openrouter stub bound $origin" ;;
	esac
	export UNREAL_REVIEW_OPENROUTER_API="${origin}/api/v1"
	say "openrouter stub $UNREAL_REVIEW_OPENROUTER_API"
}

require_openrouter_stub() {
	local bad
	if [ ! -s "$STUB_LOG" ]; then
		miss "local openrouter stub received no requests"
	fi
	bad=$(grep -v -E '^POST /api/v1/responses host=127\.0\.0\.1:[0-9]+$' "$STUB_LOG" || true)
	if [ -n "$bad" ]; then
		miss "local openrouter stub saw unexpected requests: $bad"
	fi
	if grep -R -F -q -- 'openrouter.ai' "$VERIFY_EVIDENCE"; then
		miss "recipe evidence names openrouter.ai"
	fi
}

miss() {
	if [ "${CANARY_QUIET:-}" = 1 ]; then
		return 1
	fi
	echo "canary: $*" >&2
	echo "evidence: ${VERIFY_EVIDENCE:-unset}" >&2
	exit 1
}

require_contains() {
	local file=$1 needle=$2 label=$3
	if ! grep -F -q -- "$needle" "$file"; then
		miss "$label missing [$needle] in $file"
	fi
}

require_absent() {
	local file=$1 needle=$2 label=$3
	if grep -F -q -- "$needle" "$file"; then
		miss "$label unexpectedly contains [$needle]"
	fi
}

require_exit() {
	local name=$1 want=$2
	local got
	got=$(cat "$VERIFY_EVIDENCE/$name/exit.txt")
	if [ "$got" != "$want" ]; then
		miss "$name exit=$got want=$want stderr=$(cat "$VERIFY_EVIDENCE/$name/stderr.txt")"
	fi
}

require_last_line() {
	local file=$1 want=$2 label=$3
	local got
	got=$(tail -n 1 "$file")
	if [ "$got" != "$want" ]; then
		miss "$label last line [$got] want [$want]"
	fi
}

feature_has() {
	local file=$1 needle=$2
	if ! grep -F -q -- "$needle" "$FEATURES/$file"; then
		miss "features/$file no longer states [$needle]"
	fi
}

# Needle must still be written in the recipe, and the artifact must show it.
expect() {
	local feature=$1 needle=$2 file=$3 label=$4
	feature_has "$feature" "$needle"
	require_contains "$file" "$needle" "$label"
}

run_json() {
	python3 - "$1" "$2" <<'PY'
import json, sys
path, query = sys.argv[1], sys.argv[2]
run = None
summary = ""
for line in open(path):
    line = line.strip()
    if not line:
        continue
    obj = json.loads(line)
    if obj.get("type") == "run":
        run = obj
    elif obj.get("type") == "summary":
        summary = obj.get("body", "")
if run is None:
    sys.exit("no run record in " + path)
src = run.get("source") or {}
if query == "head":
    print("ABSENT" if "head" not in src else src.get("head") or "")
    sys.exit(0)
vals = {
    "id": run.get("id", ""),
    "status": run.get("status", ""),
    "base": src.get("base", ""),
    "base_sha": src.get("base_sha", ""),
    "head_sha": src.get("head_sha", ""),
    "diff_sha": src.get("diff_sha", ""),
    "summary": summary,
}
if query not in vals:
    sys.exit("unknown field " + query)
print(vals[query])
PY
}

require_field() {
	local file=$1 query=$2 want=$3 label=$4
	local got
	got=$(run_json "$file" "$query")
	if [ "$got" != "$want" ]; then
		miss "$label $query=$got want=$want"
	fi
}

require_field_ne() {
	local file=$1 query=$2 unwanted=$3 label=$4
	local got
	got=$(run_json "$file" "$query")
	if [ "$got" = "$unwanted" ]; then
		miss "$label $query still [$unwanted]"
	fi
}

require_eq_file() {
	local got=$1 want=$2 label=$3
	if ! cmp -s "$got" "$want"; then
		diff -u "$want" "$got" >&2 || true
		miss "$label differs"
	fi
}

stdout_of() { printf '%s\n' "$VERIFY_EVIDENCE/$1/stdout.txt"; }
stderr_of() { printf '%s\n' "$VERIFY_EVIDENCE/$1/stderr.txt"; }

cli() { "$SCRIPTS/cli.sh" "$@"; }

new_repo() {
	local name=$1 branch=${2:-main}
	local root="$VERIFY_SCRATCH/repos/$name"
	rm -rf "$root"
	mkdir -p "$root"
	git -C "$root" init -q -b "$branch"
	git -C "$root" config user.email "verify@example.com"
	git -C "$root" config user.name "Verify"
	git -C "$root" commit -q --allow-empty -m base
	printf '%s\n' "$root"
}

commit_all() {
	git -C "$1" add -A
	git -C "$1" commit -q -m head
}

write_file() {
	local path=$1
	mkdir -p "$(dirname "$path")"
	cat >"$path"
}

prove_require_fails() {
	local tmp
	tmp=$(mktemp)
	printf 'hello\n' >"$tmp"
	CANARY_QUIET=1
	if ( require_contains "$tmp" "goodbye" "self-check" ); then
		unset CANARY_QUIET
		rm -f "$tmp"
		echo "canary: require_contains accepted a missing needle" >&2
		exit 1
	fi
	unset CANARY_QUIET
	rm -f "$tmp"
}

expect_payload() {
	local file=$1
	shift
	python3 - "$file" "$@" <<'PY'
import json, sys
payload = json.load(open(sys.argv[1]))
review = payload["review"]
comments = review.get("comments") or []
checks = sys.argv[2:]
i = 0
while i < len(checks):
    kind = checks[i]
    if kind == "owner":
        assert payload["owner"] == checks[i + 1], payload["owner"]
        i += 2
    elif kind == "repo":
        assert payload["repo"] == checks[i + 1], payload["repo"]
        i += 2
    elif kind == "pull":
        assert payload["pull_number"] == int(checks[i + 1]), payload["pull_number"]
        i += 2
    elif kind == "event":
        assert review["event"] == checks[i + 1], review["event"]
        i += 2
    elif kind == "body_has":
        assert checks[i + 1] in review.get("body", ""), review.get("body")
        i += 2
    elif kind == "ncomments":
        assert len(comments) == int(checks[i + 1]), comments
        i += 2
    elif kind == "right_path":
        assert comments and all(c.get("side") == "RIGHT" and c.get("path") == checks[i + 1] for c in comments), comments
        i += 2
    elif kind == "span":
        line = int(checks[i + 1])
        start = int(checks[i + 2])
        match = [c for c in comments if c.get("line") == line]
        assert len(match) == 1, comments
        assert match[0].get("start_line") == start, match[0]
        i += 3
    elif kind == "line_only":
        line = int(checks[i + 1])
        match = [c for c in comments if c.get("line") == line]
        assert len(match) == 1, comments
        assert "start_line" not in match[0], match[0]
        i += 2
    elif kind == "left_path":
        match = [c for c in comments if c.get("side") == "LEFT" and c.get("path") == checks[i + 1]]
        assert len(match) == 1, comments
        i += 2
    else:
        sys.exit("unknown check " + kind)
PY
}

run_line_ends() {
	local file=$1 suffix=$2 label=$3
	python3 - "$file" "$suffix" "$label" <<'PY'
import sys
text = open(sys.argv[1]).read()
suffix, label = sys.argv[2], sys.argv[3]
runs = [line.strip() for line in text.splitlines() if "unreal-review run" in line]
if not any(line.endswith(suffix) for line in runs):
    sys.exit(label + " missing run line ending " + suffix + "\n" + "\n".join(runs))
PY
}

run_line_has_both() {
	local file=$1 a=$2 b=$3 label=$4
	python3 - "$file" "$a" "$b" "$label" <<'PY'
import sys
text = open(sys.argv[1]).read()
a, b, label = sys.argv[2], sys.argv[3], sys.argv[4]
runs = [line for line in text.splitlines() if "unreal-review run" in line]
if not any(a in line and b in line for line in runs):
    sys.exit(label + " has no run line with both files\n" + "\n".join(runs))
PY
}

workspace_run_lines() {
	local file=$1
	python3 - "$file" <<'PY'
import sys
text = open(sys.argv[1]).read()
runs = [line for line in text.splitlines() if "unreal-review run" in line]
if not runs:
    sys.exit("no run line")
for line in runs:
    for flag in ("--from", "--to", "--commit", "--branch"):
        if flag in line.split():
            sys.exit(f"{flag} in {line}")
if "working tree" not in text or "hello.txt" not in text or "extra.txt" not in text:
    sys.exit(text)
PY
}

markdown_example() {
	python3 - "$FEATURES/render-markdown.md" "$1" <<'PY'
from pathlib import Path
import sys
text = Path(sys.argv[1]).read_text()
start = text.index("Cost: USD 0.004200")
end = text.index("```", start)
block = text[start:end]
if not block.endswith("\n"):
    block += "\n"
Path(sys.argv[2]).write_text(block)
PY
}

start_openrouter_stub

say "comparator fails closed when a needle is missing"
prove_require_fails

say "launch and doctor"
"$SCRIPTS/launch.sh"
"$SCRIPTS/doctor.sh"
require_contains "$VERIFY_EVIDENCE/doctor.txt" "doctor: ok" "doctor"

say "fixture"
"$SCRIPTS/fixture-repo.sh"
# shellcheck disable=SC1091
. "$VERIFY_SCRATCH/fixture.env"

say "cli usage"
feature_has cli-usage.md "Usage:"
cli --name usage-help -- help
require_exit usage-help 0
expect cli-usage.md "Usage:" "$(stdout_of usage-help)" "usage-help"
expect cli-usage.md "unreal-review run" "$(stdout_of usage-help)" "usage-help"
expect cli-usage.md "unreal-review group" "$(stdout_of usage-help)" "usage-help"

cli --name usage-required --
require_exit usage-required 1
feature_has cli-usage.md 'unreal-review: command required'
require_last_line "$(stderr_of usage-required)" "unreal-review: command required" "usage-required"

cli --name usage-unknown -- frob
require_exit usage-unknown 1
expect cli-usage.md 'unreal-review: unknown command "frob"' "$(stderr_of usage-unknown)" "usage-unknown"

env -u UNREAL_HARNESS_LLM_MODEL "$SCRIPTS/cli.sh" --name run-model -- run --workspace "$VERIFY_SCRATCH" --out "$VERIFY_SCRATCH/unused.jsonl"
require_exit run-model 1
expect cli-usage.md "unreal-review: set --model or UNREAL_HARNESS_LLM_MODEL" "$(stderr_of run-model)" "run-model"

env -u OPENROUTER_API_KEY "$SCRIPTS/cli.sh" --name run-key -- run --model x --workspace "$VERIFY_SCRATCH" --out "$VERIFY_SCRATCH/unused.jsonl"
require_exit run-key 1
expect cli-usage.md "unreal-review: set OPENROUTER_API_KEY" "$(stderr_of run-key)" "run-key"

OPENROUTER_API_KEY=dummy cli --name run-thinking -- run --model x --thinking-level nope --workspace "$VERIFY_SCRATCH" --out "$VERIFY_SCRATCH/unused.jsonl"
require_exit run-thinking 1
expect cli-usage.md 'thinking level "nope": want low, medium, high, xhigh, or max' "$(stderr_of run-thinking)" "run-thinking"

OPENROUTER_API_KEY=dummy cli --name run-exclude-empty -- run --model x --exclude '' --workspace "$VERIFY_SCRATCH" --out "$VERIFY_SCRATCH/unused.jsonl"
require_exit run-exclude-empty 1
expect cli-usage.md "empty --exclude" "$(stderr_of run-exclude-empty)" "run-exclude-empty"

cli --name render-unknown -- render html
require_exit render-unknown 1
expect cli-usage.md 'unreal-review: unknown render target "html"' "$(stderr_of render-unknown)" "render-unknown"

say "review git range"
OPENROUTER_API_KEY=dummy cli --name range-empty -- run --model x --workspace "$VERIFY_FIXTURE" --from HEAD --to HEAD --out "$VERIFY_SCRATCH/empty.jsonl"
require_exit range-empty 0
expect review-git-range.md "cost: USD 0.000000" "$(stderr_of range-empty)" "range-empty"
feature_has review-git-range.md 'status":"complete"'
require_field "$VERIFY_SCRATCH/empty.jsonl" status complete "range-empty"
require_field "$VERIFY_SCRATCH/empty.jsonl" base HEAD "range-empty"
require_field "$VERIFY_SCRATCH/empty.jsonl" head HEAD "range-empty"
require_field "$VERIFY_SCRATCH/empty.jsonl" diff_sha "$EMPTY" "range-empty"
feature_has review-git-range.md "No material issues: the selected range has no changes."
require_field "$VERIFY_SCRATCH/empty.jsonl" summary "No material issues: the selected range has no changes." "range-empty"
base_sha=$(run_json "$VERIFY_SCRATCH/empty.jsonl" base_sha)
head_sha=$(run_json "$VERIFY_SCRATCH/empty.jsonl" head_sha)
if [ "$base_sha" != "$head_sha" ] || [ -z "$base_sha" ]; then
	miss "range-empty base_sha=$base_sha head_sha=$head_sha"
fi

OPENROUTER_API_KEY=dummy cli --name range-workspace -- run --model x --workspace "$VERIFY_FIXTURE" --out "$VERIFY_SCRATCH/workspace.jsonl"
require_exit range-workspace 1
expect review-git-range.md "401" "$(stderr_of range-workspace)" "range-workspace"
require_field "$VERIFY_SCRATCH/workspace.jsonl" status failed "range-workspace"
require_field "$VERIFY_SCRATCH/workspace.jsonl" base HEAD "range-workspace"
require_field "$VERIFY_SCRATCH/workspace.jsonl" head ABSENT "range-workspace"
require_field_ne "$VERIFY_SCRATCH/workspace.jsonl" diff_sha "$EMPTY" "range-workspace"

OPENROUTER_API_KEY=dummy cli --name range-detect -- run --model x --workspace "$VERIFY_FIXTURE" --branch HEAD --out "$VERIFY_SCRATCH/detect.jsonl"
require_exit range-detect 0
require_field "$VERIFY_SCRATCH/detect.jsonl" base main "range-detect"
require_field "$VERIFY_SCRATCH/detect.jsonl" head HEAD "range-detect"
require_field "$VERIFY_SCRATCH/detect.jsonl" diff_sha "$EMPTY" "range-detect"

dev=$(new_repo develop develop)
OPENROUTER_API_KEY=dummy cli --name range-missing-from -- run --model x --workspace "$dev" --branch HEAD --out "$VERIFY_SCRATCH/develop.jsonl"
require_exit range-missing-from 1
expect review-git-range.md "could not find main or master; set --from" "$(stderr_of range-missing-from)" "range-missing-from"

OPENROUTER_API_KEY=dummy cli --name range-to-only -- run --model x --workspace "$VERIFY_FIXTURE" --to HEAD --out "$VERIFY_SCRATCH/to-only.jsonl"
require_exit range-to-only 1
expect review-git-range.md "set --from or use --branch" "$(stderr_of range-to-only)" "range-to-only"

OPENROUTER_API_KEY=dummy cli --name range-mixed -- run --model x --workspace "$VERIFY_FIXTURE" --from HEAD --commit HEAD --out "$VERIFY_SCRATCH/mixed.jsonl"
require_exit range-mixed 1
expect review-git-range.md "use only one of --from/--to, --commit, --branch, or --pr" "$(stderr_of range-mixed)" "range-mixed"

OPENROUTER_API_KEY=dummy cli --name range-bad-rev -- run --model x --workspace "$VERIFY_FIXTURE" --from nosuchrev --to HEAD --out "$VERIFY_SCRATCH/badfrom.jsonl"
require_exit range-bad-rev 1
expect review-git-range.md "git rev-parse nosuchrev" "$(stderr_of range-bad-rev)" "range-bad-rev"
expect review-git-range.md "unknown revision" "$(stderr_of range-bad-rev)" "range-bad-rev"

OPENROUTER_API_KEY=dummy cli --name range-working-tree -- run --model x --workspace "$VERIFY_FIXTURE" --from main --out "$VERIFY_SCRATCH/dirty.jsonl"
require_exit range-working-tree 1
expect review-git-range.md "status: failed" "$(stderr_of range-working-tree)" "range-working-tree"
expect review-git-range.md "401" "$(stderr_of range-working-tree)" "range-working-tree"
require_field "$VERIFY_SCRATCH/dirty.jsonl" status failed "range-working-tree"
require_field "$VERIFY_SCRATCH/dirty.jsonl" head ABSENT "range-working-tree"
require_field "$VERIFY_SCRATCH/dirty.jsonl" head_sha "$HEAD_SHA" "range-working-tree"
require_field_ne "$VERIFY_SCRATCH/dirty.jsonl" diff_sha "$EMPTY" "range-working-tree"

OPENROUTER_API_KEY=dummy cli --name range-commit -- run --model x --workspace "$VERIFY_FIXTURE" --commit HEAD --out "$VERIFY_SCRATCH/commit.jsonl"
require_exit range-commit 1
expect review-git-range.md "401" "$(stderr_of range-commit)" "range-commit"
require_field "$VERIFY_SCRATCH/commit.jsonl" head HEAD "range-commit"
require_field "$VERIFY_SCRATCH/commit.jsonl" base 'HEAD^' "range-commit"

OPENROUTER_API_KEY=dummy cli --name range-pathspec -- run --model x --workspace "$VERIFY_FIXTURE" --from "$BASE_SHA" --to HEAD --out "$VERIFY_SCRATCH/path-lock.jsonl" -- skip.lock
require_exit range-pathspec 1
require_field "$VERIFY_SCRATCH/path-lock.jsonl" status failed "range-pathspec"
require_field_ne "$VERIFY_SCRATCH/path-lock.jsonl" diff_sha "$EMPTY" "range-pathspec"

OPENROUTER_API_KEY=dummy cli --name range-exclude -- run --model x --workspace "$VERIFY_FIXTURE" --from "$BASE_SHA" --to HEAD --exclude '*.lock' --exclude 'hello.txt' --exclude 'cmd/*' --out "$VERIFY_SCRATCH/excl.jsonl"
require_exit range-exclude 0
require_field "$VERIFY_SCRATCH/excl.jsonl" status complete "range-exclude"
require_field "$VERIFY_SCRATCH/excl.jsonl" summary "No material issues: the selected range has no changes." "range-exclude"
require_field "$VERIFY_SCRATCH/excl.jsonl" diff_sha "$EMPTY" "range-exclude"
require_field "$VERIFY_SCRATCH/excl.jsonl" base "$BASE_SHA" "range-exclude"
require_field "$VERIFY_SCRATCH/excl.jsonl" head HEAD "range-exclude"

say "group"
cli --name group-empty -- group --workspace "$VERIFY_FIXTURE" --from HEAD --to HEAD
require_exit group-empty 0
feature_has group-files.md "no changes to group"
printf 'no changes to group\n' >"$VERIFY_SCRATCH/group-empty-want.txt"
require_eq_file "$(stdout_of group-empty)" "$VERIFY_SCRATCH/group-empty-want.txt" "group-empty"

cli --name group-dirs -- group --workspace "$VERIFY_FIXTURE" --from "$BASE_SHA" --to HEAD
require_exit group-dirs 0
expect group-files.md "cmd/main.go" "$(stdout_of group-dirs)" "group-dirs"
expect group-files.md "hello.txt" "$(stdout_of group-dirs)" "group-dirs"
expect group-files.md "skip.lock" "$(stdout_of group-dirs)" "group-dirs"
run_line_ends "$(stdout_of group-dirs)" "-- cmd" "group-dirs"

cli --name group-exclude -- group --workspace "$VERIFY_FIXTURE" --from "$BASE_SHA" --to HEAD --exclude '*.lock'
require_exit group-exclude 0
require_absent "$(stdout_of group-exclude)" "skip.lock" "group-exclude"
feature_has group-files.md '--exclude *.lock'
require_contains "$(stdout_of group-exclude)" "--exclude *.lock" "group-exclude"

paired=$(new_repo paired)
write_file "$paired/pkg/a/a.py" <<<'a = 1'
write_file "$paired/pkg/b/b.py" <<<'b = 1'
write_file "$paired/tests/test_a.py" <<<'def test_a(): pass'
write_file "$paired/tests/test_b.py" <<<'def test_b(): pass'
write_file "$paired/tests/test_c.py" <<<'def test_c(): pass'
commit_all "$paired"
cli --name group-tests -- group --workspace "$paired" --from HEAD~1 --to HEAD
require_exit group-tests 0
expect group-files.md "pkg/a/a.py" "$(stdout_of group-tests)" "group-tests"
expect group-files.md "tests/test_a.py" "$(stdout_of group-tests)" "group-tests"
expect group-files.md "pkg/b/b.py" "$(stdout_of group-tests)" "group-tests"
expect group-files.md "tests/test_b.py" "$(stdout_of group-tests)" "group-tests"
expect group-files.md "tests/test_c.py" "$(stdout_of group-tests)" "group-tests"
run_line_ends "$(stdout_of group-tests)" "-- pkg/a/a.py tests/test_a.py" "group-tests"
run_line_has_both "$(stdout_of group-tests)" "pkg/b/b.py" "tests/test_b.py" "group-tests"

i18n=$(new_repo i18n)
write_file "$i18n/messages_en.properties" <<<'hello=hi'
write_file "$i18n/messages_zh.properties" <<<'hello=nihao'
write_file "$i18n/locales/en/auth.json" <<<'{}'
write_file "$i18n/locales/zh/auth.json" <<<'{}'
write_file "$i18n/README.md" <<<'readme'
commit_all "$i18n"
cli --name group-locales -- group --workspace "$i18n" --from HEAD~1 --to HEAD
require_exit group-locales 0
run_line_has_both "$(stdout_of group-locales)" "messages_en.properties" "messages_zh.properties" "group-locales"
run_line_has_both "$(stdout_of group-locales)" "locales/en/auth.json" "locales/zh/auth.json" "group-locales"
run_line_ends "$(stdout_of group-locales)" "-- README.md" "group-locales"

stem=$(new_repo stem)
write_file "$stem/Button.tsx" <<<'export const Button = 1'
write_file "$stem/Button.module.css" <<<'.b{}'
write_file "$stem/foo.go" <<<'package p'
write_file "$stem/foo_linux.go" <<<'package p'
write_file "$stem/README.md" <<<'readme'
commit_all "$stem"
cli --name group-stem -- group --workspace "$stem" --from HEAD~1 --to HEAD
require_exit group-stem 0
run_line_has_both "$(stdout_of group-stem)" "Button.tsx" "Button.module.css" "group-stem"
run_line_has_both "$(stdout_of group-stem)" "foo.go" "foo_linux.go" "group-stem"
run_line_ends "$(stdout_of group-stem)" "-- README.md" "group-stem"

mod=$(new_repo mod)
write_file "$mod/go.mod" <<<'module example'
write_file "$mod/go.sum" <<<'example v0.0.0 h1:abc'
write_file "$mod/main.go" <<<'package main'
commit_all "$mod"
cli --name group-lock -- group --workspace "$mod" --from HEAD~1 --to HEAD
require_exit group-lock 0
run_line_has_both "$(stdout_of group-lock)" "go.mod" "go.sum" "group-lock"
run_line_ends "$(stdout_of group-lock)" "-- main.go" "group-lock"

imports=$(new_repo imports)
write_file "$imports/go.mod" <<<'module example'
write_file "$imports/internal/api/billing.go" <<'EOF'
package api

import "example/internal/billing"

func Bill() { billing.Charge() }
EOF
write_file "$imports/internal/billing/charge.go" <<'EOF'
package billing

func Charge() {}
EOF
write_file "$imports/internal/other/x.go" <<'EOF'
package other

func X() {}
EOF
commit_all "$imports"
cli --name group-imports -- group --workspace "$imports" --from HEAD~1 --to HEAD
require_exit group-imports 0
feature_has group-files.md "with the unique package it imports across directories"
run_line_has_both "$(stdout_of group-imports)" "internal/api/billing.go" "internal/billing/charge.go" "group-imports"
run_line_ends "$(stdout_of group-imports)" "-- internal/other" "group-imports"

cli --name group-workspace -- group --workspace "$VERIFY_FIXTURE"
require_exit group-workspace 0
feature_has group-files.md "working tree"
workspace_run_lines "$(stdout_of group-workspace)"

cli --name group-missing-from -- group --workspace "$dev" --branch HEAD
require_exit group-missing-from 1
expect group-files.md "could not find main or master; set --from" "$(stderr_of group-missing-from)" "group-missing-from"

say "checkpoint"
OPENROUTER_API_KEY=dummy cli --name ckpt-seed-empty -- run --model x --workspace "$VERIFY_FIXTURE" --from HEAD --to HEAD --out "$VERIFY_SCRATCH/empty.jsonl"
require_exit ckpt-seed-empty 0
require_field "$VERIFY_SCRATCH/empty.jsonl" status complete "ckpt-seed-empty"
require_field "$VERIFY_SCRATCH/empty.jsonl" diff_sha "$EMPTY" "ckpt-seed-empty"
seed_id=$(run_json "$VERIFY_SCRATCH/empty.jsonl" id)

OPENROUTER_API_KEY=dummy cli --name ckpt-empty-overwrite -- run --model x --workspace "$VERIFY_FIXTURE" --from HEAD --to HEAD --out "$VERIFY_SCRATCH/empty.jsonl"
require_exit ckpt-empty-overwrite 0
overwrite_id=$(run_json "$VERIFY_SCRATCH/empty.jsonl" id)
if [ "$overwrite_id" = "$seed_id" ] || [ -z "$overwrite_id" ]; then
	miss "ckpt-empty-overwrite run id did not change ($overwrite_id)"
fi
require_field "$VERIFY_SCRATCH/empty.jsonl" diff_sha "$EMPTY" "ckpt-empty-overwrite"

OPENROUTER_API_KEY=dummy cli --name ckpt-complete-refuse -- run --model x --workspace "$VERIFY_FIXTURE" --from "$BASE_SHA" --to HEAD --out "$VERIFY_SCRATCH/empty.jsonl"
require_exit ckpt-complete-refuse 1
expect checkpoint.md "is a complete review of from" "$(stderr_of ckpt-complete-refuse)" "ckpt-complete-refuse"
expect checkpoint.md "pass --fresh to start over" "$(stderr_of ckpt-complete-refuse)" "ckpt-complete-refuse"
require_field "$VERIFY_SCRATCH/empty.jsonl" id "$overwrite_id" "ckpt-complete-refuse"
require_field "$VERIFY_SCRATCH/empty.jsonl" diff_sha "$EMPTY" "ckpt-complete-refuse"
require_field "$VERIFY_SCRATCH/empty.jsonl" status complete "ckpt-complete-refuse"

OPENROUTER_API_KEY=dummy cli --name ckpt-fresh -- run --fresh --model x --workspace "$VERIFY_FIXTURE" --from "$BASE_SHA" --to HEAD --out "$VERIFY_SCRATCH/empty.jsonl"
require_exit ckpt-fresh 1
expect checkpoint.md "401" "$(stderr_of ckpt-fresh)" "ckpt-fresh"
require_field "$VERIFY_SCRATCH/empty.jsonl" status failed "ckpt-fresh"
require_field "$VERIFY_SCRATCH/empty.jsonl" base "$BASE_SHA" "ckpt-fresh"
require_field_ne "$VERIFY_SCRATCH/empty.jsonl" diff_sha "$EMPTY" "ckpt-fresh"
fresh_id=$(run_json "$VERIFY_SCRATCH/empty.jsonl" id)

OPENROUTER_API_KEY=dummy cli --name ckpt-failed-resume -- run --model x --workspace "$VERIFY_FIXTURE" --from "$BASE_SHA" --to HEAD --out "$VERIFY_SCRATCH/empty.jsonl"
require_exit ckpt-failed-resume 1
expect checkpoint.md "401" "$(stderr_of ckpt-failed-resume)" "ckpt-failed-resume"
require_field "$VERIFY_SCRATCH/empty.jsonl" id "$fresh_id" "ckpt-failed-resume"

OPENROUTER_API_KEY=dummy cli --name ckpt-working-failed -- run --model x --workspace "$VERIFY_FIXTURE" --from main --out "$VERIFY_SCRATCH/dirty.jsonl"
require_exit ckpt-working-failed 1
expect checkpoint.md "401" "$(stderr_of ckpt-working-failed)" "ckpt-working-failed"
dirty_sum=$(sha256sum "$VERIFY_SCRATCH/dirty.jsonl" | awk '{print $1}')
OPENROUTER_API_KEY=dummy cli --name ckpt-sha-mismatch -- run --model x --workspace "$VERIFY_FIXTURE" --from "$BASE_SHA" --to HEAD --out "$VERIFY_SCRATCH/dirty.jsonl"
require_exit ckpt-sha-mismatch 1
feature_has checkpoint.md "workspace is"
if ! grep -Eq 'is a review of from [0-9a-f]{12} to [0-9a-f]{12} diff [0-9a-f]{12}; workspace is from [0-9a-f]{12} to [0-9a-f]{12} diff [0-9a-f]{12}' "$(stderr_of ckpt-sha-mismatch)"; then
	miss "ckpt-sha-mismatch stderr=$(cat "$(stderr_of ckpt-sha-mismatch)")"
fi
dirty_after=$(sha256sum "$VERIFY_SCRATCH/dirty.jsonl" | awk '{print $1}')
if [ "$dirty_sum" != "$dirty_after" ]; then
	miss "ckpt-sha-mismatch changed dirty.jsonl"
fi

say "render markdown"
markdown_example "$VERIFY_SCRATCH/md-want.txt"
cli --name md-example -- render markdown "$VERIFY_EXAMPLE"
require_exit md-example 0
require_eq_file "$(stdout_of md-example)" "$VERIFY_SCRATCH/md-want.txt" "md-example"
expect render-markdown.md "internal/cache/cache.go" "$(stdout_of md-example)" "md-example"

cli --name md-out -- render markdown --out "$VERIFY_SCRATCH/report.md" "$VERIFY_EXAMPLE"
require_exit md-out 0
if [ -s "$(stdout_of md-out)" ]; then
	miss "md-out stdout is not empty"
fi
require_eq_file "$VERIFY_SCRATCH/report.md" "$(stdout_of md-example)" "md-out"

cli --name md-stdin --stdin "$VERIFY_EXAMPLE" -- render markdown -
require_exit md-stdin 0
require_eq_file "$(stdout_of md-stdin)" "$(stdout_of md-example)" "md-stdin"

"$SCRIPTS/sample-findings.sh" empty "$VERIFY_SCRATCH/empty-md.jsonl"
cli --name md-empty -- render markdown "$VERIFY_SCRATCH/empty-md.jsonl"
require_exit md-empty 0
feature_has render-markdown.md "No findings."
printf 'No findings.\n' >"$VERIFY_SCRATCH/md-empty-want.txt"
require_eq_file "$(stdout_of md-empty)" "$VERIFY_SCRATCH/md-empty-want.txt" "md-empty"

cli --name md-missing -- render markdown "$VERIFY_SCRATCH/does-not-exist.jsonl"
require_exit md-missing 1
expect render-markdown.md "unreal-review: open" "$(stderr_of md-missing)" "md-missing"
expect render-markdown.md "does-not-exist.jsonl" "$(stderr_of md-missing)" "md-missing"

"$SCRIPTS/sample-findings.sh" running "$VERIFY_SCRATCH/running.jsonl"
cli --name md-running -- render markdown "$VERIFY_SCRATCH/running.jsonl"
require_exit md-running 0
feature_has render-markdown.md "Status: running"
if ! grep -q '^Status: running' "$(stdout_of md-running)"; then
	miss "md-running does not start with Status: running"
fi
expect render-markdown.md '## `internal/cache/cache.go`' "$(stdout_of md-running)" "md-running"

say "render github dry-run"
cli --name gh-dry-run --no-github-auth -- render github --dry-run --pr owner/repo#12 "$VERIFY_EXAMPLE"
require_exit gh-dry-run 0
feature_has render-github.md '"event": "COMMENT"'
expect_payload "$(stdout_of gh-dry-run)" \
	owner owner repo repo pull 12 event COMMENT \
	body_has "Concurrent writes to the shared cache map can corrupt it or crash the process; the TTL check also mixes milliseconds and seconds." \
	body_has "Cost: USD 0.004200" \
	ncomments 2 right_path internal/cache/cache.go span 42 40 line_only 88
require_absent "$(stderr_of gh-dry-run)" "posted" "gh-dry-run"

cli --name gh-pr-url --no-github-auth -- render github --dry-run --pr https://github.com/acme/widgets/pull/9 "$VERIFY_EXAMPLE"
require_exit gh-pr-url 0
expect_payload "$(stdout_of gh-pr-url)" owner acme repo widgets pull 9

cli --name gh-pr-number --no-github-auth -- render github --dry-run --pr 9 --repo acme/widgets "$VERIFY_EXAMPLE"
require_exit gh-pr-number 0
expect_payload "$(stdout_of gh-pr-number)" owner acme repo widgets pull 9

cli --name gh-pr-number-bare --no-github-auth -- render github --dry-run --pr 9 "$VERIFY_EXAMPLE"
require_exit gh-pr-number-bare 1
expect render-github.md "pull request number 9 needs owner/repo" "$(stderr_of gh-pr-number-bare)" "gh-pr-number-bare"

cli --name gh-pr-required --no-github-auth -- render github --dry-run "$VERIFY_EXAMPLE"
require_exit gh-pr-required 1
expect render-github.md "unreal-review: set --pr" "$(stderr_of gh-pr-required)" "gh-pr-required"

cli --name gh-incomplete --no-github-auth -- render github --pr owner/repo#1 "$VERIFY_SCRATCH/running.jsonl"
require_exit gh-incomplete 1
expect render-github.md "findings are running; resume the review before posting" "$(stderr_of gh-incomplete)" "gh-incomplete"

"$SCRIPTS/sample-findings.sh" failed "$VERIFY_SCRATCH/failed.jsonl"
cli --name gh-incomplete-failed --no-github-auth -- render github --pr owner/repo#1 "$VERIFY_SCRATCH/failed.jsonl"
require_exit gh-incomplete-failed 1
expect render-github.md "findings are failed" "$(stderr_of gh-incomplete-failed)" "gh-incomplete-failed"

cli --name gh-dry-incomplete --no-github-auth -- render github --dry-run --pr owner/repo#1 "$VERIFY_SCRATCH/running.jsonl"
require_exit gh-dry-incomplete 0
expect_payload "$(stdout_of gh-dry-incomplete)" event COMMENT body_has "Cost:"

"$SCRIPTS/sample-findings.sh" legacy "$VERIFY_SCRATCH/legacy.jsonl"
cli --name gh-legacy --no-github-auth -- render github --dry-run --pr owner/repo#3 "$VERIFY_SCRATCH/legacy.jsonl"
require_exit gh-legacy 0
expect_payload "$(stdout_of gh-legacy)" left_path a.go body_has "legacy"

cli --name gh-no-token --no-github-auth -- render github --pr owner/repo#12 "$VERIFY_EXAMPLE"
require_exit gh-no-token 1
expect render-github.md "set GH_TOKEN or GITHUB_TOKEN to post a review" "$(stderr_of gh-no-token)" "gh-no-token"

require_openrouter_stub

"$SCRIPTS/cleanup.sh"
say "offline recipes ok ($VERIFY_EVIDENCE)"
