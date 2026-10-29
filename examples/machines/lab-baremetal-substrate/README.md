# Physical-install rehearsal substrate

This input creates one empty libvirt guest and its CA-issued HTTPS Redfish BMC.
It installs no OS and owns no DNS, NTP or artifact service. A
separate [lab-baremetal](../lab-baremetal/README.md) context installs into that
guest through the physical path, so the physical install is rehearsed without
hardware.

## Worth noting

- Topology: Machine `rehearsal-01` (`os.provided: false`, no install profile)
  on the managed NAT bridge `virbr-lab` (`198.51.100.1/24`); the emulated BMC
  binds loopback `127.0.0.1:8000`. The guest reserves 4 CPUs, 8192 MiB RAM and
  a 60 GiB disk with a TPM; the provider also reserves 1 CPU, 2048 MiB and 10 GiB
  for the host.
- Only this context owns `virbr-lab`, its subnet and BMC port 8000; never apply
  another context managing the same bridge, provider pool or port.
- The `lab-bmc-ca` CA issues the `lab-bmc-tls` serving leaf; the generated BMC
  account is `admin`. The physical context imports only the public CA and the
  account password, never the CA private key. Renewing the CA does not reissue
  the leaf.
- Qualification: the operator rehearsal is unrun; offline tests qualify
  declaration, generation and trust. Installation, private publication cleanup,
  reboot and interruption/continuation stay unrun until an operator executes
  them.
- Operator inputs: a supported Linux/amd64 controller with virtualization;
  replace the synthetic controller address and FQDN in the copied Machine (and
  the bridge, subnet or port if already allocated) before import. Destroy the
  physical context first, since destroying this substrate deletes the virtual
  disk.

Run it with the e2e [`lab-baremetal-rehearsal`](../../../e2e/lab-baremetal-rehearsal/runbook.md) case.
