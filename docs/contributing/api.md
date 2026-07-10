---
title: API
description: Contributor guide to the Bootwright desired-state API and its extension points — where the kinds live, the strict-decode rule, and how to add a substrate adapter, a managed service, or a CLI verb.
---

# API

This page is for contributors changing Bootwright's code, not operators
authoring desired state. It maps the desired-state API onto the packages that
own it and walks the most common extension tasks — adding a substrate adapter,
adding a managed service, and adding a CLI verb — pointing at the normative
contracts each change must honour.

For the operator-facing view of the kinds and the substrates that already exist,
read the [desired-state model](../concepts/index.md). For the execution internals
behind these extension points, read [Architecture](architecture.md).

!!! note "Read the contracts first"
    Everything below summarizes binding contracts that live under `specs/`. The
    normative sources are
    [`specs/architecture.md`](https://github.com/crmarques/bootwright/blob/main/specs/architecture.md),
    [`specs/state-model.md`](https://github.com/crmarques/bootwright/blob/main/specs/state-model.md)
    (the CLI contract and API schema), and
    [`specs/adr/0002-ansible-provider-dispatch.md`](https://github.com/crmarques/bootwright/blob/main/specs/adr/0002-ansible-provider-dispatch.md).
    The Go registry in `internal/roles` is the single source of truth for role
    names. When this page and a spec disagree, the spec wins — please fix this
    page.

## Where the API lives

Bootwright is a desired-state loader, validator, renderer, and idempotent apply
pipeline. The flow is `load and strict decode -> normalize defaults -> validate
ownership and references -> render effective state and tool inputs -> apply`. A
new extension almost always touches one or more of these layers and subsystems:

| Layer | Package | What it owns |
| --- | --- | --- |
| Desired-state API | `api/v1alpha1` | The typed `bootwright.io/v1alpha1` kinds, their capability arms, and decode-time shape. |
| Validation and normalization | `internal/state` | Cross-field rules, ownership checks, and normalize-injected defaults (its `desired` package). |
| Cross-kind state | `internal/state` | Stateless views and cross-kind joins (`view`), the multi-kind graph and scoped-apply conflicts (`graph`), example scaffolding (`scaffold`), and non-blocking best-practice advisories (`advice`). |
| Rendering | the render packages | Turning validated state into installer, provider, and storage CLI inputs. |
| Entitlements | `internal/entitlements` | Resolving `Entitlement` declarations into subscription, registry-credential, and license references. |
| Secrets | `internal/secrets` | The context secret store: declared-secret resolution, AES-256-GCM encryption, and generated credentials, certificates, and SSH key pairs. |
| Storage domain | `internal/storage/{topology,cephprovider,cephstate,cephdiff,cephadopt}` | Ceph resolution and apply-result state: topology and placement (`topology`), package/image/registry source per distribution (`cephprovider`), live read-only observation (`cephstate`), desired-vs-live diff (`cephdiff`), and the `diff --adopt` fold-back (`cephadopt`). |
| Add-on subsystem | `internal/addons/{plan,render,oc,records,inputs,hooks,nativecatalog}` | Post-install add-on apply: binding/profile expansion and input effects (`plan`, `inputs`), plan and manifest rendering (`render`), the `oc` apply boundary (`oc`), hook helpers (`hooks`), non-secret apply records (`records`), and the built-in catalog plus machine-local store (`nativecatalog`). |
| Dispatch registry | `internal/roles` | Role names and `RoleContract`s for substrate, BMC, boot, service, and storage work. |
| Orchestration | `internal/converge/workflow` | The dependency graph, locking, run ledger, and apply/destroy phases. |
| Execution | `ansible/` (`bootwright.core` collection) | The roles that do per-host work, recording ownership as they mutate. |
| CLI | `internal/cli` | Thin command adapters that translate flags into workflow options. |

The typed kinds and their fields all live in `api/v1alpha1`. That package owns
the decode-time shape of every authored `bootwright.io/v1alpha1` object and the
capability arms that discriminate substrate and service behaviour.

### Strict decode and no backward compatibility

Desired state is decoded strictly: unknown fields are rejected before
normalization. There are **no aliases and no migrations** — `v1alpha1` may change
cleanly, and retired field names are *rejected, not translated*. A schema change
that renames or removes a field is a flag-day: stale desired state is expected to
fail strict validation rather than be silently rewritten. Keep it that way when
you change the API; do not add compatibility shims.

### Where validation and normalization live

Cross-field rules, ownership checks, and reference resolution live in the
desired-state validators. Defaults consumed by more than one stage are
materialized by the normalize phase — validators and renderers read the
normalized value rather than recomputing it. The desired-state ownership boundary
is enforced here: physical machine facts must not move into cluster intent, and
cluster release intent must not move into environment defaults.

## Adding a substrate adapter

A substrate is a `Machine` backend such as libvirt, bare metal, vSphere, or
KubeVirt. Unlike a managed service, a substrate is *not* its own kind — it is
dispatched by a Go registry, so adding one is a registry-plus-role operation
rather than a new API kind.

Provider adapters consume capability arms instead of inferring behaviour from
names. The existing arms are machine profiles for `libvirt`, `vsphere`, and
`kubevirt`, and explicit machines for `baremetal`. Adding a substrate means
adding a capability arm, validation, renderer support, and an apply adapter —
and it must never move physical facts into cluster intent.

1.  **Add the capability arm and validation** in `api/v1alpha1` and the
    desired-state validators. Use a machine-profile arm for virtual substrates,
    or explicit `Machine.spec.hardware` inventory for physical ones.
2.  **Register the dispatch triplet** (`substrateRole`, `bmcRole`, `bootRole`)
    and its `RoleContract` in `internal/roles`. Real backends use status
    `supported`; schema-only backends use `scaffold`; no-op arms resolve to the
    explicit `*_none` roles so dispatch stays visible rather than silently
    absent.
3.  **Add the converging roles** under the matching families in
    `ansible/collections/ansible_collections/bootwright/core/roles/`:
    `machine_substrate_*`, `provider_service_bmc_*`, `container_cluster_boot_*`,
    and the optional media hook. The renderer projects their exact names; roles
    must never branch on the dispatch discriminators themselves.
4.  **Map the installer platform render mode** where it applies. This is the
    installer *platform render mode*, not the substrate type — substrate
    ownership stays on the `Machine` and `InfraProvider`. See
    [platform render mode and substrate type](../concepts/index.md#platform-render-mode-and-substrate-type)
    for how the mode is derived.

!!! note "`scaffold` vs `supported`"
    The schema can accept provider facts for a substrate before an apply adapter
    exists. A `scaffold` status lets the type system describe a backend that is
    not yet apply-supported; promote it to `supported` only when the converging
    roles and renderer support land. Keep "schema-accepts" and "apply-supported"
    distinct in code, tests, and docs.

The normative contracts are in `specs/architecture.md` (Providers and Platform
Rendering) and `specs/adr/0002-ansible-provider-dispatch.md`. Prefer the official
CLI capabilities of the tools Bootwright already drives (for example,
`openshift-install agent wait-for install-complete`) over custom orchestration,
and prefer capability discovery and advertised metadata over supplier-specific
branching — isolate, minimize, test, and document any workaround that discovery
genuinely cannot express.

## Adding a managed service

If you are adding a machine-bound shared service rather than a substrate, it *is*
a typed kind. Keep the service path orthogonal to substrate dispatch: add a typed
`InfraComponent`/`Environment` arm, register its role, image, and defaults in
`internal/roles`, add its consumer discovery to the service graph, project the
resolved graph into Ansible vars, and place the converging role under
`ansible/collections/ansible_collections/bootwright/core/roles/infra_component_*`.

## Adding a CLI verb

CLI commands live in `internal/cli` and are wired in `cli.go`. Each command
should be a **thin adapter**: translate flags into options, then call into
`internal/converge/workflow`. Orchestration logic stays in the workflow package,
not in the command. Human-readable output goes through `internal/cli/output`.

### Decide whether the verb is read-only

Before shipping a verb, decide whether it mutates anything. The read-only verbs
are `status`, `diff`, `render`, `plan`, `apply --dry-run`, `validate`,
help, and discovery. A read-only verb must not:

- write runtime records (convergence-safety, install, ownership, or ledger),
- acquire a mutating run lease, or
- mutate provider, BMC, cluster, or storage state.

Most read-only verbs must not contact hosts at all. Two deliberate carve-outs:

- `render` *does* write generated outputs — rendered tool inputs and
  `effective-state.yaml` — into context state. Those are outputs, not runtime
  records, and `render` still never contacts hosts.
- `preflight` (and `apply` before its host check) may record SSH server-key
  trust on first use, under interactive confirmation only.

!!! warning "Honour the existing flag vocabulary"
    Reuse the established narrowing flags rather than inventing parallel ones.
    `--stage` accepts the two families `infra` and `clusters` and, on `apply`,
    `plan`, and `state-check`, their five ordered sub-phases (`fabric`,
    `machines`, `deps`, `base`, `add-ons`); `destroy --stage` takes only the two
    families. `--clusters` takes a comma-separated list of `ContainerCluster`
    *and* `StorageCluster` names from one shared namespace, so each bare name must
    resolve to exactly one cluster root. A new verb that narrows scope should
    accept the same flags with the same meaning.

The binding rules for the read-only contract — and for every flag a verb may
accept — are in `specs/state-model.md` (CLI Contract). New verbs are expected to
keep that contract green in the CLI-contract tests.

## What rendering must guarantee

If your extension produces tool inputs, the renderer is a *second enforcement
line*, not a best-effort formatter. Every render entry point must fail before
writing anything when an endpoint load-balancer bind or a managed Ceph topology
host address does not resolve, rather than degrading to output with empty values.

Two more rules apply to any new render path:

- **Normalize before render.** A default consumed by more than one stage is
  materialized by the normalize phase; validators and renderers read the
  normalized value rather than recomputing it. Emit a diagnostic on any
  normalize-injected reference the author never wrote, stating that it was
  defaulted and how to override it.
- **Reuse the shared resolvers.** ISO references are resolved by the Bootwright
  managed media resolver; providers, OS installers, and future user-supplied ISO
  fields must not duplicate `local-media:`, `file://`, or HTTP(S) parsing. Shared
  parsing and resolution live behind one reusable package or adapter before any
  provider-specific role consumes it.

## The ownership-record contract

Anything your roles create or configure on a host must be recorded so that
`destroy`, host package-removal gating, orphan reporting, and `diff` can
reason about it. Ownership evidence is a named cross-boundary contract: executing
collection roles record per-host resource and package ownership through
`bootwright.core.ownership_record` at mutation time, and Go reads those records.
Run, install, and convergence-safety ledgers stay Go-written — do not write those
from a role.

!!! warning "No secret bytes, ever"
    Bootwright desired state and rendered output are safe to commit because they
    reference secrets by name, never by value. A new arm, renderer, or role must
    not inline secret bytes into desired state, rendered installer files,
    inventories, effective-state snapshots, or ownership records. See
    [Secrets](../concepts/secrets.md) for the full model.

## Further reading

- [Architecture](architecture.md) — the execution pipeline, locking, and the
  four-outcome classifier in depth.
- [The desired-state model](../concepts/index.md) — the operator-facing view of
  the substrate arms and kinds you are extending.
- [`specs/architecture.md`](https://github.com/crmarques/bootwright/blob/main/specs/architecture.md),
  [`specs/state-model.md`](https://github.com/crmarques/bootwright/blob/main/specs/state-model.md),
  and
  [`specs/adr/0002-ansible-provider-dispatch.md`](https://github.com/crmarques/bootwright/blob/main/specs/adr/0002-ansible-provider-dispatch.md)
  — the binding contracts.
