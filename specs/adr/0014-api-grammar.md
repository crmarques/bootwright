# ADR 0014: Public API Grammar — References, Unions, Collections, Enablement

## Status

Accepted

## Context

Package `api/v1alpha1` defines every Bootwright desired-state struct. As the
schema grew, a small set of authoring grammars kept the API predictable:
how reference fields are named and resolved, how one-of-several unions are
spelled, when a named list is used instead of a map, and how optional
feature blocks are enabled or disabled. That contract lived in the
`api/v1alpha1/doc.go` package comment and in per-field comments. With prose
comments removed from code (ADR 0006), the contract needs a durable home so
new fields keep following it instead of re-inventing shapes.

## Decision

### References

Reference fields carry the `Ref`/`Refs` suffix and are authored and rendered
as plain name strings. In Go they are wrapped in `LocalObjectReference` (or
`SecretRef`, which resolves to a `Secret` by `metadata.name`): the wrapper
exists only so the resolution namespace stays distinct from ordinary strings
and every reference rejects the `{name: ...}` object form with one shared
error. The field comment-free schema relies on `specs/state-model.md` or the
kind's `docs/concepts/` page to state each reference's resolution namespace.

Deliberate exceptions:

- `Environment` `spec.containerClusters` and `spec.storageClusters` are fleet
  selection lists, not references — plain strings, no `Ref` suffix.
- `CustomPlaybook` `spec.target.{clusters,machines,hostGroups}` (and
  `ClusterAddonStep` `target.static.{clusters,machines}`, alongside the
  `target.fromInput` and bound arms) are inventory selection lists, not
  references — same rule.
- `proxyFor.{bootwright,containerClusterInstall,machineOSInstall}` name a
  `spec.infraComponents.proxies[]` entry **or the `none` sentinel**, so they
  stay plain strings without the `Ref` suffix.
- KubeVirt `networkRef` (`KubeVirtNetworkRef`) is the sole sanctioned
  object-form reference: the network object lives on the host cluster,
  outside the loaded state, so it is identified by an external GVK plus
  `{name, namespace}` (the Kubernetes `TypedObjectReference` idiom;
  `kind`/`apiGroup` default to `ClusterUserDefinedNetwork` / `k8s.ovn.org`).

### Unions

The API has exactly two union grammars.

A **discriminated union** carries a `type` field whose value is
byte-identical to the populated arm key: `InfraProvider` spec,
`ContainerCluster` `install.platform`, `InfraComponent` spec, `ClusterAddon`
spec, `StorageExport` spec, and `StoragePool` `spec.ceph`. Two carve-outs:

- `StorageExport` `type` names only the export flavor (`dataFoundation`);
  which optional block is populated follows the referenced `StorageCluster`'s
  `spec.management`. An external cluster carries `externalDetails`
  (operator-supplied payload via `fromSecretRef`) and leaves `dataFoundation`
  empty; a managed cluster carries `dataFoundation` and may omit
  `externalDetails` (the consuming add-on's hook produces the payload).
- `Entitlement` `spec.type` selects a **set** of required arms rather than a
  single populated arm, so it is neither a plain discriminated nor a
  presence union:

  | `spec.type`        | required arms                                            |
  | ------------------ | -------------------------------------------------------- |
  | `redhat-rhel`      | `rhsm`                                                    |
  | `redhat-ceph`      | `rhsm` + `registry.credentialsRef`                        |
  | `ibm-storage-ceph` | `registry.credentialsRef` + `license.accept: true` (an inline `rhsm` arm is rejected; RHEL registration is named by the node `MachineInstallProfile.spec.subscription` or `StorageCluster.spec.ceph.osSubscriptionRef`) |

A **presence union** carries no discriminator — authoring exactly one arm
selects the kind. It is used where the surrounding document already fixes
which arm is legal (`InfraProvider` `spec.networkAttachments`, where the
provider's `spec.type` is the kind and a mismatched arm is rejected), and
where the arm itself is the author's choice and doubles as the
discriminator: `MachineInstallProfile` `packageSource`
(`mirror`/`fromSubscription`/`hostedTree`), the install profile `installer`
(`anaconda` is the only backend; its presence is the discriminator), and
`Secret` `spec.source` (`contextStore`/`file`/`generated`, where omitting
every arm selects `contextStore`).

### Named collections

A named set of things is a list of entries with a `name` field wherever the
names are user-invented (`bindAddresses`, `listeners`, machine `addresses`,
`networkAttachments`, `machineProfiles`, ...). Name-keyed maps appear only
where the key set is a closed, validated vocabulary: `ContainerCluster`
`install.endpoints` (`api`, `api-int`, `ingress`) and `Environment`
`spec.componentImages` (the component type/implementation catalog).

### Enable/disable

Optional feature blocks are presence-managed: omitting the block keeps the
upstream tool's default behavior, whether that default is off
(`spec.ceph.topology.stretch` — presence is the enablement signal) or on
(`spec.ceph.monitoring`, libvirt `bmcEmulationDefaults`). A block whose
upstream default is on carries an `enabled *bool` defaulting to true, so
authoring the block with `enabled: false` is the opt-out. A plain bool
`enabled` appears only where false and unset mean the same thing (the three
FIPS gates: `customizations.security.fips`, `spec.ceph.security.fips`,
`ContainerCluster` `spec.security.fips`). A tri-state `*bool` is kept where
explicit false must render a real disable while unset renders nothing
(`customizations.security.firewall`).

### Reserved spellings

The word `type` is reserved API-wide for kind-of-thing discriminators; the
who-runs-it axis is spelled `management` (`external` | `managed`), and
generated-parameter fields avoid `type` (`keyType` on generated sshKeyPair).
`none` is a reserved component name/ref sentinel and never a `management`
value.

## Consequences

- A new reference field must take the `Ref`/`Refs` suffix and the
  plain-name-string form, and either `specs/state-model.md` or the owning
  `docs/concepts/` page must state its resolution namespace. New object-form
  references are prohibited short of a documented exception like `networkRef`.
- A new one-of-several field must pick one of the two union grammars; a
  discriminator value must stay byte-identical to its arm key so strict
  decode and the exactly-one-of validators stay mechanical.
- New optional feature blocks follow the enablement table above; an
  `enabled` field of the wrong shape (plain bool where unset must differ
  from false, or a pointer where they must not) is an API review defect.
- Selection lists stay suffix-free so a reader can tell "resolve this name"
  fields from "filter to these names" fields at a glance.
