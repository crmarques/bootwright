# OpenShift on an existing OpenShift Virtualization host, installer VIPs

This synthetic preparation profile is the lab-kubevirt profile of the
[coverage profiles](../../specs/openshift-coverage.md#acceptance-profiles), the
one operator run of [B731](../../specs/milestones/m3.md#b731) (Q19 (a)). It
declares three `controlPlane` nodes and two workers on authored
`platform.baremetal` with installer VIPs: the installer's keepalived holds the
API and ingress addresses on the nodes' shared layer 2 and its haproxy balances
them, so no external load balancer is needed. It claims no accepted run. The existing host is reached
through a context-bound kubeconfig; Bootwright does not create the host cluster.

The API validates this profile, [B87](../../specs/milestones/m1.md#b87)
realizes the provider proof and the guests, and
[B88](../../specs/milestones/m3.md#b88) pushes, boots, releases and removes
the cluster in-tree; its live run on an external host is B731's operator gate.
The provider derives each interface's MAC from the context, Machine and
interface name, so no Machine authors a MAC or an `interfaceBinding`
([B87](../../specs/milestones/m1.md#b87)).

## Host prerequisites

Customize a private copy before any live rehearsal. All names, addresses
and storage identifiers in this directory are documentation fixtures.
The five profiles request 8 vCPU, 16 GiB memory and a 120 GiB root disk each;
provide that capacity plus the host's own reserve and upload-image storage.
The installer owns each guest OS (`provided: false`) and targets `/dev/vda`.

| Prerequisite | Required preparation |
| --- | --- |
| Existing host | OpenShift Virtualization and CDI ready; record their exact versions and the pinned guest OpenShift release. |
| Namespace | Create `bootwright-guests` as operator-owned; do not give this rehearsal permission to delete the namespace. |
| Storage | Supply `guest-block-storage` and a working CDI StorageProfile with the intended access and volume modes. Confirm immediate binding of the stopped VM's root DataVolume, upload capacity, and deletion of owned PVCs. |
| Network | Create a bridge-type NetworkAttachmentDefinition `bootwright-guests/guest-l2` attached to the guest L2, with `macspoofchk` off for the VIPs. Ensure every eligible host has its backing bridge, routing and MTU. The provider references this existing object; it must remain untouched on cleanup. |
| Controller routing | The controller must reach the host API, verified CDI upload proxy, and every guest installation address on `198.51.100.0/24`. Guest nodes need the declared gateway, DNS/NTP services, release registry and external endpoints. |
| Installer VIPs | Keep `198.51.100.10` (API and API-internal) and `198.51.100.11` (ingress) free on `198.51.100.0/24` and outside any DHCP range, with DNS records for `api`, `api-int`, `*.apps` and each node. The attachment must let a guest answer for an address that is not its own: a bridge NetworkAttachmentDefinition with `macspoofchk` off, or a `Localnet` ClusterUserDefinedNetwork with `ipam.mode: Disabled`. The pre-boot ARP probe does not reach a KubeVirt attachment, so prove the addresses free yourself before the first boot. |
| Upload trust | Provide a verified HTTPS upload-proxy route reachable from the controller. The planned adapter reads the host ingress CA from `openshift-config-managed/default-ingress-cert` through the verified API; confirm this actually covers the upload route before qualifying B87. Never disable TLS verification. |
| Host credentials | Bind the `host-kubeconfig` opaque Secret to a kubeconfig for exactly the intended API server with its CA and credentials. Keep material outside this input tree; never depend on ambient kubeconfig. |

The host credential's RBAC must cover the operations in the
[KubeVirt realization contract](../../specs/substrates.md#kubevirt-machine-realization):
read/watch the guest namespace's VirtualMachines, VirtualMachineInstances,
DataVolumes and PVCs; create owned VMs and DataVolumes; update the owned VM's
media specification; use the VM start/stop subresources; delete owned VMs,
DataVolumes and PVCs with identity checks; and request CDI upload tokens.
It also needs read access to the referenced network, storage class and
StorageProfile, host version/virtualization discovery and the named ingress-CA
ConfigMap. Namespace and network writes are not required. The provider proof reviews exactly the verbs its frozen request lists
(`internal/substrate/kubevirt/catalog.go`), and the exact deployable
Role/ClusterRole remains part of B87 qualification;
this runbook does not invent an accepted RBAC manifest or prescribe cluster-admin.
Verify the resulting least-privilege permissions against that implementation
before its first live run.

This profile manages DNS and NTP on the local controller. Replace their binds,
upstreams and the controller's conditional resolver route together. It has no
ArtifactServer: its private ISO is intended for per-node CDI push, not HTTP
publication.

## Prepare and validate

```sh
m3_input=$(mktemp -d /tmp/bootwright-lab-kubevirt.XXXXXX)
cp -R examples/openshift-clusters/lab-kubevirt/. "$m3_input/"
make build
./bin/bootwright validate -f "$m3_input"
```

Customize and import this private copy into an unused `lab-kubevirt` context. Follow the
[compact staging sequence](lab-compact.md#stage-and-install), adapting
its context, input directory and Secret names. Bind `host-kubeconfig` with
`secret set --value-file`, bind `openshift-pull-secret`, and generate
`kubevirt-cluster-admin-ssh-key`. Stage controller tools and DNS/NTP, configure
the conditional resolver, then run preflight and inspect the plan before apply.
Omit artifact-server and BMC steps: neither is part of this profile.

## Acceptance, interruption and cleanup

Use the [acceptance ledger](../../docs/acceptance.md) for a sanitized build
and release record; keep actual identities, credentials and private artifacts
in the operator's private evidence store. Record media-build duration, each
node's upload and boot duration, bootstrap wait, install wait and media release.
Record host and virtualization versions, attachment kind, storage class and
StorageProfile, upload throughput and whether the frozen boot budget sufficed.

Interrupt a push, bootstrap wait and install wait separately. Preserve the
context and observe the frozen operation before continuing; prove each replay
converges without foreign-object adoption or duplicate owned disks. Complete an
unchanged settle apply, verify every declared node Ready, obtain the cluster
kubeconfig through its custody command and protect that exported credential.
Validation and projection goldens do not replace these live observations.

A released node keeps the claim that held its pushed image, which embeds the
pull secret, until its VirtualMachine next stops
([security](../../specs/security.md#network-remote-systems-and-privilege)); record
that each claim is gone after the node's first stop.

Stop all five guests explicitly with `bootwright machine stop`, prove every
VirtualMachineInstance absent, then request destroy and follow its destructive
authorization and quiescence checks. Prove every owned VirtualMachine,
DataVolume and PVC absent, including a claim that outlived its DataVolume.
Diagnose a stuck claim at the storage layer; do not remove finalizers to fake
completion. Verify the namespace, attachment and all foreign objects remain.
Restore controller resolver changes and retain the context until removal is
proved. Owner acceptance and the interruption ledger remain open B731 gates.
