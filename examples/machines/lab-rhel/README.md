# Managed RHEL guest on libvirt

One existing controller Machine hosts DNS, NTP, a proxy, an artifact server
and libvirt. Bootwright creates one 4-vCPU, 8-GiB guest with a 60-GiB disk,
boots it through an emulated Redfish BMC and installs RHEL 9.8 from a hosted
DVD package tree. The installation creates the `bootwright` account with the
fleet SSH key and records the installed host key for subsequent sessions.

The input is split into [environment](environment.yaml),
[services](infra/components), [Machines](infra/machines),
[provider](infra/providers/lab-libvirt.yaml),
[network](infra/networkconfigs/lab-guests.yaml), [OS profiles](infra/os) and
[Secret descriptors](secret-descriptors). Descriptors contain no credentials;
every Secret is generated locally in its context.

## Worth noting

- Topology: controller `192.0.2.1` runs dnsmasq (`53`), chrony (`123`), squid
  (`3128`) and the artifact server (`8443` https, `8080` http); guest
  `rhel-01` at `198.51.100.11/24` sits on the NAT bridge `virbr-lab` that the
  provider creates. The BMC listens on loopback `127.0.0.1:8000`. The provider
  reserves 1 vCPU, 2 GiB and 10 GiB of disk for the host.
- Every address and name is a public documentation placeholder; customize the
  controller, resolver and time-source addresses, the guest subnet and the base
  domain together in a copy outside Git.
- The guest profile uses a TPM, an enforcing SELinux mode and no password SSH.
  On a FIPS-mode controller choose `rsa` or `ecdsaP256` for
  `bootwright-machine-key`.
- Host-restart qualification is tracked in the
  [acceptance ledger](../../../docs/acceptance.md); this example does not claim
  that every reboot path has passed its operator gate.
- Operator inputs: entitled RHEL 9.8 x86_64 boot and DVD ISOs added as local
  media (`rhel-9.8-x86_64-boot.iso`, `rhel-9.8-x86_64-dvd.iso`; Bootwright does
  not download them), hardware virtualization, and free service sockets. Run
  one of these labs at a time: other examples reuse the bridge and sockets.

Runbook: [e2e/runbooks/lab-rhel.md](../../../e2e/runbooks/lab-rhel.md).
The templated e2e [`rhel` case](../../../e2e/README.md#case-rhel) covers the same
scenario with its own inputs.
