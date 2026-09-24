#!/bin/bash
set -euo pipefail

if [ -z "${VERIFY_RUN_ID:-}" ]; then
	VERIFY_RUN_ID=$(date +%Y%m%dT%H%M%S)-$$
fi
export VERIFY_RUN_ID

. "$(dirname "$0")/lib.sh"

mkdir -p "$VERIFY_EVIDENCE" "$VERIFY_SCRATCH"
printf '%s\n' "$VERIFY_RUN_ID" >"$VERIFY_ROOT/current-run"
make -C "$VERIFY_REPO" build
if [ ! -x "$VERIFY_BIN" ]; then
	echo "verify-unreal-review: make build did not produce $VERIFY_BIN" >&2
	exit 1
fi

{
	print_env
	echo "git_head=$(git -C "$VERIFY_REPO" rev-parse HEAD)"
	echo "binary_size=$(wc -c < "$VERIFY_BIN" | tr -d ' ')"
} >"$VERIFY_EVIDENCE/launch.txt"

print_env
echo "ready: $VERIFY_BIN"
echo "evidence: $VERIFY_EVIDENCE"
echo "scratch: $VERIFY_SCRATCH"
