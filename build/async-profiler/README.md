# Java window completion

Flashcat already ships async-profiler 4.5. Its independent loop and timeout
clocks enforce the JVM deadline. This patch adds an idempotent final collection
protocol; it does not downgrade the agent or change the profiling budget.

`build/install-tools.sh` overlays the patched native CLI/library on the verified
4.5 distribution. `build.sh` verifies the source archive SHA-256 and builds in
upstream's pinned release container, keeping its glibc compatibility baseline
and static musl CLI. Docker, curl, patch and a native Linux builder are required.
Both architectures have pinned builder images. The build provenance is installed
as `window-completion-build.txt`. Production builds exclude the test barrier.

The JVM publishes each immutable `.collapsed` window using a temporary file and
rename. Natural timeout and `finish,completion=<session path>` take the same
state lock. The terminal marker contains the protocol and last sequence. Retry
of the same finish does not dump a window twice; another session cannot finish
this one. A delayed timer cannot terminate a replacement session.

Huatuo checks the **loaded JVM agent** before starting. A JVM that still has an
unpatched agent must be restarted after installing the bundle. Replacing the
library on disk alone does not replace a loaded library.

Java opts into acknowledged queue insertion. A full or stopped queue is an
error, and does not advance the delivered sequence. Other samplers retain the
existing nonblocking enqueue behavior. Completion requires every sequence,
including an empty terminal window. Atomic publication replaces file-size
stability polling.

## Recovery boundary

Session files remain at `/tmp/huatuo-asprof-<session>-<type>-<pid>-*.collapsed`
(and `.complete`) in the target JVM mount namespace, even after queue acceptance.
A killed controller therefore leaves a complete bounded session available for
recovery after the JVM deadline. These files contain profiling stacks: restrict
access and remove them after confirming export or after recovering the session.
There is no automatic cross-process replay in this change. Do not replay files
into an already exported profile without checking its session identity.

Queue acceptance is not durable storage acknowledgement. This change guarantees
one delivery per sequence within the running collector and preserves files for
manual recovery; it does not claim exactly-once datastore writes across SIGKILL,
power loss, disk failure, or a killed JVM. Intermediate publication failure is
reported as an incomplete session rather than silently claiming full coverage.
After storage recovers, explicit finish can publish retained samples while still
returning the recording error; a new session can then start. Failed initial
starts do not reserve a session. Publication cannot restart a window after the
overall deadline.

Async-profiler loop mode stops and restarts its sampling engine between windows.
Calls that execute during that existing gap are not samples collected by the
agent. This patch preserves loop mode; it does not promise gap-free sampling.
The regression releases 170 and 85 method calls in two windows after checking
that the engine is running, so acquisition gaps cannot be confused with lost or
duplicated window delivery. A continuous-call comparison against unmodified
4.5 confirms that both versions can miss calls during window transitions.

## Regression

`integration/testdata/java-timeout/protocol.py` runs counted Java method events,
allocation weights, timeout/stop races, repeated finish, wrong-session rejection,
marker-write retry, and controller SIGKILL. Build the test agent with
`CPPFLAGS=-DWINDOW_PROTOCOL_TESTING` to enable the deterministic in-JVM finish
barrier; never distribute that build. Run the harness for Java 8 and Java 17.
Go runtime/queue/registry tests cover sequence gaps, unpublished files, enqueue
rejection/retry, and enqueue racing shutdown.

Build the Huatuo integration driver after `make gen-build`:

```sh
go build -o /tmp/java-runtime-driver ./integration/testdata/java-timeout/driver
sudo python3 integration/testdata/java-timeout/runtime.py \
  --driver /tmp/java-runtime-driver --java-home "$JAVA_HOME" \
  --profiler /path/to/test-agent/build --output /tmp/runtime-results.json
```

The runtime harness covers natural completion, explicit stop, SIGKILL during
sampling, SIGKILL after the JVM owns final collection, and three repeated
finalizations. The final-collection SIGKILL case requires the test-only barrier.
For production binaries pass `--cases natural,stop,kill`. Use `--old-profiler`
to additionally prove that an unpatched library already loaded in a JVM is
rejected even after replacing its file on disk.
