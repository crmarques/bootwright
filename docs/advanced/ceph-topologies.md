---
title: Ceph Storage Topologies
description: Managed vs imported Ceph, distribution and release pinning, entitlements, the bootstrap seed host, FQDN host identity, stretch mode, FIPS, the HA dashboard management gateway, RGW ingress, exports to Data Foundation, and migrating an existing cluster.
---

# Ceph storage topologies

This page is the task-oriented guide to standing up and evolving Ceph storage
clusters. A `StorageCluster` of `type: ceph` with `management: managed` is
bootstrapped by Bootwright with `cephadm`. Ceph keeps no kubeconfig-style admin
file on the controller — the admin keyring and `ceph.conf` live on the seed node
— so day-to-day access is by SSH to the seed node plus `cephadm shell`.

For the full `StorageCluster` object model and every field of the storage kinds,
see the [storage reference](../concepts/storage.md). For a guided first
managed-Ceph apply, see [Provisioning a Ceph cluster](../getting-started/ceph.md).
The complete reference trees that exercise each topology are catalogued under
[Reference examples](examples.md).

!!! note "Scope: managed Ceph vs imported Ceph"
    This page covers `management: managed` clusters — the ones Bootwright stands
    up and converges with `cephadm`. A `StorageCluster` can also be imported with
    `management: external`, in which case Bootwright runs no storage task at all
    and only references the cluster (typically consumed through a `StorageExport`
    and Data Foundation external mode). For the imported path, see the
    [`StorageCluster` reference](../concepts/storage.md#storagecluster) and the
    `baremetal-redfish-imported-ceph-odf` [reference example](examples.md). The
    behavior below applies only when `management` is `managed`.

## Choosing a distribution and pinning a release

`spec.ceph.distribution` selects where Ceph comes from: `oss` (the default, the
upstream community packages and images), `redhat` (Red Hat Ceph Storage), or
`ibm` (IBM Storage Ceph). The subscription distributions install from
entitlement-backed repositories, so `spec.ceph.entitlementRef` must name an
`Entitlement` that resolves to a `redhat-ceph` or `ibm-storage-ceph`
entitlement; `oss` takes no entitlement. An `ibm-storage-ceph` entitlement
carries only the IBM registry and license; the storage nodes register RHEL
through a separate `redhat-rhel` subscription named by their
`MachineInstallProfile.spec.subscription` (or the cluster `osSubscriptionRef`),
which the machines-phase registration task registers each node with — after the OS is in
place and before the Ceph deps work, not inside the cephadm flow. An
entitlement with `rhsm.management: external` delegates that registration to an
operator-supplied `CustomPlaybook` instead. The entitlement model — the
`spec.type` values, license acceptance, the `rhsm.management` axis, and the
credential plumbing — lives in
[Secrets and entitlements](../concepts/secrets.md#entitlements).

`spec.ceph.release` selects which release to install for the chosen
distribution, and Bootwright derives the artifact coordinates from it instead of
looking them up — the leading component is the product stream, and the stream
plus each node's RHEL major build the tools repo, the vendor `.repo` URL, and the
daemon image repository. `oss` takes an upstream release name (`tentacle`) or an
exact `x.y.z` version; the default `20.2.2` pins both the package repository and
`quay.io/ceph/ceph:v20.2.2`. Red Hat and IBM take a dot-separated numeric product
version of any length, defaulting to `9.1` and `9.9.1.0`.

Bootwright keeps no list of releases and no vendor support matrix, so it never
judges the release you declare, nor the RHEL version you run it on. A release
published today installs today. Neither the vendor image build tag nor the Ceph
package build is derivable from a product release, so name them yourself with
`spec.ceph.image.version` and `spec.ceph.packageVersion` to lock the exact build
on both axes. Both pins are required for IBM; Bootwright proves their native
Ceph versions equal before cluster work, without maintaining a release catalog.
To mirror upstream packages for a
disconnected `oss` install, set an HTTPS `spec.ceph.community.mirror`.
Check what a release supports against the vendor's own sources — the upstream
[Ceph releases](https://docs.ceph.com/en/latest/releases/) page, the
[Red Hat Ceph Storage compatibility guide](https://docs.redhat.com/en/documentation/red_hat_ceph_storage/9/pdf/compatibility_guide/Red_Hat_Ceph_Storage-9-Compatibility_Guide-en-US.pdf),
and IBM's [node prerequisites](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=installation-registering-storage-ceph-nodes).

```yaml
ceph:
  distribution: redhat
  release: "9.1"
  packageVersion: "19.2.1-245.el9cp"   # the cephadm RPM build from the vendor's table
  image:
    version: "9-1234"                  # the daemon image build; base is derived
  entitlementRef: rhcs                 # names a redhat-ceph Entitlement
  cephadm:
    bootstrap:
      node: node-01                    # topology node cephadm bootstraps on (node name)
```

The `ceph-distribution-oss`, `ceph-distribution-redhat`, and
`ceph-distribution-ibm` [reference examples](examples.md) show each source end
to end, including the entitlement and license-acceptance workflow.

!!! note "Pin the native build"
    IBM requires `spec.ceph.packageVersion` and `spec.ceph.image.version` from
    one row of its release table. `packageVersion` is the **IBM Storage Package
    Version**, including its RPM release component, not the separate Cephadm
    Ansible Package Version. Bootwright installs that exact native `cephadm`
    RPM, verifies its installed coordinate, runs `ceph --version` inside the
    exact declared image, and refuses unless both native versions match the
    package declaration. Red Hat keeps both fields optional; omit the image pin
    only if accepting its floating vendor default is intentional.

    The package pin names the host CLI, not the daemons. An equal or forward
    package transaction may reconcile in place, but Bootwright never downgrades
    a newer installed Ceph package. Image or release changes on a live cluster
    remain vendor upgrade operations performed out of band.

!!! note "IBM stock preflight is a separate path"
    Bootwright invokes native `cephadm` directly and does not run
    `cephadm-ansible`. IBM's stock preflight uses a moving major-stream
    repository and unversioned package names. Its default `present` state keeps
    an installed package, but a clean host resolves the newest available build;
    `upgrade_ceph_packages: true` selects `latest`. To retain one exact build on
    that stock path, publish only its complete package closure through a frozen
    custom repository or Satellite content view and follow IBM's
    [disconnected preflight procedure](https://www.ibm.com/docs/en/storage-ceph/9.9.0?topic=installation-running-preflight-playbook-disconnected)
    with `ceph_origin=custom` and `custom_repo_url`.

!!! warning "IBM Call Home is an explicit choice"
    IBM Storage Ceph enables Call Home when the license is accepted.
    Author `spec.ceph.ibm.callHome: enabled` to retain it or `disabled` to turn
    it off after bootstrap. Validation rejects an IBM cluster that leaves the
    outbound-communication choice implicit.

## The bootstrap seed host

`spec.ceph.cephadm.bootstrap.node` names the topology node cephadm bootstraps on
(the seed node). The rendered `cephadm bootstrap --mon-ip` is always an address
of that host: the address named by `cephadm.bootstrap.addressRef`, falling back
to `cephadm.addressRef`, and finally the host machine's SSH address.

`bootstrap.node` names a node by its name — the FQDN or its short label
(`node-01`); both resolve, and Bootwright canonicalizes them to the registered
hostname cephadm expects. A `Machine` name is rejected with guidance naming
the node. The same is true of the other authored node references —
`placement.hosts[]` and `topology.stretch.tiebreaker.node`.

## The cluster's node login

A managed cluster owns the account cephadm orchestrates its hosts with.
`spec.ceph.cephadm.clusterSSH.user` defaults to `cephadm` on a managed cluster
(`root` only on an external one), and because the user is not `root`,
`clusterSSH.keyRef` is **required** — it is the key cephadm reaches every host
with, and the key the cluster authorizes for that account. It must name a
second generated `sshKeyPair` `Secret`, distinct from
`Environment.spec.machineAccess.keyRef`: `cephadm bootstrap` moves the cluster
identity into the Ceph mon config-key store, so the fleet service-account key
must not be the one it takes. See
[Storage → The Ceph node login](../concepts/storage.md#the-ceph-node-login) for
the field table and the node-side posture.

## Host identity and FQDN node naming

cephadm registers every `spec.ceph.topology.nodes[]` entry under its
`name` — the cluster's name for the node, independent of the bound machine's
name and **required** on every entry. `name` is a strict DNS label and composes
to the fully-qualified `<name>.<cluster>.<domains.storageClusters>` (e.g.
`node-03.ceph-ibm.bootwright.test`); a dotted value is rejected — author the
sibling `topology.nodes[].fqdn` to pin a name outside that zone (ADR 0025). The
composed FQDN is rendered verbatim into the cephadm host spec, is
the name the mgr dashboard uses to reach monitoring services (Prometheus,
Grafana, Alertmanager), and must equal the host's real OS hostname **and
resolve** cluster-wide:

- For machines whose OS Bootwright installs, the contract holds by
  construction: the installer writes the node FQDN as the OS hostname, and a
  `nameResolution` component the node's `NetworkConfig` references publishes a
  record for it.
- For `os.provided: true` machines, **`apply` writes it**. If a machine's real
  hostname must stay as it is, set the node's `fqdn:` field — `name` stays a
  strict label and remains the token you author in `placement.hosts[]`,
  `bootstrap.node` and the stretch tiebreaker, while `fqdn` pins the name
  cephadm registers, and `apply` then writes that name instead.

A mismatch passes `validate`, which never reaches the host. It is settled on the
host itself: **`apply` writes the name cephadm will register onto every storage
node before it touches the cluster**, then refuses the node if the write did not
hold — which means something on the machine owns the name, such as cloud-init
without `preserve_hostname: true`. `bootwright preflight` reports the pending
rewrite read-only and refuses nothing. cephadm matches that string literally
against the kernel's hostname, so a DNS record for the node name — a CNAME to
the machine, or an A record — does not satisfy it; only the machine's own
hostname does. See
[ADR 0035](https://github.com/crmarques/bootwright/blob/main/specs/adr/0035-a-storage-node-answers-to-the-name-cephadm-registers.md)
and
[ADR 0036](https://github.com/crmarques/bootwright/blob/main/specs/adr/0036-bootwright-writes-the-name-a-storage-node-answers-to.md).

The machine's own DNS name is separate: every `Machine` carries a `fqdn`
address (`<machineName>.<domains.machines>` by default) that Bootwright connects to
and that the node FQDN resolves through — see
[Machines](../concepts/machines.md#the-fqdn-address). A cluster-bound
node's OS hostname must equal its node FQDN, so
`hostname.source: machineName` on a `MachineInstallProfile` is valid only for
machines not bound to any cluster.

### Name resolution

The registered hostname must resolve from the mgr (and every node), or the
dashboard's monitoring integration logs errors such as *"Could not reach
Alertmanager's API on `http://<host>:9093` … Name or service not known"*. A
managed `nameResolution` (dnsmasq) component referenced by the nodes'
`NetworkConfig.nameResolutionRefs` publishes, for every machine it serves, a
`host-record` for the machine's `fqdn` name at its IP and a `cname` from
each node FQDN to the bound machine's `fqdn` (the bare machine label is not
published), and publishes each object gateway's composed public name at its
ingress VIP. Point each storage node's `NetworkConfig` `dns-resolver` at that
component. On provided (external) DNS the operator creates the records, and
the "Name resolution" preflight group names any missing one. See
[Networking](networking.md#name-resolution) for the resolver wiring and record
model.

## OSD device selection

Every `spec.ceph.topology.nodes[]` entry carrying the `osd` role must say which
disks it contributes, in one of three ways: the lean `devices:` list of literal
paths, the drivegroup-shaped `osd:` selection, or coverage by a cluster-wide
`spec.ceph.topology.osdDrivegroups[]` entry. `bootwright validate` rejects an
osd-role host that neither authors `devices`/`osd` nor is covered by an
`osdDrivegroups[]` entry, and rejects either per-host form on a host without the
`osd` role. There is no implicit all-devices default — handing every available
(blank) disk on a host to Ceph is the explicit opt-in
`osd: {dataDevices: {all: true}}`:

```yaml
nodes:
- machineRef: ceph-0
  name: node-01
  roles: [mon, mgr, osd]
  devices:            # literal paths ...
  - /dev/vdb
- machineRef: ceph-1
  name: node-02
  roles: [osd]
  osd:                # ... or a drivegroup-shaped selection
    dataDevices:
      all: true       # explicit opt-in: every available device becomes an OSD
```

The drivegroup form mirrors the cephadm OSD service spec field for field; the
[storage reference](../concepts/storage.md#osd-device-selection) carries the
exact field table.

!!! warning "Dynamic OSD filters are read-only gated"
    Before a managed data, DB, or WAL filter becomes a persistent cephadm
    service, Bootwright resolves it from complete device inventory and refuses
    any unknown probe result. A matching dirty disk is also refused unless it is
    inside the explicitly authorized unbounded auto-reclaim. `--mode rebuild`
    and `--authorize data-loss` do not bypass a narrowing filter. Pin the refused
    disk to `paths`/`pathSpecs`, then run `bootwright apply --clusters <cluster>
    --reclaim-devices <path> --authorize data-loss,unowned-devices`. Only an
    effectively unbounded managed data `all: true` selection with no `limit`
    may use automatic reclaim; see
    [Reclaiming OSD disks](operations.md#managed-os-reinstall-and-owned-ceph-rebuild).

A production drivegroup typically selects data disks by stable `model`/`vendor`
(not `/dev` paths, which reorder across boots) and carves the BlueStore DB onto a
shared NVMe, one slot per OSD:

```yaml
nodes:
- machineRef: ceph-0
  name: node-01
  roles: [osd]
  osd:
    dataDevices:
      model: MZ7LH3T8         # all SAS data disks of this model
      rotational: false
    dbDevices:
      paths: [/dev/nvme0n1]   # one fast device shared by every data OSD
    dbSlots: 8                # carve it into 8 equal DB slices
    encrypted: true
    tpm2: true                # seal the LUKS key in the host TPM
    unmanaged: false          # set true to freeze the drivegroup (no new claims)
```

For a homogeneous rack, do not repeat that block per host: one
`topology.osdDrivegroups[]` entry renders a single cephadm OSD service spanning
every host its `placement` selects, which is the dominant declarative cephadm
idiom. A host covered by a drivegroup must not also author per-host
`devices`/`osd`.

```yaml
ceph:
  topology:
    osdDrivegroups:
    - serviceID: rack-a-nvme
      placement:
        sites:
        - dc1
      osd:
        dataDevices:
          model: MZ7LH3T8
          rotational: false
```

On a mixed-disk host that needs distinct CRUSH classes per device, use the
expanded `pathSpecs` form so pool placement can tier onto a class:

```yaml
osd:
  dataDevices:
    pathSpecs:
    - {path: /dev/sdb, crushDeviceClass: ssd}
    - {path: /dev/sdc, crushDeviceClass: hdd}
```

## Production layout

Bootwright accepts small and single-node Ceph clusters for labs, but
`bootwright validate` emits **warnings** (it never blocks apply) when a cluster
departs from the layout Ceph recommends for production:

- **Monitors** — give the `mon` role to at least **3 hosts**, and keep the count
  **odd** (3, 5, 7) so the cluster holds quorum through a monitor failure.
- **Managers** — give the `mgr` role to at least **2 hosts** for an
  active/standby pair; a single manager is a single point of failure for
  orchestration, the dashboard, and metrics.

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

## FIPS

FIPS has two independent gates. `spec.ceph.security.fips` is the **cluster
gate**: it asserts that the storage cluster runs in FIPS mode. The **node
profile** is set on each storage node's `MachineInstallProfile` for machines
whose OS Bootwright installs, so the RHEL install comes up in FIPS mode.

The desired-state gate checks every managed-OS storage node's install profile:
the profile must also enable FIPS, or validation fails closed. It does not check
the reverse direction — FIPS-enabled node profiles under a non-FIPS cluster
gate pass. A provided-OS node (`os.provided: true`) carries no install profile,
so Bootwright cannot enable FIPS there; that installation remains the
operator's responsibility.

The live gate closes the provided-OS gap before mutation. Standalone storage
preflight, storage-node access apply, and Ceph apply read
`/proc/sys/crypto/fips_enabled` on every selected topology host and require the
value `1`. That proves the running kernel mode on the third-site arbiter just as
it does on managed data nodes. It does not prove the provenance of the vendor's
FIPS build or turn FIPS on after installation.

For IBM Storage Ceph, the
[compatibility matrix](https://www.ibm.com/docs/en/storage-ceph/9.9.0?topic=compatibility-matrix)
directs customers to obtain the FIPS-enabled build through their IBM
representative. The Bootwright kernel gate and package/image parity check cannot
substitute for that IBM-supplied artifact or establish its certification
provenance.

FIPS is an install-time customization, so changing it on an installed machine is
a reinstall; see [managed-OS reinstall](operations.md#managed-os-reinstall-and-owned-ceph-rebuild).

## Stretch mode

`spec.ceph.topology.stretch` is enabled by presence and models a two-site
cluster with a tiebreaker monitor. Each node's site is its failure-domain
bucket: outside stretch mode the failure domain is `host` and the site renders
nothing; under stretch it becomes the host's cephadm CRUSH location, and
`placement.sites` (on monitoring, passthrough services, and the
gateway/filesystem surfaces) selects against it.

A node's site comes from the machine it binds —
[`Machine.spec.placement.site`](../concepts/machines.md#placement), checked
against the [`Environment.spec.sites`](../concepts/environment.md#sites)
registry — so you author it once, on the machine. `nodes[].site` may still be
stated, and is then cross-checked: a node whose site disagrees with its machine
is refused, naming both. Because a site is inert outside stretch, it is
required *exactly where it has effect* — when `stretch` is set or any
`placement` narrows by `sites` — and may be omitted everywhere else.

Two-site stretch comes with **fixed pool replication**: Ceph requires `size: 4`
/ `minSize: 2`. Every `StoragePool` without a `placementPolicyRef` inherits the
stretch CRUSH rule and that replication — including pools that pre-date the
stretch block. Author a `placementPolicyRef` only for genuinely divergent
placement, and note that erasure pools are not allowed on stretch-mode clusters.

The tiebreaker monitor lives at a third site. The estate declares its sites,
each machine says which one it stands in, and the topology just binds machines:

```yaml
# Environment
spec:
  sites:
    - name: dc1
    - name: dc2
    - name: dc3
      description: arbiter-only site
```

```yaml
# Machine/ceph-arbiter
spec:
  capabilities: [ceph-node, ceph-arbiter]
  placement:
    site: dc3
```

```yaml
# StorageCluster — no site is repeated here
ceph:
  topology:
    stretch:
      tiebreaker:
        node: node-03             # the tiebreaker monitor node
    nodes:
    - machineRef: ceph-dc1-0      # site dc1, from the machine
      name: node-01
      roles: [mon, mgr, osd]
      devices: [/dev/vdb]
    - machineRef: ceph-dc2-0      # site dc2
      name: node-02
      roles: [mon, mgr, osd]
      devices: [/dev/vdb]
    - machineRef: ceph-arbiter    # site dc3; tiebreaker node -> node-03
      name: node-03
      roles: [mon]
```

!!! warning "Authoring `stretch` on an existing cluster re-rules its pools"
    Adding `stretch` re-rules and resizes every policy-less pool on the next
    apply, with no change to any `StoragePool` file, and Ceph rebalances the data
    accordingly. `bootwright validate` prints a one-line notice naming the
    inheriting pools, and the resulting `ceph osd pool set` commands are visible
    in the rendered `ceph/operations.yaml`.

### Replacing the arbiter

Moving the tiebreaker to another machine — a data centre going down for
maintenance, arbiter hardware being replaced, the third site lost — is the one
storage change `apply` cannot make. `apply` converges the authored topology, but
a cluster already in stretch mode reaches a new arbiter only through
`ceph mon set_new_tiebreaker`, and a re-apply neither issues it nor retires the
mon it replaces. `storage-cluster replace-arbiter` does exactly that work and
nothing else:

```bash
bootwright storage-cluster replace-arbiter --name ceph-prd-01 \
  --new-arbiter-machine ceph-arbiter-b --yes
```

!!! warning "Editing `tiebreaker.node` and re-running `apply` does not move the arbiter"
    A re-authored `stretch.tiebreaker.node` is structural drift: `apply` refuses
    it before mutating and routes the one-field move to
    `storage-cluster replace-arbiter`. Adding an arbiter to a built cluster or
    removing its last arbiter is a different, unsupported bootstrap-shape
    change: neither this verb nor `--mode rebuild` makes that transition in
    place.

Mark every machine that may hold the tiebreaker with the `ceph-arbiter`
capability (it requires `ceph-node`), give it the site it stands in, and keep it
declared whether or not it carries the arbiter today:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: ceph-arbiter-b
spec:
  capabilities:
    - ceph-node
    - ceph-arbiter
  placement:
    site: dc4
```

`placement.site` is required on every arbiter candidate in a stretched estate,
including a standby that holds no tiebreaker today. It is what lets the
tiebreaker move to a **different** third site — dc3 to dc4 — and be recorded
where it actually landed: the promotion takes the candidate machine's site, so
the replacement mon's CRUSH location is true and the same-site check compares
against a site the host is really in. There is no `--new-arbiter-site` flag;
moving the arbiter to another site means pointing `--new-arbiter-machine` at a
machine that stands there.

!!! warning "Size an arbiter's root disk before you apply it"
    An arbiter is usually a mon-only node, and a mon-only node's root-filesystem
    budget is **20 GiB base + 15 GiB for `mon` = 35 GiB**. On a virtual
    substrate that number is the machine profile's `diskGiB`, and it has to be
    right the first time: the substrate gate refuses an in-place root-disk
    resize, so correcting it costs a `destroy --stage infra` plus a reinstall —
    on the machine holding, or about to hold, the tiebreaker vote. Work the
    number out from
    [Node root-filesystem budget](../concepts/storage.md#node-root-filesystem-budget)
    before the first apply, and give standby candidates the same size as the
    arbiter they may replace.

`--new-arbiter-machine` authors the intent for you: it rewrites the context input
so the stretch tiebreaker names that machine — snapshotting the previous input to
`input-history/` first — and then reconciles the live cluster onto it. Edit the
input by hand and drop the flag if you would rather author the change yourself;
the run reconciles either way. The rewrite lands in the context's input copy,
not your source tree — see
[The context holds a copy of your input](../concepts/index.md#the-context-holds-a-copy-of-your-input)
for the round-trip and the `context update` clobber hazard.

The order never removes before it adds. Bootwright prepares and installs the
replacement machine and Ceph on it, deploys its mon with the stretch CRUSH
location, and waits for that mon to be in the monmap, in quorum, and located —
Ceph's three preconditions for `set_new_tiebreaker` — before moving the
tiebreaker. Only then does it retire the replaced mon and remove its host. Any
failure ahead of the swap therefore leaves the original arbiter in place with the
cluster's quorum intact, every step is idempotent, and re-running resumes where
it stopped. A run whose desired arbiter already holds the tiebreaker reports a
no-op.

Preview the live plan with `--dry-run`. Add `--output json` for a clean
machine-readable report containing `plan`, `inputRewrite`, the four-step
`order`, and an always-present `requiredAuthorizations` array. JSON is
preview-only; omitting `--dry-run` is a usage error and changes nothing.

If live evidence prevents a plan, copy only the continuation Bootwright prints.
It preserves the selected context, SSH identity, authorizations, and output
intent; a JSON dry-run remedy keeps both `--dry-run` and `--output json`, and a
remedy never adds `--yes`. `stretch_mode: false` offers an apply prerequisite
only for a storage task positively classified to create. On a recorded cluster,
diagnose the live/recorded shape mismatch instead: a plain apply may skip, and no
Bootwright retry safely turns a built non-stretch cluster into a stretch one. A
preview shows only the read-only form of that prerequisite; choosing the real
apply remains a separate explicit decision. A
missing live tiebreaker instead names the authored mon in the exact external
`ceph mon set_new_tiebreaker` repair; ambiguous leftover mons must be identified
from Ceph evidence before any one is retired.

Three situations fail closed and name the token that proceeds:

| Situation | Why it stops | Token |
| --- | --- | --- |
| The replacement shares a site with the data-site mons | An arbiter inside a data site cannot break a tie between the two — lose that site and two votes go at once. Ceph refuses it too, without `--yes-i-really-mean-it` | `--authorize same-site-arbiter` |
| Declared mons are outside quorum | `set_new_tiebreaker` needs a quorum to commit, and swapping the arbiter during a site outage removes the vote holding the remaining quorum together | `--authorize degraded-quorum` |
| The arbiter being replaced cannot be contacted | Retiring it needs `ceph orch host rm --offline --force` and skips host-local cleanup — only ever for a host the probes *prove* absent, never one that answered and refused an identity | `--authorize unreachable-nodes` |

The machine that was replaced keeps running with its OS intact; only its Ceph
membership is removed. Tear it down separately when you no longer want it. If
you authored the new tiebreaker by hand and the old machine remains a declared
node, completion prints the exact scoped destroy. When
`--new-arbiter-machine` performed the promotion, the old machine has already
left the topology and Bootwright's machine selector refuses it as an orphan, so
completion tells you to decommission it out of band and prints no unusable
destroy command.

!!! warning "The same-site fallback is a shape you have to leave"
    `--authorize same-site-arbiter` puts the tiebreaker inside a data site while
    the third site is gone. That state is authorable and re-appliable — the
    input the verb writes loads cleanly and `bootwright validate` reports it as
    a WARN rather than refusing it — precisely so an emergency is not also a
    validation dead end. It is still a cluster that stops serving if that data
    site fails, because its two replicas and the tiebreaker vote go together.
    Move the arbiter back to a third site as soon as one exists. Note the
    asymmetry: a cluster *already* in stretch mode tolerates this, but one that
    has never entered stretch mode cannot be bootstrapped this way — Ceph
    refuses `enable_stretch_mode` outright when the tiebreaker shares a data
    site.

## The management gateway and HA dashboard

`spec.ceph.mgmtGateway` renders a native cephadm **management gateway**
(`mgmt-gateway`) fronted by a `keepalive_only` ingress, giving the Ceph
dashboard a single highly-available VIP that floats across the mgr hosts instead
of pinning operators to the active mgr's address. The VIP and its DNS name show
up in `bootwright cluster info`, and a managed `nameResolution` component
should publish that name.

You do not spell that name out. `spec.ceph.mgmtGateway.dnsLabel` is the leftmost
label only — Bootwright composes the published name as
`<dnsLabel>.<StorageCluster name>.<domains.storageClusters>`, the same
composition that gives Ceph nodes their FQDNs. `dnsLabel` defaults to `mgr`, so
a `ceph-ibm` cluster on `example.com` publishes `mgr.ceph-ibm.example.com`
untouched, and `dnsLabel: dashboard` publishes
`dashboard.ceph-ibm.example.com`. A dotted value is rejected: the cluster and
domain arms are not overridable per cluster.

`mgmtGateway.ingress` is **required** whenever the block is present — it is the
`keepalive_only` VIP itself, so `name`, `address`, and `prefixLength` must all be
set (`virtualInterfaceNetworks[]`, `placement`, and `firstVirtualRouterID` are
optional, and follow the RGW ingress rules below):

```yaml
ceph:
  mgmtGateway:
    dnsLabel: dashboard
    exposure: http
    ingress:
      name: lab
      address: 192.168.140.81
      prefixLength: 24
```

`exposure` declares the scheme the gateway itself serves and defaults to
`https`. On `redhat`/`ibm` it must be authored explicitly: current vendor
cephadm builds reconfigure any https gateway forever — cephadm's own
certificate included — so `exposure: http` (plain HTTP on the authored port)
is the only shape that converges there today, and an explicit
`exposure: https` is the deliberate flip for a vendor build that repairs the
dependency recording (ADR 0047 and ADR 0049 tell that story). With `http`,
`tls` and `oauth2Proxy` are rejected — neither certificates nor SSO cookies
belong on a cleartext listener.

The `ceph-ibm-libvirt-lab` and
`ceph-ibm-baremetal-redfish` [reference examples](examples.md) build the HA
dashboard end to end.

The management gateway needs **Ceph 20 (tentacle) or later** — no earlier
release defines the `mgmt-gateway` or `oauth2-proxy` service. With
`distribution: oss`, where `spec.ceph.release` names the Ceph version itself,
`validate` refuses an older release outright. For a vendor distribution the
product version is not a Ceph version, so the release is checked against the
running manager daemons at apply time instead, before any spec is written.

Two optional blocks secure the gateway:

- `spec.ceph.mgmtGateway.tls` (`certificateRef` + `keyRef`, both required
  together) supplies a real certificate for the gateway frontend (cephadm
  `ssl_cert` / `ssl_key`). Without it an https gateway
  serves a self-signed cert that browsers reject. Accepted only with
  `distribution: oss` and `exposure: https` (ADR 0047).
- `spec.ceph.mgmtGateway.enableAuth: true` puts the dashboard behind SSO and
  **requires** `spec.ceph.mgmtGateway.oauth2Proxy` — Bootwright deploys the
  cephadm `oauth2-proxy` daemon (`providerDisplayName`, `clientId`,
  `clientSecretRef`, `oidcIssuerUrl`, optional `redirectUrl`, `httpsAddress`,
  `allowlistDomains`, `cookieSecretRef`). `enableAuth` without `oauth2Proxy`, or
  an `oauth2Proxy` block without `enableAuth`, is rejected by `validate`.

The cert and OIDC secrets are read from their secrets and inlined only into a
`0600` spec on the seed host (applied separately from the static service spec),
so they never land in a locally-rendered file.

## RGW and ingress

`StorageObjectGateway` owns the RGW service, its storage-owned public S3
endpoint, and its ingress VIPs. `spec.public` is the public endpoint;
`spec.ceph.ingresses[]` are the concrete keepalived ingress VIPs, each with a
`prefixLength` and `virtualInterfaceNetworks[]` telling cephadm which site-local
subnet can host the VIP. The endpoint and ingress fields are detailed under
[Networking](networking.md#storage-rgw-endpoints); the gateway object itself is
in the [storage reference](../concepts/storage.md#storageobjectgateway).

For a stretch cluster, the site-local pattern is one `StorageObjectGateway` per
data site, each with `spec.ceph.placement.sites` and its ingress `placement.sites`
narrowed to that site, its own VIP, its own `firstVirtualRouterID`, and its own
`public.dnsLabel` — every RGW daemon, ingress VRRP group, and HAProxy backend set
then stays inside one data center, and each ODF cluster is pointed at the one
gateway local to it. Siblings may share a `realm`/`zoneGroup`/`zone` (or all
leave it unset) without that being RGW multisite — Bootwright never configures
cross-zone replication. A single cluster-wide gateway with one unnarrowed
ingress (or several site-scoped ingresses under one gateway) is also accepted
for a simpler, non-stretch-local topology, such as an L2 network extended
across both data centers; that shape does not give per-site backend locality.

## Pools, filesystems, and placement

`StoragePool` owns one Ceph pool (`replicated` or `erasure`),
`StorageFilesystem` owns a CephFS filesystem and its MDS placement, and
`StoragePlacementPolicy` holds reusable CRUSH placement plus replicated-pool
defaults that pools select with `placementPolicyRef`. The pool's structural
identity is its `type` and erasure profile: changing it is the only desired-state
change that rebuilds a live pool (data-destroying, `--mode rebuild` only); replicas,
CRUSH rule, and application reconcile in place. The full field tables are in the
[storage reference](../concepts/storage.md#storagepool).

## Exporting storage to Data Foundation

`StorageExport` exposes storage services for downstream consumers. The common
consumer is OpenShift Data Foundation in **external** mode: a Data Foundation
add-on declares a `storageExportAttachment` input effect, and a
`ClusterAddonBinding` supplies the `StorageExport` name for one installed
cluster. The `baremetal-redfish-imported-ceph-odf` and
`baremetal-redfish-multidc-virtualized-odf-ceph` [reference examples](examples.md)
wire export-to-ODF for imported and managed Ceph respectively. The export object
model is in the [storage reference](../concepts/storage.md#storageexport).

That wiring is not a support certification. Verify the exact Data Foundation
and external Ceph releases in Red Hat's authenticated
[ODF Supportability and Interoperability Checker](https://access.redhat.com/labs/odfsi/)
before deployment. A public advisory that names a matching Red Hat Ceph basis
does not by itself certify an IBM Storage Ceph external-cluster pairing; for a
FIPS deployment, also confirm access to the IBM FIPS-enabled build.

`spec.dataFoundation.objectGatewayRef` names the `StorageObjectGateway` ODF's
object storage should use; set it and the exporter step passes that gateway's
public endpoint as `--rgw-endpoint`, so external mode also gets S3 alongside
RBD and CephFS. Leave it unset for a block/file-only export. Each ODF cluster
should get its own `StorageExport` naming its own site-local gateway, so
distinct OCP clusters never share one S3 endpoint.

The step also stages the certificate the gateway's ingresses declare and passes
it as `--rgw-tls-cert-path`. That is what lets the exporter verify an https
gateway — the endpoint is dialled with python-requests, which trusts neither the
node's store nor a certificate Bootwright generated for the ingress — and it is
also what carries the certificate into the export as its `ceph-rgw-tls-cert`
entry, from which Data Foundation reads the endpoint as TLS. Without it an
`https` gateway is attached as cleartext on its TLS port. Every ingress of one
gateway must therefore point `tls.certificateRef` at the same certificate; the
step refuses a gateway that declares several, because only one can reach Data
Foundation.

## Cephx key types

A cephx key is a typed structure, not a bare secret: a crypto-type ID, a
timestamp, a length, then the secret bytes. A client that does not recognise the
type ID cannot load the keyring at all — it fails before it ever authenticates.

Some vendor builds mint AES-256 keys (`aes256k`, base64 starting `Ag`) and
refuse to mint the older AES-128 type (`aes`, starting `AQ`) at all, because the
set of ciphers they permit is policy held in the **mon map**. A consumer built
against an older Ceph client — Data Foundation 4.21 is the case in the field —
has no handler for `aes256k` and reports `Malformed input` and
`monclient: keyring not found` against a key that is perfectly valid. Upstream
Ceph does not mint `aes256k` at all, so this is a property of the vendor build
rather than of the Ceph version number.

`spec.ceph.security.cephx.keyType` declares which cipher this cluster mints:

```yaml
spec:
  ceph:
    security:
      cephx:
        keyType: aes
```

Leave it unset and Bootwright emits no cipher operations, so the build's own
policy stands. Declaring `aes` widens the mon map's allowed ciphers to
`aes,aes256k` — never narrowing, so keys already in use keep working — and makes
`aes` the type new keys are minted with. Declaring `aes256k` restores the
vendor default, which will break any client still holding an `aes` key.
Bootwright never touches `auth_service_cipher`: rotating the monitors' own
service cipher invalidates every client's service key.

Two consequences are worth stating plainly. First, this is a **cluster-wide
security downgrade** — every client of the cluster, not only Data Foundation,
gets AES-128 keys from that point on. Prefer a consumer that understands
`aes256k`, and check the vendor support matrix for the pairing before declaring
this. Second, policy alone does not rewrite keys that already exist: the Data
Foundation exporter adopts an existing cephx entity verbatim. When a key type is
declared, the export step removes the `client.healthchecker` and `client.csi-*`
entities whose keys were minted at another type so the exporter remints them —
and never touches `client.admin`, mon, mgr, or OSD keys.

## Convergence is additive-only

`apply` creates and converges what desired state declares and never removes a
live Ceph object whose declaration was deleted. The rule is storage-wide,
covering every surface: `spec.ceph.config` keys, `mgrModules[]`, `monitoring`
services, `services[]` passthrough entries, and the `StoragePool`,
`StorageFilesystem`, and `StorageObjectGateway` kinds. Deleting one from git and
running `bootwright apply` reconciles cleanly while the live pool, filesystem, or
service keeps running — remove it on the cluster with the `ceph`/`cephadm` CLI
when you mean it.

`--mode rebuild` does not prune undeclared objects either: it rebuilds only
still-declared objects whose structural identity changed: a pool's `type` and
erasure profile, or a CephFS metadata pool — changing the metadata pool is a
data-destroying `ceph fs rm` recreate, not an in-place reconcile. See
[Operations and recovery](operations.md#managed-os-reinstall-and-owned-ceph-rebuild)
for the owned-Ceph wipe-and-rebuild path.

!!! note "Storage sub-objects are not orphan-tracked"
    `bootwright diff` correlates whole clusters, machines, and providers,
    so a deleted pool, filesystem, gateway, or passthrough service inside a
    still-declared `StorageCluster` does not appear under **"Owned but no longer
    declared"**.

## Accessing a managed cluster

`bootwright cluster info` prints everything needed to reach a managed Ceph
cluster, derived entirely from desired state — the seed node SSH line, the
monitor list, a health-check command, the dashboard URL, and the
`cluster info --name <cluster> --secrets` command that reveals the dashboard
password. The seed login is the cluster's own account (`cephadm` by default,
[above](#the-clusters-node-login)), not root. Run the **Health check** line;
`HEALTH_OK` from `ceph -s` confirms the cluster is reachable and healthy.

When `spec.domains.storageClusters` is set, the dashboard URL is
`https://mgr.<cluster>.<domains.storageClusters>` instead of the seed node's
bare address, even without an explicit `spec.ceph.mgmtGateway` block — the same
`mgr.` alias convention the HA gateway uses below. Bootwright does not publish
that DNS record itself outside the HA gateway case; without a `nameResolution`
component (or another record) pointing it at the active mgr, resolve it
manually or fall back to the seed node's address.

`cephadm bootstrap` enables the dashboard and generates a one-time random
`admin` password. Bootwright captures it **during the install** and saves it on
the controller (`clusters/<storage-cluster>/secrets/dashboard-password`, mode
`0600`), exactly like the kubeadmin password for OpenShift clusters.

!!! note "Dashboard password is captured at install only"
    The password is captured solely from the one-time `cephadm bootstrap`. It is
    never re-read or re-synced on later applies. `cluster info` prints a reveal
    command by default and the password bytes only when you pass `--secrets`;
    the on-disk credential is an encrypted envelope, so reading the file
    directly does not work. The file persists after the cluster is destroyed; delete the
    cluster's `secrets/` directory by hand if you want the credential gone. If
    the file is lost or the in-cluster password was changed, see
    [Recovering the Ceph dashboard password](../troubleshooting.md#recovering-the-ceph-dashboard-password).

## Migrating an existing cluster

Two desired-state changes carry data-movement risk on an already-bootstrapped
cluster:

- **Renaming a node's Ceph identity** — `topology.nodes[].name` is required and
  declared explicitly on every node. Setting it to a value cephadm has never
  registered makes the rendered topology name a host cephadm has never seen —
  cephadm would re-add it and move data. On an already-bootstrapped cluster set
  each `topology.nodes[].name` to the identity cephadm already registered
  before the next apply.
- **Authoring `stretch`** re-rules and resizes every policy-less pool, as
  described above. Confirm the validate notice naming the inheriting pools, and
  plan for the rebalance.

For the destructive rebuild paths (managed-OS reinstall, owned-Ceph
wipe-and-rebuild) and the `--mode rebuild` authorization model, see
[Operations and recovery](operations.md).
