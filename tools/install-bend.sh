#!/bin/sh
# Install Bend 2.0.27 under ~/.bend for make prove and hooks/prove-stop.sh.
set -eu

version=2.0.27
checksum=58adc86af6605ed0c48f7d84e4c23028f78893ce4a867a20a4f004b11582687b
bend_bin="${HOME}/.bend/bin/bend"
base="${HOME}/.bend/bend2/base.bend"

if [ -x "$bend_bin" ] && [ -f "$base" ]; then
	if "$bend_bin" version 2>/dev/null | grep -q "$version"; then
		exit 0
	fi
fi

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

url="https://github.com/bendlang/bend/releases/download/v${version}/bend-${version}-linux-x64.tar.gz"
curl -fsSL "$url" -o "$tmpdir/bend.tar.gz"
printf '%s  bend.tar.gz\n' "$checksum" | (cd "$tmpdir" && sha256sum -c)
tar -xzf "$tmpdir/bend.tar.gz" -C "$tmpdir"
test -x "$tmpdir/bend/bin/bend"
test -f "$tmpdir/bend/bend2/base.bend"

mkdir -p "${HOME}/.bend/bin" "${HOME}/.bend/bend2"
cp "$tmpdir/bend/bin/bend" "$bend_bin"
rm -rf "${HOME}/.bend/bend2"
cp -a "$tmpdir/bend/bend2" "${HOME}/.bend/"

echo "installed bend $version at $bend_bin"
