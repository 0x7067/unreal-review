#!/bin/bash
set -euo pipefail

. "$(dirname "$0")/lib.sh"

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
