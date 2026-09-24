#!/usr/bin/env bash
# Gate an agent's turn on the repo's Bend law proof.
#
# A repo that keeps LAWS.bend / PROOF.bend (root, or under bend/) must have a
# green proof before an agent may end its turn. Run with no arguments from any
# agent's Stop/turn-end hook.
#
# Usage: prove-stop.sh [--format claude|codex|grok|cursor|json|plain] [--check]
#   claude,codex,grok  block via exit 2 + reason on stderr
#   cursor             block via {"followup_message": "..."} on stdout, exit 0
#                      (exit 2 is a silent no-op on Cursor's stop)
#   json               block via {"decision":"block","reason":"..."}, exit 0
#   plain              print reason, exit 1
#   --check            do not block; print OK/FAIL, exit 0/1 (for humans and CI)
set -u

format="claude"
check_only=0
while [ $# -gt 0 ]; do
  case "$1" in
    --format)
      [ $# -ge 2 ] || { echo "prove-stop: --format needs a value" >&2; exit 2; }
      format="$2"; shift 2 ;;
    --check) check_only=1; shift ;;
    *)
      echo "prove-stop: unknown argument: $1" >&2
      exit 2 ;;
  esac
done

# Resolve the repo root. This script is intended to be registered project-level
# (<repo>/hooks/prove-stop.sh), so derive the root from the script's own location
# rather than the hook process cwd, which differs per harness.
if [ -n "${BEND_PROOF_ROOT:-}" ]; then
  echo "prove-stop: BEND_PROOF_ROOT override in effect ($BEND_PROOF_ROOT)" >&2
  root="$BEND_PROOF_ROOT"
else
  self_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
  root="$(dirname -- "$self_dir")"
  [ -f "$root/LAWS.bend" ] || root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
fi

# Find the proof: PROOF.bend beside LAWS.bend, at the root or under bend/.
proof=""
for candidate in "$root/PROOF.bend" "$root/bend/PROOF.bend"; do
  if [ -f "$candidate" ] && [ -f "$(dirname "$candidate")/LAWS.bend" ]; then
    proof="$candidate"; break
  fi
done
[ -n "$proof" ] || exit 0    # no Bend laws in this repo: nothing to gate

bend_bin="$(command -v bend 2>/dev/null || true)"
if [ -z "$bend_bin" ] && [ -x "$HOME/.bend/bin/bend" ]; then
  bend_bin="$HOME/.bend/bin/bend"
fi
if [ -z "$bend_bin" ]; then
  echo "prove-stop: bend not found; skipping law proof for $root" >&2
  exit 0
fi

rel="${proof#"$root"/}"
bend_wrap=""
if command -v timeout >/dev/null 2>&1; then bend_wrap="timeout 240"
elif command -v gtimeout >/dev/null 2>&1; then bend_wrap="gtimeout 240"
fi
proof_out="$(cd "$root" && $bend_wrap "$bend_bin" "$rel" --check-only 2>&1)"
status=$?

# Bend exits 0 and prints "All terms check." on success, but prints
# "All terms check, but N defs rely on unsafe or foreign code:" when a proven law
# leans on an @unsafe def (this repo does, via pack). Match the prefix, not the
# period, or a passing proof reads as a failure and the agent is blocked forever.
pass=0
if [ "$status" -eq 0 ] && printf '%s\n' "$proof_out" | grep -q 'All terms check'; then
  pass=1
fi

# Loop-guard state. Pick a directory we can actually write: an unwritable
# state dir would silently disable the guard and block the agent forever.
state_dir="${XDG_CACHE_HOME:-$HOME/.cache}/prove-stop"
if ! mkdir -p "$state_dir" 2>/dev/null || ! touch "$state_dir/.probe" 2>/dev/null; then
  state_dir="${TMPDIR:-/tmp}/prove-stop-$(id -u)"
  if ! mkdir -p "$state_dir" 2>/dev/null || ! touch "$state_dir/.probe" 2>/dev/null; then
    state_dir=""
  fi
fi
rm -f "${state_dir:-/nonexistent}/.probe" 2>/dev/null
key="${state_dir:-/nonexistent}/$(printf '%s' "$root" | tr -c 'A-Za-z0-9' '_').count"

if [ "$pass" -eq 1 ]; then
  rm -f "$key" 2>/dev/null
  [ "$check_only" -eq 1 ] && { printf 'OK %s\n' "$rel"; exit 0; }
  exit 0
fi

if [ "$check_only" -eq 1 ]; then
  printf 'FAIL %s\n%s\n' "$rel" "$proof_out"
  exit 1
fi

# Without somewhere to keep the count there is no way to bound retries, and
# blocking forever would wedge the agent. Fail open and let CI be the backstop.
if [ -z "$state_dir" ]; then
  echo "prove-stop: no writable state dir; cannot bound retries, allowing the stop (CI still enforces)" >&2
  exit 0
fi

# Loop guard: after N consecutive failures let the agent stop, or it can never
# finish. The count resets on a pass and after the staleness window.
now="$(date +%s)"
count=0; stamp=0
if [ -n "$state_dir" ]; then
  [ -f "$key" ] && read -r count stamp < "$key"
  [ "${stamp:-0}" -gt 0 ] && [ "$((now - stamp))" -gt 1800 ] && count=0
  count=$((count + 1))
  printf '%s %s\n' "$count" "$now" > "$key" 2>/dev/null || true
fi
if [ "$count" -gt 3 ]; then
  echo "prove-stop: $count consecutive failed proofs in $root; standing down (CI still enforces)" >&2
  exit 0
fi

reason="The Bend law proof in $root is red ($rel). Fix the laws/proof, or the code the laws bind, before ending the turn.

$proof_out"

# Emit an escaped JSON string body, without the surrounding quotes. A hand-rolled
# sed/awk pipeline is not a JSON encoder: it leaves tabs and other C0 controls
# literal, and JSON forbids those inside a string.
json_body() {
  if command -v python3 >/dev/null 2>&1; then
    printf '%s' "$1" | python3 -c 'import json,sys; sys.stdout.write(json.dumps(sys.stdin.read())[1:-1])'
  else
    printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' | tr -d '\000-\011\013-\037' | awk '{printf "%s\\n", $0}'
  fi
}

case "$format" in
  cursor)
    printf '{"followup_message":"%s"}\n' "$(json_body "$reason")"
    exit 0 ;;
  json)
    printf '{"decision":"block","reason":"%s"}\n' "$(json_body "$reason")"
    exit 0 ;;
  plain)
    printf '%s\n' "$reason"
    exit 1 ;;
  *)
    printf '%s\n' "$reason" >&2
    exit 2 ;;
esac
