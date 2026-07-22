# IBM Storage Ceph on bare metal — 3-node cluster (Redfish BMC, RHEL pre-install)

A Bootwright example that builds a **managed IBM Storage Ceph** cluster on
**three physical servers**. Bootwright drives each server's **Redfish BMC** to
**install RHEL 9.8** (anaconda + virtual media) *before* cephadm runs, then
bootstraps IBM Storage Ceph on the freshly installed OS.

It is wired for an **enterprise network**: an outbound **HTTP/HTTPS proxy**,
**external DNS**, and **external NTP** — Bootwright consumes them, it does not
run them. The only managed service is a small **artifact server** on the bastion
that serves the RHEL ISO to the BMCs.

| Machine | Node | Ceph roles | OSDs | Purpose |
| --- | --- | --- | --- | --- |
| `ceph-1` | `node-01` | mon, mgr, osd, mds, rgw, ingress | 3 | full node (block + file + object) |
| `ceph-2` | `node-02` | mon, mgr, osd, mds, rgw, ingress | 3 | full node (block + file + object) |
| `ceph-3` | `node-03` | mon | 0 | **monitor-only tie-breaker** (quorum) |

The machines keep their `Machine` names (`ceph-1`…`ceph-3`); the cluster names
its nodes `node-01`–`node-03` (declared explicitly as `topology.nodes[].name`), and
the cluster YAML references nodes — `bootstrap.node: node-01`, placements on
`node-01`/`node-02` — never machine names.

All three storage types are configured: **block (RBD)**, **file (CephFS)**, and
**object (RGW with an ingress VIP)**, plus an HA **Ceph Dashboard** behind the
IBM `mgmt-gateway` on its own VIP.

> **Not stretch mode.** This is a flat single-site cluster with three monitors
> (one on a dedicated OSD-less node). True Ceph stretch mode needs two data
> sites with two mons each plus an arbiter (5+ nodes); see
> `examples/baremetal-redfish-multidc-virtualized-odf-ceph`. Because there are
> only two OSD hosts, pools replicate `size 2 / minSize 2` across hosts
> (`placement-policy.yaml`): losing one OSD host pauses I/O (quorum still holds).

---

## Addresses used by this example

Edit these to match your site (search-and-replace `example.com` and the
`10.20.30.0/24` addresses across the files):

| Thing | Value |
| --- | --- |
| Base domain | `example.com` |
| Machine network / gateway | `10.20.30.0/24` / `10.20.30.1` |
| Bastion + artifact server | `bastion.example.com` → `10.20.30.10` |
| `ceph-1` / `ceph-2` / `ceph-3` | `10.20.30.21` / `.22` / `.23` |
| BMCs | `ceph-{1,2,3}-bmc.example.com` (Redfish) |
| RGW S3 endpoint (ingress VIP) | `rgw.ceph-ibm.example.com` → `10.20.30.80` |
| Dashboard (mgmt-gateway VIP) | `dashboard.ceph-ibm.example.com:8443` → `10.20.30.81` |
| NFS export (ingress VIP) | `10.20.30.82` |
| External DNS resolvers | `10.20.30.2`, `10.20.30.3` |
| External NTP servers | `ntp1.example.com`, `ntp2.example.com` |
| Outbound proxy | `http://proxy.example.com:3128` |
| OSD devices (full nodes) | `/dev/sdb`, `/dev/sdc`, `/dev/sdd` (root: `/dev/sda`) |

---

## 0. Prerequisites

### 0a. A current `bootwright` binary

This example uses fields on `main` (`spec.ceph.release`, the `mgmt-gateway`
`management` block, external proxy/NTP/DNS catalog entries). Build and use the
repo's binary:

```bash
cd /path/to/bootwright
make build                  # produces bin/bootwright
sudo install -m 0755 bin/bootwright /usr/local/bin/bootwright   # optional
```

### 0b. The hardware and network

- **Three x86_64 servers** with a Redfish BMC (iDRAC/iLO/OpenBMC) that supports
  **virtual media**. Each has a NIC on `10.20.30.0/24` and blank data disks
  (`/dev/sdb..sdd` on the two full nodes) for OSDs.
- A **bastion host** (existing RHEL/Fedora, provided OS) on `10.20.30.10` that
  can run **podman** and is reachable over **HTTPS from the BMCs** — it hosts the
  artifact server that serves the RHEL ISO as virtual media.
- The machine network's **default route reaches the proxy, DNS, and NTP**, and
  reaches RHSM / `cp.icr.io` / IBM's repo host **through the proxy**.

### 0c. External DNS records (you create these)

Because DNS is external, add these records to your site resolvers
(`10.20.30.2/.3`) **before** apply:

```
ceph-1.example.com.            A   10.20.30.21     # machine fqdn names
ceph-2.example.com.            A   10.20.30.22
ceph-3.example.com.            A   10.20.30.23
node-01.ceph-ibm.example.com.   CNAME ceph-1.example.com.   # node FQDNs -> machines
node-02.ceph-ibm.example.com.   CNAME ceph-2.example.com.
node-03.ceph-ibm.example.com.   CNAME ceph-3.example.com.
ceph-1-bmc.example.com.        A   <ceph-1 BMC IP>
ceph-2-bmc.example.com.        A   <ceph-2 BMC IP>
ceph-3-bmc.example.com.        A   <ceph-3 BMC IP>
bastion.example.com.           A   10.20.30.10
rgw.ceph-ibm.example.com.      A   10.20.30.80     # RGW ingress VIP
dashboard.ceph-ibm.example.com. A  10.20.30.81     # mgmt-gateway VIP
```

`bootwright preflight` runs a **Name resolution** group that resolves each
machine's `fqdn` name (`ceph-N.example.com`) and each node FQDN
(`node0N.ceph-ibm.example.com`) and fails naming the exact record to create if
one is missing or wrong.

### 0d. An SSH key for the bastion

```bash
ssh-keygen -t ed25519 -f ~/.ssh/bootwright-ssh-key -N ''
ssh-copy-id -i ~/.ssh/bootwright-ssh-key.pub <user>@bastion.example.com
```

---

## 1. Credentials you need

None of this goes in YAML — it lives encrypted in the Bootwright context.

1. **RHEL subscription** — an **organization ID** and an **activation key**
   (no-cost *Red Hat Developer Subscription* or a RHEL trial). The machines-phase
   registration task registers each node with `subscription-manager` right after
   the RHEL install; the clusters-stage Ceph work then enables the
   BaseOS/AppStream repos cephadm needs. (Setting the RHEL entitlement's
   `rhsm.management: external` instead delegates registration to a corporate
   `ProvisioningPlaybook` — see `examples/ceph-external-rhsm`.)
2. **IBM Storage Ceph entitlement key** — from the IBM Container Software Library
   (`https://myibm.ibm.com/products-services/containerlibrary`). The registry
   login is username **`cp`** with that key as the password. (The IBM license is
   accepted automatically because the entitlement sets `license.accept: true`;
   the StorageCluster explicitly keeps IBM Call Home disabled.)
3. **BMC username/password** — the Redfish account on each server's BMC.
4. **Proxy username/password** — if your proxy authenticates. If it does not,
   drop the `auth:` block and the `proxy-credentials` secret from
   `environment.yaml`.
5. **The RHEL 9.8 DVD ISO** — `rhel-9.8-x86_64-dvd.iso` from
   `https://access.redhat.com/downloads/content/rhel`.

---

## 2. Stage the RHEL ISO

```bash
bootwright media add --name rhel-9.8-x86_64-dvd.iso --from-file /path/to/rhel-9.8-x86_64-dvd.iso
bootwright media list
```

The MachineImage references it as `local-media:rhel-9.8-x86_64-dvd.iso`.

---

## 3. Validate and import the context

```bash
cd /path/to/bootwright/examples/ceph-ibm-baremetal-redfish

bootwright validate -f .
bootwright context init --name ceph-ibm-baremetal -f .
bootwright context current
bootwright secret list          # shows which secrets still need bytes
```

> Run `bootwright` as your normal user, never `sudo bootwright` — it re-escalates
> on its own. After any step, `bootwright status` prints the suggested next
> command.

---

## 4. Set the secrets

```bash
# Red Hat subscription (plain strings).
printf '%s' 'YOUR_ORG_ID'             > /tmp/rhel-org.txt
printf '%s' 'YOUR_ACTIVATION_KEY'     > /tmp/rhel-activation-key.txt
bootwright secret set --name rhel-org            --raw-file /tmp/rhel-org.txt
bootwright secret set --name rhel-activation-key --raw-file /tmp/rhel-activation-key.txt
shred -u /tmp/rhel-org.txt /tmp/rhel-activation-key.txt

# IBM entitlement key for cp.icr.io (username "cp", key as the password).
printf '%s\n' 'YOUR_IBM_ENTITLEMENT_KEY' | \
  bootwright secret set --name ibm-ceph-registry --username cp --password-stdin

# Real Redfish BMC account, and the proxy account (drop this one if the proxy
# is unauthenticated).
printf '%s\n' 'YOUR_BMC_PASSWORD'   | bootwright secret set --name bmc-credentials  --username <bmc-user>   --password-stdin
printf '%s\n' 'YOUR_PROXY_PASSWORD' | bootwright secret set --name proxy-credentials --username <proxy-user> --password-stdin

# Converge the auto-managed secrets: generates the ceph-node-ssh keypair and
# copies the bastion-host-ssh file into the context.
bootwright secret generate

# Record SSH host-key trust for the bastion, then re-check.
bootwright machine trust
bootwright secret list
bootwright validate
```

`bastion-host-ssh` is the operator-owned key at `~/.ssh/bootwright-ssh-key` from
step 0d — declared `file:`, so you do not "set" it.

---

## 5. Check and apply

```bash
bootwright bastion setup --yes      # installs the artifact-server host prerequisites
bootwright preflight all
bootwright plan                     # preview the task graph (no changes)
bootwright apply --yes
bootwright status --watch
```

What apply does, in order:

1. **infra** — starts the artifact server on the bastion, then for each node
   drives its Redfish BMC to mount the RHEL 9.8 ISO as virtual media and runs the
   **anaconda install** (static IP on `10.20.30.0/24`, external DNS, NTP via
   chrony to the external servers, proxy for outbound). With RHEL in place, the
   machines-phase registration task registers each node with RHSM through the
   proxy.
2. **clusters** — on every node (through the proxy): enables the RHEL +
   IBM Storage Ceph repos, accepts the IBM license, logs in to `cp.icr.io`,
   installs cephadm, then bootstraps from `node-01`, adds `node-02` and the
   `node-03` tie-breaker monitor, creates the OSDs, the RBD/CephFS/RGW pools, the
   CephFS filesystem, the RGW service with its ingress VIP, the NFS export
   service with its ingress VIP, and the `mgmt-gateway` dashboard with its VIP.

Re-running `apply --yes` is idempotent. For a focused storage rerun:
`bootwright apply --stage clusters --clusters ceph-ibm --yes`.

---

## 6. Verify

```bash
# SSH into the seed node with the generated key:
sudo ssh -i /var/lib/bootwright/contexts/ceph-ibm-baremetal/secrets/ceph-node-ssh \
  root@10.20.30.21 'cephadm shell -- ceph -s'
# Expect HEALTH_OK, 3 mons, 2 mgr, 6 OSDs, 1 cephfs, an rgw service, an nfs
# service, a mgmt-gateway, and three ingress services.

bootwright cluster info --name ceph-ibm
# Dashboard: https://dashboard.ceph-ibm.example.com:8443  (user admin; password file printed)
```

The S3 endpoint is `http://rgw.ceph-ibm.example.com` (RGW ingress VIP
`10.20.30.80`). Both names resolve from the external DNS records you added in
step 0c.

---

## 7. Tear down

This environment sets `safety.destroyProtection: requiredOverride`, so `destroy`
refuses to run without `--force`.

```bash
bootwright destroy --stage clusters --force --yes   # remove Ceph services
bootwright destroy --stage infra    --force --yes   # power off / deprovision nodes
```

---

## File map

```
environment.yaml                              Environment: secrets, IBM entitlement,
                                              proxy + external DNS/NTP, artifact access
infra/providers/baremetal.yaml                InfraProvider: baremetal, external boot
infra/machines/bastion.yaml                   Machine: the artifact-server host (provided OS)
infra/components/artifact-server.yaml         InfraComponent: HTTPS ISO server for the BMCs
infra/networkconfigs/ceph-net.yaml            NetworkConfig: 10.20.30.0/24, external DNS refs
infra/os/rhel-9-8-dvd.yaml                    MachineImage: RHEL 9.8 DVD (local-media)
infra/os/rhel-9-ceph-node.yaml                MachineInstallProfile: anaconda RHEL install
clusters/storage/ceph-ibm/cluster.yaml        StorageCluster: distribution ibm, release 9.9.1,
                                              mgmt-gateway HA dashboard
clusters/storage/ceph-ibm/placement-policy.yaml  size 2 / minSize 2, failureDomain host
clusters/storage/ceph-ibm/nodes/ceph-{1,2,3}.yaml Machines: BMC + Redfish virtual media
clusters/storage/ceph-ibm/pools/*.yaml        StoragePools: rbd, cephfs-data/metadata, rgw
clusters/storage/ceph-ibm/filesystems/cephfs.yaml  StorageFilesystem (CephFS)
clusters/storage/ceph-ibm/object-gateways/rgw.yaml StorageObjectGateway (RGW + ingress VIP)
clusters/storage/ceph-ibm/nfs-exports/nfs.yaml    StorageNFSExport (NFS-Ganesha + ingress VIP)
```
