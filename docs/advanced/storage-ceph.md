---
title: Ceph Storage Clusters
description: Host identity, OSD device selection, stretch pool inheritance, additive-only convergence, and accessing a managed Ceph storage cluster.
---

# Ceph storage clusters

A `StorageCluster` of `type: ceph` with `management: managed` is bootstrapped by
Bootwright with `cephadm`. Ceph keeps no kubeconfig-style admin file on the
controller — the admin keyring and `ceph.conf` live on the seed node — so
day-to-day access is by SSH to the seed node plus `cephadm shell`.

## Host identity

cephadm registers every `spec.ceph.topology.hosts[]` entry under its
`hostname`, which defaults to the `machineRef` name. The name is rendered
verbatim into the cephadm host spec and must equal the host's real OS
hostname:

- For machines whose OS Bootwright installs, the contract holds by
  construction: the installer sets the OS hostname to the `Machine` name.
- For `os.provided: true` machines, the operator guarantees it. If a
  machine's real hostname differs from its `Machine` name, author
  `hostname:` explicitly — a mismatch passes `validate` and surfaces only at
  apply, as a cephadm host-add failure.

Because the default follows the `Machine` name, renaming a `Machine` also
renames the Ceph host identity of every host entry that left `hostname`
unauthored. On a live cluster that makes the rendered topology name a host
cephadm has never seen; pin `hostname:` to the original name before renaming.

## OSD device selection

Every `spec.ceph.topology.hosts[]` entry carrying the `osd` role must say which
disks it contributes: either the lean `devices:` list of literal paths or the
drivegroup-shaped `osd:` selection. `bootwright validate` rejects an osd-role
host that authors neither, and rejects either form on a host without the `osd`
role. There is no implicit all-devices default — handing every available
(blank) disk on a host to Ceph is the explicit opt-in
`osd: {dataDevices: {all: true}}`:

```yaml
hosts:
- machineRef: ceph-0
  roles: [mon, mgr, osd]
  devices:            # literal paths ...
  - /dev/vdb
- machineRef: ceph-1
  roles: [osd]
  osd:                # ... or a drivegroup-shaped selection
    dataDevices:
      all: true       # explicit opt-in: every available device becomes an OSD
```

## Production best practices

Bootwright accepts small and single-node Ceph clusters for labs, but
`bootwright validate` emits **warnings** (it never blocks apply) when a cluster
departs from the layout IBM Storage Ceph recommends for production:

- **Monitors** — give the `mon` role to at least **3 hosts**, and keep the count
  **odd** (3, 5, 7) so the cluster holds quorum through a monitor failure.
- **Managers** — give the `mgr` role to at least **2 hosts** for an
  active/standby pair; a single manager is a single point of failure for
  orchestration, the dashboard, and metrics.
- **Pinned image (`redhat`/`ibm`)** — set `spec.ceph.image` to a digest-pinned
  reference. Left unset, the install uses the distribution-packaged `cephadm`'s
  default image tag, which floats, so the running Ceph version is not
  reproducible across re-installs.

### Networks

`cephadm bootstrap` runs all Ceph traffic on the **public** network by default.
For production, IBM recommends a dedicated **cluster** network so OSD
replication, recovery, and heartbeat traffic does not contend with client I/O.
Declare both under `spec.ceph.networks`: the public CIDRs seed `public_network`
before the first monitor binds, and the cluster CIDRs render
`cephadm bootstrap --cluster-network`.

```yaml
ceph:
  networks:
    publicCIDRs:  [10.0.10.0/24]   # client + monitor traffic
    clusterCIDRs: [10.0.20.0/24]   # OSD replication/recovery (production)
```

Omit `clusterCIDRs` to keep IBM's default of one network carrying everything.

## Stretch mode re-rules policy-less pools

`spec.ceph.topology.stretch` is enabled by presence, and two-site stretch
comes with fixed pool replication: Ceph requires `size: 4` / `minSize: 2`.
Every `StoragePool` without a `placementPolicyRef` inherits the stretch CRUSH
rule and that replication — including pools that pre-date the stretch block.

Authoring `stretch` on an existing cluster therefore re-rules and resizes
every policy-less pool on the next apply, with no change to any `StoragePool`
file, and Ceph rebalances the data accordingly. `bootwright validate` prints
a one-line notice naming the inheriting pools, and the resulting
`ceph osd pool set` commands are visible in the rendered
`ceph/operations.yaml`.

## Convergence is additive-only

`apply` creates and converges what desired state declares and never removes a
live Ceph object whose declaration was deleted. The rule is storage-wide,
covering every surface: `spec.ceph.config` keys, `mgrModules[]`, `monitoring`
services, `services[]` passthrough entries, and the `StoragePool`,
`StorageFilesystem`, and `StorageObjectGateway` kinds. Deleting one from git
and running `bootwright apply` reconciles cleanly while the live pool,
filesystem, or service keeps running — remove it on the cluster with the
`ceph`/`cephadm` CLI when you mean it.

`--override` does not prune undeclared objects either: it rebuilds only
still-declared pools whose structural identity (pool `type` and erasure
profile) changed. Removal semantics under `--override` are an open design
item; until they land, treat removal as out of band.

!!! note "Storage sub-objects are not orphan-tracked"
    `bootwright state-check` correlates whole clusters, machines, and
    providers, so a deleted pool, filesystem, gateway, or passthrough service
    inside a still-declared `StorageCluster` does not appear under
    **"Owned but no longer declared"**.

## Access details

`bootwright cluster access` prints everything needed to reach a managed Ceph
cluster, derived entirely from desired state:

```text
$ bootwright cluster access --cluster ceph-libvirt

Storage cluster ceph-libvirt
  Type: ceph (managed)
  Seed node: ceph-0
  SSH: ssh root@192.168.134.20
  Monitors: ceph-0=192.168.134.20:6789, ceph-1=192.168.134.21:6789, ceph-2=192.168.134.22:6789
  Health check: ssh root@192.168.134.20 sudo cephadm shell -- ceph -s
  Cluster shell: ssh root@192.168.134.20 sudo cephadm shell
  Dashboard: https://192.168.134.20:8443
  Dashboard user: admin
  Dashboard password file: /var/lib/bootwright/contexts/<ctx>/clusters/ceph-libvirt/secrets/dashboard-password
  Show dashboard password: sudo cat /var/lib/bootwright/contexts/<ctx>/clusters/ceph-libvirt/secrets/dashboard-password
  [OK] dashboard password: OK /var/lib/bootwright/contexts/<ctx>/clusters/ceph-libvirt/secrets/dashboard-password
  [INFO] health: run the health check to confirm Ceph reports HEALTH_OK
```

Run the **Health check** line; `HEALTH_OK` from `ceph -s` confirms the cluster is
reachable and healthy.

## Dashboard credentials

`cephadm bootstrap` enables the Ceph dashboard and generates a one-time random
`admin` password, printed once during bootstrap. Bootwright captures that
password **during the install** and saves it on the controller exactly like the
kubeadmin password for OpenShift clusters:

```text
clusters/<storage-cluster>/secrets/dashboard-password   # mode 0600
```

View it without printing it anywhere else, then log in at the **Dashboard** URL as
`admin`:

```bash
sudo cat /var/lib/bootwright/contexts/<ctx>/clusters/ceph-libvirt/secrets/dashboard-password
```

!!! note "Captured at install only"
    The password is captured solely from the one-time `cephadm bootstrap`. It is
    never re-read or re-synced on later applies, and — like every secret —
    `cluster access` only ever shows its file path and a `sudo cat` command,
    never the bytes. As with the kubeadmin password, the file persists after the
    cluster is destroyed; delete the cluster's `secrets/` directory by hand if you
    want the credential gone.

## Recovering the dashboard password

If the `dashboard-password` file is lost, or the in-cluster password was changed
and no longer matches the stored copy, reset it directly on the cluster. The
`ceph` CLI is on the seed node's PATH after bootstrap, so no `cephadm shell` is
needed:

```bash
# SSH to the seed node (the SSH line from cluster access)
ssh root@192.168.134.20

# Set a new admin password. Modern Ceph requires the password to be supplied
# from a file via -i (a positional password argument is rejected), and enforces
# a policy: at least 8 characters and not a common word.
umask 077
printf 'NewStr0ngPassw0rd' > /tmp/dash-pass
sudo ceph dashboard ac-user-set-password admin -i /tmp/dash-pass
rm -f /tmp/dash-pass

# confirm the dashboard URL the active mgr is serving
sudo ceph mgr services
```

To keep `bootwright cluster access` accurate, write the same value back to
the stored file on the controller:

```bash
P=/var/lib/bootwright/contexts/<ctx>/clusters/ceph-libvirt/secrets/dashboard-password
printf 'NewStr0ngPassw0rd' | sudo tee "$P" >/dev/null
sudo chmod 0600 "$P"
```

A clean reinstall (`bootwright apply ... --override`, which clears `/etc/ceph` and
re-bootstraps) re-captures a fresh dashboard password into the stored file
automatically.
