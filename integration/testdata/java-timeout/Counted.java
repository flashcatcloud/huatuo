// Copyright 2026 The HuaTuo Authors
// SPDX-License-Identifier: Apache-2.0
import java.nio.file.*;

public class Counted {
    static volatile Object sink;
    static void hit() { sink = new byte[4096]; }
    public static void main(String[] args) throws Exception {
        Path dir = Paths.get(args[0]);
        while (!Files.exists(dir.resolve("go"))) Thread.sleep(10);
        // Do not confuse loop stop/restart sampling gaps with lost output.
        // The harness releases the second burst after a published window and
        // a status command confirms that the next window is running.
        for (int i = 0; i < 170; i++) hit();
        Files.write(dir.resolve("first.done"), new byte[0]);
        while (!Files.exists(dir.resolve("go-second"))) Thread.sleep(10);
        for (int i = 0; i < 85; i++) hit();
        Files.write(dir.resolve("workload.done"), "255".getBytes("UTF-8"));
        while (true) Thread.sleep(100);
    }
}
