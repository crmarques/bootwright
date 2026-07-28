---
title: Storage
description: StorageCluster and the Ceph sub-objects — managed cephadm or imported clusters, pools, filesystems, gateways, and exports.
---

# Storage

Storage is a separate domain from [`ContainerCluster`](container-clusters.md):
Ceph is never a cluster field, it is its own set of kinds the
[`Environment`](environment.md) selects and binds. Two operating modes share the
same `StorageCluster` kind:

- **Managed Ceph** (`management: managed`) — Bootwright bootstraps and converges
  Ceph on selected machines via **cephadm**, then applies pools, filesystems,
  RGW, and prepares downstream export data.
- **Imported / external Ceph** (`management: external`) — Bootwright consumes an
  operator-supplied cluster through a [`StorageExport`](#storageexport) and skips
  the cephadm storage task entirely.

A `StorageCluster` references machines **by node**: each topology node names a
[`Machine`](machines.md) with the `ceph-node` capability. A storage export can
then feed an [add-on](add-ons.md) input — for example an OpenShift Data
Foundation external-mode attachment that names a `StorageExport` for one
installed cluster.

See [conventions](index.md) for the object envelope and the Required/Default
field-table convention every table below follows.

!!! warning "Storage convergence is additive-only"
    Across the whole storage domain, `apply` creates and converges what desired
    state declares and **never** removes a live Ceph object whose declaration was
    deleted — pools, filesystems, gateways, passthrough services, mgr modules,
    and config keys keep running until removed on the cluster out of band.
    `apply --converge-drifted` does not prune undeclared objects either; it rebuilds only
    still-declared pools whose structural identity changed. See
    [Operations and recovery](../advanced/operations.md) for removal patterns.

A minimal managed cluster and its OSS distribution:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: StorageCluster
metadata:
  name: ceph-oss
spec:
  type: ceph
  ceph:
    distribution: oss
    release: "20.2.2"
    cephadm:
      bootstrap:
        node: node-01
    topology:
      nodes:
        - name: node-01
          machineRef: ceph-0
          roles:
            - mon
```

An imported cluster carries no `ceph` block:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: StorageCluster
metadata:
  name: imported-ceph
spec:
  type: ceph
  management: external
```

## StorageCluster

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.type` | Yes | — | Currently `ceph`. |
| `spec.management` | No | `managed` | `managed` or `external`. External (imported) clusters are consumed through `StorageExport` and skip the cephadm storage task entirely. |
| `spec.ceph` | When `management: managed` | — | Ceph distribution, cephadm bootstrap, networks, topology, config, monitoring, and passthrough services. |

!!! note "Cross-field rules"
    - `spec.ceph` is **required when** the cluster is managed and **must be
      empty** for external clusters.
    - A `Machine` is node-bound by at most one cluster (and at most one node
      entry) across every `ContainerCluster` and `StorageCluster`.

### Ceph

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `ceph.distribution` | No | `oss` | One of `oss`, `redhat`, or `ibm`. |
| `ceph.release` | No | `20.2.2` (`oss`); `9.1` (`redhat`); `9.9.1.0` (`ibm`) | Ceph release for the chosen distribution. Bootwright keeps no list of releases and never checks yours against one: it derives the package repository, `.repo` URL, and image repository from the string you give it, so a release published after your Bootwright build installs without an update. `oss` takes an upstream release name (`tentacle`) or an exact `x.y.z` version; an exact version pins the package repository and derives `quay.io/ceph/ceph:vX.Y.Z`. `redhat` and `ibm` take a dot-separated numeric product version of any length (`9.1`, `9.9.1.0`), whose leading component is the product stream. The value is used verbatim — nothing is rewritten to a Bootwright-preferred release. |
| `ceph.packageVersion` | No | — | Pins the exact Ceph package build to install, as an RPM `[epoch:]version[-release]` such as `19.2.1-245.el9cp` — the build your vendor's release-to-package-version table names. It governs the `cephadm` RPM on each storage node and nothing else: the daemons run from the container image, so bumping it reconciles the host CLI in place rather than upgrading or rebuilding the cluster. `redhat` and `ibm` only; for `oss` the build is already named by `ceph.release`. |
| `ceph.image.version` | No | `vX.Y.Z` from an exact `x.y.z` `oss` `ceph.release`; otherwise none | The daemon image build, as a tag or a `sha256:` digest, applied as the default image for every Ceph daemon. A mutable `latest` is not a pin. There is no `redhat`/`ibm` default because vendor tags are build-numbered and a product release such as `9.9.1.0` is not a tag; left unset, the install uses the distribution-packaged cephadm's own default tag, which floats. |
| `ceph.image.base` | No | The repository derived from `ceph.distribution`, `ceph.release` and the entitlement registry | The `<registry>/<path>` the version hangs off — `quay.io/ceph/ceph`, `registry.redhat.io/rhceph/rhceph-<stream>-rhel9`, or `cp.icr.io/cp/ibm-ceph/ceph-<stream>-rhel9`. Leave it unset in the normal case: the derived value keeps the vendor namespace and stream welded to the release. Author it to mirror the image or to name a vendor build base Bootwright has not recorded. Must carry no tag, digest, or scheme. |
| `ceph.community.mirror` | No | `https://download.ceph.com` | HTTPS upstream package base URL for mirrored or disconnected environments. `oss` only. |
| `ceph.community.checksum` | No | — | Optional `sha256:<hex>` pin on the community package payload fetched from `community.mirror`. `oss` only. |
| `ceph.entitlementRef` | When `redhat` or `ibm` | — | Names an `Entitlement` object. Must resolve to a `redhat-ceph` (for `redhat`) or `ibm-storage-ceph` (for `ibm`) entitlement. Must be empty for `oss`. See [Secrets](secrets.md#entitlements). |
| `ceph.osSubscriptionRef` | No | — | Names a `redhat-rhel` `Entitlement` supplying the RHEL subscription for provided-OS Ceph nodes (managed-OS nodes name it on their `MachineInstallProfile.spec.subscription` instead). Must resolve to a `redhat-rhel` entitlement. Chiefly paired with `distribution: ibm`, whose product entitlement does not itself entitle RHEL. See [Secrets](secrets.md#entitlements). |
| `ceph.ibm.callHome` | When `ibm` | — | Explicit IBM Call Home outbound-communication intent: `enabled` or `disabled`. License acceptance enables Call Home by default, so omission is rejected. |
| `ceph.cephadm.addressRef` | No | — | Default address name used to resolve cephadm host addresses. |
| `ceph.cephadm.clusterSSH.user` | No | `cephadm` on a managed cluster; `root` on an external one | The account cephadm manages every host as (`--ssh-user`), the account a node the cluster installs is created with, and the account Bootwright connects as. Bootwright provisions it on any topology node that does not already carry it. Rejected as `root` when a node authors a non-root `access.ssh.user`. See [The Ceph node login](#the-ceph-node-login). |
| `ceph.cephadm.clusterSSH.keyRef` | Required when `clusterSSH.user` is non-root (the default) | None | Names the `sshKeyPair` secret that is the cluster identity — the key cephadm reaches every host with, and the key the install authorizes for the node account. Must not be a `Secret` a `Machine` authors as its own `access.ssh.auth.privateKeyRef`. |
| `ceph.cephadm.bootstrap.node` | Yes | — | Topology node that cephadm bootstraps on, named by its node name (FQDN or short label). A machine name is rejected with guidance naming the node. |
| `ceph.cephadm.bootstrap.addressRef` | No | `ceph.cephadm.addressRef`, then the node machine's SSH address | Address used for the rendered cephadm `--mon-ip`, resolved in that fallback order. |
| `ceph.cephadm.bootstrap.singleHostDefaults` | No | `false` | Renders cephadm's `--single-host-defaults` at bootstrap (relaxed defaults for a one-node cluster). Valid only for a **single-host, non-stretch** topology and requires at least two declared OSDs. It owns `osd_pool_default_size`, `osd_pool_default_min_size`, and `osd_crush_chooseleaf_type` at bootstrap, so those keys are rejected in `ceph.config[global]`. Referenced by the [`StoragePool`](#storagepool) cross-field rules. |
| `ceph.networks.publicCIDRs[]` | No | — | Public-network CIDRs (renders `public_network`). |
| `ceph.networks.clusterCIDRs[]` | No | — | Cluster-network CIDRs for replication and recovery traffic (renders `cluster_network`). |
| `ceph.security.fips.enabled` | No | `false` | `true` requires a `redhat` or `ibm` distribution and that **every** Ceph node's `MachineInstallProfile` sets `customizations.security.fips.enabled: true`. Ceph runs FIPS by running on FIPS-installed RHEL nodes — there is no cephadm FIPS flag. |
| `ceph.config` | No | — | Ceph config database options as `section -> key -> value`, rendered as idempotent `ceph config set` after bootstrap. |
| `ceph.mgrModules[]` | No | — | mgr modules to enable (`ceph mgr module enable`). |
| `ceph.monitoring` | No | cephadm default stack (block absent) | cephadm monitoring stack controls; see [Monitoring](#monitoring). |
| `ceph.management` | No | — | Native cephadm management gateway (`mgmt-gateway`) fronting the Ceph dashboard behind a highly-available VIP; the block's presence enables it. `management.dnsLabel` is the leftmost label only (default `mgr`) — the published name is composed as `<dnsLabel>.<StorageCluster name>.<domains.storageClusters>`, so a dotted value is rejected. See [The management gateway and HA dashboard](../advanced/ceph-topologies.md#the-management-gateway-and-ha-dashboard). |
| `ceph.services[]` | No | — | Raw cephadm service-spec passthrough for unmodeled service types; see [Passthrough services](#passthrough-services). |
| `ceph.topology` | Yes | — | Nodes, roles, OSD devices, sites, and stretch mode; see [Topology](#topology). |

!!! warning "Release/image fields are install-time intent, not a day-2 upgrade"
    `ceph.release` and `ceph.image` select what cephadm bootstraps. Changing them
    on a live cluster is drift, and the only in-band resolution is a rebuild
    (`apply --converge-drifted` runs `cephadm rm-cluster --zap-osds` and re-bootstraps —
    data-destroying). Upgrade a running cluster out of band with `cephadm`/`ceph
    orch upgrade`; the desired state then names the old release, so `diff`
    reports drift until a future apply refreshes the record. Adopting an
    out-of-band upgrade into the recorded desired state is an open design item.

    `ceph.packageVersion` is the exception: it pins the host `cephadm` RPM, not
    the daemons, so a change to it is reconciled in place on the next apply and
    never proposes a rebuild.

!!! note "Cross-field rules"
    - `ceph.config` **rejects** `public_network` and `cluster_network` keys in
      any section — they are owned by `ceph.networks` (`publicCIDRs` /
      `clusterCIDRs`). Config sections are `global`, `mon`, `mgr`, `osd`, `mds`,
      `client`, or a `<type>.<id>` daemon section, optionally suffixed with a
      single CRUSH mask — `<section>/class:<class>` or
      `<section>/<crush-bucket-type>:<value>` (e.g. `osd/class:ssd`,
      `osd/rack:r1`) for per-class or per-location tuning. Option values must
      not be empty.
    - mgr module settings are declared in `ceph.config` under the `mgr` section
      (`mgr/<module>/<key>`), not on `mgrModules[]`.
    - Removed `config` keys, `mgrModules[]`, and `services[]` are **not** undone
      (additive-only).
    - `ceph.security.fips.enabled: true` requires distribution `redhat` or `ibm`
      (the community `oss` images are not FIPS-validated) and that every Ceph
      node Bootwright installs uses a `MachineInstallProfile` with
      `customizations.security.fips.enabled: true`. Provided-OS nodes (no
      install profile) are not checked and must be FIPS-installed out of band.

Distribution requirements:

| Distribution | Requirements |
| --- | --- |
| `oss` | Community package and image sources; `entitlementRef` must be empty; `community.mirror` may override `download.ceph.com`. |
| `redhat` | `entitlementRef` resolves to `redhat-ceph`. |
| `ibm` | `entitlementRef` resolves to `ibm-storage-ceph` with accepted license terms; the RHEL subscription is named by the nodes' `MachineInstallProfile.spec.subscription` or the cluster `osSubscriptionRef`. `ibm.callHome` is required. |

### Version compatibility is yours, not Bootwright's

Bootwright holds no list of Ceph releases and no vendor support matrix. It will
not tell you a release is unknown, too new, too old, or mismatched against your
node OS, and it has no opinion on which RHEL version a given Ceph release runs
on. Those facts live in the vendor's compatibility guide and change on the
vendor's schedule; a copy inside Bootwright would be wrong the moment the vendor
moves.

What it does instead: it takes the release string you wrote, treats the leading
component as the product stream, and builds the artifact coordinates from it —
the tools repo, the vendor `.repo` URL, and the daemon image repository, with
each node's own RHEL major filling the repo templates at run time. Declare
`release: "9.9.2.0"` the day IBM ships it and it installs, with no Bootwright
update and no warning.

The same applies to the two build pins. `ceph.packageVersion` and
`ceph.image.version` are exactly the coordinates you read off your vendor's own
release-to-build table, and Bootwright takes both verbatim: it never checks them
against `ceph.release`, never checks them against each other, and never warns
that a build is unknown. Pin them together to make an install reproducible on
both axes — the `cephadm` RPM on the hosts and the container image the daemons
run from:

```yaml
spec:
  ceph:
    distribution: ibm
    release: "9.9.1.0"
    packageVersion: "19.2.1-245.el9cp"
    image:
      version: "9.9.1.0-123"
```

Two things are still checked, and neither is a version claim. A Ceph node's
`MachineInstallProfile` must declare `family: rhel`, because the subscription-backed
provider only implements RHEL-family package sources — a Bootwright capability
limit. And an authored `ceph.image.base` must sit in your cluster's own vendor
namespace and stream, so a Red Hat cluster cannot silently run an IBM image.
Leaving `base` unset sidesteps that entirely: the derived value is built from the
release, so it cannot drift from it.

When `Entitlement.spec.registry.url` overrides the vendor namespace,
`ceph.image.base` becomes required and must name the vendor repository below that
mirror root — for stream `9`, `mirror.example.test/vendor/rhceph/rhceph-9-rhel9`
for Red Hat or `mirror.example.test/vendor/ibm-ceph/ceph-9-rhel9` for IBM. A
`version` alone does not satisfy it: the derived base names the vendor registry,
which a mirrored estate cannot pull. The
check covers the namespace and stream prefix (`.../rhceph/rhceph-<stream>-rhel`);
the trailing build base is whatever the vendor compiled that release against and
is yours to declare. The registry override controls credentials and trust; it
does not permit an arbitrary image repository below the namespace.

### The Ceph node login

A managed Ceph `StorageCluster` owns the login its nodes carry. You do not
declare it on each `Machine`: `spec.ceph.cephadm.clusterSSH` names the account
and the key once, and every topology node the cluster installs derives its
login from it.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: ceph-cluster-ssh-key
spec:
  type: sshKeyPair
  source:
    generated:
      comment: bootwright-ceph-oss-cephadm
---
apiVersion: bootwright.io/v1alpha1
kind: StorageCluster
metadata:
  name: ceph-oss
spec:
  type: ceph
  ceph:
    cephadm:
      clusterSSH:
        keyRef: ceph-cluster-ssh-key
      bootstrap:
        node: node-01
```

`clusterSSH.user` defaults to `cephadm`. `clusterSSH.keyRef` is required
whenever the account is not `root`, and must resolve to an `sshKeyPair`
`Secret` — it is the key cephadm orchestrates every host with, and the key the
install authorizes for the account.

A node the cluster installs therefore needs no `access` block at all:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: ceph-0
spec:
  capabilities:
    - ceph-node
  substrate:
    providerRef: lab-baremetal
  os:
    provided: false
    installProfileRef: rhel-ceph-node
  addresses:
    - name: ssh
      address: 192.0.2.50
```

Bootwright derives `access.ssh.user: cephadm`, an
`access.ssh.auth.provision.keyRef` naming the cluster key, and
`access.rootLogin: revoke`. The kickstart creates the account, authorizes the
cluster public key for it, leaves the root password locked, writes
`PermitRootLogin no`, and grants the account a per-principal sudoers drop-in at
`/etc/sudoers.d/60-bootwright-cephadm`. **The node never accepts a root login at
any point**, and there is no second account to provision afterwards.

### Nodes the cluster does not install

A node whose OS you supplied (`os.provided: true`) already exists, so the
cluster cannot create its login at install time. Declare on the `Machine` how
Bootwright reaches it, and the cluster still supplies the orchestration account
Bootwright provisions there:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: ceph-arbiter
spec:
  capabilities:
    - ceph-node
  os:
    provided: true
  addresses:
    - name: ssh
      address: 192.0.2.60
  access:
    ssh:
      auth:
        controllerIdentity: {}
```

`controllerIdentity` reaches the machine as the operator running Bootwright,
with that operator's own SSH identity — the common shape for a box you already
log into. Use `auth.privateKeyRef` instead to name a `Secret`, or
`auth.passwordRef` for a machine that only accepts a password. See
[Machines → Access](machines.md#access).

On apply Bootwright connects with that identity, creates `cephadm`, authorizes
the cluster key for it, writes the sudoers drop-in, and proves the account
answers `sudo -n true` before cephadm uses it. Because the machine's own login
is not the cluster's, `rootLogin` stays `keep` unless you set it — hardening a
machine whose OS you own is your decision, not the cluster's.

### What apply does, in order

1. Creates the orchestration account on every topology node that lacks it —
   locked password, not in `wheel`, the cluster public key in its
   `authorized_keys`, and a sudoers drop-in at
   `/etc/sudoers.d/60-bootwright-<user>`.
2. Proves the account answers `sudo -n true` on **every** node.
3. For a node whose `access.rootLogin` is `revoke`, writes
   `/etc/ssh/sshd_config.d/01-bootwright-access.conf` with `PermitRootLogin no`,
   checks it with `sshd -t`, reloads `sshd`, and re-proves the account.
4. Bootstraps or reconciles cephadm with `--ssh-user <user>`
   (`ceph cephadm set-user` on an already-bootstrapped cluster) and records the
   posture at `/etc/bootwright/access-marker.json`.

Because verification precedes revocation, a node whose account is not working
stops the run with root still reachable.

To reverse a revoke, set `rootLogin: keep` and re-apply: the sshd drop-in is
removed and root is re-authorized. The orchestration account is not deleted by
that — change `clusterSSH.user` to `root` as a separate, deliberate step.

!!! warning "The login is fixed for the life of the machine"
    The account a node carries is its *install-window* identity — what
    Bootwright installs the machine as, probes it as, and proves ownership
    with. Changing `clusterSSH.user` on an installed fleet makes the ownership
    probe fail closed, and the next apply **refuses** those machines until the
    account exists on them. Choose it before the first install. See
    [Machines → Access](machines.md#access).

!!! note "What this buys, precisely"
    The account holds `NOPASSWD: ALL`, because cephadm's manager `sudo`-wraps
    every remote command when its SSH user is not `root` and a command-scoped
    policy cannot orchestrate a cluster. So this is **not** privilege
    separation — the account can become root on demand. What you get is: no
    standing root SSH, a named principal in the audit and sudo logs instead of
    anonymous `root`, key-only authentication, and a cluster credential you can
    rotate or revoke without touching the root account. The binding rules are
    in [`specs/security.md`](https://github.com/crmarques/bootwright/blob/main/specs/security.md)
    and the rationale in
    [ADR 0024](https://github.com/crmarques/bootwright/blob/main/specs/adr/0024-machine-access-union-and-cluster-owned-node-login.md).

!!! danger "The cluster key must not be an authored machine key"
    If a `Machine` authors `access.ssh.auth.privateKeyRef` and the cluster names
    that same `Secret` as `clusterSSH.keyRef`, validation refuses: `cephadm
    bootstrap --ssh-private-key` moves the cluster identity into the Ceph mon
    config-key store, where the cluster's manager can read it, and that key
    also opens machines outside the cluster. A key the cluster derives for its
    own nodes is fine — its blast radius is exactly those nodes.


## StoragePlacementPolicy

Reusable placement and replicated-pool defaults for pools that select it via
`placementPolicyRef`.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.storageClusterRef` | Yes | — | Managed `StorageCluster`. |
| `spec.ceph.ruleName` | Yes | — | CRUSH rule name. |
| `spec.ceph.failureDomain` | No | — | CRUSH failure domain. |
| `spec.ceph.crushDeviceClass` | No | — | Pin the replicated rule to one device class (`ssd`/`hdd`/`nvme`), the trailing argument of `crush rule create-replicated`. Fixed at rule creation; route to a different class with a new `ruleName`. |
| `spec.ceph.replicated.size` | No | Ceph default | Replica count. |
| `spec.ceph.replicated.minSize` | No | Ceph default | Minimum replicas to serve I/O. |

!!! note "Cross-field rule"
    A pool with `placementPolicyRef` set **must not** also set `ceph.replicated`;
    the referenced policy owns the pool's replication.

    For `failureDomain: host`, the effective replica size cannot exceed the
    number of topology nodes carrying the `osd` role. Effective `minSize`
    cannot exceed `size`.

## StoragePool

Owns one Ceph pool. Deleting the object from desired state leaves the live pool
running (additive-only).

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.storageClusterRef` | Yes | — | Managed `StorageCluster`. |
| `spec.placementPolicyRef` | No | — | `StoragePlacementPolicy` (same cluster) supplying replicated defaults. |
| `spec.ceph.type` | No | `replicated` | Immutable data-protection strategy: `replicated` or `erasure`. |
| `spec.ceph.role` | No | — | `rbd`, `cephfs-metadata`, `cephfs-data`, or `rgw`. Drives `StorageExport` wiring and the inferred application (`rbd`→`rbd`, `cephfs-*`→`cephfs`, `rgw`→`rgw`). |
| `spec.ceph.application` | No | inferred from `role` | Overrides the inferred `ceph osd pool application enable` value. |
| `spec.ceph.replicated.size` | No | Ceph default | Replica count (`replicated` only). |
| `spec.ceph.replicated.minSize` | No | Ceph default | Minimum replicas (`replicated` only). |
| `spec.ceph.erasure.dataChunks` | Yes (when `type: erasure`) | — | Erasure `k`; must be positive. |
| `spec.ceph.erasure.codingChunks` | Yes (when `type: erasure`) | — | Erasure `m`; must be positive. |
| `spec.ceph.erasure.plugin` | No | Ceph default (`jerasure`) | EC plugin: `jerasure`, `isa`, `clay`, `lrc`, or `shec`. |
| `spec.ceph.erasure.technique` | No | — | Plugin-specific coding technique (`technique`, e.g. `reed_sol_van`). |
| `spec.ceph.erasure.crushDeviceClass` | No | — | Tier the EC pool onto a device class (`crush-device-class`). |
| `spec.ceph.erasure.crushRoot` | No | — | CRUSH subtree root for the profile (`crush-root`). |
| `spec.ceph.erasure.stripeUnit` | No | — | Per-chunk stripe size (`stripe_unit`, e.g. `4K`). |
| `spec.ceph.erasure.parameters` | No | — | Remaining `erasure-code-profile set` key=value pairs (`l`, `c`, `d`, `w`, `packetsize`, …), rendered verbatim. Keys owned by the fields above or the derived `crush-failure-domain` are rejected. |
| `spec.ceph.autoscale` | No | cephadm default | PG autoscaler intent; see [Pool tuning](#pool-tuning). |
| `spec.ceph.quota` | No | no limit | Pool quota; see [Pool tuning](#pool-tuning). |
| `spec.ceph.compression` | No | — | Inline compression; see [Pool tuning](#pool-tuning). |

!!! note "Cross-field rules"
    - `type: replicated` must not set `erasure`; `type: erasure` must not set
      `replicated` and requires `erasure`.
    - Erasure pools are not allowed on stretch-mode clusters; on a stretch
      cluster any authored `replicated.size`/`minSize` must be `4`/`2`.
    - Outside stretch mode, host-domain replica size and erasure `k+m` cannot
      exceed the number of OSD hosts. With `singleHostDefaults: true`, a
      policy-less pool uses cephadm's OSD-level `2/1` defaults and provisioning
      requires at least two in OSDs before pool creation.
    - Effective replicated `minSize` cannot exceed `size`.
    - The pool's structural identity is its `type` and erasure profile. Changing
      it is the only desired-state change that rebuilds a live pool
      (data-destroying, `apply --converge-drifted` only); replicas, CRUSH rule, and
      application reconcile in place.

A role-typed RBD pool selecting its cluster:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: StoragePool
metadata:
  name: odf-rbd
spec:
  storageClusterRef: ceph-storage
  ceph:
    role: rbd
```

### Pool tuning

The autoscaler, quota, and compression blocks render their fields as idempotent
`ceph osd pool set` / `set-quota` operations. Each reconciles in place
(last-write-wins) and none is part of the pool's structural identity, so they
never trigger a rebuild. `pg_num`/`pgp_num` are deliberately not modeled — the
autoscaler owns them.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `autoscale.mode` | No | cephadm default | `pg_autoscale_mode`: `on`, `off`, or `warn`. |
| `autoscale.targetSizeRatio` | No | — | `target_size_ratio` capacity hint. Mutually exclusive with `targetSizeBytes`. |
| `autoscale.targetSizeBytes` | No | — | `target_size_bytes` capacity hint (e.g. `10G`). |
| `autoscale.pgNumMin` | No | — | `pg_num_min` lower bound. |
| `autoscale.pgNumMax` | No | — | `pg_num_max` upper bound. |
| `autoscale.bulk` | No | — | `bulk`: start the pool large. |
| `quota.maxBytes` | No | no limit | `set-quota max_bytes`. An authored `0` is the native "no limit"; omit to leave a live quota untouched. |
| `quota.maxObjects` | No | no limit | `set-quota max_objects`. Same `0`/omit semantics. |
| `compression.mode` | No | — | `compression_mode`: `none`, `passive`, `aggressive`, or `force`. Required to set any other compression field. |
| `compression.algorithm` | No | — | `compression_algorithm`: `lz4`, `snappy`, `zlib`, or `zstd`. |
| `compression.requiredRatio` | No | — | `compression_required_ratio` `(0,1]`. |
| `compression.minBlobSize` | No | — | `compression_min_blob_size` (e.g. `8K`). |
| `compression.maxBlobSize` | No | — | `compression_max_blob_size`. |
| `mirroring.mode` | No | — | Enable RBD mirroring (`rbd mirror pool enable`): `image` or `pool`. Additive-only (never disabled). Deploy the `rbd-mirror` daemon via `spec.ceph.services[]`; peer setup is out of scope today. |

## StorageFilesystem

Owns one CephFS filesystem and its MDS placement. Deleting the object from
desired state leaves the live filesystem running (additive-only).

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.storageClusterRef` | Yes | — | Managed `StorageCluster`. |
| `spec.cephfs.metadataPoolRef` | Yes | — | Metadata `StoragePool`. |
| `spec.cephfs.dataPoolRefs[]` | Yes | — | One or more data pools, each authored as a plain pool name (the `{name, default}` object form is only for electing the default). |
| `spec.cephfs.dataPoolRefs[].name` | Yes (object form) | — | Data pool name. |
| `spec.cephfs.dataPoolRefs[].default` | No | `true` for a single entry; otherwise `false` | Marks the default data pool. |
| `spec.cephfs.mds.activeCount` | No | Ceph default | Active MDS count (`max_mds`). |
| `spec.cephfs.mds.standbyReplay` | No | `false` | Enable hot standby-replay MDS (`allow_standby_replay`), the standard production HA posture. |
| `spec.cephfs.mds.standbyCountWanted` | No | Ceph default | Standby daemons the cluster wants (`standby_count_wanted`, non-negative). |
| `spec.cephfs.mds.placement` | No | every host carrying the `mds` role | MDS service placement; see [Shared placement](#shared-placement). |
| `spec.cephfs.mds.serviceSpec.unmanaged` | No | `false` | Freeze the MDS daemon set (`unmanaged`). |
| `spec.cephfs.mds.serviceSpec.extraContainerArgs[]` | No | — | `extra_container_args` for the daemon container. |
| `spec.cephfs.mds.serviceSpec.extraEntrypointArgs[]` | No | — | `extra_entrypoint_args` for the daemon. |
| `spec.cephfs.mds.serviceSpec.networks[]` | No | — | Bind the daemon to CIDRs (`networks`). |
| `spec.cephfs.subvolumeGroups[]` | No | — | Static subvolume groups; see [Subvolume groups](#subvolume-groups). |

!!! note "Cross-field rule"
    A single data pool becomes the default automatically. With multiple data
    pools you must mark **exactly one** as `default: true`.

!!! warning "Changing the metadata pool or default data pool recreates the filesystem"
    The CephFS metadata pool and its default data pool are part of the
    filesystem's structural identity — Ceph cannot move a live CephFS to a
    different metadata or default data pool — so changing
    `spec.cephfs.metadataPoolRef`, or which `dataPoolRefs[]` entry is the default,
    is a data-destroying, `apply --converge-drifted`-only recreate (`ceph fs rm` then
    recreate), not an in-place reconcile.

### Subvolume groups

`spec.cephfs.subvolumeGroups[]` declares the static `ceph fs subvolumegroup
create` boundaries that CSI and other tools provision subvolumes into.
Individual subvolumes are out of scope (apps/CSI own those). Additive-only: a
removed group keeps running.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `name` | Yes | — | Subvolume group name; unique within the filesystem. |
| `poolLayoutRef` | No | — | `StoragePool` (same cluster) for the group's data layout (`--pool_layout`). |
| `mode` | No | — | Directory mode in octal (`--mode`, e.g. `0755`). |
| `uid` | No | — | Owner UID (`--uid`, non-negative). |
| `gid` | No | — | Owner GID (`--gid`, non-negative). |
| `sizeBytes` | No | — | Group quota in bytes (`--size`, non-negative). |

## StorageObjectGateway

Owns one RGW service and its ingress endpoints. Deleting the object from desired
state leaves the live service running (additive-only).

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.storageClusterRef` | Yes | — | Managed `StorageCluster` (external clusters reject Bootwright-managed gateways). |
| `spec.public.dnsLabel` | No | `metadata.name` | Leftmost label of the public S3 endpoint. The published name is composed as `<dnsLabel>.<storageClusterRef name>.<domains.storageClusters>`, so a dotted value is rejected. |
| `spec.public.scheme` | No | — | Public endpoint scheme. |
| `spec.public.port` | No | — | Public endpoint port. |
| `spec.ceph.serviceID` | Yes | — | RGW service ID. |
| `spec.ceph.placement` | No | every host carrying the `rgw` role | RGW placement; see [Shared placement](#shared-placement). |
| `spec.ceph.frontendPort` | No | cephadm default | RGW frontend port (0–65535). |
| `spec.ceph.realm` | No | implicit default | Multisite realm (`rgw_realm`). Set with `zoneGroup` and `zone` (all-or-nothing). Bootwright creates the realm/zonegroup/zone and commits the period; even a single site benefits from a stable named zone. |
| `spec.ceph.zoneGroup` | No | — | Multisite zonegroup (`rgw_zonegroup`). |
| `spec.ceph.zone` | No | — | Multisite zone (`rgw_zone`). |
| `spec.ceph.config` | No | — | Per-RGW options applied as `ceph config set client.rgw.<serviceID>`. Values must not be empty; `rgw_frontend_port` is owned by `frontendPort`; a key must not also appear in the cluster `config[client.rgw.<serviceID>]` section. |
| `spec.ceph.ingresses[]` | No | — | cephadm ingress VIPs. |
| `spec.ceph.ingresses[].name` | Yes (per entry) | — | Ingress name; unique within the gateway. |
| `spec.ceph.ingresses[].address` | Yes (per entry) | — | VIP address. |
| `spec.ceph.ingresses[].prefixLength` | Yes (per entry) | — | VIP prefix length. |
| `spec.ceph.ingresses[].virtualInterfaceNetworks[]` | No | — | Renders verbatim to the cephadm ingress `virtual_interface_networks`. |
| `spec.ceph.ingresses[].firstVirtualRouterID` | No | cephadm default (`50`) | Keepalived VRRP router ID (1–255), rendered verbatim as `first_virtual_router_id`. |
| `spec.ceph.ingresses[].placement` | No | every host carrying the `ingress` role | Ingress placement; see [Shared placement](#shared-placement). |

!!! note "Storage owns the endpoint"
    RGW public endpoints and ingress VIPs are owned by the storage gateway, not
    by `ContainerCluster`. Downstream consumers reference the gateway. See
    [Networking](../advanced/networking.md).

!!! note "Per-site gateways on a stretch cluster"
    On a stretch-mode `StorageCluster`, a gateway's `spec.ceph.placement` and its
    ingresses' combined placement must each cover at least two role-capable hosts
    per data site — unless narrowed by `sites`, in which case only the named
    sites need that coverage. Author one `StorageObjectGateway` per data site,
    each with `placement.sites` and its ingress `placement.sites` narrowed to
    that site, to keep RGW daemons, the ingress VRRP group, and HAProxy backends
    entirely site-local. `firstVirtualRouterID` collisions are rejected only
    between ingress groups (RGW, NFS, or the management ingress) that also
    declare an overlapping `virtualInterfaceNetworks` entry — distinct, disjoint
    per-site subnets may reuse the same ID.

## StorageNFSExport

Owns one cephadm NFS-Ganesha service and its exports. Deleting the object from
desired state leaves the live service and exports running (additive-only). cephadm
auto-provisions the backing `.nfs` pool, so no pool/namespace is modeled.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.storageClusterRef` | Yes | — | Managed `StorageCluster`. |
| `spec.ceph.serviceID` | Yes | — | NFS service ID (`--cluster-id`). |
| `spec.ceph.placement` | Yes | — | Must set `hosts` or `sites` (there is no `nfs` topology role); see [Shared placement](#shared-placement). |
| `spec.ceph.ingresses[]` | No | — | Ingress VIPs fronting `nfs.<serviceID>` (same shape as the RGW gateway ingress). |
| `spec.exports[]` | No | — | NFS exports; each renders an idempotent `ceph nfs export create`. |
| `spec.exports[].pseudo` | Yes (per entry) | — | NFSv4 pseudo path (`--pseudo-path`); unique within the service. |
| `spec.exports[].filesystemRef` | One of two FSALs | — | CephFS export (`--fsname`); a `StorageFilesystem` in the same cluster. |
| `spec.exports[].path` | No | `/` | Directory within the filesystem (`--path`). |
| `spec.exports[].bucket` | One of two FSALs | — | RGW export (`--bucket`). |
| `spec.exports[].accessType` | No | — | `RW`, `RO` (renders `--readonly`), or `NONE`. |
| `spec.exports[].squash` | No | — | NFS squash mode (`--squash`, e.g. `no_root_squash`). |
| `spec.exports[].clients[]` | No | — | Restrict access by address/CIDR (`--client-addr`). |

!!! note "Cross-field rule"
    Each export sets **exactly one** of `filesystemRef` (CephFS, FSAL CEPH) or
    `bucket` (RGW, FSAL RGW).

## Shared placement

`StoragePlacement` selects where a Ceph service runs. It appears on monitoring
services, passthrough services, MDS, RGW, NFS, and ingress.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `placement.hosts[]` | No | — | Explicit node names (FQDN or short label; machine names are rejected); narrows below site granularity. |
| `placement.sites[]` | No | — | Topology sites; narrows to hosts in the named sites. |
| `placement.countPerHost` | No | — | Renders to the cephadm `count_per_host` (non-negative). |

When `placement` is omitted, a service defaults to every topology node carrying
that service's role. Passthrough services are the exception: their placement
must set `hosts` or `sites`.

## StorageExport

`StorageExport` owns storage surfaces prepared for downstream consumers, such as
OpenShift Data Foundation external mode. The export name is what an
[add-on](add-ons.md) binding supplies to a `storageExportAttachment` input.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.type` | Yes, unless `dataFoundation` is set | `dataFoundation` when `spec.dataFoundation` is set | Currently `dataFoundation`. |
| `spec.storageClusterRef` | Yes | — | Imported or managed `StorageCluster`. |
| `spec.dataFoundation` | When `storageClusterRef` is managed Ceph | — | References managed storage services to export; see [Data Foundation](#data-foundation). |
| `spec.externalDetails` | When `storageClusterRef` is external Ceph | — | Operator-supplied external-cluster details; see [External details](#external-details). |

!!! note "Cross-field rules"
    - For a **managed** `storageClusterRef`, `dataFoundation` is required and
      `externalDetails` must be empty: the consuming add-on produces the
      external-cluster details itself — its hook runs the exporter on a Ceph
      node of the export's cluster and captures the payload as a hook output.
    - For an **external** `storageClusterRef`, `externalDetails` is required and
      `dataFoundation` must be empty.
    - `spec.type` may be omitted only when `dataFoundation` is set (it then
      normalizes to `dataFoundation`); an external export must always author it
      explicitly.

A managed export wiring the RBD pool and CephFS filesystem (the consuming
add-on's exporter hook reads these refs):

```yaml
apiVersion: bootwright.io/v1alpha1
kind: StorageExport
metadata:
  name: odf-external-ceph
spec:
  type: dataFoundation
  storageClusterRef: ceph-storage
  dataFoundation:
    rbdPoolRef: odf-rbd
    filesystemRef: odf-cephfs
```

An external export taking operator-supplied details from a secret:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: StorageExport
metadata:
  name: imported-ceph-odf
spec:
  type: dataFoundation
  storageClusterRef: imported-ceph
  externalDetails:
    fromSecretRef: imported-ceph-odf-details
```

### Data Foundation

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `dataFoundation.rbdPoolRef` | Yes | — | RBD `StoragePool` (same cluster). |
| `dataFoundation.filesystemRef` | Yes | — | CephFS `StorageFilesystem` (same cluster). |
| `dataFoundation.objectGatewayRef` | No | — | RGW `StorageObjectGateway` (same cluster). When set, the consuming add-on's exporter hook passes that gateway's composed public name and `public.port` as `--rgw-endpoint`, adding S3 to the export; omitted, the export is RBD/CephFS only. |

### External details

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `externalDetails.fromSecretRef` | Yes within `externalDetails` | — | Secret with the operator-supplied external-cluster-details JSON. |

!!! note "Who produces the details"
    The export itself never gathers credentials. Either the operator supplies
    the external-cluster-details JSON as a secret (`fromSecretRef` — the only
    option for external Ceph, where Bootwright manages no nodes), or the
    consuming [add-on](add-ons.md) ships a hook that fetches Rook's
    external-cluster-details exporter from the installed operator, runs it on a
    node of the managed Ceph cluster, and consumes the captured output. See the
    add-ons page for the hook shapes.

## Where to go next

- [Ceph topologies](../advanced/ceph-topologies.md) — stretch clusters, FIPS,
  the HA dashboard, and importing an existing cluster.
- [The Ceph lab](../getting-started/ceph.md) — a guided first managed-Ceph
  apply.
- [Add-ons](add-ons.md) — how a `StorageExport` feeds a Data Foundation
  external-mode attachment.
