#!/usr/bin/env python3
# Copyright 2026 The HuaTuo Authors
# SPDX-License-Identifier: Apache-2.0
"""Exercise Huatuo's real Java runtime against isolated counted JVMs."""
import argparse
import glob
import hashlib
import json
import os
from pathlib import Path
import subprocess
import shutil
import tempfile
import time

from protocol import wait_for


def counts(paths):
    return [sum(int(line.rsplit(" ", 1)[1]) for line in Path(path).read_text().splitlines() if line) for path in paths]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--driver", required=True)
    parser.add_argument("--java-home", required=True)
    parser.add_argument("--profiler", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--old-profiler")
    parser.add_argument("--cases", default="natural,stop,kill,kill-finish")
    args = parser.parse_args()
    root = Path(tempfile.mkdtemp(prefix="huatuo-java-runtime-"))
    subprocess.run([args.java_home + "/bin/javac", "-d", str(root), str(Path(__file__).with_name("Counted.java"))], check=True)
    results = []
    for mode in args.cases.split(","):
        directory = root / mode
        directory.mkdir()
        ready = directory / "ready.json"
        output = directory / "output.json"
        with (directory / "log").open("w") as log:
            env = dict(os.environ)
            gate = directory / "finish-gate"
            if mode == "kill-finish":
                env["ASPROF_FINISH_GATE"] = str(gate)
            jvm = subprocess.Popen([args.java_home + "/bin/java", "-cp", str(root), "Counted", str(directory)], env=env, stdout=log, stderr=log)
            controller = None
            try:
                time.sleep(0.3)
                controller = subprocess.Popen([args.driver, "-pid", str(jvm.pid), "-tool", args.profiler, "-mode", "stop" if mode == "kill-finish" else mode, "-ready", str(ready), "-output", str(output)], stdout=log, stderr=log)
                wait_for(lambda: ready.exists() or controller.poll() is not None)
                assert ready.exists(), (directory / "log").read_text()
                paths = json.loads(ready.read_text())
                pattern = paths[str(jvm.pid)]
                marker = Path(pattern.removesuffix("-*.collapsed") + ".complete")
                (directory / "go").touch()
                if mode == "kill":
                    time.sleep(0.2)
                    controller.kill()
                    controller.wait()
                wait_for(lambda: (directory / "first.done").exists())
                wait_for(lambda: Path(pattern.replace("*", "0")).exists())
                phase_status = directory / "phase-status"
                subprocess.run([args.profiler + "/bin/asprof", "load", str(jvm.pid), "/tmp/libasyncProfiler.so", "true", "status,file=" + str(phase_status)], check=True, stdout=log, stderr=log)
                assert "is running" in phase_status.read_text(), phase_status.read_text()
                (directory / "go-second").touch()
                if mode == "kill-finish":
                    wait_for(lambda: Path(str(gate) + ".entered").exists())
                    assert not marker.exists(), "finish completed before controller was killed"
                    controller.kill()
                    controller.wait()
                    gate.touch()
                elif mode != "kill":
                    assert controller.wait(timeout=15) == 0, (directory / "log").read_text()
                wait_for(marker.exists)
                files = sorted(glob.glob(pattern))
                values = counts(files)
                assert sum(values) == 255, (mode, values)
                assert jvm.poll() is None
                if mode not in ("kill", "kill-finish"):
                    records = json.loads(output.read_text())
                    delivered = sum(int(line.rsplit(" ", 1)[1]) for item in records for line in item["output"].splitlines() if line)
                    assert delivered == 255, (mode, delivered)
                    assert len(records) == len(files), (mode, len(records), len(files))
                before = {path: hashlib.sha256(Path(path).read_bytes()).hexdigest() for path in files}
                time.sleep(1.2)
                assert sorted(glob.glob(pattern)) == files
                assert before == {path: hashlib.sha256(Path(path).read_bytes()).hexdigest() for path in files}
                result = dict(mode=mode, counts=values, expected=255, jvm_alive=True, recovery_files=len(files))
                results.append(result)
                print(json.dumps(result), flush=True)
            finally:
                if controller is not None and controller.poll() is None:
                    controller.kill()
                    controller.wait()
                jvm.kill()
                jvm.wait()
    if args.old_profiler:
        directory = root / "old-loaded-agent"
        directory.mkdir()
        with (directory / "log").open("w") as log:
            jvm = subprocess.Popen([args.java_home + "/bin/java", "-cp", str(root), "Counted", str(directory)], stdout=log, stderr=log)
            try:
                time.sleep(0.3)
                shutil.copyfile(args.old_profiler + "/lib/libasyncProfiler.so", "/tmp/libasyncProfiler.so")
                subprocess.run([args.old_profiler + "/bin/asprof", "load", str(jvm.pid), "/tmp/libasyncProfiler.so", "true", "version"], check=True, stdout=log, stderr=log)
                run = subprocess.run([args.driver, "-pid", str(jvm.pid), "-tool", args.profiler, "-ready", str(directory / "ready"), "-output", str(directory / "output")], text=True, capture_output=True, timeout=15)
                assert run.returncode != 0 and "without window-completion-v1" in run.stderr, (run.returncode, run.stdout, run.stderr)
                assert not (directory / "ready").exists()
                assert jvm.poll() is None
                results.append(dict(mode="old-loaded-agent", rejected=True, jvm_alive=True))
                print(json.dumps(results[-1]), flush=True)
            finally:
                jvm.kill()
                jvm.wait()
    Path(args.output).write_text(json.dumps(dict(root=str(root), results=results), indent=2))


if __name__ == "__main__":
    main()
