#!/bin/bash
set -u

. "$(dirname "$0")/lib.sh"
require_launch

name=""
no_github=0
stdin_file=""
while [ $# -gt 0 ]; do
	case "$1" in
	--name)
		name=$2
		shift 2
		;;
	--no-github-auth)
		no_github=1
		shift
		;;
	--stdin)
		stdin_file=$2
		shift 2
		;;
	--)
		shift
		break
		;;
	-*)
		echo "usage: cli.sh --name <step> [--no-github-auth] [--stdin file] -- <unreal-review args>" >&2
		exit 2
		;;
	*)
		break
		;;
	esac
done

if [ -z "$name" ]; then
	echo "cli.sh: --name is required" >&2
	exit 2
fi
case "$name" in
*[!A-Za-z0-9._-]*)
	echo "cli.sh: --name must be a single path segment of letters, digits, dot, underscore, or hyphen" >&2
	exit 2
	;;
esac

step="$VERIFY_EVIDENCE/$name"
mkdir -p "$step"

bin=$VERIFY_BIN
path=$PATH
if [ "$no_github" -eq 1 ]; then
	shim="$VERIFY_SCRATCH/no-github-bin"
	mkdir -p "$shim"
	printf '%s\n' '#!/bin/sh' 'exit 1' >"$shim/gh"
	chmod +x "$shim/gh"
	path="$shim:$PATH"
fi

{
	echo "bin=$bin"
	echo "cwd=$(pwd)"
	echo "no_github_auth=$no_github"
	if [ -n "$stdin_file" ]; then
		echo "stdin=$stdin_file"
	fi
	printf 'args='
	printf '%q ' "$bin" "$@"
	echo
} >"$step/cmd.txt"

set +e
if [ "$no_github" -eq 1 ]; then
	if [ -n "$stdin_file" ]; then
		env -u GH_TOKEN -u GITHUB_TOKEN PATH="$path" "$bin" "$@" <"$stdin_file" >"$step/stdout.txt" 2>"$step/stderr.txt"
	else
		env -u GH_TOKEN -u GITHUB_TOKEN PATH="$path" "$bin" "$@" >"$step/stdout.txt" 2>"$step/stderr.txt"
	fi
else
	if [ -n "$stdin_file" ]; then
		"$bin" "$@" <"$stdin_file" >"$step/stdout.txt" 2>"$step/stderr.txt"
	else
		"$bin" "$@" >"$step/stdout.txt" 2>"$step/stderr.txt"
	fi
fi
status=$?
set -e
echo "$status" >"$step/exit.txt"
echo "$name exit=$status evidence=$step"
exit 0
