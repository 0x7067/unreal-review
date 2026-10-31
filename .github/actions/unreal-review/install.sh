#!/bin/bash
# Install unreal-review from a 0x7067/unreal-agent release asset.
#
# Asset URL (from .goreleaser.yaml at e9ed99e9):
#   https://github.com/0x7067/unreal-agent/releases/download/<tag>/unreal-agent_<version>_<os>_<arch>.tar.gz
# <version> is <tag> with one leading "v" removed (goreleaser {{ .Version }}).
# That archive currently packs unreal-agent-runner and unreal-agent-tui only.
# A tag that does not contain an unreal-review binary fails this step.
set -euo pipefail

version="${UNREAL_AGENT_VERSION:-}"
if [ -z "$version" ] || [ "$version" = "v0.0.0-PLACEHOLDER" ]; then
	echo "::error::UNREAL_AGENT_VERSION is '${version:-<empty>}' (.github/actions/unreal-review/action.yml). Set that default to a 0x7067/unreal-agent release tag that contains the unreal-review binary. The review is not skipped."
	exit 1
fi

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
linux | darwin) ;;
*)
	echo "::error::unsupported OS ${os}; 0x7067/unreal-agent releases publish linux and darwin"
	exit 1
	;;
esac

machine="$(uname -m)"
case "$machine" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*)
	echo "::error::unsupported architecture ${machine}; 0x7067/unreal-agent releases publish amd64 and arm64"
	exit 1
	;;
esac

case "$version" in
v*) ver="${version#v}" ;;
*) ver="$version" ;;
esac

asset="unreal-agent_${ver}_${os}_${arch}.tar.gz"
base="https://github.com/0x7067/unreal-agent/releases/download/${version}"
url="${base}/${asset}"

if [ -z "${GITHUB_PATH:-}" ]; then
	echo "::error::GITHUB_PATH is unset"
	exit 1
fi

work="$(mktemp -d "${RUNNER_TEMP:-/tmp}/unreal-review-install.XXXXXX")"
trap 'rm -rf "$work"' EXIT

if ! curl -fsSL -o "${work}/${asset}" "$url"; then
	echo "::error::failed to download ${url}"
	exit 1
fi
if ! curl -fsSL -o "${work}/SHA256SUMS" "${base}/SHA256SUMS"; then
	echo "::error::failed to download ${base}/SHA256SUMS"
	exit 1
fi

checksum_line="$(awk -v asset="$asset" '$2 == asset { print }' "${work}/SHA256SUMS")"
if [ -z "$checksum_line" ]; then
	echo "::error::SHA256SUMS has no entry for ${asset}"
	exit 1
fi
printf '%s\n' "$checksum_line" | (cd "$work" && sha256sum -c -)

tar -xzf "${work}/${asset}" -C "$work"
if [ ! -f "${work}/unreal-review" ]; then
	echo "::error::${url} does not contain unreal-review. The job fails instead of skipping the review."
	exit 1
fi

dest="${RUNNER_TEMP:-/tmp}/unreal-review-bin"
mkdir -p "$dest"
install -m 0755 "${work}/unreal-review" "${dest}/unreal-review"
echo "$dest" >>"$GITHUB_PATH"
"${dest}/unreal-review" help >/dev/null
