# Disconnected three-node Ceph (P6)

Profile [P6](../../../specs/ceph-coverage.md#profile-catalog), the input for
[B493](../../../specs/milestones/m2.md#b493) and the B484 core cells: three
RHEL 9.8 guests on a libvirt network with `forward: none` (no default route)
running an IBM Storage Ceph cluster (`ceph-lab`) whose core image, six
auxiliary images, signed RPM repository and publisher key all come from an
operator-prepared mirror at `mirror.lab.example.test`, trusted through the
`mirror-ca` Secret. Prometheus runs on `rhel-01`, Alertmanager on `rhel-02`,
Grafana on `rhel-03`, node-exporter on all three, and RGW plus an HTTP ingress
VIP (`198.51.100.51:8082`) on `rhel-01` and `rhel-02`. Host-independent
validation and planning only; no live acceptance is claimed.

## Worth noting

- Topology: 4 vCPU and 20 GiB RAM per guest, 60 GiB root and one 80 GiB SSD
  data disk. Total 12 vCPUs, 60 GiB RAM; reserve 4 GiB controller memory plus
  mirror storage. The memory covers the 11/11/8 GiB core baseline, 2 GiB OS
  headroom and a provisional 7 GiB auxiliary allowance per guest: a
  qualification budget, not a vendor figure or measured peak.
- Bootwright consumes the mirror and does not install or populate it. The key
  URL and repository share one HTTPS host and port with no redirect; the
  registry serves all blobs locally. The publisher key fingerprint must match
  the pin in
  [native_signatures.py](../../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/native_signatures.py);
  a mirror CA never substitutes for publisher authentication.
- Every image digest in `storage/cluster.yaml` is synthetic
  (`sha256:1111...`); replace each with the verified linux/amd64 manifest
  digest from your mirror. Package selections (`20.2.2-349.el9cp`,
  cephadm-ansible `5.0.3-1.el9cp`) are authored, not qualified.
- Call Home stays `enabled` (the selected 20.2.x build); with public egress
  blocked expect a connectivity warning. Offline reporting is not claimed.
- Controller side: `setup --download-only --dest-dir` on a connected host and
  `--wheel-mirror` on the isolated one prepare automation wheels only. The
  ingress is HTTP; add a qualified TLS binding before sensitive object data.
- Required inputs: `mirror-ca` and the `ibm-registry` account for the mirror
  (not necessarily IBM's), RHEL 9.8 boot and DVD ISOs, local DNS and NTP. The
  Entitlement `license.accept: true` is a placeholder; review IBM's terms.

Runbook: [e2e/runbooks/libvirt-3-nodes-ceph-disconnected.md](../../../e2e/runbooks/libvirt-3-nodes-ceph-disconnected.md).
