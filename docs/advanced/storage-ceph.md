---
title: Ceph Storage Clusters
description: Host identity, OSD device selection, cluster config and mgr modules, the monitoring stack, cephadm service passthrough, pools and placement policies, stretch pool inheritance, additive-only convergence, and accessing a managed Ceph storage cluster.
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
  `hostname:` explicitly — a mismatch passes `validate` (which never reaches
  the host) but fails `bootwright preflight`, which compares each storage
  node's real hostname against the declared topology hostname before cephadm
  ever sees the host spec.

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

The drivegroup form mirrors the cephadm OSD service spec field for field:
`dataDevices`, `dbDevices`, and `walDevices` each select by `paths` or
`all: true`, optionally narrowed by `rotational`, `size`, and `limit`
(upstream spellings), alongside `encrypted`, `osdsPerDevice`, and
`crushDeviceClass`.

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

## Cluster configuration and mgr modules

`spec.ceph.config` declares Ceph configuration database options as
`<section>.<key>: <value>` — sections are `global`, `mon`, `mgr`, `osd`,
`mds`, `client`, or a `<type>.<id>` daemon — rendered as idempotent
`ceph config set` operations after bootstrap. `public_network` and
`cluster_network` are owned by `spec.ceph.networks`
(`publicCIDRs`/`clusterCIDRs`) and rejected here.

`spec.ceph.mgrModules[]` declares mgr modules to enable, rendered as
idempotent `ceph mgr module enable` operations. Module settings are plain
config options under the `mgr` section (`mgr/<module>/<key>`):

```yaml
ceph:
  config:
    global:
      osd_pool_default_pg_autoscale_mode: "on"
    mgr:
      mgr/balancer/mode: upmap
  mgrModules:
  - balancer
```

Both surfaces are additive set-operations (see
[Convergence is additive-only](#convergence-is-additive-only)): a key
removed from `config` is never unset on the cluster, and a module removed
from `mgrModules` is never disabled.

## Monitoring stack

`spec.ceph.monitoring` declares the cephadm monitoring stack:

- Absent means the cephadm default stack deploys, with cephadm's own
  placement.
- `enabled: false` renders `cephadm bootstrap --skip-monitoring-stack`; the
  per-service blocks must then be empty.
- An authored `prometheus`, `grafana`, or `alertmanager` block places by the
  topology role of the same name, exactly like `mon`/`mgr`:
  `placement.sites`/`hosts` narrow within the role holders, and the result
  must resolve to at least one host.
- `nodeExporter` deliberately has no role: cephadm deploys it on every host,
  so an authored block narrows by explicit `placement` only.

Service knobs render 1:1 into the cephadm service spec: `port` on any
service, `retentionTime`/`retentionSize` (`retention_time`/`retention_size`)
on `prometheus` only.

```yaml
ceph:
  monitoring:
    prometheus:
      retentionTime: 30d
    grafana:
      port: 3001
  topology:
    hosts:
    - machineRef: ceph-0
      roles: [mon, mgr, osd, prometheus, grafana, alertmanager]
      devices:
      - /dev/vdb
```

## Cephadm service passthrough

`spec.ceph.services[]` is the cephadm service-spec passthrough for service
types Bootwright does not model first-class (`nfs`, `loki`, ...):
`serviceType`, `serviceID`, `placement`, and `spec` render field for field
into a `ceph orch apply` document.

```yaml
ceph:
  services:
  - serviceType: nfs
    serviceID: shares
    placement:
      hosts: [ceph-0, ceph-1]
    spec:
      port: 2049
```

Every service type has exactly one owner. Types owned by a first-class
surface — the topology roles (`mon`, `mgr`, `osd`, `mds`, `rgw`, `ingress`),
`monitoring` (`prometheus`, `grafana`, `alertmanager`, `node-exporter`), and
the gateway kinds — are rejected here; declare them on that surface. With no
role to default from, a passthrough `placement` requires explicit `hosts` or
`sites`.

## Pools and placement policies

`StoragePool` owns one Ceph pool on the cluster named by its
`storageClusterRef`. `spec.ceph.type` is the pool's data-protection
strategy, in the upstream `ceph osd pool create` words: `replicated`
(default) or `erasure`, and the populated arm key equals the type value. An
erasure pool authors `erasure.{dataChunks,codingChunks}` — rendered as the
erasure-code profile `k=`/`m=` — must not set `replicated`, and is not
allowed on stretch-mode clusters:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: StoragePool
metadata:
  name: rgw-data
spec:
  storageClusterRef: ceph-libvirt
  ceph:
    type: erasure
    role: rgw
    erasure:
      dataChunks: 4
      codingChunks: 2
```

The pool's structural identity is its `type` and erasure profile: changing
it is the only desired-state change that rebuilds a live pool
(data-destroying, `--override` only); replicas, CRUSH rule, and application
reconcile in place. `role` (`rbd`, `cephfs-metadata`, `cephfs-data`, `rgw`)
drives `StorageExport` wiring and infers the
`ceph osd pool application enable` value; `application` overrides the
inference.

`StoragePlacementPolicy` owns reusable placement and replicated-pool
defaults for the pools that select it via `placementPolicyRef`: a required
CRUSH `ruleName`, plus `failureDomain` and `replicated.{size,minSize}`. The
referenced policy owns the pool's replication, so a pool with a
`placementPolicyRef` must not also set `ceph.replicated`:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: StoragePlacementPolicy
metadata:
  name: rack-spread
spec:
  storageClusterRef: ceph-libvirt
  ceph:
    ruleName: rack-spread
    failureDomain: rack
    replicated:
      size: 3
      minSize: 2
```

On stretch-mode clusters, policy-less pools inherit the stretch CRUSH rule
and the fixed stretch replication automatically (next section); a
`placementPolicyRef` is needed only for genuinely divergent placement.

`StorageFilesystem` (CephFS pools and MDS placement) and
`StorageObjectGateway` (the RGW service, its storage-owned public endpoint,
and ingress VIPs with `virtualInterfaceNetworks`) complete the per-cluster
surface; the gateway endpoints are covered in
[Networking](networking.md#endpoints).

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
