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
| `ceph.cephadm.clusterSSH.user` | No | `cephadm` when any node `Machine` sets `access.rootLogin: revoke`; otherwise `root` | The cluster's **post-install** account: the OS user cephadm manages every host as (`--ssh-user`), and the account Bootwright itself connects as once that node revokes root login. A non-root value is provisioned by Bootwright on every topology node, or reconciled in place when it equals that node's `access.ssh.user`. Rejected as `root` when a node's `access.ssh.user` is non-root. See [Revoking root SSH on Ceph nodes](#revoking-root-ssh-on-ceph-nodes). |
| `ceph.cephadm.clusterSSH.keyRef` | No (required when `clusterSSH.user` is non-root) | the first topology node's `access.ssh` key | Names the `sshKeyPair` secret cephadm uses as its cluster identity — the key Bootwright authorizes on, and cephadm reaches, every host. Must be a different `Secret` from the nodes' `access.ssh.keyRef`, so the key Bootwright drives the machines with never enters the Ceph mon config-key store. |
| `ceph.cephadm.bootstrap.node` | Yes | — | Topology node that cephadm bootstraps on, named by its node name (FQDN or short label). A machine name is rejected with guidance naming the node. |
| `ceph.cephadm.bootstrap.addressRef` | No | `ceph.cephadm.addressRef`, then the node machine's SSH address | Address used for the rendered cephadm `--mon-ip`, resolved in that fallback order. |
| `ceph.cephadm.bootstrap.singleHostDefaults` | No | `false` | Renders cephadm's `--single-host-defaults` at bootstrap (relaxed defaults for a one-node cluster). Valid only for a **single-host, non-stretch** topology and requires at least two declared OSDs. It owns `osd_pool_default_size`, `osd_pool_default_min_size`, and `osd_crush_chooseleaf_type` at bootstrap, so those keys are rejected in `ceph.config[global]`. Referenced by the [`StoragePool`](#storagepool) cross-field rules. |
| `ceph.networks.publicCIDRs[]` | No | — | Public-network CIDRs (renders `public_network`). |
| `ceph.networks.clusterCIDRs[]` | No | — | Cluster-network CIDRs for replication and recovery traffic (renders `cluster_network`). |
| `ceph.security.fips.enabled` | No | `false` | `true` requires a `redhat` or `ibm` distribution and that **every** Ceph node's `MachineInstallProfile` sets `customizations.security.fips.enabled: true`. Ceph runs FIPS by running on FIPS-installed RHEL nodes — there is no cephadm FIPS flag. |
| `ceph.config` | No | — | Ceph config database options as `section -> key -> value`, rendered as idempotent `ceph config set` after bootstrap. |
| `ceph.mgrModules[]` | No | — | mgr modules to enable (`ceph mgr module enable`). |
| `ceph.monitoring` | No | cephadm default stack (block absent) | cephadm monitoring stack controls; see [Monitoring](#monitoring). |
| `ceph.management` | No | — | Native cephadm management gateway (`mgmt-gateway`) fronting the Ceph dashboard behind a highly-available VIP; the block's presence enables it. See [The management gateway and HA dashboard](../advanced/ceph-topologies.md#the-management-gateway-and-ha-dashboard). |
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

### Revoking root SSH on Ceph nodes

By default Bootwright reaches Ceph nodes as `root` and cephadm orchestrates
them as `root`. If your policy forbids standing root SSH there are two shapes,
and which one fits depends on whether the nodes are already installed.

**Nodes already installed as `root`** keep `root` as their install-window
identity and gain a separate orchestration account, which Bootwright provisions
and then uses to revoke root. That is the rest of this section.

**Nodes not yet installed** can carry the orchestration account from their first
boot instead, so root SSH is never enabled at all — see
[Orchestrating as the install-window identity](#orchestrating-as-the-install-window-identity)
below.

For the retrofit shape, declare a dedicated orchestration account on the cluster
and revoke root on the nodes. Two fields, on two objects:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: ceph-0
spec:
  access:
    ssh:
      addressRef: ip
      keyRef: lab-machine-key
    rootLogin: revoke
---
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
        user: cephadm
        keyRef: ceph-cluster-ssh-key
      bootstrap:
        node: node-01
```

`clusterSSH.user` may be omitted — it already defaults to `cephadm` once any
node `Machine` revokes root. `clusterSSH.keyRef` may not: a dedicated cluster
key is required when revoking, so that the key cephadm stores inside the
cluster is never the same key Bootwright administers the machines with.

On the next `apply` Bootwright, in this order:

1. Creates `cephadm` on every topology node — locked password, not in `wheel`,
   the machine access public key in its `authorized_keys`, and a sudoers
   drop-in at `/etc/sudoers.d/60-bootwright-cephadm`.
2. Proves the account answers `sudo -n true` on **every** node.
3. Writes `/etc/ssh/sshd_config.d/01-bootwright-access.conf` with
   `PermitRootLogin no`, checks it with `sshd -t`, reloads `sshd`, and re-proves
   the account.
4. Bootstraps or reconciles cephadm with `--ssh-user cephadm`
   (`ceph cephadm set-user` on an already-bootstrapped cluster) and records the
   posture at `/etc/bootwright/access-marker.json`.

Because verification precedes revocation, a node whose account is not working
stops the run with root still reachable.

To reverse it, set `rootLogin: keep` and re-apply: the sshd drop-in is removed
and root is re-authorized. The orchestration account is not deleted by that —
change `clusterSSH.user` back to `root` as a separate, deliberate step.

!!! warning "Do not repoint `access.ssh.user` on an *installed* node"
    `Machine.spec.access.ssh.user` is the *install-window* identity — the
    account Bootwright installs and probes the machine as. Changing it on a node
    that is already installed makes the ownership probe fail closed, and the
    next apply **refuses** that machine until the account exists on it.
    Retrofitting an installed fleet is expressed by `access.rootLogin` and
    `clusterSSH.user`, which leave the install-window identity untouched. See
    [Machines → Root login posture](machines.md#root-login-posture).

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
    [ADR 0019](https://github.com/crmarques/bootwright/blob/main/specs/adr/0019-node-root-posture-and-orchestration-identity.md).

### Orchestrating as the install-window identity

On nodes Bootwright has not installed yet, name the orchestration account in
`Machine.spec.access.ssh.user` as well. The kickstart then creates it at install
time — machine access key authorized, passwordless `sudo`, root password locked,
no `PermitRootLogin yes` — so the node never accepts a root login at any point
and there is no second account to provision:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: ceph-0
spec:
  os:
    provided: false
    installProfileRef: rhel-ceph-node
  access:
    ssh:
      user: cephadm
      addressRef: ip
      keyRef: lab-machine-key
    rootLogin: revoke
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
        user: cephadm
        keyRef: ceph-cluster-ssh-key
```

`clusterSSH.keyRef` is still required and still must be a different `Secret`
from `access.ssh.keyRef` — more so in this shape, since the machine access key
now opens the passwordless-sudo account directly and must never reach the Ceph
mon config-key store.

`rootLogin: revoke` remains worth setting: the posture is already implicit here,
and `revoke` is what turns it into a declared `PermitRootLogin no` that Bootwright
verifies and reconciles.

Apply reconciles rather than creates: it finds the account present, writes
`/etc/sudoers.d/60-bootwright-cephadm`, drops the account's install-time `wheel`
membership *after* that grant is in place, locks its password, proves
`sudo -n true`, revokes root, and stamps the marker.

#### Preparing a provided-OS node

A node with `os.provided: true` — a stretch-mode arbiter, say — is not installed
by Bootwright, so the account has to exist before the first apply. Prepare it
with your own credentials; Bootwright never needs them, and never stores them.
Its own key is generated, and only the **public** half leaves the context:

```sh
bootwright secret generate
bootwright secret show --name lab-machine-key --part public
```

Then, on the node, as an operator with `sudo`:

```sh
sudo useradd --create-home --user-group --shell /bin/bash cephadm
sudo passwd --lock cephadm
sudo install -d -m 0700 -o cephadm -g cephadm /home/cephadm/.ssh
printf '%s\n' '<the public key>' | sudo tee /home/cephadm/.ssh/authorized_keys >/dev/null
sudo chown cephadm:cephadm /home/cephadm/.ssh/authorized_keys
sudo chmod 0600 /home/cephadm/.ssh/authorized_keys
sudo restorecon -R /home/cephadm/.ssh
printf '%s\n' 'Defaults:cephadm !requiretty' 'cephadm ALL=(ALL) NOPASSWD: ALL' \
  | sudo tee /etc/sudoers.d/60-bootwright-cephadm.tmp >/dev/null
sudo chmod 0440 /etc/sudoers.d/60-bootwright-cephadm.tmp
sudo visudo -cf /etc/sudoers.d/60-bootwright-cephadm.tmp \
  && sudo mv -f /etc/sudoers.d/60-bootwright-cephadm.tmp /etc/sudoers.d/60-bootwright-cephadm
```

Finally record the node's host key so the probe pins it:

```sh
bootwright machine trust --machines ceph-arbiter
```

Apply then reconciles this node exactly like the installed ones. If the account
does not answer, the run refuses the node and names it rather than guessing
another identity.

### Monitoring

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `monitoring.enabled` | No | `true` (when the `monitoring` block is present) | `false` renders the bootstrap `--skip-monitoring-stack` flag and no monitoring specs. |
| `monitoring.prometheus` | No | — | Per-service tuning; placement derives from the `prometheus` role. |
| `monitoring.grafana` | No | — | Per-service tuning; placement derives from the `grafana` role. |
| `monitoring.alertmanager` | No | — | Per-service tuning; placement derives from the `alertmanager` role. |
| `monitoring.nodeExporter` | No | every host (cephadm behavior) | node-exporter has no topology role; an authored block narrows by explicit placement only. |
| `monitoring.loki` | No | — | Centralized-logging aggregator (`service_type: loki`); role-less, placement authored explicitly. cephadm provisions Grafana's Loki datasource itself; no dashboard mgr command is emitted. `retentionTime`/`retentionSize` do not apply (cephadm's `MonitoringSpec` rejects them). |
| `monitoring.promtail` | No | — | Log shipper (`service_type: promtail`); role-less. Ships to loki, so it has no dashboard wiring. |

!!! note "Absent versus present"
    Omitting the `monitoring` block deploys cephadm's **default** monitoring
    stack with cephadm's own placement. Authoring the block switches to
    role-derived placement (like `mon`/`mgr`), with `enabled` defaulting to
    `true`; `enabled: false` skips the stack.

Each monitoring-service block (`prometheus`, `grafana`, `alertmanager`,
`nodeExporter`, `loki`, `promtail`) carries:

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `placement` | No | every host carrying the service's role | See [Shared placement](#shared-placement). |
| `port` | No | cephadm default | Service port. |
| `retentionTime` | No | cephadm default | Retention time (Prometheus only; `retention_time`/`retention_size` exist only on cephadm's `PrometheusSpec`, and every other monitoring service rejects the keys). |
| `retentionSize` | No | `10GB` | Retention size (Prometheus only). cephadm leaves the TSDB size-unbounded and caps it by time alone, so on a Ceph node the TSDB grows on the same filesystem as `/var/lib/ceph` for the whole retention window. Bootwright always renders a `retention_size` to bound it; raise it when you have the headroom. |

### Node root-filesystem budget

Everything cephadm keeps outside the OSD data disks — container images, the
`/var/lib/ceph/<fsid>` data directory, the mon RocksDB store, the crash spool,
the Prometheus TSDB, Grafana, Alertmanager and Loki — lands on the node's root
filesystem. `bootwright preflight` sizes that filesystem per node from the roles
the node declares:

| Term | GiB | Applies to |
| --- | --- | --- |
| Base | 20 | Every storage node (container images, cephadm data dir, crash spool, journal) |
| `mon` | +15 | Nodes carrying the `mon` role (the RocksDB store grows during peering and backfill, and only trims once all PGs are `active+clean`) |
| `mgr` | +5 | Nodes carrying the `mgr` role |
| `prometheus` | +`retentionSize` + 4 | Nodes where the Prometheus service is placed |
| `grafana` | +2 | Nodes where Grafana is placed |
| `alertmanager` | +1 | Nodes where Alertmanager is placed |
| `loki` | +20 | Nodes where Loki is placed (cephadm sets no Loki retention) |

Preflight **fails** below an absolute floor of 20 GiB free and **warns** below
the computed budget. A node that installs under budget still comes up, but its
root filesystem is expected to run short and Ceph's `CephNodeDiskspaceWarning`
alert will fire on the trailing fill rate — see
[Ceph disk-space alerts flap after install](../troubleshooting.md#ceph-disk-space-alerts-flap-after-install).
| `networks` | No | — | Bind the service to one or more CIDRs (cephadm `networks`), e.g. a dedicated management VLAN on multi-homed nodes. |

### Passthrough services

For cephadm service types Bootwright does not model first-class (for example
`rbd-mirror`, `snmp-gateway`), `ceph.services[]` renders field-for-field into a
`ceph orch apply` document.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `services[].serviceType` | Yes | — | cephadm service type. |
| `services[].serviceID` | No | — | cephadm service ID. The `serviceType/serviceID` pair must be unique. |
| `services[].placement` | Yes | — | Must set `hosts` or `sites`; see [Shared placement](#shared-placement). |
| `services[].spec` | No | — | Raw service spec map, rendered 1:1 into cephadm. |

!!! warning "Do not duplicate first-class surfaces"
    Do not declare service types already owned by Bootwright surfaces —
    monitors, managers, OSDs, MDS, RGW, NFS, ingress, or the monitoring
    services.

### Topology

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `topology.nodes` | Yes | — | At least one node. |
| `topology.nodes[].machineRef` | Yes | — | `Machine` with the `ceph-node` capability and declared SSH access. |
| `topology.nodes[].name` | Yes | — | The cluster's name for this node — independent of the machine name and unique within the cluster. A bare label composes to `<name>.<cluster>.<domains.storageClusters>` (the storage-cluster zone; see [Environment → Domain model](environment.md#domain-model)); a dotted value is an explicit FQDN used verbatim. The composed FQDN is the cephadm host-spec hostname, rendered verbatim, and must equal the host's actual OS hostname. |
| `topology.nodes[].site` | When stretch is enabled or any placement narrows by `sites` | — | Failure-domain bucket. Becomes the cephadm host-spec CRUSH location only in stretch mode; `placement.sites` selects against it. No effect otherwise. |
| `topology.nodes[].roles[]` | Yes | — | Ceph roles, such as `mon`, `mgr`, `osd`, `mds`, `rgw`, `prometheus`, `grafana`, `alertmanager`. Roles always become host labels. |
| `topology.nodes[].labels[]` | No | — | Additional free-form cephadm host labels (for example `_admin`). Must not duplicate a role. |
| `topology.nodes[].devices[]` | No | — | Literal OSD device paths; shorthand for `osd.dataDevices.paths`. Requires the `osd` role. Mutually exclusive with `osd`. |
| `topology.nodes[].osd` | No | — | Drivegroup-shaped OSD device selection; see [OSD device selection](#osd-device-selection). Requires the `osd` role. Mutually exclusive with `devices`. |
| `topology.osdDrivegroups[]` | No | — | Fleet OSD specs spanning many hosts; see [Fleet OSD drivegroups](#fleet-osd-drivegroups). |

!!! note "Cross-field rules"
    - An `osd`-role host **must** select devices via `devices[]`, `osd`, or a
      fleet `osdDrivegroups[]` entry that covers it. Consuming all available
      devices is the explicit opt-in `osd: {dataDevices: {all: true}}`, never the
      omission default.
    - `devices[]` and `osd` are mutually exclusive (`devices[]` is the shorthand
      for `osd.dataDevices.paths`).
    - A host is owned by **one** OSD spec: a host covered by a fleet
      `osdDrivegroups[]` entry must not also author per-host `devices[]`/`osd`,
      and no two fleet entries may claim the same host.

#### Fleet OSD drivegroups

For homogeneous racks, `topology.osdDrivegroups[]` renders **one** cephadm OSD
service spanning many hosts (the dominant declarative cephadm idiom) instead of
one spec per host. Per-host `nodes[].osd` remains the override for heterogeneous
nodes.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `serviceID` | Yes | — | cephadm OSD service ID; unique across drivegroups. |
| `placement` | No | every `osd`-role host | Narrow the span by `sites`/`hosts`; see [Shared placement](#shared-placement). |
| `osd` | Yes | — | The drivegroup-shaped selection (same fields as [OSD device selection](#osd-device-selection)). |

#### OSD device selection

All field names render 1:1 into the cephadm OSD drivegroup spec.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `osd.dataDevices` | Yes (when `osd` is set) | — | Data device selector. |
| `osd.dbDevices` | No | — | DB device selector. |
| `osd.walDevices` | No | — | WAL device selector. |
| `osd.filterLogic` | No | `AND` | How cephadm combines the device filters across selectors: `AND` (intersect) or `OR` (union). Spec-level (`filter_logic`). |
| `osd.encrypted` | No | `false` | Enable encrypted (LUKS) OSDs. |
| `osd.tpm2` | No | `false` | Seal the OSD LUKS key in the host TPM (`tpm2`). Requires `encrypted: true`. |
| `osd.osdsPerDevice` | No | cephadm default | OSDs per selected device (non-negative). |
| `osd.crushDeviceClass` | No | — | CRUSH device class for the whole drivegroup. |
| `osd.blockDBSize` | No | — | Per-OSD DB slice size carved from `dbDevices` (`block_db_size`, e.g. `60G`). |
| `osd.blockWALSize` | No | — | Per-OSD WAL slice size carved from `walDevices` (`block_wal_size`). |
| `osd.dbSlots` | No | — | Number of equal DB slices per shared `dbDevices` device (`db_slots`, non-negative). |
| `osd.walSlots` | No | — | Number of equal WAL slices per shared `walDevices` device (`wal_slots`, non-negative). |
| `osd.dataAllocateFraction` | No | `1` | Fraction `(0,1]` of each data device to allocate (`data_allocate_fraction`), reserving headroom. |
| `osd.unmanaged` | No | `false` | Freeze this OSD service so cephadm stops claiming new devices (`unmanaged`, a top-level service-spec key). |
| `osd.serviceOverrides` | No | — | cephadm common service-spec escape hatch: `extraContainerArgs[]`, `extraEntrypointArgs[]`, `networks[]` (CIDRs), and `customConfigs[]` (`{mountPath, content}`, absolute mount path). Rendered as top-level service-spec keys. |

Each device selector (`dataDevices`, `dbDevices`, `walDevices`) mirrors the
cephadm drivegroup device filter:

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `*.paths[]` | No | — | Literal device paths. |
| `*.pathSpecs[]` | No | — | Expanded path form `{path, crushDeviceClass}` pinning a per-device CRUSH class; renders to the cephadm `paths: [{path, crush_device_class}]` mapping. |
| `*.all` | No | `false` | Select all matching devices. |
| `*.model` | No | — | Device model filter (`model`). |
| `*.vendor` | No | — | Device vendor filter (`vendor`). |
| `*.rotational` | No | — | Rotational filter. |
| `*.size` | No | — | Size filter: a single size (`10G`) or a range (`10G:40G`, `:40G`, `10G:`). |
| `*.limit` | No | — | Cap on matching devices (non-negative). |

!!! note "Cross-field rules"
    - `paths`, `pathSpecs`, and `all` are **mutually exclusive** within one
      selector; set at most one. `pathSpecs[].path` is required.
    - A selector must select something: set at least one of `paths`, `pathSpecs`,
      `all`, `model`, `vendor`, `rotational`, `size`, or `limit`. `model`,
      `vendor`, `rotational`, `size`, and `limit` only narrow the match.
    - Address fleet disks by `model`/`vendor` rather than `/dev` paths, which can
      reorder across boots.

!!! warning "Stable device addressing"
    Kernel `/dev/sdX` / `/dev/vdX` names are **not stable** — they can reorder
    across boots, so a name recorded on one boot may point at a different disk on
    the next. Bootwright's OSD ownership marker and the empty-device / destroy
    gates key on the **literal** path string, so an unstable name can make them
    reason about the wrong disk. For explicitly-addressed OSDs prefer a stable
    path — `/dev/disk/by-id/...`, `/dev/disk/by-path/...`, or a `wwn-...` link —
    which is accepted anywhere a path is (`devices[]`, `dataDevices.paths`,
    `pathSpecs[].path`). For homogeneous fleets, a `model`/`vendor`/`size`/
    `rotational` filter avoids per-disk paths entirely.

    There is deliberately **no `wwn`/`serial` filter field** — the selector
    filters mirror cephadm exactly (`model`, `vendor`, `rotational`, `size`,
    `limit`). Per-disk stable selection is expressed as a `/dev/disk/by-id` or
    `wwn` **path** in `paths`/`pathSpecs`, not as a filter.

#### Stretch mode

Authoring the `stretch` block is the enablement signal — its presence turns on
stretch mode. Only `failureDomain` and the tiebreaker node are facts the
operator alone knows; normalize derives the rest from the topology.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `topology.stretch.failureDomain` | Yes (when stretch is enabled) | — | CRUSH failure domain mapping sites to real buckets. |
| `topology.stretch.dataSites[]` | No | the topology's non-tiebreaker sites | Must resolve to exactly the two mon-bearing data sites. Author only when extra OSD-only sites would be wrongly derived. |
| `topology.stretch.tiebreaker.node` | Yes (when stretch is enabled) | — | Mon-only node with no OSD devices, in the tiebreaker site; named by its node name (FQDN or short label), never the machine name. |
| `topology.stretch.tiebreaker.site` | No | the tiebreaker node's site | Must be distinct from `dataSites`. |
| `topology.stretch.ruleName` | No | `stretch-rule` | Stretch CRUSH rule inherited by policy-less stretch pools. |

!!! note "Fixed stretch replication"
    Stretch is supported only as two data sites plus one mon-only tiebreaker
    site. Policy-less replicated pools get `size: 4` / `minSize: 2` as a
    render-time constant — non-4/2 stretch is unsupported and the replication is
    not authorable. Erasure pools are not allowed on stretch-mode clusters. See
    [Ceph topologies](../advanced/ceph-topologies.md) for the full stretch
    walkthrough.

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
| `spec.public.dnsName` | Yes | — | Public S3 endpoint DNS name. |
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
| `dataFoundation.objectGatewayRef` | No | — | RGW `StorageObjectGateway` (same cluster). When set, the consuming add-on's exporter hook passes that gateway's `public.dnsName:port` as `--rgw-endpoint`, adding S3 to the export; omitted, the export is RBD/CephFS only. |

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
