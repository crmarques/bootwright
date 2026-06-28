# IBM Storage Ceph on libvirt — 3-node trial lab (2 full + 1 tie-breaker)

A self-contained Bootwright example that provisions **three libvirt VMs on this
machine**, installs **RHEL 9.7** on them as Bootwright-managed OS, and builds a
**managed IBM Storage Ceph** cluster via cephadm:

| Node | Profile | Ceph roles | OSDs | Purpose |
| --- | --- | --- | --- | --- |
| `ceph-1` | `ceph-full` | mon, mgr, osd, mds, rgw, ingress | 3 | full node (block + file + object) |
| `ceph-2` | `ceph-full` | mon, mgr, osd, mds, rgw, ingress | 3 | full node (block + file + object) |
| `ceph-3` | `ceph-mon`  | mon | 0 | **monitor-only tie-breaker** (quorum) |

All three storage types are configured: **block (RBD)**, **file (CephFS)**, and
**object (RGW with an ingress VIP)**.

The lab also installs in **FIPS mode**: `clusters/storage/ceph-ibm/cluster.yaml`
sets `spec.ceph.security.fips.enabled` and `infra/os/rhel-9-ceph-node.yaml` sets
`customizations.security.fips.enabled`, so each RHEL node is laid down with
`fips=1` on the installer kernel command line. FIPS is an OS-level property —
there is no separate cephadm switch — so Bootwright gates it to the
`redhat`/`ibm` distributions and requires every Ceph node's install profile to
agree. Drop both `security.fips` blocks to build the cluster without FIPS.

## A note on "tie-breaker" vs Ceph "stretch mode"

You asked for 2 full nodes + 1 monitor-only tie-breaker. That is exactly what
this lab builds: **three monitors** (one of them on a dedicated, OSD-less node)
so the cluster keeps mon quorum if one node is lost.

This is **not** Ceph *stretch mode*. Bootwright's stretch validation
(see `specs/state-model.md`) requires **exactly two
data sites with two monitors each, plus a separate tie-breaker site** — i.e. a
minimum of **5 nodes** (4 data + 1 arbiter). A 3-node cluster cannot satisfy
that, so this example uses a flat single-site topology where the third node runs
only the tie-breaking monitor. If you later want true stretch (site-level data
HA), scale to 5 nodes and add the `spec.ceph.topology.stretch` block — see
`examples/baremetal-redfish-multidc-virtualized-odf-ceph` in the Bootwright repo.

Because there are only two OSD hosts, pools replicate with `size: 2, minSize: 2`
across hosts (`clusters/storage/ceph-ibm/placement-policy.yaml`). Losing one OSD
host pauses I/O (quorum still holds); that is expected for a 2-node data tier.

---

## 0. Prerequisites

### 0a. A current `bootwright` binary (important)

This lab uses fields added on `main` (`spec.ceph.release`, `StorageObjectGateway`
`public:`/ingress `address:`), so it needs a current binary. Build the repo's
`bin/bootwright` from `main` and use it:

```bash
cd /path/to/bootwright                  # your clone of the Bootwright repo
make build                              # produces bin/bootwright
bin/bootwright version                  # commit should match `git rev-parse HEAD`
# Optional: put it on PATH so the commands below work as `bootwright`:
sudo install -m 0755 bin/bootwright /usr/local/bin/bootwright
```

All commands below were validated with `bin/bootwright` built from current `main`.
A stale release binary will reject this lab's schema.

### 0b. Host capacity and libvirt

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

### 0c. An SSH key for the libvirt host

Bootwright reaches the host (as `localhost`) over SSH:

```bash
ssh-keygen -t ed25519 -f ~/.ssh/bootwright-ssh-key -N ''
ssh-copy-id -i ~/.ssh/bootwright-ssh-key.pub "$USER@localhost"
ssh -i ~/.ssh/bootwright-ssh-key -o StrictHostKeyChecking=accept-new "$USER@localhost" true
```

---

## 1. Get the credentials (Red Hat + IBM)

You need **three** pieces of credential material. None of it goes in YAML — it is
stored, encrypted, in the Bootwright context with `bootwright secret set`.

### 1a. RHEL trial subscription → org ID + activation key

The RHEL VMs register with `subscription-manager` to enable the RHEL
BaseOS/AppStream repos that cephadm/ceph need.

1. Create a Red Hat account and get RHEL entitlements with either:
   - the **no-cost Red Hat Developer Subscription for Individuals**
     (`https://developers.redhat.com/register` — up to 16 systems), **or**
   - a **60-day RHEL trial** (`https://www.redhat.com/en/technologies/linux-platforms/enterprise-linux/try-it`).
2. In the Hybrid Cloud Console, create an **activation key**:
   `https://console.redhat.com/insights/connector/activation-keys` →
   *Create activation key*. Give it a name (e.g. `ceph-lab`).
3. Note your **Organization ID** (shown on the same activation-keys page, or run
   `subscription-manager identity` on any registered RHEL box). It is a number.

You now have an **org ID** and an **activation key name**.

### 1b. The RHEL 9.7 DVD ISO

Download `rhel-9.7-x86_64-dvd.iso` from
`https://access.redhat.com/downloads/content/rhel` (or the Red Hat Developer
downloads page). Save it somewhere local; you stage it into Bootwright in step 2.

### 1c. IBM Storage Ceph entitlement key (the "IBM-provided" part)

IBM Storage Ceph container images come from IBM's entitled registry
`cp.icr.io/cp`. The RPMs come from IBM's public repo (no auth), but the images
need an **IBM entitlement key**:

1. Create/sign in to an **IBM account** and obtain IBM Storage Ceph
   (trial/eval or entitled): start at
   `https://www.ibm.com/products/storage-ceph`.
2. Get your entitlement key from the **IBM Container Software Library**:
   `https://myibm.ibm.com/products-services/containerlibrary` → *Get entitlement
   key* → *Copy key*.
3. The registry login is username **`cp`** with that key as the password.

> The IBM Storage Ceph license is accepted automatically on each node because
> the `ibm-storage-ceph` entitlement sets `license.accept: true`. That entitlement
> holds only the IBM registry + license and names the separate `rhel` entitlement
> (the org ID + activation key from step 1a) via `rhelEntitlementRef`.

**Version:** the StorageCluster pins `spec.ceph.release: "9"` — IBM Storage Ceph
**9**, the latest published stream (`public.dhe.ibm.com/.../ceph` ships
`ibm-storage-ceph-9-rhel-9.repo` as the newest). `spec.ceph.image` is left unset
so cephadm pulls the latest 9.x image at bootstrap; pin a digest there if you
want a frozen, reproducible version (a `:latest` tag is rejected by validation).

---

## 2. Stage the RHEL ISO into Bootwright

```bash
bootwright media add --name rhel-9.7-x86_64-dvd.iso --from-file /path/to/rhel-9.7-x86_64-dvd.iso
bootwright media list
```

The MachineImage references it as `local-media:rhel-9.7-x86_64-dvd.iso`.

---

## 3. Validate, import the context

Run everything from this directory:

```bash
cd /path/to/bootwright/examples/ceph-ibm-libvirt-lab

bootwright validate -f .
bootwright context init --name ceph-ibm-lab -f .
bootwright context current
bootwright validate             # re-validate the imported context
bootwright secret list          # shows which secrets still need bytes
```

> Run `bootwright` as your normal user, never `sudo bootwright` — it re-escalates
> on its own. After any step, `bootwright status` prints the suggested next
> command.

---

## 4. Set the secrets

```bash
# Red Hat subscription: organization ID and activation key (plain strings).
printf '%s' 'YOUR_ORG_ID'            > /tmp/rhel-org.txt
printf '%s' 'ceph-lab'               > /tmp/rhel-activation-key.txt
bootwright secret set --name rhel-org            --raw-file /tmp/rhel-org.txt
bootwright secret set --name rhel-activation-key --raw-file /tmp/rhel-activation-key.txt
shred -u /tmp/rhel-org.txt /tmp/rhel-activation-key.txt

# IBM entitlement key for cp.icr.io (username "cp", key as the password).
printf '%s\n' 'YOUR_IBM_ENTITLEMENT_KEY' | \
  bootwright secret set --name ibm-ceph-registry --username cp --password-stdin

# Converge the auto-managed secrets: generates the ceph-node-ssh keypair and
# bmc-credentials, and copies the bastion-host-ssh file: source into the context.
bootwright secret generate

# Record SSH host-key trust for the libvirt host, then re-check.
bootwright host trust
bootwright secret list
bootwright validate
```

`bastion-host-ssh` is the operator-owned key at `~/.ssh/bootwright-ssh-key` from
step 0c — declared `file:`, so you do not "set" it.

---

## 5. Check and apply

```bash
bootwright bastion setup --yes      # installs host prerequisites
bootwright preflight all
bootwright plan                     # preview the task graph (no changes)
bootwright apply --yes
bootwright status --watch
```

What apply does, in order:

1. **infra** — defines the NAT'd libvirt network `vbr-ceph-ibm`, brings up the
   dnsmasq resolver on the host, creates the three VMs with emulated Redfish
   BMCs, and installs RHEL 9.7 on each via the anaconda kickstart. (Time sync is
   each node's own `chronyd`, reaching public NTP through the NAT'd network.)
2. **clusters** — on every node: registers with RHSM, enables the RHEL +
   IBM Storage Ceph repos, accepts the IBM license, logs in to `cp.icr.io`,
   installs cephadm, then bootstraps the cluster from `ceph-1`, adds `ceph-2`
   and the `ceph-3` tie-breaker monitor, creates the OSDs, the RBD/CephFS/RGW
   pools, the CephFS filesystem, and the RGW service with its ingress VIP.

Re-running `apply --yes` is idempotent. For a focused storage rerun:
`bootwright apply --stage clusters --clusters ceph-ibm --yes`.

---

## 6. Verify

```bash
# SSH into the seed node with the generated key:
sudo ssh -i /var/lib/bootwright/contexts/ceph-ibm-lab/secrets/ceph-node-ssh \
  root@192.168.140.21 'cephadm shell -- ceph -s'

# Expect: HEALTH_OK, 3 mons (ceph-1, ceph-2, ceph-3), 2 mgr, 6 OSDs,
# 1 cephfs, an rgw service, a mgmt-gateway, and two ingress services
# (rgw.lab + mgmt-gateway.lab). Inspect more:
#   ceph orch host ls
#   ceph osd tree
#   ceph fs status
#   ceph orch ls
```

The S3 endpoint is `http://rgw.ceph.bootwright.test` (RGW ingress VIP
`192.168.140.80`) and the Ceph Dashboard is served HA through the native
`mgmt-gateway` at `https://dashboard.ceph.bootwright.test:8443` (a separate
mgmt-gateway VIP `192.168.140.81`, fronted by a `keepalive_only` ingress).
`bootwright cluster access` reports that dashboard URL plus the admin password
file:

```bash
bootwright cluster access --name ceph-ibm
# Dashboard: https://dashboard.ceph.bootwright.test:8443
# Dashboard user: admin
# Dashboard password file: .../secrets/dashboard-password
```

### Resolving the lab names from your workstation

The lab dnsmasq (the `lab-dns` component on the bastion, `192.168.140.1`) is
authoritative for `*.bootwright.test` — it serves the node FQDNs, the RGW
endpoint, and the dashboard VIP. The Ceph nodes point at it automatically; your
**workstation does not**, so `rgw.ceph.bootwright.test` and
`dashboard.ceph.bootwright.test` will not resolve in your browser until you
point the host at it.

First confirm the record is actually served — this queries the dnsmasq directly,
bypassing your host resolver. If it comes back empty, run `bootwright apply
--yes` first (apply is what generates the records into the dnsmasq config):

```bash
dig +short @192.168.140.1 dashboard.ceph.bootwright.test   # expect 192.168.140.81
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
resolvectl query dashboard.ceph.bootwright.test   # expect 192.168.140.81 via vbr-ceph-ibm
```

If your workstation does not run systemd-resolved, the fallback is a static
`/etc/hosts` entry (`192.168.140.81 dashboard.ceph.bootwright.test`,
`192.168.140.80 rgw.ceph.bootwright.test`) — simpler, but it bypasses the lab
dnsmasq and you maintain it by hand.

---

## 7. Tear down

This environment sets `safety.destroyProtection: requiredOverride`, so `destroy`
refuses to run without `--override` — a routine `destroy --yes` cannot tear it
down by accident.

```bash
bootwright destroy --stage clusters --override --yes   # remove Ceph services/records
bootwright destroy --stage infra --override --yes      # remove VMs, network, services
```

---

## File map

```
environment.yaml                              Environment: secrets, IBM entitlement, lab DNS
infra/providers/libvirt.yaml                  InfraProvider: libvirt + VM profiles + bridge
infra/machines/bastion.yaml                   Machine: the libvirt host (localhost)
infra/networkconfigs/ceph-net.yaml            NetworkConfig: 192.168.140.0/24, static IPs
infra/components/lab-dns.yaml                  InfraComponent: dnsmasq resolver + forwarders
infra/os/rhel-9-x86-64-dvd.yaml               MachineImage: RHEL 9.7 DVD (local-media)
infra/os/rhel-9-ceph-node.yaml                MachineInstallProfile: anaconda RHEL install
clusters/storage/ceph-ibm/cluster.yaml        StorageCluster: distribution ibm, release 9, mgmt-gateway HA dashboard
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
