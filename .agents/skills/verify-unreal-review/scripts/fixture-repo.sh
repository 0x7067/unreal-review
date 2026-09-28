#!/bin/bash
set -euo pipefail

. "$(dirname "$0")/lib.sh"
require_launch

name=${1:-review}
root="$VERIFY_SCRATCH/repos/$name"
rm -rf "$root"
mkdir -p "$root/cmd"
git -C "$root" init -q -b main
git -C "$root" config user.email "verify@example.com"
git -C "$root" config user.name "Verify"

printf 'hello\n' >"$root/hello.txt"
git -C "$root" add hello.txt
git -C "$root" commit -q -m "base"
base=$(git -C "$root" rev-parse HEAD)

printf 'hello world\n' >"$root/hello.txt"
printf 'skip me\n' >"$root/skip.lock"
printf 'package main\n' >"$root/cmd/main.go"
git -C "$root" add hello.txt skip.lock cmd/main.go
git -C "$root" commit -q -m "head"
head=$(git -C "$root" rev-parse HEAD)

printf 'hello dirty\n' >"$root/hello.txt"
printf 'dirty\n' >"$root/extra.txt"

meta="$VERIFY_EVIDENCE/fixture-$name.txt"
envfile="$VERIFY_SCRATCH/fixture.env"
{
	echo "VERIFY_FIXTURE=$root"
	echo "BASE_SHA=$base"
	echo "HEAD_SHA=$head"
} | tee "$meta" "$envfile"
