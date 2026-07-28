# ADR 0025: Composed Names Are Labels Plus Explicit Overrides

## Status

Accepted (implemented)

## Context

Bootwright composes several DNS names from a leftmost label, an owning object's
name, and a zone from `Environment.spec.domains` (ADR 0018). Two different
authoring idioms had grown up around that one composition.

Cluster node names (`ContainerCluster.spec.nodes[].name`,
`StorageCluster.spec.ceph.topology.nodes[].name`) overloaded a single field:
per ADR 0017 a bare label composed to `<name>.<cluster>.<zone>`, while a value
containing a dot was taken as an explicit FQDN and used verbatim. One field
meant two things, and which one depended on whether the string happened to
contain a `.`.

The Ceph mgmt-gateway and RGW public endpoints took the opposite approach.
Their `dnsLabel` fields are strictly a leftmost label; the published name is
always composed, and a dotted value is rejected.

The overloaded form has real costs. The author cannot state intent — a typo'd
`node01.` or a name that legitimately contains a dot silently changes which
rule applies. Nothing can validate a label as a label, because a dotted value
must stay legal in the same field. And the reader of an input file cannot tell a
composed name from a pinned one without knowing the rule.

`Machine` already avoided this: `metadata.name` is the label and the `fqdn`
address entry is a separate, explicitly authored override. Node names were the
sole remaining place where the two meanings shared a field.

## Decision

Every authored name that Bootwright composes is a strict DNS label. Where an
operator must pin a name outside the composed zone, that override is a separate,
explicitly named field — never a different-looking value in the same field.

For cluster nodes:

```yaml
nodes:
  - name: node-01                    # strict DNS label
    machineRef: ceph-1               # -> node-01.<cluster>.<domains.storageClusters>

  - name: node-02                    # label still required: it is the node's identity
    fqdn: srv4009.corp.example.com   # explicit override, used verbatim
    machineRef: ceph-2
```

- `nodes[].name` must match `[a-z0-9]([-a-z0-9]*[a-z0-9])?`. A dotted value is
  rejected with an error naming `fqdn` as the remedy.
- `nodes[].fqdn` is optional and must be a DNS subdomain. When set it is the
  node's FQDN verbatim, unaffected by the cluster zone.
- `name` remains required even when `fqdn` is set: it is the node's identity
  within the cluster, independent of the machine name, and is what
  `placement.hosts[]` and the bootstrap node reference resolve against.

This refines ADR 0017: the node-identity model, the uniqueness rule, and every
downstream use of the composed FQDN stand unchanged — only the way the verbatim
override is authored moves from "a value with a dot in it" to a named field. The
`Machine.fqdn` address, whose two-field shape this generalizes, is untouched.

Because `Normalize` resolves each node's FQDN into `name` before `Validate`
runs, the label rule is enforced on the authored input during load, ahead of
normalization. A state that is normalized before validation therefore cannot
re-derive the violation — this is a decode-shape rule, like strict unknown-field
rejection, not a rule `Validate()` re-checks.

## Consequences

- One idiom fleet-wide: every composed name is authored as a label, and every
  escape from composition is a named field. `dnsLabel` on the Ceph mgmt-gateway
  and RGW endpoints, and `name` on cluster nodes, now read the same way.
- Breaking for inputs that relied on a dotted node name. The migration is
  mechanical: move the value to the sibling `fqdn` and give `name` a label.
- Invalid labels are now caught. Previously `node01.` or a stray dot silently
  switched the node to verbatim mode and produced a name in the wrong zone.
- The foreign-zone capability ADR 0017 introduced is preserved in full: a node
  bound to a pre-existing host whose OS hostname sits in a corporate zone still
  pins that hostname, and it still flows into the cephadm host spec, the
  installer manifests, DNS records, and placement resolution.
- Node names and machine `fqdn` values are now authored the same way, so an
  operator learns one rule instead of two.
