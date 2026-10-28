# Three-node compact OpenShift on emulated bare metal

Installs three schedulable masters and no workers on one libvirt host through
OpenShift's agent installer. The installer owns the API and ingress virtual
IPs on the managed NAT network; the derived platform is `baremetal` with
`provisioningNetwork: disabled`. This is the
[B66 rehearsal](../../../specs/milestones/m3.md#b66); operator acceptance
remains open and requires B61's accepted [lab-sno](../lab-sno/) row.

## Worth noting

- Three guests `compact-01` to `compact-03` (198.51.100.21 to .23), each 6
  vCPU, 16 GiB memory, a 120 GiB root disk and an emulated TPM. API VIP
  198.51.100.10 and ingress VIP 198.51.100.11 are installer-owned and must stay
  distinct from every Machine and the bridge.
- Host capacity: at least 22 logical CPUs, 56 GiB available memory and 400 GiB
  free pool disk, plus about 7 GiB for five image copies. The provider
  reserves 4 CPUs, 8 GiB and 40 GiB of it. See the
  [acceptance profiles](../../../specs/openshift-coverage.md#acceptance-profiles).
- Connected install, release 4.21.15, NAT network `virbr-lab`
  (`198.51.100.0/24`), loopback HTTPS emulated BMCs from port 8000, and
  managed DNS, NTP, proxy and a private artifact server on the controller.
- All addresses and names are synthetic. Replace the controller address, guest
  subnet, bridge, VIPs, upstream DNS and NTP sources and domain consistently.
  A context holding a completed lab-sno apply must be destroyed before
  importing this topology.
- Operator input: the OpenShift pull secret. The BMC credentials and TLS,
  artifact-server TLS and node SSH key are generated.

Runbook: [e2e/runbooks/lab-compact.md](../../../e2e/runbooks/lab-compact.md).
The templated e2e [`compact` case](../../../e2e/README.md#case-compact) covers the same
scenario with its own inputs.
