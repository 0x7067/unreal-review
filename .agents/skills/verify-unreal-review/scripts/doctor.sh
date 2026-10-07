#!/bin/bash
set -euo pipefail

. "$(dirname "$0")/lib.sh"
require_launch

fail=0
report="$VERIFY_EVIDENCE/doctor.txt"
: >"$report"

note() {
	printf '%s\n' "$*" | tee -a "$report"
}

check() {
	label=$1
	shift
	if "$@"; then
		note "ok  $label"
	else
		note "FAIL $label"
		fail=1
	fi
}

note "VERIFY_BIN=$VERIFY_BIN"
note "git_head=$(git -C "$VERIFY_REPO" rev-parse HEAD)"

check "binary is repo bin/unreal-review" \
	[ "$(cd "$(dirname "$VERIFY_BIN")" && pwd)/$(basename "$VERIFY_BIN")" = "$VERIFY_REPO/bin/unreal-review" ]
check "binary is executable" [ -x "$VERIFY_BIN" ]
check "help exits 0" sh -c '"$VERIFY_BIN" help >/dev/null'
check "help names run" sh -c '"$VERIFY_BIN" help | grep -q "unreal-review run"'
check "help names group" sh -c '"$VERIFY_BIN" help | grep -q "unreal-review group"'
check "go is on PATH" sh -c 'command -v go >/dev/null'
check "git is on PATH" sh -c 'command -v git >/dev/null'
check "scratch is outside the repo" [ "$(under_repo "$VERIFY_SCRATCH")" = no ]
check "evidence is outside the repo" [ "$(under_repo "$VERIFY_EVIDENCE")" = no ]

newer=0
for src in "$VERIFY_REPO"/cmd/unreal-review/*.go "$VERIFY_REPO"/internal/*/*.go; do
	[ -f "$src" ] || continue
	if [ "$src" -nt "$VERIFY_BIN" ]; then
		newer=1
		break
	fi
done
if [ "$newer" -eq 0 ]; then
	note "ok  binary is not older than Go sources"
else
	note "FAIL binary is older than Go sources; re-run launch"
	fail=1
fi

if [ -n "${OPENROUTER_API_KEY:-}" ]; then
	note "ok  OPENROUTER_API_KEY is set"
else
	note "info OPENROUTER_API_KEY unset (required for run)"
fi

if [ -n "${UNREAL_HARNESS_LLM_MODEL:-}" ]; then
	note "ok  UNREAL_HARNESS_LLM_MODEL is set"
else
	note "info UNREAL_HARNESS_LLM_MODEL unset (live review skipped unless --model is passed)"
fi

if [ -n "${GH_TOKEN:-}" ]; then
	note "info GH_TOKEN is set (dry-run GETs the pull request)"
else
	note "info GH_TOKEN unset (posting and --pr fail; --dry-run does not call the API)"
fi

if [ "$fail" -ne 0 ]; then
	note "doctor: failed"
	exit 1
fi
note "doctor: ok"
exit 0
