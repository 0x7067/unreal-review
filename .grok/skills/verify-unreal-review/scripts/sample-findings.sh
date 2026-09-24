#!/bin/bash
set -euo pipefail

. "$(dirname "$0")/lib.sh"
require_launch

kind=${1:-}
out=${2:-}
if [ -z "$kind" ] || [ -z "$out" ]; then
	echo "usage: sample-findings.sh complete|running|failed|legacy|empty <path>" >&2
	exit 2
fi

mkdir -p "$(dirname "$out")"

complete() {
	cat "$VERIFY_EXAMPLE"
}

running() {
	cat <<'EOF'
{"v":1,"type":"run","id":"00000000-0000-0000-0000-000000000002","created_at":"2026-09-23T12:00:00Z","model":"anthropic/claude-sonnet-4.5","status":"running","source":{"kind":"git","base":"main","head":"HEAD","base_sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","head_sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","diff_sha":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"},"cost":{"amount_usd":0.010000,"currency":"USD","input_tokens":10,"output_tokens":2,"requests":1}}
{"v":1,"type":"finding","id":"c0ffee00c0ffee00","path":"internal/cache/cache.go","start_line":40,"end_line":42,"anchor":"new","severity":"error","body":"Concurrent map writes on the shared cache are not synchronized."}
EOF
}

failed() {
	cat <<'EOF'
{"v":1,"type":"run","id":"00000000-0000-0000-0000-000000000003","created_at":"2026-09-23T12:00:00Z","model":"x","status":"failed","source":{"kind":"git","base":"main","head":"HEAD","base_sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","head_sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","diff_sha":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"},"cost":{"amount_usd":0,"currency":"USD","input_tokens":0,"output_tokens":0,"requests":0}}
EOF
}

legacy() {
	cat <<'EOF'
{"v":1,"type":"run","id":"00000000-0000-0000-0000-000000000004","created_at":"2026-09-23T12:00:00Z","model":"x","source":{"kind":"git","base":"main","head":"HEAD","base_sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","head_sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","diff_sha":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"},"cost":{"amount_usd":0,"currency":"USD","input_tokens":0,"output_tokens":0,"requests":0}}
{"v":1,"type":"finding","id":"oldline00000001","path":"a.go","start_line":1,"end_line":1,"anchor":"old","severity":"error","body":"deleted"}
{"v":1,"type":"summary","body":"legacy"}
EOF
}

empty() {
	: >"$out"
	return 0
}

case "$kind" in
complete) complete >"$out" ;;
running) running >"$out" ;;
failed) failed >"$out" ;;
legacy) legacy >"$out" ;;
empty) empty ;;
*)
	echo "sample-findings.sh: unknown kind $kind" >&2
	exit 2
	;;
esac

echo "wrote $out ($kind)"
