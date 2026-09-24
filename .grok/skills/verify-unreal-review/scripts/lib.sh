#!/bin/bash

abs_path() {
	python3 -c 'import os, sys; print(os.path.realpath(os.path.abspath(sys.argv[1])))' "$1"
}

under_repo() {
	python3 -c '
import os, sys
repo = os.path.realpath(sys.argv[1])
path = os.path.realpath(os.path.abspath(sys.argv[2]))
print("yes" if path == repo or path.startswith(repo + os.sep) else "no")
' "$VERIFY_REPO" "$1"
}

VERIFY_SKILL_DIR=$(cd "$(dirname "$0")/.." && pwd)
VERIFY_REPO=$(abs_path "$VERIFY_SKILL_DIR/../../..")
VERIFY_BIN=${VERIFY_BIN:-"$VERIFY_REPO/bin/unreal-review"}
VERIFY_BIN=$(abs_path "$VERIFY_BIN")
VERIFY_ROOT=${VERIFY_ROOT:-/tmp/verify-unreal-review}
VERIFY_ROOT=$(abs_path "$VERIFY_ROOT")
if [ -z "${VERIFY_RUN_ID:-}" ]; then
	if [ -f "$VERIFY_ROOT/current-run" ]; then
		VERIFY_RUN_ID=$(cat "$VERIFY_ROOT/current-run")
	else
		VERIFY_RUN_ID=$(date +%Y%m%dT%H%M%S)-$$
	fi
fi
VERIFY_EVIDENCE=$(abs_path "$VERIFY_ROOT/evidence/$VERIFY_RUN_ID")
VERIFY_SCRATCH=$(abs_path "$VERIFY_ROOT/scratch/$VERIFY_RUN_ID")
VERIFY_EXAMPLE="$VERIFY_REPO/examples/findings.jsonl"
VERIFY_EMPTY_DIFF_SHA=e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855

export VERIFY_SKILL_DIR VERIFY_REPO VERIFY_BIN VERIFY_ROOT VERIFY_RUN_ID VERIFY_EVIDENCE VERIFY_SCRATCH VERIFY_EXAMPLE VERIFY_EMPTY_DIFF_SHA

require_launch() {
	if [ ! -x "$VERIFY_BIN" ]; then
		echo "verify-unreal-review: binary missing; run scripts/launch.sh" >&2
		exit 2
	fi
	if [ ! -d "$VERIFY_EVIDENCE" ]; then
		echo "verify-unreal-review: evidence dir missing; run scripts/launch.sh" >&2
		exit 2
	fi
}

print_env() {
	cat <<EOF
VERIFY_SKILL_DIR=$VERIFY_SKILL_DIR
VERIFY_REPO=$VERIFY_REPO
VERIFY_BIN=$VERIFY_BIN
VERIFY_ROOT=$VERIFY_ROOT
VERIFY_RUN_ID=$VERIFY_RUN_ID
VERIFY_EVIDENCE=$VERIFY_EVIDENCE
VERIFY_SCRATCH=$VERIFY_SCRATCH
EOF
}
