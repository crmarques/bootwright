# Secondary-network and MetalLB rehearsal

A synthetic preparation profile for [B729](../../../specs/milestones/m3.md#b729)
and the [acceptance matrix](../../../specs/openshift-coverage.md#acceptance-profiles):
a localnet secondary network, OVN host routing and global forwarding, with
NMState and MetalLB add-ons. No live run or owner acceptance is claimed, and
the accepted [lab-sno](../lab-sno/) and [lab-compact](../lab-compact/) rows are
prerequisites to the consuming M3d run.

## Worth noting

- Three masters and two workers, each 8 vCPU, 16 GiB memory, a 120 GiB root
  disk and an emulated TPM. Host capacity including the reserve: at least 44
  logical CPUs, 88 GiB available memory and 640 GiB pool disk, plus media
  space.
- Each node has three NICs: `enp1s0` on the machine network, an addressless
  `enp2s0` localnet uplink on `lab-secondary`, and a statically addressed
  `enp3s0` on `lab-routed` (192.0.2.128/25).
- The cluster declares the `workloads` Localnet secondary network (bridge
  `br-workloads` on `enp2s0`, IPAM disabled, namespaces labelled
  `network.example.test/workloads: enabled`), `routingViaHost` and
  `ipForwarding: Global`. No other forwarding variant is selected; the choice
  and the native operator rows remain the owning B729 and M4 gates.
- The binding selects NMState and MetalLB with two pools: `services` on the
  machine network and `routed-services` on `enp3s0`. Reserve pool addresses so
  no other host or DHCP service allocates them, and route both pools from the
  test client.
- Replace `example-channel` and the `*.v0.0.0` CSVs in `add-ons/binding.yaml`
  with the channel and CSV the selected release's catalog serves, e.g. with
  `oc get packagemanifest metallb-operator -n openshift-marketplace`; passing
  validation does not prove those versions exist.

Runbook: [e2e/runbooks/lab-networks.md](../../../e2e/runbooks/lab-networks.md).
