#!/bin/bash

VERIFY_SKILL_DIR=$(cd "$(dirname "$0")/.." && pwd)
VERIFY_REPO=$(cd "$VERIFY_SKILL_DIR/../../.." && pwd)
VERIFY_BIN=${VERIFY_BIN:-"$VERIFY_REPO/bin/unreal-review"}
VERIFY_ROOT=${VERIFY_ROOT:-/tmp/verify-unreal-review}
if [ -z "${VERIFY_RUN_ID:-}" ]; then
	if [ -f "$VERIFY_ROOT/current-run" ]; then
		VERIFY_RUN_ID=$(cat "$VERIFY_ROOT/current-run")
	else
		VERIFY_RUN_ID=$(date +%Y%m%dT%H%M%S)-$$
	fi
fi
VERIFY_EVIDENCE="$VERIFY_ROOT/evidence/$VERIFY_RUN_ID"
VERIFY_SCRATCH="$VERIFY_ROOT/scratch/$VERIFY_RUN_ID"
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
