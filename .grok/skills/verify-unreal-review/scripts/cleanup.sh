#!/bin/bash
set -euo pipefail

. "$(dirname "$0")/lib.sh"

if [ -f "$VERIFY_SCRATCH/pids" ]; then
	while read -r pid; do
		[ -n "${pid:-}" ] || continue
		if kill -0 "$pid" 2>/dev/null; then
			kill "$pid" 2>/dev/null || true
		fi
	done <"$VERIFY_SCRATCH/pids"
fi

if [ -d "$VERIFY_SCRATCH" ]; then
	rm -rf "$VERIFY_SCRATCH"
fi

echo "removed scratch: $VERIFY_SCRATCH"
if [ -d "$VERIFY_EVIDENCE" ]; then
	echo "kept evidence: $VERIFY_EVIDENCE"
	ls "$VERIFY_EVIDENCE"
else
	echo "verify-unreal-review: evidence missing at $VERIFY_EVIDENCE" >&2
	exit 1
fi
