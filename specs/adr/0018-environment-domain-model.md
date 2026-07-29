# ADR 0018: Environment Domain Model

## Status

Accepted

## Context

ADR 0017 gave every `Machine` an `fqdn` address and every cluster node an
independent FQDN, both composed from a single fleet domain: the
`Environment.spec.baseDomain` string. One domain for the whole fleet cannot
express a common real-world split: the physical machines live in a corporate
zone (`srv4009.corp.example.net`) while the clusters Bootwright builds live in
a separate cloud zone, and container clusters and storage clusters may sit in
different subzones of that cloud zone. Forcing all of these under one
`baseDomain` means either the machine names or the cluster names end up in the
wrong zone.

`Environment` is the single owner of fleet DNS facts, so the split belongs
there, expressed per identity class rather than as one flat domain.

## Decision

`Environment.spec.baseDomain` (a single string) is replaced by
`Environment.spec.domains`, an object with one key per identity class:

```yaml
spec:
  domains:
    base: example.net              # required; the default for the others
    machines: corp.example.net     # machine fqdn zone
    clusters: cloud.example.net    # cluster zone umbrella
    containerClusters: ocp.cloud.example.net
    storageClusters: ceph.cloud.example.net
```

### Fields and defaulting

- `base` is the only required domain. It is the default every other key falls
  back to.
- `machines` defaults to `base`.
- `clusters` defaults to `base`.
- `containerClusters` defaults to `clusters`.
- `storageClusters` defaults to `clusters`.

So an `Environment` that sets only `base` behaves exactly like today's single
`baseDomain`: every identity resolves under one domain.

`spec.domains` is distinct from the existing `spec.containerClusters[]` and
`spec.storageClusters[]` selection lists (which are membership, not DNS): the
domain keys live under `spec.domains`, the selection lists stay at the top of
`spec`.

### Machine FQDN

A machine's DNS name is `Machine.spec.addresses[].fqdn`. When not authored, it
defaults to `<Machine.metadata.name>.<domains.machines>`. The `fqdn` mechanics
(canonical connection address, uniqueness, foreign-zone override, the two
IP-connection carve-outs) are unchanged from ADR 0017 — only the default
domain source moves from `baseDomain` to `domains.machines`.

### Cluster zones and node FQDNs

- A container cluster's zone is `<ContainerCluster.metadata.name>.<domains.containerClusters>`.
  A node's `name` composes to
  `<name>.<ContainerCluster.metadata.name>.<domains.containerClusters>`,
  and the cluster's `install-config.yaml` `baseDomain` is
  `domains.containerClusters` (OpenShift then forms `api.<cluster>.<domain>`).
- A storage cluster's zone is `<StorageCluster.metadata.name>.<domains.storageClusters>`.
  A node's `name` composes to
  `<name>.<StorageCluster.metadata.name>.<domains.storageClusters>`.

An explicit `nodes[].fqdn` is used verbatim, unaffected by the zone (ADR 0025).

This refines ADR 0017: the node-identity and `fqdn` model stand; the domain
each composition draws from generalizes from one `baseDomain` to the matching
`domains` key.

## Consequences

- `spec.baseDomain` is removed; `domains.base` is its successor, so a
  single-domain fleet is `domains: {base: …}`. Existing inputs migrate by
  wrapping the value: `baseDomain: x` → `domains: {base: x}`.
- Split-horizon fleets become expressible: corporate-zone machines resolved by
  the managed resolver, cloud-zone clusters, and separate container/storage
  subzones, without any node or machine name landing in the wrong zone.
- Every domain consumer that reads `baseDomain` today (machine `fqdn`
  injection, node-FQDN composition, install-config `baseDomain`, dnsmasq
  cluster records, `no_proxy` fan-out, install-state, cluster-access output)
  must select the class-specific domain instead — container-cluster consumers
  read `domains.containerClusters`, storage consumers `domains.storageClusters`,
  machine consumers `domains.machines`, and the generic fleet-wide `no_proxy`
  umbrella reads `domains.base`.
