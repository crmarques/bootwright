# OpenShift on OpenShift Virtualization the same context builds

A synthetic preparation profile for [B732](../../../specs/milestones/m3.md#b732),
the lab-kubevirt-nested profile of the
[coverage profiles](../../../specs/openshift-coverage.md#acceptance-profiles):
a three-node compact host cluster on libvirt with the `nmstate` and `kubevirt`
packages and one localnet secondary network, and a three-node `child` cluster
on its virtual machines through a provider naming `hostClusterRef`. It claims
no accepted run.

## Worth noting

- Host nodes `host-01` to `host-03` (198.51.100.21 to .23), each 12 vCPU,
  40 GiB memory, a 200 GiB root disk and an emulated TPM. Host capacity
  including the reserve: at least 40 logical CPUs, 128 GiB available memory
  and 640 GiB pool disk, plus media space, and nested virtualization enabled.
- The host declares the `nested-guests` Localnet network on `enp2s0`
  (`lab-secondary`, 203.0.113.0/24, NAT through the controller), selecting
  namespaces labelled `network.example.test/nested-guests: enabled`. The
  `kubevirt` instance creates the provider namespace `bootwright-nested` with
  that label, its ServiceAccount and roles; no kubeconfig Secret is declared,
  because each run is lent a token minted for that ServiceAccount.
- Child nodes `child-01` to `child-03` (203.0.113.21 to .23), each 4 vCPU,
  16 GiB and 120 GiB, on authored `baremetal` with installer VIPs 203.0.113.10
  and .11. The VMs of one role prefer distinct host nodes.
- The storage class `nested-guest-storage`, whose StorageProfile offers
  `ReadWriteMany`, is an operator prerequisite on the host: no package of this
  profile provides one.
- `bootwright machine stop` does not yet reach the child's VMs
  ([B930](../../../specs/milestones/m3.md#b930)): stop them through the host's
  API before `destroy`, which removes the child before the host.
- Replace `example-channel` and the `*.v0.0.0` CSVs in `add-ons/binding.yaml`
  with what the selected release's catalog serves; passing validation does not
  prove those versions exist. All names and addresses are fixtures.
