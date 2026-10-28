# OpenShift on an existing OpenShift Virtualization host, external endpoints

This synthetic preparation profile is the lab-kubevirt-external variant of the
[coverage profiles](../../specs/openshift-coverage.md#acceptance-profiles) for
[B731](../../specs/milestones/m3.md#b731). It declares three `controlPlane`
nodes, no workers, external API and ingress endpoints, and the derived `none`
installer platform. It is not the baremetal-VIP lab-kubevirt profile Q19 ranks
first, and it claims no accepted run. The existing host is reached
through a context-bound kubeconfig; Bootwright does not create the host cluster.

The API validates this profile, but the KubeVirt realization and native port
implementation remain [B87](../../specs/milestones/m1.md#b87). Runtime
planning refuses that unqualified provider. B88's pushed-media preparation
alone does not make this profile runnable. Do not bypass the refusal.
The provider derives each interface's MAC from the context, Machine and
interface name, so no Machine authors a MAC or an `interfaceBinding`
([B87](../../specs/milestones/m1.md#b87)).

## Host prerequisites

Customize a private copy before any live rehearsal. All names, addresses
and storage identifiers in this directory are documentation fixtures.
The three profiles request 6 vCPU, 16 GiB memory and a 120 GiB root disk each;
provide that capacity plus the host's own reserve and upload-image storage.
The installer owns each guest OS (`provided: false`) and targets `/dev/vda`.

| Prerequisite | Required preparation |
| --- | --- |
| Existing host | OpenShift Virtualization and CDI ready; record their exact versions and the pinned guest OpenShift release. |
| Namespace | Create `bootwright-guests` as operator-owned; do not give this rehearsal permission to delete the namespace. |
| Storage | Supply `guest-block-storage` and a working CDI StorageProfile with the intended access and volume modes. Confirm immediate binding of the stopped VM's root DataVolume, upload capacity, and deletion of owned PVCs. |
| Network | Create a bridge-type NetworkAttachmentDefinition `bootwright-guests/guest-l2` attached to the guest L2. Ensure every eligible host has its backing bridge, routing and MTU. The provider references this existing object; it must remain untouched on cleanup. |
| Controller routing | The controller must reach the host API, verified CDI upload proxy, and every guest installation address on `198.51.100.0/24`. Guest nodes need the declared gateway, DNS/NTP services, release registry and external endpoints. |
| External endpoints | Provide API/API-internal load balancing at `198.51.100.10` and ingress at `198.51.100.11`, with the native OpenShift ports, backend health checks and DNS records appropriate to the chosen release. All three control-plane nodes also carry workloads in this compact shape. |
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
ConfigMap. Namespace and network writes are not required. The exact deployable
Role/ClusterRole and native request set remain part of B87 qualification;
this runbook does not invent an accepted RBAC manifest or prescribe cluster-admin.
Verify the resulting least-privilege permissions against that implementation
before its first live run.

This profile manages DNS and NTP on the local controller. Replace their binds,
upstreams and the controller's conditional resolver route together. It has no
ArtifactServer: its private ISO is intended for per-node CDI push, not HTTP
publication.

## Prepare and validate

```sh
m3_input=$(mktemp -d /tmp/bootwright-lab-kubevirt-external.XXXXXX)
cp -R examples/openshift-clusters/lab-kubevirt-external/. "$m3_input/"
make build
./bin/bootwright validate -f "$m3_input"
```

After B87 and B88 qualify runtime execution, customize and import this private
copy into an unused `lab-kubevirt-external` context. Follow the
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

Stop all three guests explicitly with `bootwright machine stop`, prove every
VirtualMachineInstance absent, then request destroy and follow its destructive
authorization and quiescence checks. Prove every owned VirtualMachine,
DataVolume and PVC absent, including a claim that outlived its DataVolume.
Diagnose a stuck claim at the storage layer; do not remove finalizers to fake
completion. Verify the namespace, attachment and all foreign objects remain.
Restore controller resolver changes and retain the context until removal is
proved. Owner acceptance and the interruption ledger remain open B731 gates.
