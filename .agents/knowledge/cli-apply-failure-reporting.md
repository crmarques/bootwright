# Apply failure attribution and log pointers

**Semantics: log-pointer policy.** Ansible output never reaches the
terminal, so each cluster gets a concise per-cluster bootwright log
pointer; the verbose OpenShift installer log path is added ONLY for a
container cluster that did not finish cleanly, so a green fleet is not
buried under installer paths. An install-wait task failure additionally
points straight at the OpenShift installer log because its root cause is
there, not in the ansible task log. The shared run log carries only
per-cluster lifecycle markers — never cluster ansible stdout/stderr — so
it stays a readable index of the run (asserted by `apply_tasks_test`).

**Semantics: blocked tasks blame the failed root, not a sibling.**
`applyBlockedReason` walks the dependency chain to the actually-FAILED
root task and, when that root lives in another cluster (the KubeVirt
host parent), names that cluster — a blocked child reads
`host cluster dc1-metal-ocp not ready (blocked by Install dc1-metal-ocp)`
instead of leaking a raw internal task ID or blaming a sibling task one
hop up; a transitively-blocked task must resolve to the same parent
(`cluster_display_test`). Underneath, `ApplyBlockingRoot`
(`internal/status`) walks the blocked task's dependency graph
breadth-first and returns the FIRST FAILED ancestor as the root cause,
falling back to the nearest blocked/cancelled ancestor only when no
failed one exists.

**Semantics: failure details are shortened with a middle ellipsis.**
`middleEllipsisDetail` (180-rune limit, fixed 44-rune tail) elides the
MIDDLE instead of tail-truncating, so a trailing actionable clause such
as "rerun with --override to rebuild it" survives next to the leading
description. The implementation is rune-based so a multibyte character
is never split. (`internal/converge/workflow/apply_failures.go` applies
the same middle-ellipsis rule to over-long apply failure reasons.)
