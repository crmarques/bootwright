# oc CommandRunner: stderr corrupts JSON; nil-writer tee panic

Two related gotchas in the add-on engine's `oc` subprocess runner
(`internal/addons/oc/oc.go`).

## stderr warnings corrupt readiness-check JSON

**Symptom:** An add-on readiness check (`Ready`, `csvSucceeded`,
`getNamedResource`) fails to unmarshal the resource JSON even though the same
`oc get -o json` succeeds by hand. The captured output starts or ends with
deprecation, TLS, or auth warning lines.

**Root cause:** `oc` routinely writes deprecation/TLS/auth warnings to stderr
even on a successful `oc get -o json`. If `CommandRunner.Run` returned the
combined stdout+stderr buffer on success, the warnings would corrupt the JSON
the readiness checks unmarshal.

**Fix:** The success path returns stdout only; stderr is still captured to the
oc log file and included in the failure-path error. Pinned by
`TestCommandRunnerReturnsStdoutWithoutStderr` and
`TestReadinessChecksDecodeDespiteStderrWarnings`.

## io.MultiWriter panics on the quiet runner's nil writer

**Symptom:** Panic during an add-on readiness poll or idempotency pre-check
when the `oc` subprocess writes to both stdout and stderr.

**Root cause:** The quiet read runner leaves `Stdout`/`Stderr` nil to suppress
live console output, and the old `io.MultiWriter(&buf, nil)` construction
panics when handed a nil writer.

**Fix:** `CommandRunner`'s tee helper captures into the buffer alone when the
caller writer is nil, while still preserving the log file and error
diagnostics. Pinned by `TestCommandRunnerQuietWriterWithOutputDoesNotPanic`
(stdout-only return, both streams in the log).
