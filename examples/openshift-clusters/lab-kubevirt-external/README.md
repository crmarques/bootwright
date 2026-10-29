# OpenShift on an existing OpenShift Virtualization host, external endpoints

A synthetic preparation profile for [B731](../../../specs/milestones/m3.md#b731),
a variant of the [coverage profiles](../../../specs/openshift-coverage.md#acceptance-profiles):
three `controlPlane` nodes and no workers on guests of an existing OpenShift
Virtualization host, with external API and ingress endpoints and the derived
`none` installer platform. It is not the
[lab-kubevirt](../lab-kubevirt/README.md) profile B731 runs (Q19 (a)); its
platform is covered in-tree by projection goldens, and it claims no accepted
run.

[B87](../../../specs/milestones/m1.md#b87) realizes the provider and its
guests and [B88](../../../specs/milestones/m3.md#b88) pushes, boots, releases
and removes the cluster in-tree. The API validates the profile.

## Worth noting

- Guests `kubevirt-01` to `kubevirt-03` (198.51.100.21 to .23), each 6 vCPU,
  16 GiB memory and a 120 GiB root disk; the installer owns the OS
  (`provided: false`) and targets `/dev/vda`. The provider derives each MAC
  from the context, Machine and interface name, so no Machine authors a MAC or
  an `interfaceBinding`.
- The host is reached through the context-bound `host-kubeconfig` Secret
  (opaque, for exactly the intended API server); Bootwright does not create the
  host cluster. Guests live in the operator-owned namespace `bootwright-guests`
  on storage class `guest-block-storage`, attached through the existing
  `bootwright-guests/guest-l2` NetworkAttachmentDefinition, which cleanup must
  leave untouched.
- API and API-internal endpoints are external at 198.51.100.10 and ingress at
  198.51.100.11; provide the load balancing, health checks and DNS records.
- No ArtifactServer: the private ISO is intended for per-node CDI push to a
  verified HTTPS upload-proxy route, never with TLS verification disabled.
  Managed DNS and NTP run on the controller.
- The host credential's RBAC must cover the
  [KubeVirt realization contract](../../../specs/substrates.md#kubevirt-machine-realization);
  the exact Role and request set stay part of B87 qualification.
- Operator inputs: `host-kubeconfig`, `openshift-pull-secret`, the generated
  `kubevirt-cluster-admin-ssh-key`, and host capacity, CDI, StorageProfile and
  upload storage for the three guests. All names and addresses are fixtures.

Runbook: [e2e/runbooks/lab-kubevirt-external.md](../../../e2e/runbooks/lab-kubevirt-external.md).
