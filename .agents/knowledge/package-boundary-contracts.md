# Package boundary contracts: converge, workflow, state, cli

**converge is the mutating-run facade:** `internal/converge` is the
orchestration bounded-context root for apply/destroy and the ONLY package
that constructs `workflow.RunOptions` (guard-tested), so every mutating run
enters the engine through it. Presentation (printing, prompts, confirmations)
stays in `internal/cli`; cluster selection is a CLI concern resolved by
`internal/clusteraccess` — converge receives pre-scoped State as plain input,
never a filter function. `runOptionsForContext` is the single shared seeder
of context-derived RunOptions fields; `TestRunOptionsForContextSeedsSharedFields`
asserts it leaves every run-specific field zero so a field cannot silently
migrate into or out of the shared mapping and change one of the five run
paths (apply, destroy, dry-run, preflight, discovery).

**workflow is a pure pipeline API:** no package in
`internal/converge/workflow` imports `internal/cli`; CLI handlers are thin
flag-to-Options adapters. Human output is reported as semantic events (no
fmt.Print or log); ansible exec goes through the `ansible.Runner` interface
so tests can fake it; Options structs are flat and callers pre-resolve every
default/path so functions are deterministic from inputs alone — workflow
never consults the environment. Apply-result persistence owned here: run
ledger, per-cluster install state, converge-safety records. Add-on apply
state lives in `internal/addons/records`; managed-Ceph/DF results in
`internal/storage`.

**state layering:** `internal/state/view` provides stateless pure lookups
and cross-kind joins over a `v1alpha1.State` and never filters or scopes it;
selection and the machine-service graph live in `internal/state/graph`, built
on view. Storage-cluster lookups by name belong in view as generic
cross-kind joins, while `internal/storage/topology` stays focused on
placement, failure domains, and node-to-machine resolution.
`internal/state/advice` produces non-blocking advisories only and never
affects load->normalize->validate->render->apply.

**inventory vs installer render families:** the inventory -> installer
package edge exists for composing installer output, not for state lookups —
e.g. `clusterInstallForOCP` keeps its ClusterInstall state-graph lookup
inside the inventory package over stateview directly.

**Test-only compat aliases:** `internal/cli/converge_compat_test.go` holds
aliases (scopeDryRunReport, clustersScope, playbook constants) over symbols
that moved to `internal/converge`, kept only so the large cli_test.go avoided
a mass rewrite. Production code uses the converge package directly; do not
extend these aliases in new code.
