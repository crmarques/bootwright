# IBM Storage Ceph on libvirt — 3-node trial lab (2 full + 1 tie-breaker)

A self-contained Bootwright example that provisions **three libvirt VMs on this
machine**, installs **RHEL 9.8** on them as Bootwright-managed OS, and builds a
**managed IBM Storage Ceph** cluster via cephadm: two full nodes
(mon/mgr/osd/mds/rgw/ingress, 3 OSDs each) plus one **monitor-only tie-breaker**.
All three storage types are configured — **block (RBD)**, **file (CephFS)**, and
**object (RGW with an ingress VIP)** — and the nodes are laid down in **FIPS
mode**.

**[Provisioning a Ceph cluster](../../docs/getting-started/ceph.md) is the
step-by-step guide for this tree.** It carries the node table, what every object
in the tree does, the values you must edit, the credentials to obtain, the
`secret set` commands, and the validate/apply/verify walkthrough — including how
to drop FIPS. This file covers only what is specific to running the lab: host
capacity, the sidecar-image caveat, resolving the lab names from your
workstation, teardown, and the file map.

Install the CLI first — see
[Installation and Setup](../../docs/getting-started/installation.md#install-the-cli).
`v1alpha1` is still moving, so a released binary can reject an example tree taken
from `main`; that page's build-from-source option is the fix.

Because there are only two OSD hosts, pools replicate with `size: 2, minSize: 2`
across hosts (`clusters/storage/ceph-ibm/placement-policy.yaml`). Losing one OSD
host pauses I/O while mon quorum still holds; that is expected for a 2-node data
tier. This is **not** Ceph *stretch mode* — for site-level data HA see
[Ceph topologies](../../docs/advanced/ceph-topologies.md) and
`examples/baremetal-redfish-multidc-virtualized-odf-ceph`.

---

## Host capacity and libvirt

The VMs are deliberately **as lean as possible** for a provisioning test on a
laptop. Budget roughly:

- **RAM:** ~10 GiB free (4 GiB × 2 full nodes + 2 GiB mon).
- **vCPU:** ~5 (2 × 2 + 1).
- **Disk:** ~104 GiB ((20 GiB root + 3 × 8 GiB OSD) × 2 + 16 GiB).
- **Internet egress from the host** — the libvirt network NATs the VMs out, and
  the nodes must reach `subscription.rhsm.redhat.com`, `cdn.redhat.com`,
  `cp.icr.io`, and `public.dhe.ibm.com`.

These are minimums: each full node co-locates mon/mgr/osd/mds/rgw/ingress on
4 GiB, which is tight. If daemons OOM during bootstrap, raise `memoryMiB` in
`infra/providers/libvirt.yaml` (`machineProfiles`). For an even leaner block-only
smoke test, drop the CephFS/RGW objects and the `mds/rgw/ingress` roles.

Install the host tooling:

```bash
sudo dnf install -y libvirt qemu-kvm virtinst openssh-clients util-linux
sudo systemctl enable --now libvirtd
# Confirm the system libvirt URI works:
virsh -c qemu:///system list --all
```

---

## Before apply: the sidecar images must be pullable

This lab turns on the monitoring stack and two HA VIP ingresses (the RGW S3
endpoint and the `mgmt-gateway` dashboard), so cephadm deploys
Prometheus/Grafana/Alertmanager/node-exporter plus haproxy/keepalived. On the
**IBM** distribution these sidecar images are not guaranteed to resolve from the
entitled registry. Before `apply`, make sure the nodes can pull them —
`podman login cp.icr.io` and/or pin each to an entitled `cp.icr.io` reference via
`spec.ceph.config` under the `mgr` section
(`mgr/cephadm/container_image_prometheus`, `…_grafana`, `…_alertmanager`,
`…_node_exporter`, `…_haproxy`, `…_keepalived`) in
`clusters/storage/ceph-ibm/cluster.yaml` — otherwise the monitoring stack and HA
ingresses will not deploy.

---

## Verify

```bash
bootwright cluster list
bootwright cluster info --name ceph-ibm
```

`cluster info` prints the seed node, the SSH/health-check/cluster-shell commands
to run against it, the dashboard URL and user, and — under `--secrets` only — the
dashboard password:

```text
Dashboard: http://dashboard.ceph-ibm.bootwright.test:8443
Dashboard user: admin
Show password: bootwright cluster info --name ceph-ibm --secrets
```

Run the health check it prints, or let Bootwright resolve the node identity for
you and open a shell on the seed node:

```bash
bootwright cluster rsh --name ceph-ibm --node node-01
```

Expect `HEALTH_OK`, 3 mons (`node-01`, `node-02`, `node-03`), 2 mgr, 6 OSDs, 1
CephFS, an RGW service, a mgmt-gateway, and two ingress services (`rgw.lab` +
`mgmt-gateway.lab`). The S3 endpoint is `http://rgw.ceph-ibm.bootwright.test`
(RGW ingress VIP `192.168.140.80`); the dashboard is served HA through the
native `mgmt-gateway` at `http://dashboard.ceph-ibm.bootwright.test:8443` (a
separate VIP `192.168.140.81`, fronted by a `keepalive_only` ingress).

### Resolving the lab names from your workstation

The lab dnsmasq (the `lab-dns` component on the bastion, `192.168.140.1`) is
authoritative for `*.bootwright.test` — it serves the node FQDNs, the RGW
endpoint, and the dashboard VIP. The Ceph nodes point at it automatically; your
**workstation does not**, so `rgw.ceph-ibm.bootwright.test` and
`dashboard.ceph-ibm.bootwright.test` will not resolve in your browser until you
point the host at it.

First confirm the record is actually served — this queries the dnsmasq directly,
bypassing your host resolver. If it comes back empty, run `bootwright apply
--yes` first (apply is what generates the records into the dnsmasq config):

```bash
dig +short @192.168.140.1 dashboard.ceph-ibm.bootwright.test   # expect 192.168.140.81
```

Then add **split DNS** on the host so that only `*.bootwright.test` is sent to
the lab dnsmasq and everything else stays on your normal upstream. On a
systemd-resolved + NetworkManager host (the Fedora default), attach a
routing-only domain (the `~` prefix) to the lab bridge. The bridge is
`vbr-<storageClusterName>` — here `vbr-ceph-ibm`; find it with
`ip -br addr | grep 192.168.140.1`:

```bash
sudo nmcli connection modify vbr-ceph-ibm ipv4.dns 192.168.140.1
sudo nmcli connection modify vbr-ceph-ibm ipv4.dns-search '~bootwright.test'
sudo nmcli connection up vbr-ceph-ibm
```

That persists across reboots. For a quick, runtime-only test instead:

```bash
sudo resolvectl dns vbr-ceph-ibm 192.168.140.1
sudo resolvectl domain vbr-ceph-ibm '~bootwright.test'
```

Verify the host now resolves it, then open the URL (the dashboard serves a
self-signed cert, so accept the browser warning):

```bash
resolvectl query dashboard.ceph-ibm.bootwright.test   # expect 192.168.140.81 via vbr-ceph-ibm
```

If your workstation does not run systemd-resolved, the fallback is a static
`/etc/hosts` entry (`192.168.140.81 dashboard.ceph-ibm.bootwright.test`,
`192.168.140.80 rgw.ceph-ibm.bootwright.test`) — simpler, but it bypasses the lab
dnsmasq and you maintain it by hand.

---

## Tear down

This environment sets `safety.destroyProtection: protected`, so `destroy`
refuses to run without `--authorize protected` — a routine `destroy --yes` cannot tear it
down by accident. The OSD hosts are libvirt VMs whose disks hold the OSD data, so
both stages also cross the data-loss gate and need `--authorize data-loss`.

One command tears down the whole lab in the inverse of the apply order:

```bash
bootwright destroy --authorize protected,data-loss --yes
```

Stage by stage, when you want to keep the VMs standing between teardowns:

```bash
bootwright destroy --stage clusters --authorize protected,data-loss --yes   # remove Ceph services/records
bootwright destroy --stage infra --authorize protected,data-loss --yes     # remove VMs, network, services
```

---

## File map

```
environment.yaml                              Environment: domains, machine access, lab DNS, destroy protection
secrets.yaml                                  Secrets: bastion-host-ssh, ceph-cluster-ssh, bootwright-machine-key, bmc-credentials, rhel-org, rhel-activation-key, ibm-ceph-registry
infra/entitlements/rhel.yaml                  Entitlement: RHEL subscription (org + activation key)
infra/entitlements/ibm-storage-ceph.yaml      Entitlement: IBM registry + license acceptance
infra/providers/libvirt.yaml                  InfraProvider: libvirt + VM profiles + bridge
infra/machines/bastion.yaml                   Machine: the libvirt host (localhost)
infra/networkconfigs/ceph-net.yaml            NetworkConfig: 192.168.140.0/24, static IPs
infra/components/lab-dns.yaml                 InfraComponent: dnsmasq resolver + forwarders
infra/images/rhel-9-x86-64-dvd.yaml           MachineImage: RHEL 9.8 DVD (local-media)
infra/profiles/rhel-9-ceph-node.yaml          MachineInstallProfile: anaconda RHEL install
clusters/storage/ceph-ibm/cluster.yaml        StorageCluster: distribution ibm, release 9.9.1.0, mgmt-gateway HA dashboard
clusters/storage/ceph-ibm/nodes/ceph-{1,2,3}.yaml  Machines: ceph-1, ceph-2 (full), ceph-3 (mon)
clusters/storage/ceph-ibm/placement-policy.yaml  size 2 / minSize 2, failureDomain host
clusters/storage/ceph-ibm/pools/*.yaml        StoragePools: rbd, cephfs-data/metadata, rgw
clusters/storage/ceph-ibm/filesystems/cephfs.yaml  StorageFilesystem (CephFS)
clusters/storage/ceph-ibm/object-gateways/rgw.yaml StorageObjectGateway (RGW + ingress VIP)
```

To change the network, edit the `192.168.140.*` addresses in
`infra/networkconfigs/ceph-net.yaml`, `infra/machines/bastion.yaml`,
`clusters/storage/ceph-ibm/nodes/*.yaml`, `infra/components/lab-dns.yaml`, and
`clusters/storage/ceph-ibm/{cluster.yaml,object-gateways/rgw.yaml}`.
