#!/usr/bin/env python3
# Copyright 2026 The HuaTuo Authors
# SPDX-License-Identifier: Apache-2.0
"""Real JVM fault regression. No live production JVMs are attached."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import signal
import sys
import subprocess
import tempfile
import time


def wait_for(predicate, timeout=15):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        if predicate():
            return
        time.sleep(0.01)
    raise AssertionError("timed out waiting for test synchronization")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--java-home", required=True)
    parser.add_argument("--profiler", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--cases", default="natural,early,race-before,race-after,kill-idle,kill-finish,marker-retry,wrong-session,empty,memory,start-failure,deadline-during-publish,replaced-session,window-retry")
    args = parser.parse_args()
    root = Path(tempfile.mkdtemp(prefix="jvm-window-regression-"))
    source = Path(__file__).with_name("Counted.java")
    subprocess.run([args.java_home + "/bin/javac", "-d", str(root), str(source)], check=True)
    ap = str(Path(args.profiler) / "bin/asprof")
    lib = str(Path(args.profiler) / "lib/libasyncProfiler.so")
    results = []
    for case in args.cases.split(","):
        directory = root / case
        directory.mkdir()
        marker = directory / "complete"
        gate = directory / "gate"
        env = dict(os.environ)
        if case == "kill-finish":
            env["ASPROF_FINISH_GATE"] = str(gate)
        if case == "deadline-during-publish":
            env["ASPROF_WINDOW_GATE"] = str(gate)
        log = (directory / "jvm.log").open("w")
        jvm = subprocess.Popen([args.java_home + "/bin/java", "-cp", str(root), "Counted", str(directory)], env=env, stdout=log, stderr=log)
        controllers = []

        def command(raw, check=True):
            proc = subprocess.run([ap, "load", str(jvm.pid), lib, "true", raw], text=True, capture_output=True, timeout=15)
            if check and proc.returncode:
                raise AssertionError((case, raw, proc.returncode, proc.stdout, proc.stderr, (directory / "jvm.log").read_text()))
            return proc

        def finish():
            return command("finish,completion=" + str(marker))

        try:
            time.sleep(0.4)
            if case == "window-retry":
                windows = directory / "windows"
                windows.mkdir()
                command("start,event=Counted.hit,timeout=3s,loop=1s,collapsed,file=" + str(windows / "window-%n.collapsed") + ",completion=" + str(marker))
                (directory / "go").touch()
                wait_for(lambda: (directory / "first.done").exists())
                windows.rmdir()
                status = directory / "status"
                def stopped():
                    command("status,file=" + str(status))
                    return "not active" in status.read_text()
                wait_for(stopped)
                windows.mkdir()
                assert command("finish,completion=" + str(marker), False).returncode != 0
                assert marker.exists(), "failed recording could not publish its retained window"
                counts = [sum(int(line.rsplit(" ", 1)[1]) for line in p.read_text().splitlines() if line) for p in windows.glob("*.collapsed")]
                assert counts == [170], counts
                replacement = directory / "next.complete"
                command("start,event=Counted.hit,timeout=3s,loop=1s,collapsed,file=" + str(directory / "next-%n.collapsed") + ",completion=" + str(replacement))
                command("finish,completion=" + str(replacement))
                results.append(dict(case=case, counts=counts, expected=170, incomplete_reported=True))
                print(json.dumps(results[-1]), flush=True)
                continue
            event = "alloc=1,total" if case == "memory" else "event=Counted.hit"
            start_command = "start," + event + ",timeout=3s,loop=1s,collapsed,dot,file=" + str(directory / "window-%n.collapsed") + ",completion=" + str(marker)
            if case == "start-failure":
                assert command(start_command + ",target-cpu=0", False).returncode != 0
            if case == "kill-idle":
                ready = directory / "controller.ready"
                controller = subprocess.Popen([sys.executable, "-c",
                    "import subprocess,sys,time; from pathlib import Path; "
                    "subprocess.run(sys.argv[2:],check=True); Path(sys.argv[1]).touch(); time.sleep(30)",
                    str(ready), ap, "load", str(jvm.pid), lib, "true", start_command], stdout=log, stderr=log)
                controllers.append(controller)
                wait_for(ready.exists)
            else:
                command(start_command)
            start = time.monotonic()
            if case != "empty":
                (directory / "go").touch()
            if case == "kill-idle":
                controller.kill()
                controller.wait()
            if case == "deadline-during-publish":
                wait_for(lambda: Path(str(gate) + ".entered").exists())
                time.sleep(max(0, start + 3.2 - time.monotonic()))
                assert not list(directory.glob("*.collapsed")), "window gate released before deadline"
                gate.touch()
            elif case != "empty":
                wait_for(lambda: (directory / "first.done").exists())
                wait_for(lambda: (directory / "window-0.collapsed").exists())
                phase_status = directory / "phase-status"
                command("status,file=" + str(phase_status))
                assert "is running" in phase_status.read_text(), phase_status.read_text()
                (directory / "go-second").touch()
            if case in ("early", "kill-finish", "marker-retry", "wrong-session"):
                wait_for(lambda: (directory / "workload.done").exists())
            if case == "early":
                finish()
            elif case == "wrong-session":
                assert command("finish,completion=" + str(directory / "wrong"), False).returncode != 0
                assert not marker.exists()
                finish()
            elif case == "kill-finish":
                proc = subprocess.Popen([ap, "load", str(jvm.pid), lib, "true", "finish,completion=" + str(marker)], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                controllers.append(proc)
                wait_for(lambda: Path(str(gate) + ".entered").exists())
                assert not marker.exists(), "finish completed before controller was killed"
                proc.kill()
                proc.wait()
                gate.touch()
            elif case == "marker-retry":
                marker.mkdir()  # Forces terminal marker rename to fail after the tail is published.
                assert command("finish,completion=" + str(marker), False).returncode != 0
                before = {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in directory.glob("*.collapsed")}
                marker.rmdir()
                finish()
                assert before == {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in directory.glob("*.collapsed")}
            elif case.startswith("race-"):
                delay = 2.9 if case == "race-before" else 3.0
                time.sleep(max(0, start + delay - time.monotonic()))
                for _ in range(3):
                    controllers.append(subprocess.Popen([ap, "load", str(jvm.pid), lib, "true", "finish,completion=" + str(marker)], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL))
                for proc in controllers:
                    assert proc.wait(timeout=15) == 0
            wait_for(marker.is_file)
            content = marker.read_text().splitlines()
            assert content[0] == "window-completion-v1", content
            last = int(content[1])
            files = sorted(directory.glob("window-*.collapsed"))
            assert {p.name for p in files} == {"window-%d.collapsed" % i for i in range(last + 1)}
            counts = [sum(int(line.rsplit(" ", 1)[1]) for line in p.read_text().splitlines() if line) for p in files]
            expected = 0 if case == "empty" else (170 if case == "deadline-during-publish" else 255)
            if case == "memory":
                assert sum(counts) > 0
            else:
                assert sum(counts) == expected, (case, counts, expected)
            status = directory / "status"
            command("status,file=" + str(status))
            assert "not active" in status.read_text().lower(), status.read_text()
            if case == "deadline-during-publish":
                assert last == 0, ("restarted after deadline", last)
            fingerprints = {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in files + [marker]}
            for _ in range(3):
                finish()
            assert fingerprints == {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in files + [marker]}
            assert len(list(directory.glob("*.collapsed"))) == len(files)
            assert not list(directory.glob("*.part.*"))
            if case == "replaced-session":
                replacement = directory / "next.complete"
                command("start,event=Counted.hit,timeout=3s,loop=1s,collapsed,file=" + str(directory / "next-%n.collapsed") + ",completion=" + str(replacement))
                assert command("finish,completion=" + str(marker), False).returncode != 0
                command("status,file=" + str(status))
                assert "is running" in status.read_text()
                command("finish,completion=" + str(replacement))
                assert replacement.read_text() == "window-completion-v1\n0\n"
                assert fingerprints == {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in files + [marker]}
            assert jvm.poll() is None
            results.append(dict(case=case, counts=counts, expected=expected if case != "memory" else "positive allocation bytes", retries=3, jvm_alive=True))
            print(json.dumps(results[-1]), flush=True)
        finally:
            for proc in controllers:
                if proc.poll() is None:
                    proc.kill()
                    proc.wait()
            jvm.kill()
            jvm.wait()
            log.close()
            Path(args.output).write_text(json.dumps(dict(root=str(root), results=results), indent=2))


if __name__ == "__main__":
    main()
