#!/bin/bash
# Live OpenRouter smoke. Unlike tools/canary.sh, this calls https://openrouter.ai/api/v1.
# It does not start tools/openrouter-stub.py and does not set UNREAL_REVIEW_OPENROUTER_API.
# A set UNREAL_REVIEW_OPENROUTER_API is refused so a loopback stand-in cannot satisfy the smoke.
#
# Required:
#   OPENROUTER_API_KEY
#   UNREAL_HARNESS_LLM_MODEL   documented model: openai/gpt-6-luna-pro
# The model id is not applied as a fallback. An unset variable is an error.
#
# Range: a temporary git repo with one committed one-line change, reviewed with
# `run --commit HEAD` at --thinking-level low. Not `run --pr` (no GH_TOKEN, no
# GitHub). Not part of the CI canary job. Offline regression remains `make canary`.
set -euo pipefail

DOCUMENTED_MODEL=openai/gpt-6-luna-pro
EMPTY_DIFF_SHA=e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855

usage() {
	cat <<EOF
usage: canary-live.sh

Live OpenRouter review smoke for bin/unreal-review.

Reviews one committed one-line change in a temporary git repository
(run --commit HEAD, --thinking-level low) and requires a complete
findings file whose cost.requests is greater than 0.

Requires:
  OPENROUTER_API_KEY
  UNREAL_HARNESS_LLM_MODEL   documented model: ${DOCUMENTED_MODEL}

The documented model is not filled in when UNREAL_HARNESS_LLM_MODEL is unset.
UNREAL_REVIEW_OPENROUTER_API must be unset. This smoke calls
https://openrouter.ai/api/v1 and will not use tools/openrouter-stub.py.

Not part of CI. The offline regression is: make canary
EOF
}

fail() {
	echo "canary-live: $*" >&2
	exit 1
}

trim() {
	local s=$1
	s=${s#"${s%%[![:space:]]*}"}
	s=${s%"${s##*[![:space:]]}"}
	printf '%s' "$s"
}

redact_file() {
	python3 - "$1" <<'PY'
import os, pathlib, sys
text = pathlib.Path(sys.argv[1]).read_text(errors="replace")
key = os.environ.get("OPENROUTER_API_KEY", "")
if key:
    text = text.replace(key, "[REDACTED]")
sys.stdout.write(text)
PY
}

show_stderr() {
	if [ -s "$stderr" ]; then
		redact_file "$stderr" >&2
	fi
}

# Print the findings file before the EXIT trap deletes it. A non-zero review
# still persists a run record (often status failed or running). Cap the dump
# so a long model reply stays readable; the run record is the first line.
show_findings() {
	if [ ! -s "$out" ]; then
		echo "canary-live: findings: empty" >&2
		return
	fi
	echo "canary-live: findings:" >&2
	local redacted="$work/findings.redacted" lines
	redact_file "$out" >"$redacted"
	head -n 80 "$redacted" >&2
	lines=$(wc -l <"$redacted" | tr -d ' ')
	if [ "$lines" -gt 80 ]; then
		echo "canary-live: findings truncated after 80 lines" >&2
	fi
}

# One dump for a non-zero review and for a smoke-gate miss, so the stderr and
# findings output cannot drift apart again.
fail_review() {
	local message=$1 code=$2
	echo "canary-live: $message" >&2
	show_stderr
	show_findings
	exit "$code"
}

if [ $# -gt 0 ]; then
	case $1 in
	-h | --help | help)
		usage
		exit 0
		;;
	*)
		usage >&2
		exit 2
		;;
	esac
fi

if [ -z "$(trim "${OPENROUTER_API_KEY:-}")" ]; then
	fail "set OPENROUTER_API_KEY. This smoke calls https://openrouter.ai/api/v1. The offline regression (no secret) is make canary."
fi
if [ -z "$(trim "${UNREAL_HARNESS_LLM_MODEL:-}")" ]; then
	fail "set UNREAL_HARNESS_LLM_MODEL. Documented model: ${DOCUMENTED_MODEL}."
fi
if [ -n "${UNREAL_REVIEW_OPENROUTER_API:-}" ]; then
	fail "unset UNREAL_REVIEW_OPENROUTER_API. This smoke calls https://openrouter.ai/api/v1 and will not use a loopback stand-in (tools/openrouter-stub.py)."
fi

ROOT=$(cd "$(dirname "$0")/.." && pwd)
BIN="$ROOT/bin/unreal-review"
if [ ! -x "$BIN" ]; then
	fail "missing $BIN. Run make canary-live (or make build) from the repo root."
fi

work=$(mktemp -d "${TMPDIR:-/tmp}/unreal-review-canary-live.XXXXXX")
cleanup() {
	if [ -n "${work:-}" ]; then
		rm -rf "$work"
	fi
}
trap cleanup EXIT

repo="$work/repo"
mkdir -p "$repo"
git -C "$repo" init -q -b main
git -C "$repo" config user.email "canary-live@example.com"
git -C "$repo" config user.name "Canary Live"
cat >"$repo/value.go" <<'EOF'
package p

func Value() int { return 1 }
EOF
git -C "$repo" add value.go
git -C "$repo" commit -q -m base
printf 'package p\n\nfunc Value() int { return 2 }\n' >"$repo/value.go"
git -C "$repo" add value.go
git -C "$repo" commit -q -m head

out="$work/findings.jsonl"
stdout="$work/stdout.txt"
stderr="$work/stderr.txt"

set +e
"$BIN" run \
	--workspace "$repo" \
	--commit HEAD \
	--model "$UNREAL_HARNESS_LLM_MODEL" \
	--thinking-level low \
	--timeout 10m \
	--out "$out" \
	>"$stdout" 2>"$stderr"
code=$?
set -e

if [ "$code" -ne 0 ]; then
	fail_review "review exited $code" "$code"
fi

set +e
python3 - "$out" "$stderr" "$UNREAL_HARNESS_LLM_MODEL" "$EMPTY_DIFF_SHA" <<'PY'
import json, pathlib, sys
path, stderr_path, model, empty = sys.argv[1:5]
stderr = pathlib.Path(stderr_path).read_text(errors="replace")
if "cost:" not in stderr:
    sys.exit("canary-live: stderr missing cost:")
if "127.0.0.1" in stderr or "openrouter-stub" in stderr:
    sys.exit("canary-live: stderr names the offline openrouter stub")
run = None
summary = ""
for line in pathlib.Path(path).read_text(errors="replace").splitlines():
    line = line.strip()
    if not line:
        continue
    obj = json.loads(line)
    if obj.get("type") == "run":
        run = obj
    elif obj.get("type") == "summary":
        summary = obj.get("body") or ""
if run is None:
    sys.exit("canary-live: no run record")
if run.get("status") != "complete":
    sys.exit(f"canary-live: status={run.get('status')}")
if run.get("model") != model:
    sys.exit(f"canary-live: model={run.get('model')!r} want {model!r}")
requests = (run.get("cost") or {}).get("requests") or 0
if requests < 1:
    sys.exit(f"canary-live: cost.requests={requests}")
diff_sha = (run.get("source") or {}).get("diff_sha") or ""
if diff_sha == empty:
    sys.exit("canary-live: diff_sha is the empty diff; the review never called the model")
if not summary.strip():
    sys.exit("canary-live: missing summary")
cost = next((line for line in stderr.splitlines() if line.startswith("cost:")), "cost:")
print(f"canary-live: ok model={model} requests={requests}")
print(f"canary-live: {cost}")
PY
gate=$?
set -e
if [ "$gate" -ne 0 ]; then
	fail_review "smoke gate failed" "$gate"
fi
