# lab-kubevirt runbook

This runbook is the operator gate of [B731](../../../specs/milestones/m3.md#b731):
the [lab-kubevirt](README.md) profile applied with `bootwright apply` on an
existing OpenShift Virtualization host reached through
`spec.kubevirt.kubeconfigRef`. It also accepts the real-host qualification of
the KubeVirt Machine arm ([B87](../../../specs/milestones/m1.md#b87)) and, when
the attachment is a `Localnet` ClusterUserDefinedNetwork, the VM-on-Localnet
checks of [B1158](../../../specs/milestones/m3.md#b1158). It runs from a
controller against a host the operator supplies, so it is not an
[end-to-end case](../../../e2e/README.md). No run has been accepted.

[KubeVirt machine realization](../../../specs/substrates.md#kubevirt-machine-realization)
owns every rule this runbook exercises. Each check below is a recorded
observation, never a version rule: record what the host did, with its
OpenShift and OpenShift Virtualization versions.

## 1. Prepare the host

The host is the operator's. Bootwright reads it and never creates these:

- The namespace the provider names, `bootwright-guests` in the example.
- The attachment: a bridge NetworkAttachmentDefinition (CNI type `bridge` or
  `cnv-bridge`) with `macspoofchk` off, or a `Localnet` ClusterUserDefinedNetwork
  with `ipam.mode: Disabled` whose `namespaceSelector` labels the namespace.
  The installer's keepalived holds the API and ingress VIPs on that layer 2.
- A storage class whose StorageProfile's first claim property set offers
  `ReadWriteMany`, named in `storageClassRef`. The arm sets
  `evictionStrategy: LiveMigrate`, and the provider proof refuses any other
  class.
- At least one node labelled `kubevirt.io/schedulable: "true"` that
  allocates `devices.kubevirt.io/kvm`.
- A kubeconfig of the accepted form: one cluster, one user and one context,
  an embedded certificate authority, and a client certificate and key or a
  token. Its credential covers every access review the provider proof makes
  in the namespace (VirtualMachines, VirtualMachineInstances, the `start` and
  `stop` subresources, DataVolumes, upload token requests and
  PersistentVolumeClaims) and the proof's cluster reads: `/version`, the
  KubeVirt version, the `kube-system` namespace, the namespace,
  `cdiconfigs/config`, StorageClasses and the StorageProfile, the ConfigMaps
  `openshift-config-managed/default-ingress-cert` and
  `openshift-cnv/kubemacpool-mac-range-config`, NetworkAttachmentDefinitions,
  ClusterUserDefinedNetworks where it may read them, and `list` on nodes.
- DNS records for `api`, `api-int`, `*.apps` and each node.
- Capacity for the five guests: the coverage row's formula
  ([acceptance profiles](../../../specs/openshift-coverage.md#acceptance-profiles))
  plus about two cores per host node for OpenShift Virtualization itself.

Record the host's OpenShift and OpenShift Virtualization versions and the
storage class's StorageProfile.

## 2. Prepare the controller

- Routes to the host API, to the CDI upload proxy on `*.apps` without an
  ambient proxy, to each node on port 22, to the rendezvous node on 8090 and
  to the cluster API.
- `virtctl` pinned through `spec.dependencyVersions.virtctl`; an accepted
  run never uses `latest`. The controller stage reads its release metadata
  from api.github.com and downloads it from github.com.

```sh
export CONTEXT=lab-kubevirt
export INPUT_DIR=examples/openshift-clusters/lab-kubevirt
./bin/bootwright validate -f "$INPUT_DIR"
./bin/bootwright context init --name "$CONTEXT" --input-dir "$INPUT_DIR"
./bin/bootwright secret set --context "$CONTEXT" --name host-kubeconfig --value-file "${HOST_KUBECONFIG:?Set HOST_KUBECONFIG to the host kubeconfig file}" --yes
./bin/bootwright secret set --context "$CONTEXT" --name openshift-pull-secret --value-file "${PULL_SECRET_FILE:?Set PULL_SECRET_FILE to the pull-secret JSON file}" --yes
./bin/bootwright secret generate --context "$CONTEXT"
./bin/bootwright secret check --context "$CONTEXT"
./bin/bootwright plan --context "$CONTEXT"
./bin/bootwright apply --context "$CONTEXT" --stage controller --yes
./bin/bootwright apply --context "$CONTEXT" --stage infrastructure-services --yes
./bin/bootwright preflight --context "$CONTEXT" --stage clusters
```

## 3. Run

Apply the substrates first, so the provider proof's evidence is read before
any guest exists, then the rest:

```sh
./bin/bootwright apply --context "$CONTEXT" --stage substrates --yes
./bin/bootwright status --detail full --context "$CONTEXT"
./bin/bootwright apply --context "$CONTEXT" --yes
```

Record, in this order:

1. The provider proof's evidence: the KubeMacPool verdict (`inside`,
   `outside` or `unknown`), the root class and its first claim property set's
   access and volume mode, the schedulable KVM node count, and each
   attachment's CNI type, topology, MTU and `macspoofchk`.
2. One `virtctl image-upload` before the cluster boots: the `virtctl`
   version, the upload's duration, the image size, the `UploadReady` latency
   and whether CDI created a scratch claim.
3. The root DataVolume's phase and its claim's access and volume mode; the
   root DataVolume's and claim's labels, annotations and `ownerReferences`,
   with the `controller` flag.
4. Each pinned MAC address accepted by KubeMacPool.
5. The first node booting from its blank root disk through to the SATA
   `cdrom`, then from disk after installation, with no boot selection made.
6. For each node: push, instance start, boot, bootstrap wait, install wait and
   release durations, and the total at a fan-out of 4. These correct the arm's
   published bounds ([B731](../../../specs/milestones/m3.md#b731)).
7. The installer VIPs held on the attachment on the host's release; a VM
   reaching its own host node and another; the host's and the cluster's
   keepalived on one layer 2.
8. The live eject at release: the media DataVolume disappears and its claim
   stays until the VM next stops.
9. A graceful cluster shutdown, then `bootwright machine start` of a node its
   guest powered off; a `409` on a start or stop of a VM already in that
   state.
10. Interruption drills during a push, during the bootstrap wait and during
    the install wait, each continued by `bootwright apply`; a settle apply
    over unchanged input; `bootwright cluster kubeconfig`.
11. A drain of one host node: the VMs live-migrate. Record the migration
    duration and each VM name's length.

```sh
./bin/bootwright status --context "$CONTEXT"
./bin/bootwright machine list --context "$CONTEXT" --power-status
./bin/bootwright diff --context "$CONTEXT" --observe
./bin/bootwright apply --context "$CONTEXT" --yes
./bin/bootwright cluster kubeconfig --context "$CONTEXT" --name kubevirt > "$KUBECONFIG"
```

## 4. Destroy

12. Stop every node, then destroy. The removal proves every owned
    VirtualMachine, DataVolume and claim absent and leaves the namespace and
    the network objects untouched. Record one `404` body and the size of the
    claim listing.
13. The destroy-pause drill ([B951](../../../specs/milestones/m1.md#b951)):
    start one node again after the destroy registers, so the removal pauses,
    stop it with `bootwright machine stop`, and run `bootwright destroy`
    again. While a destroy is current, `machine stop` still reaches each
    VirtualMachine the destroy has not yet removed, held to the identity its
    apply recorded, so the host's administrator kubeconfig is never needed. A
    Machine whose VirtualMachine the destroy already removed reports `off`
    without contacting the host, `machine start` and `machine restart` refuse
    it until the destroy is finished and the context is applied again, and
    `machine list --power-status` shows no power reading for it. This also
    holds after a failed destroy is run again.

```sh
./bin/bootwright machine stop --context "$CONTEXT" --name kubevirt-01,kubevirt-02,kubevirt-03,kubevirt-04,kubevirt-05 --yes
./bin/bootwright destroy --context "$CONTEXT" --authorize data-loss
./bin/bootwright status --context "$CONTEXT"
```

## 5. Record the run

Record the row as [record a run](../../../docs/operator-guide.md#record-a-run)
says, with every observation above. The row accepts B731 only when the owner
accepts it.
