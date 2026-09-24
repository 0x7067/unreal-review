#!/bin/sh
# Prove the laws, and refuse to let the proof quietly lean on @unsafe code.
#
# Bend exits 0 both for "All terms check." and for "All terms check, but N defs
# rely on unsafe or foreign code: ...". The second is a real obligation waived:
# @unsafe skips the termination check, so a non-terminating def can discharge any
# law. Accepting it silently makes "@unsafe" the cheapest way to turn a red proof
# green, which is exactly the bypass this script closes.
set -u
cd "$(dirname "$0")/.." || exit 1

bend_bin="${BEND:-bend}"
if ! command -v "$bend_bin" >/dev/null 2>&1 && [ -x "$HOME/.bend/bin/bend" ]; then
  bend_bin="$HOME/.bend/bin/bend"
fi

out="$("$bend_bin" PROOF.bend --check-only 2>&1)"
status=$?
printf '%s\n' "$out"
[ "$status" -eq 0 ] || exit 1
printf '%s\n' "$out" | grep -q 'All terms check' || {
  echo "prove: bend did not report a checked proof" >&2
  exit 1
}

reported="$(printf '%s\n' "$out" | sed -n '/rely on unsafe or foreign code:/,$p' | sed -n 's/^- //p' | sort)"
allowed="$(sed -e 's/#.*//' -e '/^[[:space:]]*$/d' spec/unsafe-allow.txt | sort)"

if [ "$reported" != "$allowed" ]; then
  echo "" >&2
  echo "prove: the set of laws relying on @unsafe code changed." >&2
  echo "  bend reports : ${reported:-<none>}" >&2
  echo "  allowed by   : spec/unsafe-allow.txt -> ${allowed:-<none>}" >&2
  echo "A new entry means a law is no longer proven, only asserted. Prove it," >&2
  echo "or record it in spec/unsafe-allow.txt in its own commit." >&2
  exit 1
fi

echo "prove: ok${reported:+
prove: leaning on @unsafe code (recorded in spec/unsafe-allow.txt):
$(printf '%s\n' "$reported" | sed 's/^/  - /')}"
