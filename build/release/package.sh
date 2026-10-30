#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
version=${1:?usage: package.sh VERSION ARCH}
arch=${2:?usage: package.sh VERSION ARCH}
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.-]+)?$ ]] || { echo "invalid release version" >&2; exit 1; }
case "$arch:$(uname -m)" in
amd64:x86_64|arm64:aarch64) ;;
*) echo "build on native Linux $arch" >&2; exit 1 ;;
esac
[[ $(uname -s) == Linux ]] || exit 1
git diff --quiet
git diff --cached --quiet
commit=$(git rev-parse HEAD)
epoch=$(git show -s --format=%ct HEAD)
# A clean checkout avoids stale objects from another architecture.
[[ ! -e _output ]] || { echo "_output exists; use a fresh checkout" >&2; exit 1; }
make build BUILD_MODE=static APP_VERSION="$version" APP_COMMIT="$commit" APP_BUILD_TIME="$epoch"
make install-tools
for command in cmd/*/; do
  test -x "_output/bin/$(basename "$command")"
done
name="huatuo-bamai-${version}-doris-static-linux-${arch}"
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
mkdir -p "$stage/$name" dist
cp -R _output/bin _output/bpf _output/conf "$stage/$name/"
cp LICENSE "$stage/$name/"
cp _output/tools/bin/asprof "$stage/$name/bin/"
cp -R _output/tools/lib "$stage/$name/"
cp _output/tools/py-spy "$stage/$name/"
# Preserve the pinned upstream sampler licenses and metadata.
cp -R _output/tools "$stage/$name/tools"
python3 - "$stage/$name/build.json" "$version" "$commit" "$arch" "$epoch" <<'META'
import json, sys
from pathlib import Path
out, version, commit, arch, epoch = sys.argv[1:]
Path(out).write_text(json.dumps(dict(version=version, commit=commit, architecture=arch,
    source_epoch=int(epoch), storage='doris', sampler_source='build/install-tools.sh'), indent=2)+'\n')
META
python3 build/release/verify.py "$stage/$name" "$arch"
COPYFILE_DISABLE=1 tar --format=gnu --no-xattrs --no-acls --no-selinux \
  --owner=0 --group=0 --numeric-owner --sort=name --mtime="@$epoch" \
  -C "$stage" -cf - "$name" | gzip -n > "dist/$name.tar.gz"
mkdir "$stage/extracted"
tar -xzf "dist/$name.tar.gz" -C "$stage/extracted" 2> "$stage/tar.stderr"
[[ ! -s "$stage/tar.stderr" ]]
diff -r "$stage/$name" "$stage/extracted/$name"
python3 build/release/verify.py "$stage/extracted/$name" "$arch"
(cd dist && sha256sum "$name.tar.gz" > "$name.tar.gz.sha256")
