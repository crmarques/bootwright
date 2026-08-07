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

## A readiness timeout blames a condition the operator stopped updating

**Symptom:** the timeout names a cause that contradicts the cluster. On prd
2026-08-07 it said `CephCluster resource is not reporting status` while that
CephCluster read `phase: Connected`, `HEALTH_OK`, with a `lastChecked` seconds
old.

**Root cause:** a status condition is not evidence of the present unless
something is still writing it. ocs-operator sampled the CephCluster one second
before rook finished connecting, wrote `Available=False`, and then never
re-evaluated it — for nine hours, while `ReconcileComplete` and `Progressing` on
the same object kept heartbeating. `formatConditions` reported the first
unsatisfied condition in list order, so the frozen one won and the live
`Progressing=True reason=NoobaaInitializing` was filtered out entirely by
`conditionNoteworthy`, which treated any `status: "True"` as uninteresting.

**Fix**, all in `internal/addons/oc/progress.go`:

- `conditionFindings` parses `lastHeartbeatTime`, takes the newest across the
  object's conditions as the clock, and marks any condition lagging it by more
  than `staleConditionLag` (5 minutes) as stale. Staleness is only ever computed
  **within one object** — comparing against wall-clock would misread an operator
  that heartbeats slowly by design.
- `bestFinding` ranks fresh-problem, fresh-fallback, stale-problem,
  stale-fallback, so a stale condition still gets reported when nothing fresher
  exists but never outranks a live one. Stale conditions are always printed,
  annotated with how far behind they are.
- `transitionVocabulary` makes a `Progressing`-shaped condition noteworthy at
  `True` rather than `False`. Only `progress` is in that vocabulary:
  `ExternalClusterConnecting=False` means "not connected" and must keep its
  ordinary positive polarity.
- `diagnoseRelatedObjects` follows `status.relatedObjects` (bounded at
  `relatedObjectLimit`, no recursion) and reports each one's phase and
  conditions, so the object a condition blames is read rather than quoted.

Pinned by `TestFormatConditionsDemotesAConditionWhoseHeartbeatFroze`,
`TestFormatConditionsKeepsAStaleConditionWhenNothingIsFresher`, and
`TestDiagnoseObjectReadsTheObjectItsConditionBlames`.

The readiness **gate** is deliberately unchanged: it still requires the
condition the add-on declares. Passing on a related object instead would have
called Data Foundation ready while NooBaa had no object storage at all — the
exact silent failure
[data-foundation-external-attach.md](data-foundation-external-attach.md)
exists to refuse.
