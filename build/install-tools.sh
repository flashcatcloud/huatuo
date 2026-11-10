#!/usr/bin/env bash
#
# Copyright 2026 The HuaTuo Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TOOLS_DIR="$ROOT_DIR/_output/tools"
WORK_DIR=$(mktemp -d)
trap 'rm -rf "$WORK_DIR"' EXIT

ASYNC_PROFILER_VERSION=4.5
PY_SPY_VERSION=0.4.2

case "${TEST_TOOLS_ARCH:-$(uname -m)}" in
x86_64 | amd64)
	ASYNC_PROFILER_ARCH=x64
	ASYNC_PROFILER_SHA256=89546fbb9ee0fc5496c7edd4099b0709489bc78b0d8057ccbb4b801f6b032b62
	PY_SPY_URL=https://files.pythonhosted.org/packages/f9/34/dd7d3c763a00b7b965e25a5eab0acd1a345dbaf0f45fffe595278873a1c0/py_spy-0.4.2-py2.py3-none-manylinux_2_5_x86_64.manylinux1_x86_64.whl
	PY_SPY_SHA256=aeb0323409199c785f730645e9f4bb7a7b9ca2c481f2c331a55642b5d13fa52f
	;;
aarch64 | arm64)
	ASYNC_PROFILER_ARCH=arm64
	ASYNC_PROFILER_SHA256=64c41d1465d60097439c50d7e924b4946f1f62b1cbd21ce5b034fad09c0d6979
	PY_SPY_URL=https://files.pythonhosted.org/packages/89/23/3eb4c23c684ebd667674ce1d076ae855e0621d1d9bd5e052aa3f7982f757/py_spy-0.4.2-py2.py3-none-manylinux_2_17_aarch64.manylinux2014_aarch64.whl
	PY_SPY_SHA256=142887e984a4e541071c99a4401ff8c3770f255d329dbd0f64e8c1dd51882cce
	;;
*)
	echo "unsupported install-tools architecture: ${TEST_TOOLS_ARCH:-$(uname -m)}" >&2
	exit 1
	;;
esac

ASYNC_PROFILER_URL="https://github.com/async-profiler/async-profiler/releases/download/v${ASYNC_PROFILER_VERSION}/async-profiler-${ASYNC_PROFILER_VERSION}-linux-${ASYNC_PROFILER_ARCH}.tar.gz"

ASYNC_PROFILER_ARCHIVE="$WORK_DIR/async-profiler.tar.gz"
PY_SPY_WHEEL="$WORK_DIR/py-spy.whl"
curl_retry_args=(--retry 5 --retry-delay 2)
if curl --help all 2> /dev/null | grep -q -- '--retry-all-errors'; then
	curl_retry_args+=(--retry-all-errors)
fi
curl -fsSL --connect-timeout 15 --max-time 300 \
	"${curl_retry_args[@]}" \
	"$ASYNC_PROFILER_URL" -o "$ASYNC_PROFILER_ARCHIVE"
curl -fsSL --connect-timeout 15 --max-time 300 \
	"${curl_retry_args[@]}" \
	"$PY_SPY_URL" -o "$PY_SPY_WHEEL"
printf '%s  %s\n' "$ASYNC_PROFILER_SHA256" "$ASYNC_PROFILER_ARCHIVE" \
	| sha256sum --check --strict -
printf '%s  %s\n' "$PY_SPY_SHA256" "$PY_SPY_WHEEL" \
	| sha256sum --check --strict -

mkdir -p "$WORK_DIR/tools" "$(dirname "$TOOLS_DIR")"
tar -xzf "$ASYNC_PROFILER_ARCHIVE" --strip-components=1 \
	-C "$WORK_DIR/tools"
python3 - "$PY_SPY_WHEEL" "$WORK_DIR/tools/py-spy" << 'PY'
import pathlib
import sys
import zipfile

with zipfile.ZipFile(sys.argv[1]) as wheel:
    matches = [name for name in wheel.namelist() if name.endswith("/scripts/py-spy")]
    if len(matches) != 1:
        raise SystemExit("py-spy executable is missing or ambiguous in release wheel")
    pathlib.Path(sys.argv[2]).write_bytes(wheel.read(matches[0]))
PY
chmod 0755 "$WORK_DIR/tools/py-spy"
[[ -x "$WORK_DIR/tools/bin/asprof" ]] \
	|| {
		echo "async-profiler archive has no bin/asprof" >&2
		exit 1
	}
[[ -f "$WORK_DIR/tools/lib/libasyncProfiler.so" ]] \
	|| {
		echo "async-profiler archive has no lib/libasyncProfiler.so" >&2
		exit 1
	}

rm -rf "$TOOLS_DIR"
mv "$WORK_DIR/tools" "$TOOLS_DIR"
