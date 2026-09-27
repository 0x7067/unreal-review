#!/usr/bin/env bash
set -u

usage() {
  cat <<'USAGE'
Gate an agent's turn on the repo's Bend law proof.

Usage: prove-stop.sh [--format claude|codex|grok|cursor|json|plain] [--check]
  claude,codex,grok  block via exit 2 + reason on stderr
  cursor             block via {"followup_message": "..."} on stdout, exit 0
                     (exit 2 is a silent no-op on Cursor's stop)
  json               block via {"decision":"block","reason":"..."}, exit 0
  plain              print reason, exit 1
  --check            do not block; print OK/FAIL, exit 0/1 (for humans and CI)
USAGE
}

format="claude"
check_only=0
while [ $# -gt 0 ]; do
  case "$1" in
    --format)
      [ $# -ge 2 ] || { echo "prove-stop: --format needs a value" >&2; exit 2; }
      format="$2"; shift 2 ;;
    --check) check_only=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *)
      echo "prove-stop: unknown argument: $1" >&2
      usage >&2
      exit 2 ;;
  esac
done

if [ -n "${BEND_PROOF_ROOT:-}" ]; then
  echo "prove-stop: BEND_PROOF_ROOT override in effect ($BEND_PROOF_ROOT)" >&2
  root="$BEND_PROOF_ROOT"
else
  self_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
  root="$(dirname -- "$self_dir")"
  [ -f "$root/LAWS.bend" ] || root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
fi

laws=""
proof=""
for dir in "$root" "$root/bend"; do
  if [ -f "$dir/LAWS.bend" ]; then
    laws="$dir/LAWS.bend"
    [ -f "$dir/PROOF.bend" ] && proof="$dir/PROOF.bend"
    break
  fi
done
[ -n "$laws" ] || exit 0

bend_bin="$(command -v bend 2>/dev/null || true)"
if [ -z "$bend_bin" ] && [ -x "$HOME/.bend/bin/bend" ]; then
  bend_bin="$HOME/.bend/bin/bend"
fi

rel="${proof:-"${laws%LAWS.bend}PROOF.bend"}"
rel="${rel#"$root"/}"
bend_wrap=""
if command -v timeout >/dev/null 2>&1; then bend_wrap="timeout 240"
elif command -v gtimeout >/dev/null 2>&1; then bend_wrap="gtimeout 240"
fi
if [ -z "$proof" ]; then
  proof_out="$rel is missing, so the laws in $laws cannot be checked. Restore it, or remove $laws with it."
  status=1
elif [ -z "$bend_bin" ]; then
  proof_out="bend is not installed, so $laws cannot be checked (make prove fails the same way)."
  status=1
elif [ -x "$root/tools/prove.sh" ]; then
  proof_out="$(cd "$root" && $bend_wrap sh tools/prove.sh 2>&1)"
  status=$?
else
  proof_out="$(cd "$root" && $bend_wrap "$bend_bin" "$rel" --check-only 2>&1)"
  status=$?
fi

bend_checked_prefix='All terms check'
pass=0
if [ "$status" -eq 0 ] && printf '%s\n' "$proof_out" | grep -qF "$bend_checked_prefix"; then
  pass=1
fi

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

if [ -z "$state_dir" ]; then
  echo "prove-stop: no writable state dir; cannot bound retries, allowing the stop (CI still enforces)" >&2
  exit 0
fi

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

reason="The Bend law gate in $root did not pass ($rel). Fix the laws/proof, or the code the laws bind, before ending the turn.

$proof_out"

json_string_escape() {
  if command -v python3 >/dev/null 2>&1; then
    printf '%s' "$1" | python3 -c 'import json,sys; sys.stdout.write(json.dumps(sys.stdin.read())[1:-1])'
  else
    printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' | tr -d '\000-\011\013-\037' | awk '{printf "%s\\n", $0}'
  fi
}

case "$format" in
  cursor)
    printf '{"followup_message":"%s"}\n' "$(json_string_escape "$reason")"
    exit 0 ;;
  json)
    printf '{"decision":"block","reason":"%s"}\n' "$(json_string_escape "$reason")"
    exit 0 ;;
  plain)
    printf '%s\n' "$reason"
    exit 1 ;;
  *)
    printf '%s\n' "$reason" >&2
    exit 2 ;;
esac
