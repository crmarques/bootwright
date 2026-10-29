# Three RHEL guests on libvirt

Three Machines share the RHEL 9.8 install profile, package tree, four managed
services and controller-hosted libvirt provider. Each guest has 4 vCPUs,
8 GiB of memory, a 60-GiB disk and an emulated HTTPS Redfish BMC. Installation
creates the `bootwright` account and records each guest's SSH host key.

The [input](environment.yaml), [infrastructure](infra) and
[Secret descriptors](secret-descriptors) contain only documentation values; all
four Secrets are generated locally. It is the three-guest form of
[lab-rhel](../lab-rhel/README.md).

## Worth noting

- Topology: guests `rhel-01`, `rhel-02` and `rhel-03` at `198.51.100.11`, `.12`
  and `.13` (`/24`) on the NAT bridge `virbr-lab`; controller `192.0.2.1` runs
  the proxy, DNS, NTP and artifact server. Reserve 12 vCPUs, 24 GiB of guest
  memory and three 60-GiB disks beyond the host, services, ISOs and package
  tree. The provider reserves 1 vCPU, 2 GiB and 10 GiB for the host.
- One package-tree publication serves all three installations; installation
  waits can overlap, and graph waves are a preview, not a barrier.
- Customize the three Machine addresses, provider subnet and gateway,
  NetworkConfig routes and domain, controller address, service bind addresses
  and the artifact certificate together. Keep BMC listeners on loopback (the
  provider declares port `8000`).
- Operator inputs: entitled RHEL 9.8 x86_64 boot and DVD ISOs added as local
  media, and reachable upstream DNS and time sources replacing the
  placeholders. Run one lab per host; service sockets and `virbr-lab` overlap
  with other examples. FIPS-mode controllers need an RSA or P-256 fleet SSH key.
