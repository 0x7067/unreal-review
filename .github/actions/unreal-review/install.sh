#!/bin/bash
# Install unreal-review from a 0x7067/unreal-agent release asset.
#
# Asset URL (from .goreleaser.yaml at e9ed99e9):
#   https://github.com/0x7067/unreal-agent/releases/download/<tag>/unreal-agent_<version>_<os>_<arch>.tar.gz
# <version> is <tag> with one leading "v" removed (goreleaser {{ .Version }}).
# The tarball is checked against the digest pinned below. Only the archive
# member named unreal-review is extracted.
set -euo pipefail

UNREAL_AGENT_VERSION="v0.4.2-fork.1"
version="$UNREAL_AGENT_VERSION"

# A version bump must update these digests too. The same version and digest
# table is copied into examples/github-actions/review.yml (both jobs); a bump
# must update that file too.
pinned_digest() {
	case "$1" in
	darwin_amd64) printf '%s\n' 948cf8b0d95b42a47c5593e3b12d8ddb2b08c09f0f8f19fb0f531a2332cb24e7 ;;
	darwin_arm64) printf '%s\n' c675e5bff4392eb968f34ca467bef29698f0ac7fb486d9d42e3b652655fb1067 ;;
	linux_amd64) printf '%s\n' 0b1e0f705f4656e3171b7e3efaf32ae8ab403c9b561a53cc9f252e8ccd157827 ;;
	linux_arm64) printf '%s\n' 0bebeb9198be087bc70255aa340d21cd459ca8e107489945c08f964aa6419840 ;;
	*) return 1 ;;
	esac
}

file_sha256() {
	case "$1" in
	darwin) shasum -a 256 "$2" ;;
	linux) sha256sum "$2" ;;
	*) return 1 ;;
	esac | awk '{print $1}'
}

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
url="https://github.com/0x7067/unreal-agent/releases/download/${version}/${asset}"

if [ -z "${GITHUB_PATH:-}" ]; then
	echo "::error::GITHUB_PATH is unset"
	exit 1
fi
if ! expect="$(pinned_digest "${os}_${arch}")"; then
	echo "::error::no pinned digest for ${os}_${arch}. A version bump must update the digests too."
	exit 1
fi

work="$(mktemp -d "${RUNNER_TEMP:-/tmp}/unreal-review-install.XXXXXX")"
trap 'rm -rf "$work"' EXIT

if ! curl -fsSL -o "${work}/${asset}" "$url"; then
	echo "::error::failed to download ${url}"
	exit 1
fi
got="$(file_sha256 "$os" "${work}/${asset}")"
if [ "$got" != "$expect" ]; then
	echo "::error::checksum mismatch for ${asset}: got ${got}, want ${expect}"
	exit 1
fi

# The member name is the plain path unreal-review, so this does not extract
# a .. or absolute entry.
if ! tar -xzf "${work}/${asset}" -C "$work" unreal-review || [ ! -f "${work}/unreal-review" ]; then
	echo "::error::${url} has no archive member named unreal-review. The job fails instead of skipping the review."
	exit 1
fi

dest="${RUNNER_TEMP:-/tmp}/unreal-review-bin"
mkdir -p "$dest"
install -m 0755 "${work}/unreal-review" "${dest}/unreal-review"
echo "$dest" >>"$GITHUB_PATH"
"${dest}/unreal-review" help >/dev/null
