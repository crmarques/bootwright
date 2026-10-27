# KubeVirt agent node boot

How an agent ISO boots on a KubeVirt virtual machine and what the platform does
to the nodes around it. [Substrates](../../specs/substrates.md) and
[container clusters](../../specs/openshift-clusters.md) own the required
behavior; this page records native facts for
[B87](../../specs/milestones/m1.md#b87) (the KubeVirt Machine arm),
[B88](../../specs/milestones/m3.md#b88) (OpenShift on KubeVirt Machines) and
[B731](../../specs/milestones/m3.md#b731) (the lab-kubevirt acceptance).

Provenance: `R5:R5-<n>`, `R<n>:C<m>` and `R3 C<n>` are claim ids of the M3
research, `RV` marks a claim the review re-verified, and "observed by the v1
implementation (main-v1)" marks a fact to re-verify on this tree. Release
labels name where a fact was read; they are not supported or required versions
(D163). The ids name the M3 definition's research and review notes of
2026-10-20, which this repository does not track; each statement here carries
its fact, and an id only records where it was read.
Assisted-service, assisted-installer and assisted-installer-agent behavior was
read at master, not per release.

## Platform and vendor statement

- The agent installer accepts the platforms `baremetal`, `vsphere`, `nutanix`,
  `none` and `external`. It has no `kubevirt` value (R5:R5-03).
- The vendor's 4.22 documentation describes control-plane VMs on OpenShift
  Virtualization through KubeVirt Redfish as Technology Preview, with workers on
  separate infrastructure. It has no 4.20 or 4.21 guide, and an all-VM cluster
  carries only the non-tested-platform caveat (R5:R5-02, R1:C14, R5:R5-04).
- KubeVirt Redfish VMs use `runStrategy: Halted`, `rebootPolicy: Terminate` and
  the label `redfish-enabled: "true"`, and the HyperConverged resource gates
  `RebootPolicy` and `declarativeHotplugVolumes` (R3 C40).

## Networking

- Masquerade (the pod network) NATs every guest into `10.0.2.0/24`, so it
  cannot carry node addresses, virtual addresses or the rendezvous address
  (R5:R5-28). Use a `Localnet` ClusterUserDefinedNetwork, where
  `ipam.mode: Disabled` is required for VMs, or a `bridge` NetworkAttachmentDefinition
  (R5:R5-30, R5:R5-31, R3 C37).
- On a `Localnet` network with `ipam.mode: Disabled`, 4.22 enforces port
  security by the MAC address only, so keepalived virtual addresses work
  (R5:R5-32, confirmed for 4.22). A bridge NetworkAttachmentDefinition needs
  `macspoofchk` off for the same effect (R3 C38).

## Boot, storage and power

- `bootOrder`: the lower value wins, values are unique, and disks without one
  are not tried once any disk has one (R5:R5-19). That a blank disk falls
  through to the `cdrom` and an installed disk boots itself, so no boot
  selection is made, is an inference (medium confidence), and post-install boot
  entries are not reliable (R5:R5-19, R5:R5-23); M3e's first run re-verifies it.
- `runStrategy` is `Always` (respawn after any termination, guest shutdown
  included), `RerunOnFailure` (respawn only after an infrastructure failure),
  `Manual` (`virtctl start`, `stop` and `restart` decide) or `Halted` (kept
  off); upstream KubeVirt adds `Once`. `spec.running` is deprecated, and a VM
  setting both it and `runStrategy` is an error (R5:R5-20).
- The EFI default is `secureBoot: true`, which needs `features.smm.enabled`. The
  agent ISO under KubeVirt Secure Boot is unverified (R5:R5-21).
- With WaitForFirstConsumer, a DataVolume needs the bind-immediate annotation
  (`cdi.kubevirt.io/storage.bind.immediate.requested`). StorageProfile
  `claimPropertySets` is empty for an unknown provisioner (R5:R5-17).
- CD-ROM hot plug needs `declarativeHotplugVolumes` and a predefined cdrom
  (R5:R5-27, RV confirmed).
- On destroy, template DataVolumes follow the VM and standalone ones do not.
  `kubernetes.io/pvc-protection` holds a claim until its pod ends (R5:R5-44).

## Observed by v1

Observed by the v1 implementation (main-v1); re-verify on this tree (v1 lesson
ids L2-K01 to K15 and L1 KV-1 to KV-7):

- `running: false` stopped a VM that `virtctl start` had started.
- `virtctl start` returns before the VMI exists, so an immediate `kubectl wait`
  fails with `no matching resources found`.
- `virtctl image-upload` creates unlabelled DataVolumes. v1 created the upload
  DataVolume itself with its identity labels, then uploaded with `--no-create`.
- `spec.pvc` defaults to Filesystem on a Block class, so writes crawl and
  clones stall at `CloneInProgress` and never report `Failed`. The agent
  installer then reports `Writing image to disk did not sufficiently progress`.
- One slow master stalls etcd at two members and times out the whole bootstrap.
- A claim can outlive its DataVolume. A new same-name DataVolume cannot adopt it,
  and the upload fails with `No DataVolume is associated with the existing PVC`.
- The image DataVolume default is 4Gi against an image of 1 to 1.5 GiB.
- The upload proxy's certificate is signed by the ingress CA in
  `openshift-config-managed/default-ingress-cert`.
- A typed `kubectl get` hides a generic 404 behind discovery errors, so probe
  `/apis/subresources.kubevirt.io/v1/version` and then `/version`.

## Decision history

`virtctl image-upload` is kept on purpose (owner, 2026-06-28). A DataVolume
`http` pull would need host-to-controller egress, the token-bearing image URL in
the DataVolume spec and a CA ConfigMap.
