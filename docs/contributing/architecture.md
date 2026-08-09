---
title: Architecture
description: The Bootwright execution pipeline, convergence classifier, and rendering contract.
---

# Architecture

This page is the contributor-facing deep dive behind the user-facing model in
[The desired-state model](../concepts/index.md). The normative contract is
[`specs/architecture.md`](https://github.com/crmarques/bootwright/blob/main/specs/architecture.md);
this page summarizes rather than restates it. It keeps the material a
contributor needs that the spec does not spell out — execution identities, the
normalize examples, the corrected diff semantics, the rendering and
ownership-record contracts as one teaching copy, and the rendered-input
inspection workflow — and links into the spec for everything else. The
extension checklists live on [API](api.md). When this page and the spec
disagree, the spec wins — please fix this page.

For the user-facing mental model first, read these once and do not expect them
repeated here:

- The kinds, ownership rule, and references —
  [The desired-state model → Object ownership](../concepts/index.md#object-ownership).
- The two stage families, sub-phases, and the `--clusters` single-namespace rule
  — [The desired-state model → Apply stages and families](../concepts/index.md#apply-stages-and-families).
- Platform render mode versus substrate type —
  [The desired-state model → Platform render mode and substrate type](../concepts/index.md#platform-render-mode-and-substrate-type).
- The four classification outcomes at a glance —
  [The desired-state model → Convergence and drift](../concepts/index.md#convergence-and-drift).

## The pipeline

Bootwright is a desired-state loader, validator, renderer, and idempotent apply
pipeline: `load and strict decode -> normalize defaults -> validate ownership and
references -> render effective state and tool inputs -> apply substrate, machine
OS, storage, cluster, and add-on phases`. The read-only verbs (see
[API → Decide whether the verb is read-only](api.md#decide-whether-the-verb-is-read-only))
are not pipeline stages: they observe the same model without mutating it.

The full layer breakdown — the durable run ledger and lease, per-cluster install
records, the agent-install dependency stages and post-install add-on wait, the
storage peer phase, the KubeVirt parent/child graph edges, the DAG orchestrator
and executor split, and the resource-lock keys — is specified in
[`specs/architecture.md` → Layers](https://github.com/crmarques/bootwright/blob/main/specs/architecture.md).
One contributor-facing caveat the spec does not stress: those internal dependency
stages (`machine-infra`, install-wait, add-on apply) are **not** the CLI
`--stage infra|clusters` families and their
`fabric`/`machines`/`deps`/`base`/`add-ons` sub-phases, which are owned by
[The desired-state model → Apply stages and families](../concepts/index.md#apply-stages-and-families).

### Normalize before render

The rule is stated in
[API → Where validation and normalization live](api.md#where-validation-and-normalization-live);
what it materializes: an omitted endpoint `source.type` becomes `openshift`;
the derived platform render mode; an `api-int` copy of `api`; the default cluster
and service networks; `distribution: openshift`; a defaulted `attachmentRef`. Run
`render effective` to see these materialized values. A diagnostic on any
normalize-injected reference the author never wrote — Environment-defaults
copies, the `openshift-pull-secret` convention, `<cluster-name>-cluster-admin-ssh-key`
— states that the value was defaulted and how to override it.

## Execution identities

Bootwright does not treat the process as simply root or non-root. It uses
distinct execution identities so that ownership and privilege stay explicit:

- User-authored files, external secret sources, `~` expansion, and caller `PATH`
  discovery run as the original local caller.
- Context state, generated secrets, runtime installer files, workflow logs,
  managed Ansible runtime, and local package or CLI installs run as the
  protected local root state identity under `/var/lib/bootwright`. Commands that
  need context data re-exec through `sudo` when necessary.
- Provider, `InfraComponent`, and infrastructure host work connects as the
  rendered SSH user, then uses Ansible `become` for privileged host mutation.
- Controller-local Ansible work uses localhost inventory and `become` only for
  the tasks that intentionally mutate controller state.

The desired-state ownership boundary holds throughout these identities; the full
ownership table is in
[The desired-state model → Object ownership](../concepts/index.md#object-ownership).

## Convergence and the four-outcome classifier

Each mutating workflow task writes a durable **convergence-safety record** — a
non-secret desired hash plus a Bootwright owner identity — under its resource
lock. The records classifier compares that recorded evidence against current
desired state and yields four outcomes — `missing`, `match`, `drift`, `foreign`
— specified in
[`specs/architecture.md`](https://github.com/crmarques/bootwright/blob/main/specs/architecture.md)
and summarized for operators in
[The desired-state model → Convergence and drift](../concepts/index.md#convergence-and-drift).

Two contributor-facing subtleties are easy to get backwards:

!!! warning "`diff --recorded` is the offline classifier; plain `diff` is live"
    The four-outcome classification is what **`diff --recorded`** reports —
    offline, with no cluster contact, against recorded evidence. The default
    `diff` instead compares desired state against **live reality**: read-only
    Ceph discovery on each managed seed plus a shallow
    reachability/`ClusterVersion` check for `ContainerCluster`s. `diff --adopt`
    folds that live observation back into authored desired-state YAML,
    snapshotting the prior input to history first. A verb that means to stay
    fully offline must pass `--recorded`.

!!! warning "Classification is NOT an apply-time skip gate"
    That classification is a report, **not** an apply-time skip gate. Most
    provider-service and
    infra-component config tasks have no reliable external probe, so they
    **re-run and rely on idempotent execution**, and their record is marked
    `unknown` (recorded but not classified) as durable evidence rather than a
    skip decision. Apply-time fail-closed gating lives **only at the
    concrete-probe sites**: cluster install records, add-on records, managed OS
    markers, provider metadata, and storage comparison results. Cluster install
    reconcile probes live availability, skips completed installs, resumes only
    from known-safe phases, allows an `iso-created` record to reach node boot
    only when its publish time proves the ISO is less than 24 hours old, and
    fails closed on missing or different inputs after node boot unless a
    command-scoped `--mode rebuild` is given.

## The three apply modes

`apply` selects how strictly Bootwright treats pre-existing state. The
user-facing overview is in
[The desired-state model → Convergence and drift](../concepts/index.md#convergence-and-drift);
the behavioral contract is:

- **`apply --mode reconcile` (default):** creates missing objects, skips objects
  whose recorded desired state matches current, converges drift that is
  reconcilable in place, and fails closed on structural
  (destructive-identity) drift or `foreign` ownership before any mutation.
- **`apply --mode create`:** additionally refuses to proceed when any selected
  object already exists — a greenfield assertion.
- **`apply --mode rebuild`:** command-scoped break-glass past Bootwright-owned
  unsafe-mismatch checks that have an explicit override path (bypass the
  skip-if-complete install check, reinstall a managed-OS machine, cleanly rebuild
  a Bootwright-owned managed Ceph cluster — a foreign or co-resident cluster
  fails closed). It must **not** bypass active-run leases, validation, secret
  checks, or foreign-resource ownership failures.

`--mode` is single-valued, so those three are one choice rather than a
combination. A rebuild that destroys data additionally needs
`--authorize data-loss`. Under
`Environment.spec.safety.destroyProtection: protected`,
`apply --mode rebuild` fails closed before any mutation: that destruction
must cross the destroy authorization boundary
(`destroy --authorize protected` for the scope, then re-apply). See
[Operations and Recovery](../advanced/operations.md) for the recovery patterns.

## The rendering contract

Rendering turns validated desired state into the concrete installer, provider,
and storage CLI inputs Bootwright drives. The per-target render sources —
`install-config.yaml`, `agent-config.yaml` hosts, machine and endpoint provider
variables, the single shared-service graph, storage tool inputs, managed machine
OS inputs, and extension apply plans — are specified in
[`specs/architecture.md` → Ownership Boundaries](https://github.com/crmarques/bootwright/blob/main/specs/architecture.md).
Two contracts any new render path must honour:

- **Rendering is a second enforcement line** behind validation for name
  resolution. Every render entry point fails before writing anything when an
  endpoint load-balancer bind or a managed Ceph topology host address does not
  resolve, instead of degrading to output with empty values.
- **ISO references resolve only through the single Bootwright managed media
  resolver.** Providers, OS installers, and future user-supplied ISO fields must
  not duplicate `local-media:`, `file://`, or HTTP(S) parsing; the
  `local-media:<filename.iso>` form resolves against the host-local media store
  at `/var/lib/bootwright/media/`.

### Inspecting rendered inputs

`bootwright render --output-dir <dir> --clusters <cluster> --sensitive` writes the
same concrete tool inputs Bootwright would hand to the supplier or community
CLIs. OpenShift installer files land under
`<dir>/openshift-install/<cluster>/{install,agent}-config.yaml`; Ansible
inventory and vars files land under `<dir>/ansible/`; storage files land under
`<dir>/storage/<storageCluster>/`. The effective-state snapshot and lock file sit
at the top level of `<dir>`. `--clusters` accepts both ContainerCluster and
StorageCluster names (like `apply`); add `--output json` for a machine-readable
manifest of every written path.

!!! warning "Rendered runtime inputs can carry secret bytes"
    Because runtime installer files inline secret material, `render --output-dir`
    requires `--sensitive` and fails without it. Rendered **effective state**
    never includes secret bytes. See [Secrets](../concepts/secrets.md).

## The ownership-record cross-boundary contract

Ownership evidence is a named cross-boundary contract between the Ansible
executor and the Go orchestrator: executing collection roles record per-host
resource and package ownership through `bootwright.core.ownership_record` at
mutation time, and Go reads those records for destroy scoping, host
package-removal gating, orphan reporting, and `diff` classification, while run,
install, and convergence-safety ledgers stay Go-written. The full contract —
including the partial-destroy `destroyStatus=partial` stamp — is in
[`specs/architecture.md`](https://github.com/crmarques/bootwright/blob/main/specs/architecture.md);
[Operations and Recovery](../advanced/operations.md) covers how destroy consumes
these records.

## The Ansible bundle

The Ansible source tree is authored under `ansible/`; `make sync-bundle` packs it
plus pinned external collections into the generated, unversioned embedded archive
at `internal/converge/bundle/ansible_bundle.zip`, and `make build` runs that sync
before compiling the CLI. A source checkout without the generated archive must
still compile and report an empty embedded bundle for commands that need Ansible
until the operator runs `make build`. See
[Building and testing](building-and-testing.md).

## Extending the providers

The rules a provider adapter follows — capability arms instead of name
sniffing, capability discovery and advertised metadata before any
supplier-specific branching, the official CLI of a tool Bootwright already
drives before custom orchestration — are in
[`specs/architecture.md` → Providers](https://github.com/crmarques/bootwright/blob/main/specs/architecture.md).

The step-by-step checklists for **adding a substrate adapter** and **adding a
managed service** live in one place, on [API](api.md#adding-a-substrate-adapter)
— keeping them beside "adding a CLI verb" stops the lists from drifting apart.
The full per-field schema is in
[The desired-state model](../concepts/index.md), and the API grammar a new field
must follow is
[`specs/adr/0014-api-grammar.md`](https://github.com/crmarques/bootwright/blob/main/specs/adr/0014-api-grammar.md).
