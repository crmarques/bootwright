# Scoped cluster runs: hash projection and ISO-task reuse

**Constraint (task hashes must not embed the scope filter):** A task that
hashes the full planning `State` breaks under `--clusters` scoping, because
that State is the scope-filtered set: hashing it makes an unscoped
`state-check` report drift after a scoped apply, and the next reconcile then
fails closed on that phantom drift. Tasks must hash a projection of only the
desired-state inputs they actually depend on. Example: the per-host virtctl
provision hashes `virtctlDesiredHashVars` — the KubeVirt host cluster identity
plus the optional virtctl mirror override — mirroring the fabric/storage
`DesiredHashVars` projection pattern (a `map[string]string` marshals with
sorted keys, so the hash input is order-stable). The task still carries the
full planning State for execution.

**When it bites:** Any new plan task whose convergence hash is derived from
`State` directly will reproduce the bug: scoped apply succeeds, unscoped
`state-check` flips to drift, next `apply` refuses.

**Semantics (base-only runs reuse the deps ISO):** In the container-cluster
plan chain (machine infra prepare/provision/finalize → agent ISO (deps) →
boot nodes → install wait (base)), boot/wait depend on the agent-ISO task
only when the deps stage is also in scope (`installPhasePlanned`). When only
`--stage base` is selected, the dependency is omitted so the run reuses the
ISO a prior deps run published instead of blocking forever on a task that was
never planned. A cluster with no machines to boot follows the same rule: the
wait orders behind the ISO task only when deps is in scope. This is the same
conditional-omit pattern the storage/add-ons extension activities use.

**Semantics (per-host virtctl provision):** virtctl runs on the controller
(the agent-node layer connects locally), so one version-matched provision per
distinct KubeVirt host cluster suffices; every child cluster's boot task waits
on its host's provision. A host cluster that is not itself a selected
ContainerCluster (a pre-existing, externally-managed host) is assumed ready —
its virtctl provision has no prerequisites.
