# OpenShift on an existing OpenShift Virtualization host, installer VIPs

A synthetic preparation profile for [B731](../../../specs/milestones/m3.md#b731),
the lab-kubevirt profile of the
[coverage profiles](../../../specs/openshift-coverage.md#acceptance-profiles):
three `controlPlane` nodes and two workers on guests of an existing OpenShift
Virtualization host, on authored `platform.baremetal` with installer VIPs. It
claims no accepted run.

[B87](../../../specs/milestones/m1.md#b87) realizes the provider and its guests
and [B88](../../../specs/milestones/m3.md#b88) pushes the agent image to each
guest, boots, releases and removes the cluster in-tree. The API validates the
profile; its live run on an external host is B731's operator gate.

## Worth noting

- Guests `kubevirt-01` to `kubevirt-05` (198.51.100.21 to .25), each 8 vCPU,
  16 GiB memory and a 120 GiB root disk; the installer owns the OS
  (`provided: false`) and targets `/dev/vda`. The provider derives each MAC
  from the context, Machine and interface name, so no Machine authors a MAC.
- The host is reached through the context-bound `host-kubeconfig` Secret
  (opaque, for exactly the intended API server); Bootwright does not create the
  host cluster. Guests live in the operator-owned namespace `bootwright-guests`
  on storage class `guest-block-storage`, attached through the existing
  `bootwright-guests/guest-l2` NetworkAttachmentDefinition, which cleanup
  leaves untouched.
- The installer's keepalived holds the API and API-internal VIP 198.51.100.10
  and the ingress VIP 198.51.100.11 on the guests' shared layer 2, so the
  segment's network must let a guest answer for them: a bridge
  NetworkAttachmentDefinition with `macspoofchk` off, or a `Localnet`
  ClusterUserDefinedNetwork with `ipam.mode: Disabled`. Provide the DNS
  records for `api`, `api-int`, `*.apps` and each node.
- No ArtifactServer: the private image is pushed to each guest through the
  host's verified CDI upload proxy, never with TLS verification disabled. The
  claim that held it, which embeds the pull secret, stays until that guest
  next stops. Managed DNS and NTP run on the controller.
- The host credential covers the access reviews and cluster reads of the
  [provider proof](../../../specs/substrates.md#kubevirt-machine-realization),
  `list` on nodes among them. The storage class's StorageProfile offers
  `ReadWriteMany` in its first claim property set, and at least one node is
  schedulable for virtual machines with KVM.
- The controller reaches the host API and the CDI upload proxy without an
  ambient proxy, and installs the build's default `virtctl`, whose size and
  SHA-256 the build pins, from github.com.
- Operator inputs: `host-kubeconfig`, `openshift-pull-secret`, the generated
  `kubevirt-cluster-admin-ssh-key`, and host capacity for the five guests. All
  names and addresses are fixtures.
- The [runbook](runbook.md) is the operator gate: every prerequisite, the
  checks it records and the destroy.
