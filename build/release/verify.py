#!/usr/bin/env python3
"""Validate an extracted release on its native Linux architecture."""
import json
import pathlib
import subprocess
import sys


def verify(root, arch):
    root = pathlib.Path(root)
    machine = {"amd64": b"\x3e\x00", "arm64": b"\xb7\x00"}[arch]
    metadata = json.loads((root / "build.json").read_text())
    if metadata["architecture"] != arch:
        raise ValueError("manifest architecture mismatch")
    for path in root.rglob("*"):
        if path.is_symlink() or path.name.startswith("._") or path.name == ".DS_Store":
            raise ValueError(f"unsupported archive entry: {path}")
    required = ["bin/huatuo-bamai", "bin/asprof", "py-spy", "lib/libasyncProfiler.so",
                "conf/huatuo-bamai.conf", "conf/huatuo-apiserver.conf", "LICENSE"]
    for name in required:
        if not (root / name).is_file() or not (root / name).stat().st_size:
            raise ValueError(f"missing/empty release file: {name}")
    binaries = list((root / "bin").iterdir()) + [root / "py-spy", root / "lib/libasyncProfiler.so"]
    for path in binaries:
        header = path.read_bytes()[:20]
        if header[:6] != b"\x7fELF\x02\x01" or header[18:20] != machine:
            raise ValueError(f"wrong ELF architecture: {path}")
        if path.parent.name == "bin" and path.name != "asprof":
            info = subprocess.check_output(["readelf", "-l", str(path)], text=True)
            dynamic = subprocess.check_output(["readelf", "-d", str(path)], text=True)
            if "INTERP" in info or "(NEEDED)" in dynamic:
                raise ValueError(f"huatuo executable is not static: {path}")
    objects = list((root / "bpf").glob("*.o"))
    if not objects:
        raise ValueError("missing BPF objects")
    for path in objects:
        header = path.read_bytes()[:20]
        if header[:6] != b"\x7fELF\x02\x01" or header[18:20] != b"\xf7\x00":
            raise ValueError(f"not an ELF BPF object: {path}")
    # CLI loading catches missing runtime libraries without attaching to host workloads.
    for exe, flag in [("bin/huatuo-bamai", "--version"), ("bin/asprof", "--version"), ("py-spy", "--version")]:
        subprocess.run([str(root.resolve() / exe), flag], check=True, timeout=30)


if __name__ == "__main__":
    verify(*sys.argv[1:])
