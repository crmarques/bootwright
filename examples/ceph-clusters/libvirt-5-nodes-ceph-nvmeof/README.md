# NVMe-oF with two gateways

Profile [P4](../../../specs/ceph-coverage.md#profile-catalog): five RHEL
storage guests and a RHEL initiator template running an IBM Storage Ceph
cluster (`ceph-lab`) with a two-gateway NVMe-oF group. Three nodes host MONs,
two host MGRs, all five host one SSD OSD, and `rhel-04` and `rhel-05` each
host an NVMe-oF gateway. Subsystem `application` maps namespace 1 to the
owned `rbd-disk` image over TCP listeners on both gateways and authorizes only
`nqn.2026-01.test.example:initiator`.

No live run or owner acceptance is claimed.

## Worth noting

- Topology: guests `rhel` 13 GiB (x2), `osd-mon` 10 GiB, `gateway` 15 GiB and
  8 vCPU (x2), each with a 60 GiB root and one 60 GiB SSD; the initiator has
  2 vCPU, 4 GiB, 40 GiB root. Total 6 guests, 30 vCPUs, 70 GiB RAM,
  340 GiB root and 300 GiB data. Reserve 4 GiB controller memory.
- Gateway management needs native mTLS. The example authors public
  `enable_auth: false` and Bootwright derives native `enable_mtls: true`; do
  not add `enable_mtls` to the input. The selected build must support native
  certificate management; a build with only native `enable_auth` needs public
  `enable_auth: true` with all five certificate and key bindings, per the
  [native contract](../../../specs/ceph-native-api.md). An unsupported build
  refuses before the gateway service is applied.
- The gateway image `cp.icr.io/cp/ibm-ceph/nvmeof-rhel9` tag `1.6` is authored
  example input, not a default or a claim this combination has run.
- Readiness proves identities, host allowlist, gateway management, HA/ANA state
  and a TCP connection to each listener; not initiator I/O. Gateway mTLS does
  not encrypt the data path. DH-HMAC-CHAP and TLS-PSK are unconfigured. CPU
  qualification, denied-initiator access and failover are unrun.
- The provider CPU must meet IBM's NVMe-oF requirement in the
  [coverage spec](../../../specs/ceph-coverage.md). Bootwright installs the
  initiator's OS only; `nvme-cli` and I/O are operator steps. Remove the
  `initiator` Machine to use your own initiator.
- Required inputs: RHEL 9.8 media, the IBM entitlement key (Ceph and gateway
  image) for Secret `ibm-registry`. The Entitlement `license.accept: true` is
  a placeholder; review IBM's terms before use.

Runbook: [e2e/runbooks/libvirt-5-nodes-ceph-nvmeof.md](../../../e2e/runbooks/libvirt-5-nodes-ceph-nvmeof.md).
