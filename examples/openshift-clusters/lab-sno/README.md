# Single-node OpenShift on emulated bare metal

Installs one OpenShift cluster on one libvirt guest through the agent
installer. The guest is installer-provisioned (`os.provided: false` with no
install profile), so the cluster installer writes its disk. This is the
[B61 rehearsal](../../../specs/milestones/m3.md#b61); operator acceptance is
still open. The [cluster contract](../../../specs/openshift-clusters.md) lists
the current implementation limits.

## Worth noting

- One guest `sno-01` on the existing local controller: 8 vCPU, 16 GiB memory,
  a 120 GiB disk and an emulated TPM. The provider reserves another 1 CPU,
  2 GiB and 10 GiB on the controller.
- `api`, `api-int` and `*.apps` all resolve to the node: `source.type: node`
  declares it and the omitted `api-int` slot inherits `api`. The installer
  input uses `platform: none`, as one master and no workers require.
- Connected install, direct access, release 4.21.15. The managed NAT network
  `virbr-lab` (`198.51.100.0/24`), the loopback HTTPS emulated BMC (port
  8000), DNS, NTP, a proxy and a private artifact server are all managed on
  the controller. No `media add` is needed: the agent ISO is built and
  published privately.
- All addresses and domains are synthetic. Replace the controller address,
  guest subnet and bridge, upstream DNS and NTP sources, and the release pin
  consistently, including certificate SANs.
- Operator inputs: the OpenShift pull secret (`openshift-pull-secret`, bound
  from a private file). The BMC credentials and TLS, artifact-server TLS and
  node SSH key are generated.

Run it with the e2e [`lab-sno`](../../../e2e/lab-sno/runbook.md) case.
