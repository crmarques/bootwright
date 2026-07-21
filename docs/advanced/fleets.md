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

A fleet typically also uses directory selection so the input tree stays
navigable. `Environment.spec.resources[]` selects the files or directories to
load; the fleet example loads two directories:

```yaml
spec:
  resources:
    - infra
    - clusters
```

That maps onto the canonical nested layout: shared, non-cluster objects under
`infra/` (providers, network configs, shared-service `InfraComponent`s, and any
shared machines such as a bastion) and one subtree per cluster under
`clusters/container/<name>/` or `clusters/storage/<name>/`. Co-locate each
cluster's nodes with that cluster; reserve `infra/` for objects genuinely shared
across clusters.

!!! note "One object per file"
    Each input YAML file holds exactly one object, and the filename matches
    `metadata.name`. A fleet is many small files organized by directory, not a
    few multi-document files. This is the authoring layout `specs/state-model.md`
    defines; every example and `bootwright example init` follow it.

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
single artifact server, load balancer, NTP source, DNS resolver, proxy, or mirror
registry serving every cluster that needs it. In the fleet example one managed
artifact server is declared once and consumed by both clusters:

```yaml
spec:
  infraComponents:
    artifactServers:
      - name: default
        management: managed
        componentRef: artifact-server
```

The fleet may declare the artifact server **once** as an environment default,
while each cluster still declares the endpoint it consumes:

```yaml
spec:
  defaults:
    artifactServerRef: default
```

```yaml
spec:
  install:
    agent:
      redfishVirtualMedia:
        artifactServerEndpoint:
          endpointRef: bmc
```

The same pattern applies to the other shared services — a `loadBalancer`,
`nameResolution`, `ntp`, `proxy`, or `registry` component is declared under
`spec.infraComponents.*[]` and referenced by `componentRef`. A cluster that needs
a different artifact server sets `artifactServerEndpoint.serverRef`; every
consumer-owned endpoint keeps its own `endpointRef`. See
[Infrastructure](../concepts/infrastructure.md) for the component and endpoint
field reference.

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

For `destroy`, a bare `--clusters` (no `--stage`) narrows to
`destroy --stage clusters` for those roots — it tears down cluster-stage runtime
and leaves provider infrastructure standing.

### Narrow to machines

`--machines <names>` restricts the run to individual `Machine`s instead of whole
clusters (the two flags are mutually exclusive). It runs only the `fabric` and
`machines` phases, so it prepares a node up to the point a cluster install takes
over — provisioning one replaced node, or a shared bastion, without touching the
rest:

```text
bootwright apply --machines dc1-master-1 --yes
bootwright destroy --machines dc1-master-1 --force
```

A machine `destroy` tears down only that machine's substrate and never removes
shared per-cluster networking or services the survivors still use; destroying a
node of an installed cluster requires `--force`.

### Narrow to a stage family

`apply`, `plan`, and `destroy` share two stage families, `infra` and `clusters`.
Combine a stage with `--clusters` for surgical reruns:

```text
bootwright apply --stage infra --clusters dc1-ocp --yes
bootwright apply --stage clusters --clusters dc1-ocp --yes
```

`apply` and `plan` additionally accept the single-phase sub-phases
(`fabric`, `machines`, `deps`, `base`, `add-ons`) for even tighter reruns;
`destroy` accepts only the two families. The full stage model — families versus
sub-phases — is on [Concepts → Apply stages](../concepts/index.md).

### Whole-input validation

A scoped command still loads and validates the full state, so a desired-state
error anywhere in the fleet blocks even a narrowed run. Fix the offending object
(the error names it) before retrying the scoped command.

!!! warning "A scoped apply cannot silently narrow a shared service"
    The shared machine-service graph — built from every `InfraComponent`, the
    environment catalog selections, `NetworkConfig` DNS refs, and cluster
    endpoint sources — is resolved **once**, before validation, rendering, or
    scoped checks make any decision. A partial
    `apply --stage infra --clusters …` therefore cannot quietly reconfigure a
    shared service (a load balancer, DNS resolver, or artifact server) in a way
    that would break a cluster left outside the scope. If a narrowed apply would
    change a service another selected cluster still depends on, it is caught
    against the full service graph rather than degrading that service silently.

## See also

- [Operations & recovery](operations.md) — destroy stages, `--force`, and
  recovery patterns for a fleet.
- [Reference examples](examples.md) — `baremetal-redfish-fleet` and the larger
  multi-DC platform tree.
- [Infrastructure](../concepts/infrastructure.md) — `InfraComponent`, endpoints,
  and artifact access fields.
- [Concepts and APIs](../concepts/index.md) — selection lists, stages, and the
  single cluster namespace.
