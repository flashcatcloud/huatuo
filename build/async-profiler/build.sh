#!/usr/bin/env bash
# Copyright 2026 The HuaTuo Authors
# SPDX-License-Identifier: Apache-2.0
set -euo pipefail

# Use upstream's release toolchain: building on the host can silently raise
# the minimum glibc version of the injected library.
script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
output_dir=$(cd "${1:?usage: build.sh TOOL_DIRECTORY}" && pwd)
case "$(uname -m)" in
x86_64)
	arch=x64
	builder=public.ecr.aws/async-profiler/asprof-builder-x86@sha256:5244657fc3e9e3c5a0162d44777411c44031065071665776651068f94f7214f4
	;;
aarch64 | arm64)
	arch=arm64
	builder=public.ecr.aws/async-profiler/asprof-builder-arm@sha256:aabe632bc7323890d5a7330f88519793575a88f422eb298c242697acda6648cb
	;;
*)
	echo "async-profiler requires a native Linux amd64 or arm64 builder" >&2
	exit 1
	;;
esac
[[ $(uname -s) == Linux ]] || {
	echo "async-profiler must be built on Linux" >&2
	exit 1
}
[[ ${2:-$arch} == "$arch" ]] || {
	echo "async-profiler cross-architecture install is not supported" >&2
	exit 1
}
command -v docker > /dev/null || {
	echo "install Docker to build the patched Java agent" >&2
	exit 1
}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
source_sha=807b83aefc86fcb067395d526684f3ea127919d611fe3bde1647938af7522869
curl -fsSL --retry 5 --connect-timeout 15 --max-time 300 \
	https://codeload.github.com/async-profiler/async-profiler/tar.gz/refs/tags/v4.5 -o "$work/source.tar.gz"
printf '%s  %s\n' "$source_sha" "$work/source.tar.gz" | sha256sum --check --strict -
mkdir "$work/source"
tar xzf "$work/source.tar.gz" --strip-components=1 -C "$work/source"
(cd "$work/source" && patch --batch --fuzz=0 -p1 < "$script_dir/window-completion.patch")
docker run --rm --network none --user "$(id -u):$(id -g)" \
	-v "$work/source:/source" -w /source "$builder" sh -ec '
    JAVA_HOME=$(dirname "$(dirname "$(readlink -f "$(command -v javac)")")")
    export JAVA_HOME
    make -j2 CC=/usr/local/musl/bin/musl-gcc build/bin build/lib build/lib/libasyncProfiler.so build/bin/asprof
    patchelf --remove-needed ld-linux-x86-64.so.2 --remove-needed ld-linux-aarch64.so.1 build/lib/libasyncProfiler.so
    '
install -m 0755 "$work/source/build/bin/asprof" "$output_dir/bin/asprof"
install -m 0755 "$work/source/build/lib/libasyncProfiler.so" "$output_dir/lib/libasyncProfiler.so"
{
	printf 'version=4.5\nprotocol=window-completion-v1\nsource_sha256=%s\nbuilder=%s\n' "$source_sha" "$builder"
	printf 'patch_sha256=%s\n' "$(sha256sum "$script_dir/window-completion.patch" | cut -d ' ' -f1)"
} > "$output_dir/window-completion-build.txt"
