---
title: Multi-cluster fleets & shared services
description: One Environment selecting many clusters, the single selection namespace, shared InfraComponents, and scoped apply/destroy.
---

# Multi-cluster fleets & shared services

A fleet is one `Environment` that selects and converges more than one cluster —
several `ContainerCluster`s, several `StorageCluster`s, or a mix — over a shared
substrate and a shared set of services. The object model is unchanged from a
single-cluster apply; what is new is how you *select* the active clusters, how
clusters *share* infrastructure, and how you *narrow* an apply or destroy to part
of the fleet.

The committed reference is `examples/baremetal-redfish-fleet`: two OpenShift
clusters in two data-center networks over one bare-metal provider, with one
shared artifact server. For the object model behind everything here, see
[Concepts and APIs](../concepts/index.md); the field tables live on
[Infrastructure](../concepts/infrastructure.md) and the per-kind concept pages.

## One Environment, many clusters

The `Environment` selects which loaded clusters are active through two selection
lists, `spec.containerClusters[]` and `spec.storageClusters[]`. When set, only
the listed clusters participate in validation, render, apply, status, and
destroy. When both are omitted, every loaded cluster is active. These are
**selection lists, not references** — they decide scope, they do not wire one
object to another.

Every container cluster in one Environment renders the same install-config
`baseDomain` (`Environment.spec.domains.containerClusters`). A fleet spanning
two base domains needs one context per domain today, and shared
InfraComponents must then be duplicated per context.

A fleet typically also uses directory selection so the input tree stays
navigable. `Environment.spec.resources[]` selects the files or directories to
load; the fleet example loads two directories:

```yaml
spec:
  resources:
    - infra
    - clusters
```

`spec.resources[]` is for *narrowing* a shared tree; a tree whose whole content
is intended should omit it. An authored file the list excludes is not loaded,
and `bootwright validate` reports it. The generated native add-on descriptor at
`add-ons/_store/<name>/add-on.yaml` is the narrow exception: a matching
`.bootwright-addon` marker proves it is a context dependency snapshot, so it is
loaded without making operators add generated paths to this authored list.

That maps onto the canonical nested layout: shared, non-cluster objects under
`infra/` (providers, network configs, shared-service `InfraComponent`s, and any
shared machines such as a bastion) and one subtree per cluster under
`clusters/container/<name>/` or `clusters/storage/<name>/`. Co-locate each
cluster's nodes with that cluster; reserve `infra/` for objects genuinely shared
across clusters.

!!! note "One object per file"
    One object per file holds everywhere — a fleet is many small files organized
    by directory, not a few multi-document files. The one exception is that
    similar `Secret` objects are grouped into a single multi-document
    `secrets.yaml`. Filenames are role-based where the role is unambiguous
    (`cluster.yaml`, `provider.yaml`, `networks.yaml`, `cluster-machines.yaml`)
    and named after `metadata.name` otherwise. `bootwright example init` emits
    this nested tree; the small single-cluster examples are deliberately flat —
    see [Reference examples](examples.md).

## Context is the blast radius

A context is the unit the dangerous verbs act on. It is the default scope of
`destroy` — with no `--clusters` selector a teardown covers the whole context,
including the context-wide VM and orphan-ownership sweep. It is the unit
`Environment.spec.safety` protects (`destroyProtection` and `protectedKinds`
guard the context's state, not individual clusters). And it is the unit
`context delete` abandons.

Design fleets around that: put clusters that should share one destroy blast
radius and one safety policy in one context, and split production from scratch
into **separate contexts** rather than relying on remembering `--clusters` on
every teardown. A degrading service (load balancer, DNS, or NTP) cannot be
reconfigured from a second context while the first still owns or references its
exact provider/name/host identity; repair or detach the sibling evidence and
re-run the same apply. There is no apply authorization to adopt it. Destroy also
refuses sibling or unreadable evidence unless the exact retry explicitly adds
`--authorize shared-infra` — see the
[scoped-destroy shared-service warning](operations.md#bounded-ownership-gated-cleanup).

## The single cluster-selection namespace

`ContainerCluster` and `StorageCluster` names share **one** cluster-selection
namespace. A bare cluster name passed to `--clusters` must therefore be unique
across both kinds, and it resolves to exactly one cluster root. Name your
clusters so they never collide — for example `dc1-ocp` (a `ContainerCluster`) and
`dc1-ceph` (a `StorageCluster`), never two roots both called `dc1`.

Because the namespace is shared, every narrowing command — `apply`, `plan`,
`destroy`, `status`, `render` — takes the same comma-separated `--clusters` list
and resolves each name the same way. See
[Concepts → Apply stages](../concepts/index.md) for the full selection and stage
model.

## Sharing InfraComponents across clusters

Shared services are `InfraComponent` objects placed on machines and selected from
the `Environment`'s service catalog. One component can back the whole fleet: a
single artifact server, NTP source, DNS resolver, proxy, or mirror registry
serving every cluster that needs it. In the fleet example one managed artifact
server is declared once and consumed by both clusters:

```yaml
spec:
  infraComponents:
    artifactServers:
      - name: default
        management: managed
        componentRef: artifact-server
```

The fleet may mark one artifact server the **default**, while each cluster still
declares the endpoint it consumes (a single-server fleet is the default without
the flag):

```yaml
spec:
  infraComponents:
    artifactServers:
      - name: default
        default: true
        management: managed
        componentRef: artifact-server
```

```yaml
spec:
  install:
    agent:
      redfishVirtualMedia:
        artifactServerEndpoint:
          endpointRef: bmc
```

The same pattern applies to the other shared services — a `nameResolution`,
`ntp`, `proxy`, or `registry` component is declared under
`spec.infraComponents.*[]` and referenced by `componentRef`. A cluster that needs
a different artifact server sets `artifactServerEndpoint.serverRef`; every
consumer-owned endpoint keeps its own `endpointRef`. See
[Infrastructure](../concepts/infrastructure.md) for the component and endpoint
field reference.

Load balancers are the exception: there is **no** `loadBalancer`
`spec.infraComponents` catalog slot. A shared load balancer is declared as a
standalone `InfraComponent` and shared by referencing it from a cluster's
`install.endpoints[].source.componentRef`, not through the environment service
catalog — see [Infrastructure](../concepts/infrastructure.md).

!!! note "Validate shared bindings early"
    A shared default's `serverRef` and `endpointRef` are validated where they are
    declared — against the declared `artifactServers[].name` and the selected
    server's endpoints — even while no cluster consumes them yet. A typo in a
    fleet default fails immediately rather than when the first consuming cluster
    is applied.

## Scoped apply and destroy

A fleet is normally converged whole — `bootwright apply --yes` runs the full
graph across every selected cluster, with independent work running concurrently
where locks allow. Narrowing is for focused build-out, recovery, or maintenance
**after** the full graph has been reviewed, not the everyday path.

### Narrow to clusters

`--clusters <names>` restricts the run to the named cluster roots:

```text
bootwright apply --clusters dc1-ocp --yes
bootwright destroy --clusters dc1-ocp
```

For `destroy`, a bare `--clusters` (no `--stage`) tears down the full lifecycle
of those roots. Positively owned virtual machines are deleted; bare-metal
hardware and its installed OS are retained. Add `--stage clusters` when the
intent is to remove only cluster-stage runtime and leave virtual machines
standing.

### Narrow to machines

`--machines <names>` restricts the run to individual `Machine`s instead of whole
clusters — one replaced node, or a shared bastion, without touching the rest.
See [Concepts → Selecting machines](../concepts/index.md#selecting-machines) for
the rule:

```text
bootwright apply --machines dc1-master-1 --yes
bootwright destroy --machines dc1-master-1 --authorize installed-cluster-node
```

Two consequences matter in a fleet. The narrowing covers operator-supplied
[custom playbooks](../concepts/custom-playbooks.md): a `CustomPlaybook` anchored
at `fabric` or `machines` runs against the selected machines' hosts only, and is
skipped when its target resolves to none of them — so a run for one node never
reaches a sibling that happens to be down. And a machine `destroy` tears down
only that machine's substrate, never the shared per-cluster networking or
services the survivors still use.

### Narrow to a stage family

`apply`, `plan`, and `destroy` share two stage families, `infra` and `clusters`.
Combine a stage with `--clusters` for surgical reruns:

```text
bootwright apply --stage infra --clusters dc1-ocp --yes
bootwright apply --stage clusters --clusters dc1-ocp --yes
```

`apply`, `plan`, and `diff` additionally accept the single-phase sub-phases and
combine `--stage` with `--through` to select an inclusive **range** — useful for
converging a fleet up to a checkpoint, or replaying a contiguous mid-graph
slice. The full stage model — families, sub-phases, and range semantics — is on
[Concepts → Apply stages](../concepts/index.md#apply-stages-and-families); a
scoped command still validates the *whole* input, so an error in an unselected
cluster blocks the run (see
[Whole-input validation](operations.md#whole-input-validation)).

```text
bootwright apply --through machines --clusters dc1-ocp --yes
bootwright apply --stage deps --through base --clusters dc1-ocp --yes
```

!!! warning "A scoped apply cannot silently narrow a shared service"
    The shared machine-service graph — built from every `InfraComponent`, the
    environment catalog selections, `NetworkConfig` DNS refs, and cluster
    endpoint sources — is resolved **once**, before validation, rendering, or
    scoped checks make any decision. A partial
    `apply --stage infra --clusters …` therefore cannot quietly reconfigure a
    shared service (a load balancer, DNS resolver, or NTP source) in a way
    that would break a cluster left outside the scope. The run **fails before
    any mutation**, naming the service and its unscoped consumers; re-run
    without `--clusters`, or extend `--clusters` to include the unscoped
    clusters. Self-contained services — artifact server, proxy, mirror registry,
    BMC — re-provision identically under any scope and are not blocked.

    The same rule spans contexts. Real shared-service applies and destroys hold
    a controller-global lease around the sibling-evidence check and remote work,
    while the host keeps a context claim through partial failures. If the
    refusal names another context or unreadable evidence, reconcile/destroy from
    that owner or repair the evidence, then copy the exact retry command shown.
    Never remove the global lease or host claim until you have proved the named
    run is inactive and the live service is safe.

## See also

- [Operations & recovery](operations.md) — destroy stages, `--authorize` tokens, and
  recovery patterns for a fleet.
- [Reference examples](examples.md) — `baremetal-redfish-fleet` and the larger
  multi-DC platform tree.
- [Infrastructure](../concepts/infrastructure.md) — `InfraComponent`, endpoints,
  and artifact access fields.
- [Concepts and APIs](../concepts/index.md) — selection lists, stages, and the
  single cluster namespace.
