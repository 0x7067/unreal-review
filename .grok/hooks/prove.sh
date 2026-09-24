#!/bin/sh
# Gate Grok's turn end on the repo's Bend law proof.
#
# The gate itself lives in hooks/prove-stop.sh, shared by every harness, so the
# check, the message and the loop guard stay identical across agents.
set -u
self_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
exec "$self_dir/../../hooks/prove-stop.sh" "$@"
