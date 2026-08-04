# Desired-State Model

Bootwright desired state uses `apiVersion: bootwright.io/v1alpha1` and a fixed
set of user-authored kinds. The schema intentionally tracks the inputs
consumed by `openshift-install` for agent installs, Bootwright-managed machine
OS installation, and cephadm for external Ceph storage.

There is no compatibility layer for abandoned kinds or fields. Retired resource
shapes must fail strict decode or validation instead of being translated.

## Kinds

The kinds and the fact each owns are listed in `domain.md` (Operating Model).
This document specifies each kind's fields, validation, and the CLI contract.
Per-field Required/Default tables live in `docs/concepts/<kind>.md`; this
document owns the normative rules, and the docs pages link back to it.

Every object carries the same envelope: `apiVersion`, `kind`, and `metadata`.
`metadata.name` is required and must be a DNS label
(`[a-z0-9]([-a-z0-9]*[a-z0-9])?`). `metadata.labels` is an optional string map.

Block-style collections are a repository-asset convention, not a constraint on
operator-authored input: shipped add-ons, examples, e2e inputs, loader fixtures,
and `bootwright example init` output use block style — no flow-style mapping
braces, inline lists, or empty inline maps — and a repository check enforces it
over those trees. The loader accepts either style from any input.

Authored input keeps one object per file, with two exceptions: a cluster root
lives in `cluster.yaml`, and a tree's `Secret` objects are grouped into a single
multi-document `secrets.yaml` so the operator has one place to see which bytes
must be supplied. Filenames are role-based where the role is unambiguous
(`cluster.yaml`, `provider.yaml`, `networks.yaml`, `cluster-machines.yaml`) and
named after `metadata.name` otherwise. `bootwright example init` emits that
nested, role-named tree — including a multi-document `secrets.yaml` — while the
small single-cluster examples are deliberately flat. Directory layout is in
`docs/advanced/fleets.md`.

Kinds: [Environment](#environment) · [Entitlement](#entitlement) ·
[Machine](#machine) · [MachineImage](#machineimage) ·
[MachineInstallProfile](#machineinstallprofile) ·
[InfraProvider](#infraprovider) · [InfraComponent](#infracomponent) ·
[NetworkConfig](#networkconfig) · [ContainerCluster](#containercluster) ·
[StorageCluster](#storagecluster) ·
[StoragePlacementPolicy](#storageplacementpolicy) ·
[StoragePool](#storagepool) · [StorageFilesystem](#storagefilesystem) ·
[StorageObjectGateway](#storageobjectgateway) ·
[StorageNFSExport](#storagenfsexport) · [StorageExport](#storageexport) ·
[ClusterAddon](#clusteraddon) · [ClusterAddonProfile](#clusteraddonprofile) ·
[ClusterAddonBinding](#clusteraddonbinding) ·
[CustomPlaybook](#customplaybook) · [Secret](#secret).

Contracts: [References and Names](#references-and-names) ·
[Native add-on catalog store](#native-add-on-catalog-store) ·
[Rendering Contract](#rendering-contract) ·
[Validation Rules](#validation-rules) · [CLI Contract](#cli-contract)
([Diagnostics and refusals](#diagnostics-and-refusals) ·
[Selection and stages](#selection-and-stages) · [Destroy](#destroy) ·
[Authorizations](#authorizations) ·
[Apply modes and drift](#apply-modes-and-drift) ·
[Validate, machine trust, add-ons, and diff](#validate-machine-trust-add-ons-and-diff)).

## References and Names

- Every `Ref`/`Refs` field resolves globally by `metadata.name`, per kind,
  against the loaded state after `Environment` selection. The authoring grammar
  — the plain-name string form and its exceptions — is ADR 0014.
- Object names are unique within a kind: a duplicate fails validation naming the
  kind and the name. `Environment` (which carries its own cardinality rule) and
  `NetworkConfig` are exempt from that check; a duplicated `NetworkConfig` name
  resolves to the last one loaded, so keep those names unique too.
- `ContainerCluster` and `StorageCluster` additionally share one cluster
  selection namespace, and the name `artifact-server` is reserved — see
  [Validation Rules](#validation-rules).

## Environment

`Environment` is fleet-wide. It contains the fleet DNS base domain, defaults,
optional input resource selection, and secret references, never secret bytes.

Rules:

- Exactly one `Environment` must be present in the loaded, selected state. Zero
  or more than one is a validation error naming the objects found, so every
  `Environment`-scoped default, selection list, and domain has a single owner.
- `domains` owns the fleet DNS zones, one key per identity class, and
  `Environment` is their single owner (ADR 0018). `domains.base` is the only
  required key and is the default the others fall back to: `domains.machines`
  and `domains.clusters` default to `domains.base`, and
  `domains.containerClusters` and `domains.storageClusters` default to
  `domains.clusters`. Each `Machine`'s implicit `fqdn` composes from
  `domains.machines`, container-cluster node FQDNs and each cluster's
  `install-config.yaml` `baseDomain` from `domains.containerClusters`, and
  storage-cluster node FQDNs from `domains.storageClusters` (see the `Machine`
  and cluster host rules). `spec.domains` is a DNS object, distinct from the
  `spec.containerClusters[]` / `spec.storageClusters[]` selection lists below.
  A single-zone fleet sets only `domains: {base: x}`; every other key then
  defaults to it.
- `sites[]` is the estate's site registry: every site the estate spans, each an
  object with a required DNS-label `name` (it renders as a CRUSH bucket name)
  and an optional `description`. Names are unique. The registry is optional —
  an estate that never names a site declares nothing — but as soon as anything
  names a site (`Machine.spec.placement.site`, a topology node's `site`,
  `stretch.dataSites`, or any `placement.sites` filter) it becomes required and
  every reference must name a declared site. That is what makes a mistyped site
  fail at load instead of silently becoming an extra CRUSH bucket. A declared
  site no `Machine` stands in is an INFO advisory, not an error: a fallback
  arbiter site may be declared before its candidate exists (ADR 0048).
- `resources[]`, when set, is a YAML file or directory allow-list relative to
  the `Environment` file directory. The `Environment` file itself is always
  loaded.
- When `resources[]` is omitted, the current context's input directory loads
  every discovered YAML file.
- A listed file is loaded as a complete YAML file. A listed directory is walked
  deterministically for YAML files.
- `validate` reports every discovered YAML file under the context input
  directory that `resources[]` excludes, naming the file and the authored
  objects it contains — a warning, not an error, because narrowing is
  legitimate: `warning: Environment/<env> spec.resources excludes <relpath>
  (<Kind>/<name>[, ...]); remove the path from spec.resources or add "<dir>"
  to load it`. When `resources[]` is omitted nothing is excluded and no
  warning is possible. A discovered file that declares no Bootwright object is
  not reported: the warning exists for authored intent that silently never
  runs, and a stray note or a vendored YAML is neither. `<dir>` is the path to
  add — the file's own relative path when it sits beside the `Environment`,
  because `resources[]` rejects `"."` and advice that fails validation is not
  advice.
- Every referenced Bootwright resource must also be selected.
- `containerClusters[]` and `storageClusters[]`, when set, are the effective
  fleet selection lists for render, apply, status, destroy, and check flows.
  Loaded clusters outside the selection are excluded before validation runs;
  `bootwright validate` warns about each excluded cluster so an unselected
  cluster file never disappears silently. An omitted list selects every cluster.
  An authored but empty list is rejected naming the field: it reads as "select
  nothing" but would otherwise select the whole fleet, so an accidentally-emptied
  (for example templated) list cannot silently widen apply/destroy scope.
- `safety.destroyProtection`, when set, must be `allow` or
  `protected`. Empty means `allow`. Bootwright never infers protection
  from environment names, context names, labels, or cluster names.
- `safety.protectedKinds`, when set, lists object kinds — `ContainerCluster`,
  `StorageCluster`, or `Machine` — that require `destroy --authorize protected`
  even when `destroyProtection` is `allow`. `apply` has no protected-teardown
  authorization: a destructive `apply --mode rebuild` of a protected kind
  instead fails closed, routing the operator through
  `destroy --authorize protected` for the affected scope followed by a
  re-apply. It is the granular tightening: protect the fleet's Ceph and
  machines without blanket friction on scratch container clusters. An unknown kind
  fails validation naming the object and the allowed set.
- `defaults.install.pullSecretRef` and `defaults.install.nodeSSH` fill omitted
  cluster install values only.
- The `infraComponents.artifactServers[]` entry marked `default: true` (or the
  sole entry when only one is defined) is the fleet-wide default artifact server
  selector. It supplies only `serverRef` for consumer-owned
  `artifactServerEndpoint` fields; every consumer still authors its own
  `endpointRef`.
- `defaults.clientsMirror`, when set, must be an `http(s)` URL. It overrides the
  base URL Bootwright downloads the OpenShift clients (`oc`,
  `openshift-install`) from, for disconnected or mirrored labs.
- `defaults.virtctlMirror`, when set, must be an `http(s)` URL. It is the
  `clientsMirror` counterpart for `virtctl` (the KubeVirt client used for VM
  image uploads): it overrides the base URL Bootwright downloads `virtctl` from,
  for disconnected or mirrored labs.
- `defaults.helmMirror`, when set, must be an `http(s)` URL. It is the
  `clientsMirror` counterpart for `helm`: it overrides the base URL Bootwright
  downloads `helm` from. `helm` is not tied to an OpenShift release, so
  Bootwright tracks the mirror's `latest` channel and verifies the binary
  against that channel's `sha256sum.txt` rather than pinning a version.
- `infraComponents.*[]` entries are service access catalog entries. The catalog
  has five slots — `proxies[]`, `nameResolution[]`, `artifactServers[]`,
  `registries[]`, and `ntp[]`. Each entry's `management` is either `external`
  with direct access configuration or `managed` with `componentRef` pointing at
  an `InfraComponent` arm of the matching kind plus `endpointRef` naming the
  served endpoint on that component. The word `type` is reserved API-wide for
  kind-of-thing discriminators (such as `InfraComponent.spec.type`), so the
  who-runs-it axis is spelled `management` here, matching
  `StorageCluster.spec.management`.
- Load balancers have no catalog slot: they are endpoint-scoped, not fleet-wide.
  A managed one is referenced directly from the consuming endpoint through
  `ContainerCluster.spec.install.endpoints.<slot>.source.componentRef` plus
  `source.bindAddressRef`, and an operator-run one is declared at that endpoint
  as `source.type: external` with an `address`.
- `proxyFor.bootwright`, `proxyFor.containerClusterInstall`, and
  `proxyFor.machineOSInstall` override, per consumer, which proxy catalog entry
  applies: a name selects that entry, `none` opts the consumer out, and an empty
  slot inherits the entry marked `default: true`. At most one catalog entry may
  be `default`, so one external default proxy routes every consumer with no
  `proxyFor` block. `proxyFor.machineOSInstall` routes the managed-OS (Anaconda)
  install fetch: a boot ISO carries no packages, so Anaconda reaches the install
  tree or the Red Hat CDN over the network during install. Only an external proxy
  entry applies here, because the node installs before any managed proxy component
  could exist — a managed value, or inheriting a managed default, is rejected.
- `secretStorage.mode`, when set, must be `source` (default) or `context`.
  `context` requires `bootwright secret generate` to copy `file:`-sourced
  material into the encrypted context store before workflows read it; `source`
  reads operator file material in place.
- `registries.mirror`, when set, declares the disconnected mirror: optional
  `url` (external mirror) plus `trustBundleRef` and `credentialsRef` secret
  names. `registries.imageDigestSources[]` set `source`, `mirrors[]`, and an
  optional `sourcePolicy` of `NeverContactSource` or `AllowContactingSource`. A
  `ContainerCluster` with `install.mode: disconnected` requires
  `registries.mirror.trustBundleRef` plus either `registries.mirror.url` or a
  managed `infraComponents.registries[]` entry.
- `installTrust.caBundleRefs[]` are fleet-wide additional CA trust-bundle secret
  names rendered into cluster install trust.
- `componentImages` pins managed-service images as
  `componentImages.<componentType>.<implementation>`, keyed by the same
  component-type vocabulary as `InfraComponent.spec.type`. The accepted pairs
  are `loadBalancer.haproxy`, `registry.mirror-registry`, `proxy.squid`,
  `nameResolution.dnsmasq`, and `artifactServer.http`. Each entry sets at least
  one of `local` or `public`, and every reference must pin an explicit version
  tag or a `sha256:` digest; mutable references such as `:latest` or an omitted
  tag are rejected.
- Secret material is no longer an `Environment` field. The former `secrets[]`
  list was removed and promoted to a first-class [`Secret`](#secret) kind; the
  only secret-related `Environment` field is `secretStorage.mode`.
- Entitlements are not an `Environment` field either: they are the first-class
  [`Entitlement`](#entitlement) kind.

## Entitlement

`Entitlement` declares named subscription, registry entitlement, and license
references for products that need vendor-controlled access. The secret material
it names is declared as [`Secret`](#secret) objects; an `Entitlement` never
carries secret bytes.

Rules:

- `spec.type` is the discriminator, from this set; other values are rejected:
  - `redhat-rhel`: a RHEL subscription (RHSM), for the RHEL BaseOS/AppStream
    repos.
  - `redhat-ceph`: a single Red Hat subscription covering both RHEL and the
    `rhceph` tools repo, plus `registry.redhat.io` access.
  - `ibm-storage-ceph`: IBM Storage Ceph product access (registry + license).
- A `redhat-rhel` and a `redhat-ceph` entitlement require `rhsm`
  (`organizationRef`, `activationKeyRef`); `redhat-ceph` also requires
  `registry.credentialsRef`. An `ibm-storage-ceph` entitlement requires
  `registry.credentialsRef` and `license.accept: true`; it takes no inline
  `rhsm` arm. The RHEL subscription it runs on is named separately by the
  storage nodes' `MachineInstallProfile.spec.subscription` or
  `StorageCluster.spec.ceph.osSubscriptionRef`.
- `registry.url`, when set, is a scheme-less `host[:port][/namespace]` address
  with no credentials, query, fragment, empty path segment, or trailing slash.
  `registry.trustBundleRef`, when set, names a CA-bundle `Secret` trusting that
  registry.
- `rhsm.satellite`, when set, binds RHSM registration to a Red Hat
  Satellite/Capsule: `hostname` is required and must be a bare host (no scheme or
  path); `trustBundleRef` names the Capsule CA `Secret`; and `contentBaseURL` is
  a validated `http(s)` content root that normalizes to
  `https://<hostname>/pulp/content` when omitted. It is rejected when
  `rhsm.management` is `external` (the operator playbook owns Satellite binding).
- The `rhsm` arm carries a who-runs-it axis: `rhsm.management` is `managed`
  (the default when unset) or `external`. Managed registration runs as a
  machines-phase `registration.<cluster>` task on the storage nodes after the
  OS is in place and before the cluster `deps` work — Satellite binding and CA
  trust, `rhsm.conf` proxy and content-CA convergence, `subscription-manager`
  registration, and optional Insights enrollment.
- With `management: external` Bootwright plans no registration task and never
  touches `rhsm.conf`; the arm must then carry only `management`
  (`organizationRef`, `activationKeyRef`, `satellite`, and `connectToInsights`
  are rejected), and the operator owns registration — typically through a
  [`CustomPlaybook`](#customplaybook) with `gates: deps`, which runs after the
  machines phase and (with the default `onFailure: fail`) blocks the deps-phase
  Ceph work; a `follows: machines` playbook runs at the same point in time but
  does not gate later phases. The delegated playbook must leave the nodes able
  to install the distribution's packages. Under `external`, the
  subscription-backed repository enablement (which purges unlisted repos) is
  also skipped so operator-enabled repo sets survive; the cephadm install assert
  remains the fail-closed package-availability gate. Ceph commands run through
  `cephadm shell`, so Bootwright does not install a host `ceph-common` package.
- Registration is applied at the machines phase and reversed on teardown: a
  `destroy` covering a managed-RHSM storage node runs the
  `destroy.machine-registration` task, limited to the storage hosts, which
  unregisters the node from RHSM before its substrate is deleted (ADR 0023). An
  entitlement change on its own never registers or unregisters a node.

## Machine

`Machine` is the single user-facing model for raw hardware, virtual machines,
machines whose OS is already installed, and machines whose OS Bootwright should
install.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: ocp-master-0
spec:
  capabilities:
    - openshift-node
  substrate:
    providerRef: rack-a
  hardware:
    nics:
      - name: primary
        macAddress: 52:54:00:21:11:10
    boot:
      nicRef: primary
    management:
      bmc:
        address: redfish-virtualmedia+https://bmc-0.example.test/redfish/v1/Systems/1
        credentialsRef: bmc-credentials
  os:
    provided: false
    install:
      rootDeviceHints:
        deviceName: /dev/sda
  network:
    config:
      networkConfigRef: ocp-machine-net
      interfaceAddresses:
        - interface: primary
          addressRef: ip
          prefixLength: 24
  addresses:
    - name: ip
      address: 192.0.2.20
```

Rules:

- `spec.os.provided` is required, and together with `spec.os.installProfileRef`
  it selects one of three OS lifecycles. These are the names used throughout the
  documentation:
  - **OS-ready** (`os.provided: true`): the OS already exists. The machine
    declares how it is reached (`spec.access`) and Bootwright installs nothing.
  - **Bootwright-installed** (`os.provided: false` with an
    `os.installProfileRef`): Bootwright installs the OS from the referenced
    [`MachineInstallProfile`](#machineinstallprofile) and owns the resulting
    login.
  - **Installer-provisioned** (`os.provided: false`, no `os.installProfileRef`):
    Bootwright provisions substrate only and a downstream installer lays the OS
    down — `openshift-install` writing RHCOS onto an agent node.
- `spec.os.provided: false` machines must declare `spec.substrate.providerRef`.
- Bare-metal install machines declare physical inventory under
  `spec.hardware.nics`, boot NIC selection under `spec.hardware.boot.nicRef`,
  and BMC access under `spec.hardware.management.bmc` (`address`, `protocol`,
  `credentialsRef`).
- `spec.hardware.management.bmc` governs two distinct TLS legs, each defaulting
  to verify:
  - `bmc.tls.verify` controls the connection Bootwright opens **to** the BMC
    (the Redfish API leg, controller → BMC). It is tri-state; omitted means
    verify. Set `false` only for a lab/self-signed BMC certificate.
  - `bmc.virtualMedia.tls.trust` declares how the BMC comes to trust the
    **artifact server's** certificate when it fetches the boot ISO (BMC →
    artifact server). `disable-verification` (the default) asks the BMC to skip
    verification for the fetch and restores it afterwards unless
    `restoreVerificationAfterBoot: false` (best-effort; some firmware ignores
    it). `import-certificate` uploads the artifact server certificate into the
    BMC trust store before the fetch so a self-signed certificate is accepted,
    and `removeCertificateAfterBoot: true` (only valid with `import-certificate`)
    removes it once the ISO is mounted. `established` means the trust already
    exists out of band and Bootwright performs no BMC security writes at all.
    This virtual-media leg is where Bootwright reconciles the artifact server's
    typically self-signed certificate; `security.md` describes the per-mode
    fetch-window handling.
- Libvirt, vSphere, and KubeVirt install machines select VM shape through
  `spec.substrate.profileRef`.
- `spec.os.installProfileRef` selects a `MachineInstallProfile` when Bootwright
  installs a managed OS.
- `spec.os.install.rootDeviceHints` is the Machine-owned root-device hint
  location.
- `spec.network.config.networkConfigRef` selects a `NetworkConfig`.
- `spec.network.config.spec` is the inline alternative to `networkConfigRef`: it
  carries a full `NetworkConfig` `spec` (`machineNetwork[]`, `template`) for a
  one-off machine instead of a shared, reusable `NetworkConfig`. Set exactly one
  of `networkConfigRef` or `spec`. `overrides` is valid only with
  `networkConfigRef`; `interfaceAddresses` require one of `networkConfigRef` or
  `spec`.
- `spec.network.config.attachmentRef` selects an
  `InfraProvider.spec.networkAttachments[]` entry on the machine provider.
  When omitted on a provider-backed machine that sets `networkConfigRef`,
  `attachmentRef` defaults to the `networkConfigRef`; `render
  effective` materializes the default and validation resolves it against the
  provider's `networkAttachments[]`. An authored `attachmentRef` always wins.
  The default is accepted only while the provider declares a single
  attachment of its kind; with several, validation rejects the defaulted
  reference and lists the candidate names, so renaming a `NetworkConfig` can
  never silently re-bind a machine to a different attachment.
- `spec.network.config.interfaceAddresses[]` is the single owner of a node's
  static install IP. Each entry binds an NMState `interface` to a named
  `spec.addresses[]` entry through `addressRef` and sets `prefixLength` (and
  optional `family`, default `ipv4`); rendering injects the resolved address
  into that interface. Author the IP in `spec.addresses[]` once instead of
  duplicating it into an NMState override.
- `spec.network.config.overrides` merges arbitrary additional NMState (bonds,
  routes, extra interface attributes) into the rendered config for that machine;
  it must not carry the static install IP, which `interfaceAddresses` owns.
- `spec.network.interfaceBinding[]` maps hardware NIC names to effective
  NMState interface names for MAC injection.
- `spec.addresses[]` owns durable named addresses used by SSH and shared
  service endpoints.
- `spec.addresses[]` implicitly contains `{name: fqdn, address:
  <metadata.name>.<domains.machines>}` when the `Environment` declares a
  domain (`domains.machines`, which defaults to `domains.base`) and the machine
  authors no entry named `fqdn`. An authored
  `fqdn` overrides the default verbatim; it must be a DNS subdomain (it
  may live in a foreign zone) and must be unique across machines.
  `metadata.name` keeps its dot-free DNS-label validation.
- `fqdn` is the machine's canonical connection address: SSH connections
  (Ansible inventory, `machine rsh`/`exec`, storage-cluster `cluster
  rsh`/`exec`, trust bootstrap) target the `fqdn` name. The
  `access.ssh.addressRef` entry remains the machine's routable IP — what the
  `fqdn` record must resolve to and the connection fallback. Two carve-outs
  connect by IP: machines whose network configuration references no
  name-resolution entry, and the machine hosting the managed name-resolution
  component its own network references.
- A `Machine` Bootwright installs (`os.provided: false` with an
  `os.installProfileRef`) **derives** its whole access block and must author
  none: `access.ssh.user` is the product constant `bootwright`, and the key is
  `Environment.spec.machineAccess.keyRef`. Authoring `spec.access` or
  `spec.access.rootLogin` on such a Machine is rejected before normalization.
- `Environment.spec.machineAccess.keyRef` names the `sshKeyPair` `Secret` whose
  public half every installed machine authorizes for its `bootwright` account
  and whose private half Bootwright connects with. It is required as soon as any
  `Machine` installs an OS. It may not also be named as a `StorageCluster`'s
  `spec.ceph.cephadm.clusterSSH.keyRef`, because `cephadm bootstrap
  --ssh-private-key` persists the cluster identity into the mon config-key
  store, and this key opens every machine in the fleet.
- The install creates `bootwright` with a locked password (unless the profile
  names `customizations.ssh.initialPassword.secretRef`), authorizes the fleet
  key for it, writes `/etc/sudoers.d/60-bootwright` containing
  `Defaults:bootwright !requiretty` and `bootwright ALL=(ALL) NOPASSWD: ALL`,
  locks root, authorizes no key for `root`, and writes `PermitRootLogin no`.
  Because the account is constant it never perturbs the install-marker hash and
  the readiness probe always knows which account to authenticate as.
- `spec.access` is a union of `local` and `ssh` and describes only a login
  Bootwright did **not** create. `local: true` declares the machine Bootwright
  runs on; it is reached with a local connection, is valid only with
  `os.provided: true`, and is refused when the machine's address does not
  resolve to the controller. Omitting `access` on an `os.provided: true`
  machine defaults it to `ssh.auth.operatorIdentity` — the machine is reached as
  the invoking operator. Omitting it on an agent-installer node declares no
  Bootwright login, which is valid.
- `access.ssh.addressRef` resolves to the address named `ssh`, else to the
  implicit `fqdn` address, else to nothing (there is no only-address fallback).
  This default applies whether or not `access` itself was authored, and a
  machine reached over SSH that declares neither address fails validation.
- `spec.access.ssh.auth` is a discriminated union with exactly one arm, and
  every arm describes a pre-existing login. `operatorIdentity` reaches the
  machine as the invoking operator with that operator's own SSH identity;
  `privateKeyRef` names an `sshKeyPair` `Secret`; `passwordRef` names a
  `usernamePassword` `Secret` and requires `access.ssh.user`.
  `access.ssh.port` defaults to 22 and `access.ssh.sudoPasswordRef` supplies an
  escalation password for an account without passwordless `sudo`.
  `access.ssh.knownHostsRef` names a `Secret` holding an explicit `known_hosts`
  entry for the machine; omitted, Bootwright manages server-key trust in the
  context store (`security.md`, `bootwright machine trust`).
- A `Machine` a managed Ceph `StorageCluster` lists in `spec.ceph.topology.nodes`
  and installs carries the same `bootwright` account as any other installed
  machine; the cluster provisions its own `spec.ceph.cephadm.clusterSSH.user`
  orchestration account day-2 on top of it. A topology node the cluster does not
  install authors its own `access.ssh` and keeps `rootLogin: keep` by default.
- The **scope of the operation** selects between those two logins, and a login
  is never selected without the credential that opens it (ADR 0033).
  Machine-scoped work — `machine rsh`/`exec` and every `apply`, `plan` or
  `destroy` task acting on a machine — uses the `Machine`'s own `access.ssh`
  login and its `auth` arm. Cluster-scoped work — `cluster rsh`/`exec` and
  cluster-scoped tasks — uses the cluster's orchestration identity
  (`install.nodeSSH` with `core`, or `clusterSSH.user` with
  `clusterSSH.keyRef`). Cluster-scoped apply uses both in that order: the
  machine login is borrowed to create and verify the orchestration account
  before the orchestrator receives it. On a machine whose own login is `root`
  and whose `rootLogin` is `revoke`, machine scope falls back to the
  replacement orchestration identity, credential included.
- The persistent `--ssh-user` and `--ssh-id-file` flags override how a
  machine is reached for one invocation without entering desired state; they are
  specified in the [CLI Contract](#cli-contract) under Global flags.
- `spec.access.rootLogin` is the machine's OS root-login posture: `keep` (the
  default) or `revoke`. `revoke` writes
  `/etc/ssh/sshd_config.d/01-bootwright-access.conf` with `PermitRootLogin no`,
  validated with `sshd -t` before the reload; returning the field to `keep`
  removes the drop-in and re-authorizes root, so the change is reversible.
  It requires `spec.access.ssh` to be declared, is rejected on a machine
  Bootwright installs (which never permits a root login at any point in its
  life), and it is accepted only on a
  machine that a managed Ceph `StorageCluster` lists in
  `spec.ceph.topology.nodes` under a non-root
  `spec.ceph.cephadm.clusterSSH.user`. That cluster's node-access reconciliation
  is what performs the revoke, and its orchestration account is the replacement
  login; on any other machine the field would have no executor and no successor
  account, so it is rejected. Revocation is ordered
  verify-before-revoke: the orchestration account must answer `sudo -n true` on
  every topology node before root is revoked on any of them, and is re-proved
  after the `sshd` reload. Node access state is recorded on the machine at
  `/etc/bootwright/access-marker.json` (mode `0644`, non-secret). See ADR 0019
  and `security.md`.
- `spec.capabilities[]` is a closed vocabulary of five values:
  `openshift-node`, `ceph-node`, `ceph-arbiter`, `container-runtime`, and
  `libvirt`. Every value
  is enforced somewhere; there are no inventory-only labels. An unknown, empty,
  or duplicated entry fails validation naming the `Machine` and the accepted
  set. Two values assert a property of the machine itself —
  `container-runtime` (it runs containers, required of every containerised
  `InfraComponent` host) and `libvirt` (it is a libvirt hypervisor host); two
  restate a binding a reference already declares (`openshift-node`
  for a `ContainerCluster` node, `ceph-node` for a `StorageCluster` topology
  node) and are cross-checked against it. A component host is never tagged with
  a capability named after its service.
  `ceph-arbiter` is the one forward-looking value: it marks a machine eligible to
  carry a stretch tiebreaker, whether or not it holds one today, and is what
  `storage-cluster replace-arbiter --new-arbiter-machine` selects a candidate
  from. It requires `ceph-node`, because an arbiter candidate is first a storage
  node.
- `spec.placement.site` names the site the machine physically stands in, from
  the `Environment.spec.sites` registry. It is the **single writer** for
  location: a `StorageCluster` topology node takes its `site` from the machine
  it binds, and a node that authors a different one is refused. It is optional
  in general and required exactly where a site has effect — a machine bound by
  a cluster that declares `stretch` or narrows a placement by `sites`, and every
  `ceph-arbiter`-capable machine once any `StorageCluster` declares stretch, so
  `replace-arbiter` can place a promoted tiebreaker truthfully without
  inheriting the retiring arbiter's label (ADR 0048). It is rendered as an
  inventory fact on every machine; outside stretch mode nothing else consumes
  it. `placement` is the home for finer topology (zone, rack) if CRUSH depth is
  ever needed.

## MachineImage

`MachineImage` describes bootable media used by managed OS installation. It
does not declare where Anaconda gets install-time packages; that behavior
belongs to the `MachineInstallProfile` that selects the image.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: MachineImage
metadata:
  name: rhel-94-dvd-iso
spec:
  bootMedia: local-media:rhel-9.4-x86_64-dvd.iso
  checksum: sha256:0000000000000000000000000000000000000000000000000000000000000000
  trustRefs:
    - image-ca
```

Rules:

- `spec.bootMedia` is required and locates the ISO the machine boots over BMC
  virtual media. It accepts `local-media:<filename.iso>`, `file://` absolute
  paths, `http://`, or `https://`.
- `local-media:<filename.iso>` resolves to the root-managed ISO media store
  under `/var/lib/bootwright/media/`. The media key is exactly the stored
  filename; it must be a basename ending in `.iso` and must not contain path
  traversal.
- Normal OpenShift agent ISOs generated by `openshift-install` are generated
  context artifacts, not managed media-store inputs. Future user-supplied
  RHCOS ISO fields may use `local-media:<key>`, but RHCOS rootfs, kernel,
  initramfs, and release image content must not use the ISO media store.
- `checksum`, `trustRefs[]`, and `headersRefs[]` are optional.
- Secret refs point to `Secret` objects by `metadata.name`.

## MachineInstallProfile

`MachineInstallProfile` declares how Bootwright installs and customizes an OS.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: MachineInstallProfile
metadata:
  name: rhel-94-ceph-node
spec:
  os:
    family: rhel
    version: "9.4"
    architecture: x86_64
  installer:
    anaconda:
      imageRef: rhel-94-dvd-iso
  customizations:
    ssh:
      passwordAuthentication: false
    storage:
      rootDevice:
        source: machineRootDeviceHints
    packages:
      install:
        - cephadm
        - firewalld
    repositories:
      configure:
        - id: ceph-7-tools
          displayName: Ceph 7 Tools
          baseURL: https://mirror.example.com/ceph/7/tools
          gpgKeyURL: https://mirror.example.com/RPM-GPG-KEY-redhat-release
    services:
      enabled:
        - sshd
    security:
      firewall:
        enabled: true
      fips:
        enabled: true
```

Rules:

- `spec.installer` is a presence union over the install backends and must set
  **exactly one** of `anaconda` or `templateClone`; the arm is the discriminator
  (there is no `type` field). The arm selects the OS-install role the renderer
  dispatches, and it is the only thing that selects it.
- `spec.installer.anaconda.imageRef` references a `MachineImage`. Additional
  install-time package sources are owned by this Anaconda installer block.
- `spec.installer.templateClone` installs the OS by cloning a golden image that
  already carries it. It consumes no `MachineImage`, no installer media, no
  artifact server and no install-time entitlement, so those surfaces plan
  nothing for a machine on this arm. The source is the vSphere
  `machineProfiles[].template` selected through the machine's
  `substrate.profileRef`; it is required on that arm and refused on `anaconda`.
- `spec.installer.templateClone.seed` is a presence union describing how the
  clone personalizes itself on first boot and must set exactly one arm.
  `seed.cloudInit` delivers instance metadata and user data to cloud-init in the
  guest; on vSphere they are written as `guestinfo.metadata` /
  `guestinfo.userdata` in the VM's `extraConfig`. vSphere guest customization
  (`LinuxPrep`, a stored customization spec, or the `networks[]` IP keys that
  attach one implicitly) is never used and must never be enabled alongside the
  seed — the two race over hostname and addressing on the first boot.
  `seed.cloudInit.growRootFilesystem` defaults `true` and grows the root
  partition and filesystem to the cloned disk.
- The seed carries the install identity only: the machine's hostname, its static
  IPv4 primary, the `bootwright` account with the **public** half of
  `Environment.spec.machineAccess.keyRef`, the `!requiretty` sudoers drop-in,
  and the sshd hardening drop-in. It never carries a password, a private key, an
  activation key or a passphrase, and it never carries the install marker — the
  marker is stamped day-2 over SSH exactly as on the Anaconda arm, so the
  marker's desired hash stays free of itself.
- The clone install sequence is: substrate clone with the seed already in
  `extraConfig` → power on → cloud-init applies the identity and the static
  address on first boot → Bootwright reaches the machine over SSH → `nmstatectl`
  applies the full desired network → marker stamp → ownership record. The
  ownership record uses the same `managed-os-install` kind as the Anaconda arm,
  with `attributes.installer: templateClone` and no provider-host paths.
- A clone personalizes itself exactly once. The seed writes
  `/etc/cloud/cloud-init.disabled`, the substrate diffs `extraConfig` so a second
  apply issues no reconfigure, and the install probe finds a matching marker.
  Re-personalization is reachable only through `apply --mode rebuild`, which
  deletes and re-clones the VM — so "re-personalize" and "re-provision" are the
  same operation and anything on the machine's data disks is lost.
- Under `installer.templateClone` the customizations Anaconda applies while
  partitioning and installing are refused rather than silently ignored:
  `customizations.storage.rootDevice`, `customizations.packages`,
  `customizations.localization`, `customizations.security.selinux`,
  `customizations.security.firewall`, `customizations.security.fips`,
  `customizations.security.diskEncryption` and
  `customizations.ssh.initialPassword`. Each refusal names the template as the
  owner of that property, or `installer.anaconda` as the arm that can apply it.
  `customizations.services`, `customizations.repositories`,
  `customizations.ssh.passwordAuthentication` and `spec.subscription` keep their
  meaning on both arms.
- `spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint`, when set,
  selects which managed artifact-server endpoint serves the managed-OS boot ISO
  to the BMC over Redfish virtual media. It uses the same `serverRef`/`endpointRef`
  shape and managed-endpoint requirement as
  `ContainerCluster.spec.install.agent.redfishVirtualMedia`.
- `spec.installer.anaconda.packageSource` is omitted for a full DVD image — the
  DVD carries its own packages, which install offline via `cdrom`. Set it when
  `imageRef` points at a small boot ISO, which carries no packages, to declare
  where Anaconda fetches them during installation. Exactly one arm selects the
  source: `mirror`, `fromSubscription`, or `hostedTree`.
- `packageSource.mirror` installs from an HTTP(S) install tree you host:
  `baseURL` is the primary Anaconda install tree (BaseOS) and `repositories[]`
  become additional Kickstart `repo` entries (e.g. AppStream). Every `baseURL`
  must be `http://` or `https://`.
- `packageSource.fromSubscription` sets `entitlementRef`, which must resolve to a
  `redhat-rhel` `Entitlement` whose `rhsm.management` is `managed`: the
  Anaconda-time registration is the package source, so it cannot be delegated
  to a custom playbook. RHSM organization and activation key secret refs
  are owned by that entitlement.
- `packageSource.hostedTree` sets `fromMedia`, the full DVD Bootwright extracts
  once and serves from the selected managed artifact server. It must reference
  local media (`local-media:` or `file://`, not a URL), must differ from the
  referenced `MachineImage.spec.bootMedia`, and must declare an
  `artifactServerEndpoint.endpointRef` that resolves to an HTTP endpoint.
- `packageSource` affects only the Anaconda installation transaction.
  Bootwright does not render persistent repo files or `repo --install` from it.
  Repositories the installed system keeps are declared separately, under
  `customizations.repositories`, so install-time content and day-2 content stay
  independently selectable.
- `customizations.repositories` declares the repositories the installed machine
  carries. Both arms are optional and may be combined; the block is applied twice
  — written by the Kickstart `%post` at install time, then reconciled on every
  apply by the machines-phase `repositories.<cluster>` task, so editing it does
  not require a reinstall.
- `customizations.repositories.configure[]` writes one yum repo file per entry to
  `/etc/yum.repos.d/bootwright-<id>.repo`. `id` names both the repo section and
  the file, so it must not contain whitespace, quotes, or slashes, and must be
  unique within the profile. `baseURL` is required and must be `http://` or
  `https://`. `displayName` defaults to `id`. `enabled` and `gpgCheck` default to
  `true`. `gpgKeyURL` must be `http://`, `https://`, or `file:///`, and is
  required whenever `gpgCheck` is left enabled — a signed repo with no key is a
  repo that cannot install anything, so it is rejected at validation rather than
  at first `dnf` call. A `configure[]` entry whose `baseURL` is not covered by the
  effective no_proxy gets the machine-OS-install proxy, matching how Anaconda's
  own `repo` directives are proxied.
- `customizations.repositories.subscription` selects entitled repositories with
  `subscription-manager`. `enable[]` names concrete repository ids (`*` is
  rejected — it would mean "enable everything"), `disable[]` accepts ids and `*`.
  Listing `*` in `disable[]` alongside a non-empty `enable[]` renders as a purge:
  exactly the enabled set survives. An id in both lists is a validation error.
  The block requires the node to actually be registered — the profile must set
  `spec.subscription.entitlementRef` or
  `spec.installer.anaconda.packageSource.fromSubscription` — otherwise it is
  rejected with a pointer to `configure[]`. When registration happens during the
  install (`fromSubscription`), the `%post` also selects the repositories; when
  registration is day-2 (`spec.subscription`), `%post` skips the block and only
  the machines-phase task applies it, because `subscription-manager` has no
  identity yet inside the installer chroot.
- `spec.subscription.entitlementRef`, when set, must resolve to a `redhat-rhel`
  `Entitlement` (any other type is rejected). When that entitlement's
  `rhsm.management` is `managed` it drives the machines-phase
  `registration.<cluster>` task that registers the installed node with RHSM after
  the OS is in place; an `external` entitlement plans no registration (the
  operator owns it). It cannot be combined with
  `installer.anaconda.packageSource.fromSubscription`, which already registers the
  node during the Anaconda install.
- `customizations.hostname.source` accepts `machineName`: the installed OS
  hostname becomes the machine's `fqdn` name. It is valid only for
  machines not bound to any cluster; a cluster-bound node's OS hostname must
  equal its node FQDN (cephadm host matching depends on it), so the
  combination is a validation error.
- `customizations.storage.rootDevice.source` accepts
  `machineRootDeviceHints`.
- `customizations.packages.environment` currently accepts `minimal`, which
  renders the supported minimal Anaconda package environment.
- `customizations.packages.install[]` is the open allow-list of packages
  rendered into Kickstart; whatever is listed is installed. The validator
  enforces only the firewall dependency (a profile with
  `security.firewall.enabled: true` must list `firewalld`), not a closed set.
  `cephadm`, `podman`, `lvm2`, `chrony`, and `firewalld` are the recommended
  Ceph-node RHEL baseline, not a validated limit.
- `customizations.packages.excludeDocs: true` renders Kickstart package
  document exclusion.
- `customizations.packages.installWeakDeps: false` renders weak-dependency
  exclusion.
- `customizations.localization` owns the installed system's locale: `language`
  (system message locale, default `en_US.UTF-8`), `formats` (regional formatting
  locale for dates/numbers/currency; follows `language` when omitted), `keyboard`
  (default `us`), `timezone` (default `UTC`, hardware clock kept in UTC), and
  `additionalLocales[]`. `language`, `formats`, and `additionalLocales` are
  unioned into the Kickstart `%packages --inst-langs` list, which is
  authoritative over which locales survive on the installed system.
- `customizations.services.enabled[]` and
  `customizations.services.disabled[]` render Kickstart service state. A
  machine that references a managed OS install profile requires `sshd` in the
  enabled list so Bootwright can reconnect after installation.
- The install authorizes the public half of
  `Environment.spec.machineAccess.keyRef` for the `bootwright` account it
  creates, so Bootwright can reconnect after the install; a profile referenced
  by a managed-install machine must therefore enable `sshd` per the rule above.
  The install always renders `PermitRootLogin no`, locks the root password, and
  authorizes no key for `root`.
- `customizations.ssh.initialPassword.secretRef` names a `usernamePassword`
  `Secret` whose password becomes the created account's console password.
  Omitted, the account is created with a locked password — Bootwright ships no
  default password of any kind.
- The install always writes `/etc/sudoers.d/60-bootwright` granting the
  `bootwright` principal `NOPASSWD: ALL` and `!requiretty`. It is not
  configurable: Bootwright escalates with `become` throughout, so a machine
  whose service account cannot escalate is a machine it cannot use.
- `customizations.ssh.passwordAuthentication: true` enables sshd password
  authentication (default: key-only).
- `customizations.security.selinux.mode` accepts `enforcing`, `permissive`, or
  `disabled`.
- `customizations.security.firewall.enabled` renders Kickstart firewall state.
  When true, the profile must install and enable `firewalld`.
- `customizations.security.fips.enabled: true` is supported only for RHEL
  Anaconda install profiles. It renders `fips=1` into the installer kernel
  command line through `mkksiso --cmdline`; changing this field on an installed
  machine is reinstall-only.
- `customizations.security.diskEncryption` is a presence block: authoring it
  installs the node onto LUKS2 sealed to its TPM 2.0, omitting it installs the
  node unencrypted. It carries an `unlock` presence union whose only arm is
  `tpm2`, and a required `recoveryPassphraseRef`. The kickstart adds
  `--encrypted --luks-version=luks2 --passphrase=` to the partitioning line — to
  both the root and swap partitions when a root disk is selected, to `autopart`
  otherwise — and an `%post --erroronfail` section binds every resulting LUKS2
  volume with `clevis luks bind` and regenerates the initramfs. `clevis`,
  `clevis-dracut`, `clevis-luks`, `clevis-systemd`, `tpm2-tools` and `tpm2-tss`
  are added to the install transaction; the profile does not list them.
- `unlock.tpm2.pcrIds[]` seals the key against the named Platform Configuration
  Registers (`0`–`23`, unique). Omitted, no boot policy is applied and the key is
  released on any boot of that machine. `unlock.tpm2.pcrBank` selects the bank
  (`sha1`, `sha256`, `sha384`, `sha512`, default `sha256`) and is rejected
  without `pcrIds`, which it would not affect.
- `recoveryPassphraseRef` is required whenever `diskEncryption` is set: Anaconda
  creates the container from a passphrase before anything can be bound, and that
  keyslot is deliberately kept as the recovery path when a TPM stops releasing
  the key. The passphrase reaches the machine inside the generated kickstart, so
  the built install ISO is published `0600` whenever the kickstart carries one.
- `customizations.security.diskEncryption` is covered by the install marker's
  desired hash, so adding, removing, or re-policying it is reinstall-only drift.
- A machine that references a profile with `diskEncryption`, and whose substrate
  is a virtual `InfraProvider`, must select a `machineProfiles[]` entry that
  declares `tpm`. A bare-metal machine is exempt: its TPM is a firmware fact
  Bootwright cannot read before the install writes to disk.
- A `Machine` with `os.provided: false` and managed OS install must set
  `spec.os.installProfileRef`.

## InfraProvider

`InfraProvider` owns substrate capabilities and network attachments.

Rules:

- `spec.type` accepts `baremetal`, `libvirt`, `vsphere`, or `kubevirt`.
- Bare-metal providers declare boot behavior through
  `spec.baremetal.boot.method`,
  which is rendered verbatim into the machine boot variables the bare-metal
  adapter consumes; omitted, the adapter's own default applies. Physical machine
  inventory lives on `Machine.spec.hardware`.
- `spec.baremetal.defaults.bmc` supplies fleet-wide BMC defaults inherited by
  every bound `Machine` that omits them. `tls` and `virtualMedia` inherit (a
  machine value wins over the default); `credentialsRef` stays per-machine and
  is not defaulted here, so each `Machine` sets its own BMC credential.
- Libvirt, vSphere, and KubeVirt providers declare `machineProfiles[]`.
  Machines select a profile through `Machine.spec.substrate.profileRef`. Each
  entry sets a `name` unique within the provider and the VM shape — `cpu`,
  `memoryMiB`, `diskGiB`, and optional `dataDisks[]` (each a `name` plus
  `sizeGiB`). The three shape fields must be non-negative everywhere and must be
  **greater than zero on a `vsphere` provider**, whose adapter derives no
  defaults and has no shape to create the VM with otherwise. That holds in both
  install modes; it is a property of the adapter, not of `template`.
- vSphere `machineProfiles[].template` names the vCenter inventory path of a
  golden image and is consumed by the **template-clone** install mode alone. It
  is required when the machine's `MachineInstallProfile` selects
  `installer.templateClone`, and refused when that profile selects
  `installer.anaconda`, which would partition and wipe the image just cloned. A
  machine with `os.provided: true` is never installed, so a `template` on its
  profile is left alone. Under `templateClone` the template must carry exactly
  one disk, no larger than `diskGiB`: a clone can grow a root disk but never
  shrink one, and Bootwright adds `dataDisks[]` itself, so a wider template
  could never converge. Both conditions are checked before the clone and the
  refusal names the template.
- Libvirt providers require `libvirt.machineRef` (a `libvirt`-capable `Machine`,
  the hypervisor host) and `libvirt.uri` (the libvirt connection URI).
  `libvirt.bmcEmulationDefaults` configures the emulated Redfish BMC and is
  currently required for libvirt apply support: `enabled`, `protocol`, `emulator`,
  `bindAddress` (defaults to all interfaces), `port`/`vMediaPort`,
  `auth.credentialsRef`, and `disableCertificateVerification`.
- Profile fields the selected provider's adapter does not consume are
  rejected: `template` and `failureDomainRef` are vSphere-only, and
  `dataDisks` are consumed by the libvirt and vSphere adapters only.
- `machineProfiles[].tpm` is a presence block attaching an emulated TPM 2.0, and
  is consumed by the libvirt and kubevirt adapters only. libvirt renders a
  `tpm-crb` device on the `swtpm` emulator backend; kubevirt renders
  `devices.tpm`. `tpm.persistent` (default `true`) is kubevirt-only — libvirt
  keeps swtpm state per defined domain and has no ephemeral mode to opt out of.
  vSphere rejects `tpm`: a vTPM there needs a vCenter key provider and EFI
  firmware Bootwright does not configure. Bare metal ignores it.
- vSphere `machineProfiles[].failureDomainRef` must name a
  `spec.vsphere.failureDomains[]` entry, and every
  `failureDomains[].server` must equal a declared `vcenters[].server`.
  With several declared failure domains every profile must set
  `failureDomainRef`; with exactly one, an empty ref resolves to it.
- vSphere providers require `spec.vsphere.vcenters[]`: each entry sets `server`
  (unique across entries — list several `datacenters[]` under one entry rather
  than repeating a server), `credentialsRef`, and optional `port` and
  `disableCertificateVerification`. `spec.vsphere.nodeNetworking` (`external` and
  `internal`, each carrying `networkSubnetCidr[]`) is required when a failure
  domain declares more than one `topology.networks` entry.
- vSphere `spec.vsphere.isoStaging` overrides where boot and install ISOs
  are uploaded; when authored it must set at least one of
  `{datastore, folder}`. Absent fields default to the machine's
  failure-domain `topology.datastore` and the stock vmedia folder.
- `networkAttachments[]` names provider-specific attachment targets. Machines
  bind to them through `spec.network.config.attachmentRef`. Each entry sets a
  `name` and exactly one arm matching the provider `type`: `libvirt.bridge`,
  `vsphere.portgroup`, `baremetal.vlan` (`0`–`4094`), or `kubevirt.networkRef`. A
  KubeVirt `networkRef` requires `name` and `namespace` (a DNS label); `kind`
  defaults to `ClusterUserDefinedNetwork` and `apiGroup` defaults from the kind —
  `k8s.ovn.org` for `ClusterUserDefinedNetwork`/`UserDefinedNetwork`,
  `k8s.cni.cncf.io` for `NetworkAttachmentDefinition`.
- A `vsphere` attachment binds an existing portgroup by name; Bootwright never
  creates one. `vsphere.distributedSwitch` names the vDS that owns the portgroup,
  because portgroup resolution is not datacenter-scoped. It is required whenever
  the provider declares more than one `vsphere.failureDomains[]` entry, where a
  bare portgroup name is no longer uniquely resolvable, and in practice whenever
  the name is not unique across the vCenter. The attachment a machine selects
  applies to every NIC of that machine.
- KubeVirt providers set exactly one of `hostClusterRef` or `kubeconfigRef`.
  `hostClusterRef` references a Bootwright `ContainerCluster`; `kubeconfigRef`
  references a secret containing an external virtualization kubeconfig. They also
  require `kubevirt.namespace` (the target VM namespace); optional
  `kubevirt.storageClassRef` selects the DataVolume storage class.

## InfraComponent

`InfraComponent` owns shared services that run on selected machines.

Rules:

- `spec.type` is required and selects which kind of component this is:
  `artifactServer`, `loadBalancer`, `proxy`, `nameResolution`, `ntp`, or
  `registry`. The populated arm key is byte-identical to the type value.
- Each arm except `artifactServer` declares `implementation`: which software
  realises the component. Accepted implementations are `haproxy`
  (loadBalancer), `squid` (proxy), `dnsmasq` (nameResolution), `chrony` (ntp),
  and `mirror-registry` (registry). `Environment.spec.componentImages` pins
  images for a subset of these: it has no `ntp` category (chrony ships from the
  distribution repositories) and adds the pseudo-implementation
  `artifactServer.http`, which is not an authorable `implementation`.
- Component arms use `machineRef` for placement. The referenced `Machine` must
  carry the capability the service needs, which for every containerised
  component — loadBalancer/`haproxy`, artifactServer, proxy/`squid`,
  nameResolution/`dnsmasq`, registry/`mirror-registry` — is `container-runtime`,
  not a capability of the same name as the component. An `ntp`/`chrony` arm
  requires no capability.
- The `proxy`, `nameResolution`, `ntp`, and `registry` arms take `bindAddress`
  (defaults to `0.0.0.0`) and `port` (defaults to `3128`, `53`, `123`, and
  `5000` respectively).
- A `loadBalancer` arm places with `machineRef` and declares `bindAddresses[]`,
  each a `name` unique within the component plus the VIP `address`. A
  `ContainerCluster` endpoint selects one through
  `source.componentRef` + `source.bindAddressRef`.
- Endpoint entries use `addressRef` to select a named
  `Machine.spec.addresses[]` value on the placement machine.
- A `nameResolution` arm authoritatively answers its rendered records and
  forwards every other query to `forwarders[]` (IP resolvers); with no
  `forwarders` it answers only local records.
- The rendered record set includes, for every machine the component serves, a
  `host-record` for the machine's `fqdn` name at its `access.ssh.addressRef`
  IP and a `cname` from each bound node FQDN to that `fqdn` (degrading to a
  direct `host-record` when an overridden `fqdn` lives in a zone the
  resolver does not own). The bare machine-label record is not published.
- An `artifactServer` arm places with `machineRef` and optional `bindAddress`
  (defaults to `0.0.0.0`).
  `retention` is `persistent` (default) or `install-only` (reclaimed once installs
  complete). `listeners[]`, when omitted, defaults to a single `https` listener
  named `https` on port `8443`; each authored entry sets a `name` (DNS label), a
  `protocol` of `http` or `https`, and a unique `port` (`1`–`65535`).
  `endpoints[]` each name
  a served surface with `name`, `listenerRef` (a declared listener), and
  `addressRef` (a `Machine.spec.addresses[]` value). `tls` (`minVersion` — a TLS
  1.0–1.3 spelling — and `ciphers`) requires at least one `https` listener.
- An `ntp` arm takes `upstreamSources[]`: the upstream NTP servers (host or IP)
  chrony syncs from.

## NetworkConfig

`NetworkConfig` owns machine CIDRs, optional DNS catalog refs, and an NMState
template.

Rules:

- `spec.machineNetwork[].cidr` is required.
- `spec.template.networkConfig` is rendered into machine-level installer
  network config and merged deterministically with
  `Machine.spec.network.config.overrides`. The merge is the same for rendering
  and validation:
  - Maps deep-merge; the override value wins on conflict.
  - A list whose entries all carry a non-empty `name` merges by `name`: an
    override entry updates the matching base entry, and an override entry whose
    `name` is not present in the base is appended.
  - A list whose base and override entries are all maps but not all named merges
    positionally by index.
  - Any other list shape (for example scalars, or a mix of named and unnamed
    map entries) is rejected as an override error naming the owning field,
    rather than silently dropped.
- `spec.nameResolutionRefs[]` selects name-resolution catalog entries by name.

## ContainerCluster

`ContainerCluster` owns cluster install intent.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: ContainerCluster
metadata:
  name: ocp-3node
spec:
  distribution:
    type: openshift
    release:
      version: 4.21.15
  install:
    method: agent
    platform:
      type: baremetal
      baremetal:
        provisioningNetwork: disabled
    endpoints:
      api:
        address: 192.0.2.10
        source:
          type: external
      ingress:
        address: 192.0.2.11
        source:
          type: external
    agent:
      redfishVirtualMedia:
        artifactServerEndpoint:
          endpointRef: bmc
    nodeSSH:
      keyPairRef: ocp-3node-cluster-admin-ssh-key
  nodes:
    - name: master-0
      role: master
      machineRef: ocp-master-0
```

Rules:

- Cluster names share one selection namespace with `StorageCluster`, and the
  name `artifact-server` is reserved — see
  [Validation Rules](#validation-rules).
- `spec.install.platform.type` accepts `baremetal`, `vsphere`, `none`, or
  `external`.
- An omitted `spec.install.platform` derives from the single `InfraProvider`
  type behind `spec.nodes[].machineRef` →
  `Machine.spec.substrate.providerRef`: `libvirt` and `baremetal` providers
  derive `type: baremetal` with `baremetal.provisioningNetwork: disabled`;
  `vsphere` providers derive `type: vsphere`; `kubevirt` providers derive
  `type: none`. `render effective` materializes the derived platform. When the bound machines span multiple provider types
  and the platform is omitted, validation rejects the cluster naming the
  conflicting providers. When a `spec.nodes[].machineRef` does not resolve to a
  `Machine`, the derived-platform requirement is suppressed — the dangling
  `machineRef` is the root cause and is reported at the host, so validation does
  not additionally demand an authored `spec.install.platform.type`. An authored
  platform always wins.
- `spec.install.endpoints` keys are the closed slot vocabulary `api`,
  `api-int`, and `ingress`; any other key is rejected naming the accepted
  set. An omitted `api-int` slot defaults to a copy of the authored `api`
  endpoint (its `address` and `source`); `render effective` materializes the
  copy and an authored `api-int` always wins.
- `endpoints.<slot>.source.type` accepts `openshift` (default), `external`,
  `infraComponent`, or `node`. `openshift` and `external` own the endpoint's
  address directly. `infraComponent` requires `source.componentRef` pointing at a
  `loadBalancer` `InfraComponent` and must not set `address`.
  `source.bindAddressRef` names the selected `bindAddresses[]` entry; it may
  be omitted only when the load balancer declares exactly one bind address,
  and a non-empty `source.bindAddressRef` must match a `bindAddresses[].name`
  regardless of bind count.
  - `node` means the slot answers at the cluster's single node, and is valid
    only on a cluster with exactly one node — any other cluster is rejected
    naming the node count and the reason, because a multi-node cluster answers
    at a VIP no single node owns. Like `infraComponent`, it must not set
    `address`: the address is resolved from the bound `Machine`'s
    `spec.addresses[]` entry that its
    `spec.network.config.interfaceAddresses[]` points at, which is the same
    install address the VIP/node-IP collision rule and the network-config
    renderer read. A node whose `interfaceAddresses[]` resolve to no address,
    or to more than one, is rejected naming what was found rather than guessed
    at. `render effective` materializes the resolved `address`, so validators
    and renderers read one value (ADR 0044).
  - Besides `address` and `source`, each slot accepts `dnsName` (a DNS
    subdomain naming the endpoint when no address is owned here), `port` and
    `scheme` (`http` or `https`) refining the endpoint URL, and — where the slot
    owns a VIP — `prefixLength` (valid only alongside `address`) and
    `interfaceNetworks[]` (CIDRs narrowing which interface carries the VIP).
  - **Single-node clusters** additionally reject `source.type: openshift` on the
    `api`, `api-int`, and `ingress` slots. `source.type: node` is the positive
    form of the same fact and is the shape these clusters should author: all
    three slots answer at the one node, and the address is resolved once from
    the `Machine` rather than repeated three times.
  - `source.componentRef` and `source.bindAddressRef` are valid only when
    `source.type: infraComponent`. Every endpoint must set `address`, `dnsName`,
    `source.type: infraComponent`, or `source.type: node`. On a **VIP-bearing
    slot** — `api`,
    `api-int`, or `ingress` on a multi-machine cluster whose install platform is
    `baremetal` or `vsphere`, the combination that renders `apiVIPs`/`ingressVIPs`
    into install-config — `dnsName` alone does not satisfy that one-of: the slot
    must set `address` or `source.type: infraComponent`, or the VIP list would
    render empty from a name Bootwright cannot resolve to an address. `dnsName`
    alone stays legal wherever no VIP is rendered (platform `none` or `external`,
    and single-node clusters, which render platform `none`).
- `spec.nodes[].machineRef` is required and references a `Machine` with
  `openshift-node` capability and `os.provided: false`. No default is derived
  from the `name`: node names are cluster-local while `Machine` names are
  global, so an implicit same-name binding could silently capture a foreign
  `Machine`.
- `spec.nodes[].name` names the node, independent of the machine name, and is
  required: it is declared explicitly per node, with no default inferred from
  list position. It must be a DNS label (`[a-z0-9]([-a-z0-9]*[a-z0-9])?`) and
  composes to `<name>.<cluster>.<domains.containerClusters>` (the
  container-cluster zone; `domains.containerClusters` defaults to
  `domains.clusters`, which defaults to `domains.base`). A dotted value is
  rejected; `spec.nodes[].fqdn` is the explicit override, used verbatim and
  unaffected by the zone (ADR 0025). The composed FQDN is the cluster-visible node identity, and its
  DNS record resolves through the bound machine's `fqdn` (managed
  resolution renders a `cname`; provided resolution requires the operator's
  record, checked by the "Name resolution" preflight group).
- Each node name must be unique inside the cluster.
- A `Machine` is node-bound by at most one cluster (and at most one host
  entry): `machineRef` entries must be disjoint across every
  `ContainerCluster` and `StorageCluster`.
- Node network input is owned by the referenced `Machine`, not by the cluster
  node entry.
- `spec.distribution.type` accepts `openshift` (default, materialized by
  `render effective`) or `okd`; `openshift` clusters require a pull secret via
  `spec.install.pullSecretRef` or the `Environment` default. With neither
  authored, normalize injects the convention name `openshift-pull-secret`, which
  must still resolve to a declared `Secret`.
- `spec.distribution.release` selects the release to install. `version` (an
  `x.y.z`) is required for both `openshift` and `okd` unless `release.image` is
  set (a pinned release image; a mutable or `:latest` tag is rejected) — either
  one alone suffices. `channel` is openshift-only (rejected for `okd`) and, when
  omitted for an openshift cluster carrying a `version` and no `image`, defaults
  to the derived `stable-<x.y>` of the version's leading `x.y`.
- `spec.security.fips.enabled: true` renders a FIPS-mode install and is accepted
  only for `distribution.type: openshift`; `okd` (community SCOS) is not
  FIPS-validated and is rejected.
- `spec.security.diskEncryption` is a presence block carrying the same `unlock`
  presence union as a `MachineInstallProfile`, plus an optional `roles[]`
  selection over `master`, `worker`, and `infra`. It renders no
  `install-config.yaml` key: for each distinct machine config pool it selects
  (`infra` folds into `worker`) Bootwright writes a `MachineConfig` named
  `99-bootwright-<pool>-disk-encryption` into the installer's `openshift/`
  extra-manifest directory, declaring an Ignition `storage.luks` entry on
  `/dev/disk/by-partlabel/root` with `clevis.tpm2` and the matching
  `storage.filesystems` entry on `/dev/mapper/root`. No `options` are rendered:
  current releases expect the `cryptsetup` default cipher with and without FIPS.
- `roles[]` intersects with the roles `spec.nodes` declares. A selection that
  resolves to no pool is rejected rather than writing a `MachineConfig` no node
  would consume.
- `unlock.tpm2.pcrIds` and `pcrBank` are rejected on a `ContainerCluster`:
  Ignition seals with an empty TPM policy and the agent-based installer exposes
  no way to pass one, so the fields would be silently inert.
- Disk encryption is install-time intent. Ignition provisions the LUKS volume in
  the initramfs of a node's first boot, and the Machine Config Operator treats
  `Storage.Luks` and `Storage.Filesystems` as irreconcilable, so authoring the
  block against a running cluster is drift whose only resolution is reinstalling
  its nodes.
- `spec.install.nodeSSH` (and the `Environment` `defaults.install.nodeSSH` that
  fills it when omitted) sets `keyPairRef`, or `publicKeyRef` with an optional
  `privateKeyRef`. `keyPairRef` is mutually exclusive with the other two, and
  an authored `nodeSSH` without `keyPairRef` requires `publicKeyRef`. With
  neither authored, normalize injects
  `keyPairRef: <cluster-name>-cluster-admin-ssh-key`, which must still resolve
  to a declared `Secret`. Both injected conventions carry a `validate`
  diagnostic saying the value was defaulted and how to override it.
- `cluster rsh` and `cluster exec` reach a `ContainerCluster` node with the
  private material selected by `install.nodeSSH` (`keyPairRef`, otherwise
  `privateKeyRef`), the `core` user, and the node's effective primary install
  IP (falling back to its declared name). A public-only `nodeSSH` is valid
  for installation but cannot power these commands. Storage-cluster access
  connects to the node `Machine`'s `fqdn` address, port, and host-key trust per
  the `Machine` rules, but authenticates as the cluster's own orchestration
  identity — `spec.ceph.cephadm.clusterSSH.user` with the private half of
  `clusterSSH.keyRef` — because these are the cluster-scoped verbs; the
  `Machine`'s own login is what `machine rsh`/`exec` reaches (ADR 0033). An
  unmanaged orchestration account (`clusterSSH.user` resolving to `root`) has
  no cluster credential, so those nodes keep the `Machine` identity. The
  `--node` selector accepts the node name (FQDN or short label) or a
  `<role>-<ordinal>`; a machine name is rejected with guidance naming the
  node, and an unresolvable selector is refused with the cluster's node
  roster. Omitting `--node` selects the cluster's first declared node —
  `spec.nodes[0]` on a `ContainerCluster`, `spec.ceph.topology.nodes[0]` on a
  `StorageCluster` — at every cluster size, so neither verb prints anything of
  its own before the SSH stream. Container-node
  first use requires an interactive OpenSSH confirmation. A verified changed
  key is rotated by removing only its effective address from the context
  known-hosts file with `ssh-keygen -R`, then reconnecting interactively.
- `spec.networking.clusterNetwork` defaults to one entry
  `{cidr: 10.128.0.0/14, hostPrefix: 23}` and `spec.networking.serviceNetwork`
  defaults to `[172.30.0.0/16]` (the stock openshift-install networks) when
  omitted; `render effective` materializes the defaults. Each list is
  defaulted independently, and an authored list — even a partial one — is
  left untouched.
- `spec.networking.networkType`, when set, overrides the cluster CNI plugin
  (rendered verbatim as install-config `networking.networkType`); omitted uses
  the installer default (`OVNKubernetes`).
- `spec.install.mode` accepts `connected` (default) or `disconnected`;
  `spec.install.method` accepts `agent` (default). `disconnected` requires
  `Environment.spec.registries.mirror` (trust bundle plus an external mirror URL
  or a managed registry component).
- `spec.install.agent.bootArtifacts.artifactServerEndpoint`, when set, selects
  the artifact-server endpoint that serves the agent boot artifacts (the
  rootfs/kernel/initramfs the nodes fetch at boot). It takes effect only under
  `spec.install.mode: disconnected`; a connected install pulls them from the
  release payload.
- `spec.install.additionalTrustBundleRefs[]` are cluster-scoped additional CA
  trust-bundle secret names, merged with fleet-wide
  `Environment.spec.installTrust.caBundleRefs[]`.
- `spec.install.servingCertificates`, when set, supplies cluster serving
  certificates: `apiServer.namedCertificates[]` (each with `names[]` and a
  `secretRef`) and `ingress.defaultCertificateRef`.
- `spec.nodes[].role` accepts `master`, `worker`, or `infra`; a cluster
  requires at least one `master` node. `infra` is an authoring-only role:
  OpenShift has no install-time infra role, so an infra host installs as a
  worker and is promoted day-2 with the `node-role.kubernetes.io/infra`
  label, a `NoSchedule` taint, and the infra MachineConfigPool.
- `spec.nodes[].labels` (a string map, non-empty keys) and `spec.nodes[].taints[]`
  (each `key` required, optional `value`, `effect` one of `NoSchedule`,
  `PreferNoSchedule`, or `NoExecute`) are day-2-owned node intent applied after
  install — reconcilable-in-place drift, not install-config/agent-config identity.
- There are no authorable machine pools. `spec.nodes[].role` is the single
  source of the roster: the renderer derives the control-plane and compute
  replica counts from it, and the agent installer renders one
  default-architecture `master` pool and one `worker` pool. Strict decode
  rejects `spec.controlPlane` and `spec.compute[]` along with every other
  install-config machine-pool field (`replicas`, `architecture`,
  `hyperthreading`, `platform`, `name`).
- Single-node topologies render installer platform `none` unless
  `platform.type: external` is explicit.

## StorageCluster

`StorageCluster` owns imported or managed external storage intent.

### Shape

The kind has three top-level fields: `spec.type`, `spec.management`, and
`spec.ceph`.

- `spec.type` is required and must be `ceph`.
- `spec.management` accepts `managed` or `external`; omitted means `managed`.
- Managed clusters require `spec.ceph`; external clusters must not set it. An
  `external` `StorageCluster` is therefore a name and nothing more — only `type`
  and `management`. It runs no cephadm and owns no connection facts; the
  operator-supplied external-cluster-details payload is declared on the
  consuming [`StorageExport`](#storageexport) as
  `spec.externalDetails.fromSecretRef`.
- Cluster names share one selection namespace with `ContainerCluster`, and the
  name `artifact-server` is reserved — see
  [Validation Rules](#validation-rules).
- Managed storage nodes reference `Machine` objects with `ceph-node`
  capability.
- Managed storage seed/admin operations connect as the cluster's own
  orchestration identity — `spec.ceph.cephadm.clusterSSH.user` with the key
  material `clusterSSH.keyRef` names.
- Storage convergence is additive-only across `spec.ceph.config`, `mgrModules[]`,
  `monitoring`, the `services[]` passthrough, and the `StoragePool`/
  `StorageFilesystem`/`StorageObjectGateway` kinds: `apply` creates and converges
  declared objects and never removes a live Ceph object whose declaration was
  deleted; `--mode rebuild` rebuilds only still-declared pools whose structural
  identity changed, never prunes. This is the Ceph instance of the product-wide
  additive-apply rule in the [CLI Contract](#cli-contract): removal crosses the
  `destroy` authorization boundary or is performed out of band.

### Distribution and builds

- Managed Ceph `spec.ceph.distribution` accepts `oss`, `redhat`, or `ibm`;
  omitted means `oss`.
- `spec.ceph.release` selects which Ceph release to install for the chosen
  distribution, and every artifact coordinate is *derived* from it rather than
  looked up: the leading component is the product stream, which names the
  subscription tools repo, the vendor `.repo` URL, and the daemon image
  repository, while the node's own RHEL major fills the repo templates. Any
  release Bootwright can parse is therefore installable without a code change.
  Validation is syntactic: `oss` accepts an upstream release name (`^[a-z][a-z0-9]+$`,
  such as `tentacle`) or an exact `x.y.z` version; `redhat` and `ibm` accept a
  dot-separated numeric product version of any length (`9`, `9.1`, `9.9.1.0`).
  Omitted defaults to `20.2.2` (`oss`), `9.1` (`redhat`), or `9.9.1.0` (`ibm`).
- Bootwright holds **no catalog of Ceph releases and no vendor support matrix**,
  and must not acquire one. It does not know, check, or report which releases a
  vendor ships, which are current or ended, or which operating systems a release
  is supported on. Those facts change on the vendor's schedule and any copy of
  them inside Bootwright is wrong the moment the vendor moves; a release the
  operator declares is taken as given and its artifacts are fetched from the
  supplier. Bootwright therefore never emits an error, warning, or advisory about
  a release being unknown, newer, older, or mismatched against a node OS. The
  authored release is carried verbatim into normalization and convergence
  hashing; nothing is silently rewritten to a Bootwright-preferred value. The
  same rule governs `spec.ceph.packageVersion` and `spec.ceph.image.version`:
  both are read off the vendor's own release-to-build tables by the operator and
  taken verbatim, and neither is checked against `release` or against any list of
  builds Bootwright would have to carry.
- The only storage-node OS check is a capability statement, not a compatibility
  one: the subscription-backed provider implements RHEL-family package sources
  only, so a Ceph node's `MachineInstallProfile` must declare `family: rhel`
  (validation error, re-asserted on the node at apply time as
  `ansible_os_family == 'RedHat'`). No OS *version* is examined anywhere.
- `spec.ceph.packageVersion` optionally pins the exact Ceph package build to
  install, as an RPM `[epoch:]version[-release]` such as `19.2.1-245.el9cp`. It
  names the `cephadm` build on each storage node and nothing else: the daemons
  run from the container image, so a `packageVersion` bump reconciles the host
  CLI in place and is **not** a cluster upgrade and **not** a rebuild. It applies
  to `redhat` and `ibm` only; for `oss` the exact build is already named by
  `release` as an `x.y.z` version, which selects the package repository. The
  ownership record keys on the bare package name, so destroy still matches a
  pinned install. Validation is syntactic — the value must not carry a package
  name, glob, or separator — and is never checked against `release`.
- `spec.ceph.image` optionally pins the cephadm container image, which every
  apply asserts as the cluster's `container_image` so that cephadm deploys every
  Ceph daemon from it, making the running cluster version reproducible. The pin
  is delivered twice, and both are required: `cephadm bootstrap --image` for the
  daemons the bootstrap command itself creates, and a converged
  `ceph config set global container_image` before the first service spec is
  applied, for every apply thereafter. Bootstrap runs once ever, so it cannot be
  the only delivery path — without the converged assertion a cluster keeps
  whatever image it first resolved, which for a vendor package is that build's
  own floating tag, and the pin silently governs nothing. Asserting the pin is
  not an upgrade: it binds the daemons cephadm creates or redeploys from then
  on, and never restarts or re-images a running daemon. It is a block of two
  halves:
  - `version` is the build: an image tag, or a `sha256:` digest. A mutable tag
    such as `latest` is not a pin and is rejected. Left unset, nothing is
    asserted and the cluster keeps the image it already resolved, which for a
    vendor package is that build's own floating tag. There is
    no vendor default: `redhat` and `ibm` registry tags are build-numbered and a
    product release such as `9.1` or `9.9.1.0` is not a tag, so the build is
    supplied here explicitly. For `oss` an exact `x.y.z` `release` derives
    `vX.Y.Z`; a release name leaves the daemon image unpinned.
  - `base` is the `<registry>/<path>` the version hangs off, and defaults to the
    repository Bootwright derives from distribution, release and the entitlement
    registry — `quay.io/ceph/ceph`,
    `registry.redhat.io/rhceph/rhceph-<stream>-rhel9`, or
    `cp.icr.io/cp/ibm-ceph/ceph-<stream>-rhel9`. Leaving it unset is the normal
    case and keeps the namespace and stream welded to the release. It must be a
    bare reference carrying no tag, digest, or scheme, and it is also what
    `container_image_base` is pinned to.

  An authored subscription `base` must start with the cluster's own vendor
  namespace and stream. This is a cross-vendor guard (a Red Hat cluster must not
  run an IBM image, and vice versa), not a version check: the trailing build base
  is whatever the vendor compiled that release against and is never validated.
  When an entitlement overrides `registry.url`, `base` is required and the same
  namespace rule applies against that mirror root — the derived base names the
  vendor registry, which a mirrored estate cannot pull. Changing only the login
  target or using an arbitrary repository below a broad registry namespace is
  rejected.
- `distribution: oss` uses upstream/community Ceph package and image sources
  and must not set `entitlementRef`. Bootwright configures the upstream
  community repository on each node with cephadm and runs Ceph client commands
  inside `cephadm shell`; it does not add CentOS Stream repositories to RHEL or
  install host `ceph-common`. `spec.ceph.community.mirror`
  overrides the `download.ceph.com` base URL and must use HTTPS.
  `spec.ceph.community.checksum` optionally pins the fetched cephadm bootstrap
  binary as `sha256:<hex>`; the
  binary is downloaded and executed as root, so the pin adds a content check on
  top of the HTTPS transport, and when unset the binary is fetched with no
  content pin (the default). `spec.ceph.community` must be empty for `redhat`
  and `ibm`.
- `distribution: redhat` requires `entitlementRef` to resolve to a
  `redhat-ceph` `Entitlement`. Red Hat Ceph Storage repositories and registry
  access come from that entitlement and must not mix with upstream Ceph
  packages or images. Which RHEL versions a given Red Hat Ceph Storage release
  runs on is the vendor's compatibility guide to state, not Bootwright's.
- `distribution: ibm` requires `entitlementRef` to resolve to an
  `ibm-storage-ceph` `Entitlement` with accepted license terms. IBM Storage Ceph
  registry access and license acceptance come from that entitlement; the RHEL
  BaseOS/AppStream repos cephadm needs come from the `redhat-rhel` subscription
  the nodes register with (profile `subscription` or cluster `osSubscriptionRef`).
  Neither must mix with upstream Ceph packages
  or images. IBM license acceptance
  is passed non-interactively to `cephadm bootstrap`. Because acceptance enables
  IBM Call Home, `spec.ceph.ibm.callHome`
  is required as either `enabled` or `disabled`; apply reconciles the manager
  module to that explicit outbound-communication intent.
- `spec.ceph.osSubscriptionRef`, when set, must resolve to a `redhat-rhel`
  [`Entitlement`](#entitlement) whose `rhsm.management` is `managed`. It names
  the fleet-wide RHEL subscription every storage node registers with. Omitted,
  it is derived from the topology nodes'
  `MachineInstallProfile.spec.subscription.entitlementRef`; if the nodes name
  different entitlements, the derivation yields nothing and no registration is
  planned.

### cephadm identity and bootstrap

- `cephadm.addressRef`, when set, selects a named
  `Machine.spec.addresses[]` entry for cephadm traffic.
- `cephadm.clusterSSH` declares the cluster's own SSH orchestration identity —
  the post-install account and key, as opposed to each `Machine`'s
  `access.ssh`, which is the install-window identity Bootwright installs and
  probes the machine with (ADR 0019). The block has two fields, `user` and
  `keyRef`.
- `cephadm.clusterSSH.user` is the OS user cephadm manages every host as
  (`cephadm --ssh-user`, reconciled day-2 with `ceph cephadm get-user` /
  `set-user`) and the account Bootwright itself connects as once a node is
  provisioned. It must be a valid POSIX user name. It defaults to `cephadm` on a
  managed cluster and to `root` on an external one. A `root` resolution is
  rejected when any topology node revokes root, is a machine Bootwright installs
  (which never permits a root login), or authors a non-root `access.ssh.user`.
  When it resolves to a non-root name, Bootwright provisions that
  account on every topology node — locked password, no `wheel` membership, the
  public half of `clusterSSH.keyRef` authorized, and a per-user sudoers drop-in at
  `/etc/sudoers.d/60-bootwright-<user>` carrying `Defaults:<user> !requiretty`
  and `<user> ALL=(ALL) NOPASSWD: ALL`. The drop-in is necessary but not
  sufficient and is proved rather than assumed: apply proves the account answers
  `sudo -n true` over a terminal-less channel — the one cephadm's manager uses —
  before any root revocation. The three ways a correct grant is still not
  evaluated are in `security.md`, Node Login Identity and Privilege. The name
  may equal that
  node's `access.ssh.user`, which declares that the account is the machine's
  install-window identity and already exists; Bootwright then reconciles the
  account instead of creating it.
- `cephadm.clusterSSH.keyRef` names the `sshKeyPair` `Secret` that is the
  cluster's management identity — the key cephadm distributes to and reaches
  every host with, the key the install authorizes for a node the cluster
  creates, and the key Bootwright connects to that account with. It must
  resolve to a declared `sshKeyPair` `Secret` and is **required** whenever
  `clusterSSH.user` resolves to a non-root name, which is the managed default.
  It must not name a `Secret` a `Machine` authors as its own
  `access.ssh.auth.privateKeyRef`: `cephadm bootstrap --ssh-private-key`
  persists the cluster identity into the Ceph mon config-key store, and an
  authored machine key also opens machines outside the cluster
  (`security.md`, Node Login Identity and Privilege). A key the cluster derives
  onto its own nodes carries no such reach and is the normal shape.
- `cephadm.bootstrap.node` names a storage topology host by its node name
  (the FQDN or its short label); a machine name is rejected with guidance
  naming the node. The rendered cephadm `--mon-ip` is always an address of
  this host: the address named by `bootstrap.addressRef`, defaulting to
  `cephadm.addressRef` and finally the host machine's SSH address.
- `cephadm.bootstrap.singleHostDefaults: true` renders `cephadm bootstrap
  --single-host-defaults`, setting the CRUSH/replication defaults a single-node
  cluster needs to reach `active+clean`. It is valid only for a one-host,
  non-stretch topology with at least two in OSDs. A static device selection
  producing fewer than two OSDs is rejected; a dynamic selection waits for at
  least two before pool creation. The flag is rejected when
  `spec.ceph.config[global]` also sets any of the three defaults it owns
  (`osd_pool_default_size`, `osd_pool_default_min_size`,
  `osd_crush_chooseleaf_type`).
- `spec.ceph.cephadm.bootstrap.node` must match a
  `spec.ceph.topology.nodes[]` entry's node name.

### Networks and config

- `spec.ceph.networks.publicCIDRs[]` and `clusterCIDRs[]` must be valid CIDRs.
  They render to the Ceph `public_network`/`cluster_network` configuration
  (seeded at bootstrap, reconciled by `ceph config set` on later applies).
- `spec.ceph.config` declares Ceph configuration database options as
  `config.<section>.<key>: <value>`, rendered as idempotent `ceph config set`
  operations. Sections are `global`, `mon`, `mgr`, `osd`, `mds`, `client`, or
  `<type>.<id>`. Keys removed from the spec are not unset on the cluster (the
  storage-wide additive-only rule above). `public_network` and
  `cluster_network` are owned by `spec.ceph.networks` and rejected here, and
  `container_image` is owned by `spec.ceph.image` and rejected in every section:
  these operations run after the first service specs are applied, so a daemon
  image declared here would reach the cluster only after the daemons it was
  meant to govern had already been deployed from the old value.
- `spec.ceph.mgrModules[]` declares mgr modules, rendered as idempotent
  `ceph mgr module enable` operations. Modules removed from the spec are not
  disabled (additive-only); module settings are declared under `config.mgr`
  (`mgr/<module>/<key>`).

### Placement, monitoring, and management

- Ceph placement blocks — on `spec.ceph.monitoring` services, the `services[]`
  passthrough, the management ingress, CephFS `mds`, RGW, and ingress — share one
  grammar: `hosts[]` and `sites[]` narrow the resolved host set (`hosts[]`
  entries name topology hosts by node name — FQDN or short label, never
  the machine name — and `sites[]` entries name topology sites), and
  `countPerHost` (non-negative) sets how many daemons cephadm co-locates per
  resolved host.
- `spec.ceph.monitoring` declares the cephadm monitoring stack. Absent means
  the cephadm default stack with cephadm's own placement; `enabled: false`
  renders `cephadm bootstrap --skip-monitoring-stack` and forbids per-service
  blocks. The `prometheus`, `grafana`, and `alertmanager` services place by
  the topology roles of the same names exactly like `mon`/`mgr` (narrow with
  `placement.sites`/`hosts`); `nodeExporter`, `loki`, and `promtail` carry no
  topology role and keep the cephadm all-hosts default unless narrowed by
  explicit `placement.hosts`/`sites`. Service knobs render 1:1 into the cephadm
  service spec: `port`, `networks[]` (CIDRs the daemon binds), and (prometheus
  only) `retentionTime`/`retentionSize` as `retention_time`/`retention_size`. An
  authored service must resolve to at least one host.
- `spec.ceph.mgmtGateway`, when set, publishes the cephadm mgmt-gateway (the HA
  front door to the Ceph dashboard/Grafana). `ingress` is required; `dnsLabel`
  is the leftmost label only, never an FQDN, and the published name is always
  composed as `<dnsLabel>.<StorageCluster.metadata.name>.<domains.storageClusters>`
  (ADR 0018), mirroring node FQDN composition. `dnsLabel` defaults to `mgr` and
  must be a valid DNS label (`[a-z0-9]([-a-z0-9]*[a-z0-9])?`) — a dotted value is
  rejected, so the cluster and domain arms can never be overridden per cluster.
  Without an environment domain to compose from there is no published name at
  all and no DNS record is emitted.
  `ingress` mirrors the RGW ingress VIP shape — `name`, `address`, `prefixLength`,
  optional `virtualInterfaceNetworks[]`, optional `firstVirtualRouterID`
  (`1`-`255`; cephadm's keepalived VRRP router ID, rendered verbatim as
  `first_virtual_router_id` — cephadm defaults to `50` when omitted; sharing an
  ID with another ingress group, such as an RGW gateway's, on an overlapping
  `virtualInterfaceNetworks` is rejected, see StorageObjectGateway below), and a
  `placement` that defaults to every `ingress`-role host, narrowed by
  `sites`/`hosts` (under stretch it must cover both data sites regardless of
  narrowing — unlike StorageObjectGateway, there is only ever one management
  ingress, so there is no sibling to cover the other site). `port` sets the
  gateway port (`0`–`65535`). `exposure` declares the scheme the gateway itself
  serves: `https` (the default) keeps cephadm's TLS, `http` pins the gateway
  spec to `ssl: false` and forbids `tls` and `oauth2Proxy`. On
  subscription-backed distributions (`redhat`, `ibm`) the field is required
  explicitly — their cephadm builds record gateway daemon dependencies without
  the certificate entries they recompute, so every https shape reconfigures
  forever (ADR 0047); `http` converges today, and an explicit `https` is the
  declaration for a vendor build that repairs the recording (ADR 0049).
  `tls`, when set,
  supplies the gateway certificate through `certificateRef`+`keyRef`, each of
  which must name a `tlsCertificate` Secret (accepted only on `oss`). `enableAuth`
  and `oauth2Proxy` are coupled: `enableAuth: true` requires an `oauth2Proxy`
  block (`providerDisplayName`, `clientId`, `oidcIssuerUrl`, and `clientSecretRef`
  required; optional `redirectUrl`, `httpsAddress`, `allowlistDomains[]`,
  `cookieSecretRef`), and an `oauth2Proxy` block requires `enableAuth: true`;
  `oauth2Proxy` additionally requires `exposure: https` and is refused on
  `redhat`/`ibm`, whose builds loop the `oauth2-proxy` service the same way.
- `spec.ceph.security.fips.enabled: true` requires distribution `redhat` or `ibm`
  (rejected for `oss`) and gates the cluster on FIPS-mode Ceph hosts: every host
  that installs a managed OS must reference a `MachineInstallProfile` whose
  `customizations.security.fips.enabled` is also `true`.
- `spec.ceph.services[]` is the cephadm service-spec passthrough for service
  types Bootwright does not model first-class (`snmp-gateway`, `oauth2-proxy`, ...):
  `serviceType`, `serviceID`, `placement`, and `spec` render field for field
  into a `ceph orch apply` document. Types owned by a first-class surface
  (topology roles, monitoring, gateways) are rejected — the reserved set is
  `host`, `mon`, `mgr`, `osd`, `mds`, `rgw`, `ingress`, `prometheus`, `grafana`,
  `alertmanager`, `node-exporter`, `nfs`, `loki`, and `promtail`; `placement`
  requires explicit `hosts` or `sites`. `nfs` is owned by the
  [`StorageNFSExport`](#storagenfsexport) kind and `loki`/`promtail` by
  `spec.ceph.monitoring`, so they must be declared there, not through this
  passthrough.

### Topology, OSDs, and stretch

- `spec.ceph.topology.nodes[]` require a `machineRef` to a `ceph-node`
  `Machine` and at least one `roles[]` value from `mon`, `mgr`,
  `osd`, `mds`, `rgw`, `ingress`, `prometheus`, `grafana`, `alertmanager`.
  `site` is optional and **derived from the bound Machine's
  `spec.placement.site`**; authoring it is allowed and then cross-checked — a
  node whose site disagrees with its machine is a hard error, because a machine
  stands in one site and the cluster cannot place it in another. A site is
  required exactly where it has effect — when `spec.ceph.topology.stretch` is
  set (it becomes the cephadm CRUSH location) or when any placement narrows by
  `sites` — and optional otherwise (no location is rendered without stretch).
  Where it is required, the requirement lands on the Machine
  (`spec.placement.site`), which is the single writer.
  Optional `labels[]` pass additional free-form cephadm host labels (for
  example `_admin`) through verbatim; roles always become labels.
  `devices[]` is the lean OSD shorthand (literal paths ==
  `osd.dataDevices.paths`); the optional `osd` object is the drivegroup-shaped
  selection mirroring the cephadm OSD spec (`dataDevices`/`dbDevices`/
  `walDevices` with `paths|pathSpecs|all|model|vendor|rotational|size|limit`,
  plus spec-level `filterLogic`, `encrypted`, `tpm2`, `osdsPerDevice`,
  `crushDeviceClass`, `blockDBSize`/`blockWALSize`/`dbSlots`/`walSlots`,
  `dataAllocateFraction`, the top-level `unmanaged`, and a `serviceOverrides`
  escape hatch (`extraContainerArgs`/`extraEntrypointArgs`/`networks`/
  `customConfigs`)), mutually exclusive with `devices`.
  Within an `osd` object, `dataDevices` is required; `filterLogic` accepts
  `AND` or `OR`; `tpm2: true` requires `encrypted: true`; `osdsPerDevice`,
  `dbSlots`, and `walSlots` must be non-negative; and `dataAllocateFraction`,
  when set, must be in `(0, 1]`.
  `tpm2: true` additionally requires every node it covers — per-host or through
  a fleet drivegroup — to install the `tpm2-tss` libraries, because cephadm
  seals the key by running `systemd-cryptenroll` in the host's own mount
  namespace and that binary loads them dynamically. They are only a weak
  dependency of `systemd-udev`, so a node's `MachineInstallProfile` must either
  list `tpm2-tss` in `customizations.packages.install` or enable
  `customizations.security.diskEncryption`, which installs it. OSD encryption
  and machine root-disk encryption remain independent controls.
  Both require the `osd` role, and every osd-role host must author one of
  them — or be covered by a fleet `spec.ceph.topology.osdDrivegroups[]` entry:
  OSD device consumption is explicit opt-in, so consuming all available
  devices is the authored `osd: {dataDevices: {all: true}}`, never an
  omission default.
  `spec.ceph.topology.osdDrivegroups[]` are fleet OSD specs ({`serviceID`,
  `placement`, `osd`}): one cephadm OSD service spanning the resolved osd-role
  hosts instead of one spec per host. A host is owned by exactly one OSD spec —
  validation rejects a host claimed by both a fleet and a per-host
  `osd`/`devices`, or by two fleets.
  `name` names the node, independent of the machine name, and is the
  rendered cephadm host-spec hostname. It is required and declared explicitly
  per node — there is no default inferred from list position. It must be a DNS
  label (`[a-z0-9]([-a-z0-9]*[a-z0-9])?`) and composes to
  `<name>.<cluster>.<domains.storageClusters>` (the
  storage-cluster zone; `domains.storageClusters` defaults to
  `domains.clusters`, which defaults to `domains.base`; kept bare when the
  `Environment` declares no domain). A dotted value is rejected;
  `topology.nodes[].fqdn` is the explicit override, used verbatim and
  unaffected by the zone (ADR 0025). It is rendered
  verbatim as the cephadm host identity and must equal the host's real OS
  hostname — self-fulfilling for Bootwright-installed machines (the installer
  sets the OS hostname to the same node FQDN), and written by `apply` on every
  storage node, `os.provided` included, before the run touches the cluster
  (ADR 0036); a mismatch passes `validate` and is repaired on the host, and
  `apply` refuses the node only when the write does not hold. Before the cluster
  is bootstrapped or converged, `apply` refuses any topology host still running
  the systemd units of a cephadm cluster it does not own — an fsid that is
  neither the node's own `/etc/ceph/ceph.conf` identity nor the one an authorized
  rebuild is replacing — because every cephadm daemon binds its port on the host
  network and the collision surfaces only at service readiness, attributed to no
  host. `--authorize foreign-daemons` removes exactly those identities with the
  fsid-scoped `cephadm rm-cluster --force --fsid <fsid>`, zapping no disk, then
  re-probes the node and refuses again if any of their units survived
  (ADR 0038). The per-host OSD service id derives from the node short
  name (`data-<nodeShortName>`). Node names must be
  unique. All host `Machine`s in one `StorageCluster` must share one SSH user
  and `keyRef`. A host `Machine` is node-bound by at most one cluster (and at
  most one host entry) across every `ContainerCluster` and `StorageCluster`.
  After cephadm applies the OSD specs, convergence waits for the full declared
  static OSD count and at least one in-OSD on every dynamically selected host
  before creating pools or reporting success.
- Storage placement policies, pools, filesystems, gateways, and exports must
  reference the owning `StorageCluster`.
- `spec.ceph.topology.stretch` enables stretch mode by presence (no `enabled`
  flag):
  - **Required:** `failureDomain` (CRUSH failure domain for the stretch rule).
  - **Tiebreaker (arbiter-optional):** `tiebreaker.node` names a topology host
    by node name — FQDN or short label; machine names are rejected. Omitting the
    whole `tiebreaker` block is accepted with a WARN advisory: without an
    arbiter mon in a third site the cluster loses quorum if either data site
    fails, and `enable_stretch_mode` is skipped until a tiebreaker is authored.
    A *partially* authored tiebreaker (only `node`, or only `site`) is a hard
    error, as is a `tiebreaker.node` that is not mon-only or carries OSD
    devices. A tiebreaker that sits **in a data site** is a WARN advisory, not
    an error: it is the acknowledged emergency shape `--authorize
    same-site-arbiter` produces while the third site is gone, and it must load
    and re-apply. Ceph still refuses `enable_stretch_mode` in that shape, so a
    cluster not already stretched cannot enter stretch mode this way.
  - **Normalized:** `dataSites` from the sites of the topology's non-tiebreaker
    hosts (the tiebreaker is excluded **by node name**, so a tiebreaker sharing
    a data site does not erase that site), `tiebreaker.site` from the
    tiebreaker host's `site`, `ruleName` to `stretch-rule`. Author `dataSites`
    only to exclude OSD-only sites the derivation would wrongly include.
  - **Replication:** not authorable; policy-less replicated pools always render
    `size: 4` / `minSize: 2` (the two-site stretch requirement); non-4/2 is
    unsupported. Authoring `stretch` on an existing cluster re-rules and resizes
    every policy-less pool on the next apply with no `StoragePool` change;
    `bootwright validate` prints a one-line notice naming the inheriting pools.
  - **Validation (post-normalize):** `dataSites` holds exactly two sites; each
    data site holds exactly two **non-tiebreaker** `mon` hosts. When a
    tiebreaker is authored it carries the `mon` role and is mon-only with no
    OSD `devices`. Erasure-coded pools are rejected; MDS, RGW, and
    ingress placement include at least two role-capable hosts per data site.

## StoragePlacementPolicy

`StoragePlacementPolicy` owns reusable Ceph placement and replicated-pool
defaults for the pools that select it.

Rules:

- `spec.storageClusterRef` is required and must reference a managed
  (non-`external`) `StorageCluster`.
- `spec.ceph.ruleName` is required.
- `spec.ceph.failureDomain` and `spec.ceph.replicated.{size,minSize}` are the
  defaults applied to pools that reference the policy.
- `spec.ceph.crushDeviceClass`, when set, pins the CRUSH device class (for
  example `ssd`, `hdd`, `nvme`) the policy's rule targets, restricting the pool
  to OSDs of that class.
- Effective `minSize` must not exceed `size`; authored values are non-negative.
- For a host failure domain, the effective replica count must not exceed the
  number of topology hosts carrying the `osd` role.

## StoragePool

`StoragePool` owns one Ceph pool.

Rules:

- `spec.storageClusterRef` is required and must reference a managed
  `StorageCluster`.
- `spec.placementPolicyRef`, when set, must reference a
  `StoragePlacementPolicy` on the same `StorageCluster`. The referenced policy
  owns the pool's replication; `spec.ceph.replicated` must not also be set on
  the pool.
- `spec.ceph.type` accepts `replicated` (default) or `erasure` (the upstream
  `ceph osd pool create` words; the populated arm key equals the type value).
  `replicated` must not set `ceph.erasure`. `erasure` requires
  `ceph.erasure.{dataChunks,codingChunks}` (rendered as the erasure-code
  profile `k=`/`m=`), must not set `ceph.replicated`, and is not allowed on
  stretch-mode clusters. The profile also accepts `plugin`, `technique`,
  `crushDeviceClass`, `crushRoot`, `stripeUnit`, and an opaque `parameters`
  map (rendered with their `erasure-code-profile set` spellings); the whole
  profile is immutable, so every field is part of the pool's structural
  identity. Per-pool steady-state intents — `ceph.autoscale`, `ceph.quota`,
  `ceph.compression` — render as idempotent `ceph osd pool set`/`set-quota`
  ops and are explicitly NOT structural (they reconcile in place).
- `spec.ceph.role`, when set, accepts `rbd`, `cephfs-metadata`, `cephfs-data`,
  or `rgw`. It drives `StorageExport` wiring and infers the pool application
  (`rbd` → `rbd`, `cephfs-*` → `cephfs`, `rgw` → `rgw`);
  `spec.ceph.application` overrides the inference.
- The pool's structural identity is its `type` (and erasure profile): the only
  desired-state change that rebuilds a live pool (data-destroying, `--mode rebuild`
  only). Replicas, crush rule, and application reconcile in place.
- `spec.ceph.mirroring`, when set, enables RBD mirroring on the pool: `mode` is
  `image` (mirror only images with the mirror flag set) or `pool` (mirror every
  image in the pool).
- On stretch-mode clusters, pools inherit the stretch CRUSH rule and the
  fixed stretch replication (`size: 4` / `minSize: 2`) without a placement
  policy; a `placementPolicyRef` is needed only for genuinely divergent
  placement.
- On stretch-mode clusters, `ceph.replicated.size` must be `4` and `minSize`
  must be `2` when set.
- Outside stretch mode, host-domain replicated size and erasure `k+m` must not
  exceed the number of OSD hosts. Policy-less single-host pools inherit
  cephadm's `2/1` replication and OSD-level failure domain when
  `singleHostDefaults` is enabled.
- Effective replicated `minSize` must not exceed `size`; authored values are
  non-negative.

## StorageFilesystem

`StorageFilesystem` owns one CephFS filesystem and the pools that back it.

Rules:

- `spec.storageClusterRef` is required and must reference a managed
  `StorageCluster`.
- `spec.cephfs.metadataPoolRef` is required and must reference a
  `StoragePool` on the same `StorageCluster`. The metadata pool is part of the
  filesystem's structural identity — Ceph cannot move a live CephFS to a
  different metadata pool — so changing it is a data-destroying, `--mode rebuild`-only
  recreate (`ceph fs rm` then recreate), never an in-place reconcile.
- `spec.cephfs.dataPoolRefs[]` is required; each entry is a plain pool name
  (a single entry becomes the default automatically) or `{name, default}` to
  elect the default data pool on multi-pool filesystems. Each must reference a
  `StoragePool` on the same `StorageCluster`, must differ from the metadata
  pool, and exactly one must be the default. The default data pool is also part
  of the filesystem's structural identity: changing which data pool is the
  default is the same data-destroying, `--mode rebuild`-only recreate as the metadata
  pool (Ceph cannot move a live CephFS to a different default data pool in place).
- `spec.cephfs.mds.placement` defaults to every topology host with the `mds`
  role; `sites`/`hosts` narrow the selection and must resolve to at least one
  `mds`-capable host. On stretch-mode clusters the resolved placement must
  cover at least two MDS-capable hosts per data site.
- `spec.cephfs.mds` also carries `activeCount` (active MDS ranks; default `1`),
  `standbyReplay` (a hot standby per rank), `standbyCountWanted` (extra cold
  standbys; non-negative), and a `serviceSpec` escape hatch (`unmanaged`,
  `extraContainerArgs`/`extraEntrypointArgs`, `networks[]` CIDRs). An
  active+standby intent the resolved placement cannot host is rejected — it would
  converge to a permanent `MDS_INSUFFICIENT_STANDBY`.
- `spec.cephfs.subvolumeGroups[]` declare CephFS subvolume groups: each needs a
  unique `name` and optionally `mode` (an octal such as `0755`), `uid`/`gid`
  (non-negative), `sizeBytes` (a non-negative quota), and `poolLayoutRef` (a
  same-cluster `StoragePool` giving the group's data layout).

## StorageObjectGateway

`StorageObjectGateway` owns one RGW service and its ingress endpoints.

Rules:

- `spec.storageClusterRef` is required and must reference a managed
  `StorageCluster`.
- `spec.ceph.serviceID` is required; `spec.ceph.frontendPort` must be in
  `0`–`65535`.
- `spec.public` is the storage-owned public S3 endpoint; the gateway owns this
  fact, so a storage-only object store needs no `ContainerCluster`.
  `spec.public.dnsLabel` is the leftmost label only, never an FQDN, and the
  published name is always composed as
  `<dnsLabel>.<StorageCluster.metadata.name>.<domains.storageClusters>`
  (ADR 0018) — the same composition `spec.ceph.mgmtGateway.dnsLabel` feeds, with
  the cluster arm taken from `spec.storageClusterRef`. `dnsLabel` defaults to the
  gateway's own `metadata.name`, so multiple gateways on one cluster never
  collide, and must be a valid DNS label (`[a-z0-9]([-a-z0-9]*[a-z0-9])?`) — a
  dotted value is rejected. Without an environment domain to compose from there
  is no published name at all and no DNS record is emitted. Optional
  `spec.public.scheme` and `spec.public.port` refine the endpoint and are
  independent of the name: `port` still sets the ingress frontend port even when
  no name can be composed.
- `spec.ceph.placement` defaults to every topology host with the `rgw` role;
  `sites`/`hosts` narrow the selection. On stretch-mode clusters the resolved
  placement must cover at least two hosts per data site — unless `sites`
  explicitly narrows it to a subset of the cluster's data sites, in which case
  only those named sites need the two-host minimum. This is what makes a
  per-site RGW pattern expressible: author one `StorageObjectGateway` per data
  site, each with `spec.ceph.placement.sites: [<site>]` and its own
  `serviceID`, so each site's RGW daemons — and, paired with a same-site
  ingress below, that site's HAProxy backends — never span the other site.
  Sibling gateways are not required to jointly cover every data site; an
  operator may deliberately serve only one site.
- `spec.ceph.ingresses[]` require a unique `name`, a storage-owned `address` and
  `prefixLength` for the ingress VIP (optional `virtualInterfaceNetworks[]`,
  rendered verbatim to the cephadm ingress `virtual_interface_networks`; optional
  `firstVirtualRouterID`, `1`-`255`, rendered verbatim as `first_virtual_router_id`
  — cephadm defaults to `50` when omitted), and a `placement` that defaults to
  every `ingress`-role host, narrowed by `sites`/`hosts` (per-site VIPs author
  `placement.sites`; a stretched L2 network across data sites may instead
  author a single ingress with unnarrowed `placement`, spanning every
  `ingress`-role host cluster-wide). On stretch-mode clusters, a gateway's
  ingresses are held to the same site-coverage rule as `spec.ceph.placement`
  above, evaluated over their combined resolved hosts: fully unnarrowed
  ingresses must reach two hosts in every data site, while ingresses that are
  all explicitly site-scoped need only cover the sites they name. Two ingress
  groups anywhere on the same `StorageCluster` — RGW, NFS
  (`StorageNFSExport.spec.ceph.ingresses[]`), or
  `spec.ceph.mgmtGateway.ingress` — that declare the same `firstVirtualRouterID`
  **and** an overlapping `virtualInterfaceNetworks` entry are rejected: two
  keepalived VRRP instances with the same router ID on the same L2 segment
  conflict. Groups on disjoint networks (the per-site-subnet pattern) may
  reuse an ID freely; the check only fires on a declared, overlapping network.
  Each ingress's optional `tls` supplies the HAProxy frontend certificate
  through `certificateRef`+`keyRef`, each of which must name a
  `tlsCertificate` Secret — unlike `spec.ceph.mgmtGateway.tls` (two separate
  cephadm fields), the cephadm ingress spec takes one combined `ssl_cert`
  field, so Bootwright concatenates the certificate and key PEM content at
  apply time. Without `tls`, cephadm serves its own self-signed certificate
  on the ingress VIP, the same fallback `spec.ceph.mgmtGateway.tls` has.
- Optional `spec.ceph.realm`/`zoneGroup`/`zone` bind the RGW to a named
  multisite realm (rendered as `rgw_realm`/`rgw_zonegroup`/`rgw_zone`); all three
  are set together, and Bootwright creates them and commits the period before the
  service applies. Multiple `StorageObjectGateway` objects on one `StorageCluster`
  may declare the identical realm/zoneGroup/zone (or all leave it unset, landing
  in Ceph's one implicit default zone) — this is the supported way to run several
  independently-placed RGW services against one shared zone, and is not RGW
  multisite: Bootwright never configures cross-realm/cross-zone replication.
  Optional `spec.ceph.config` is a per-RGW
  `ceph config set client.rgw.<serviceID>` map (one owner: a key must not also
  appear in the cluster config map, and `rgw_frontend_port` is reserved).

## StorageNFSExport

`StorageNFSExport` owns one cephadm NFS-Ganesha service and its exports.

Rules:

- `spec.storageClusterRef` is required and must reference a managed
  `StorageCluster`. `spec.ceph.serviceID` is required.
- `spec.ceph.placement` must set `hosts` or `sites` (there is no `nfs` topology
  role). `spec.ceph.ingresses[]` mirror the RGW ingress shape and front
  `nfs.<serviceID>` on the standard `2049`, so a client always mounts the VIP on
  the port it expects.
- `spec.ceph.port` is the port the ganesha daemons themselves listen on. It
  defaults to `2049` for a directly-mounted service and to `12049` when the
  service declares an ingress, because ganesha binds every address on the host:
  a backend still holding `2049` leaves haproxy unable to take that port on the
  VIP, and cephadm refuses to deploy the daemon. Declaring `2049` together with
  an ingress is therefore rejected rather than silently broken.
- Each `spec.exports[]` sets a unique `pseudo` path and exactly one FSAL —
  `filesystemRef` (CephFS, same cluster) or `bucket` (RGW). Optional `path`,
  `accessType` (`RW`/`RO`/`NONE`), `squash`, and `clients[]` spell the
  `ceph nfs export create` flags. cephadm auto-provisions the backing `.nfs`
  pool, so no pool/namespace is modeled. Additive-only: a removed export keeps
  running.

## StorageExport

`StorageExport` owns the exported storage surface consumed by a downstream
platform.

Rules:

- `spec.type` must be `dataFoundation`. For an export of a managed
  `StorageCluster` the populated `spec.dataFoundation` arm supplies it — normalize
  materializes `type: dataFoundation` when omitted. For an export of an external
  `StorageCluster` the `dataFoundation` arm must be empty, so there is no arm to
  derive from and `spec.type: dataFoundation` must be authored explicitly.
- `spec.storageClusterRef` is required.
- For managed `StorageCluster`s, `spec.dataFoundation` is required;
  `dataFoundation.rbdPoolRef` and `filesystemRef` are required and must reference
  resources on the same `StorageCluster`; `objectGatewayRef` is optional and
  same-cluster. When set, the `openshift-data-foundation`/`fusion-data-foundation`
  add-ons' external-cluster-details exporter step passes the referenced
  `StorageObjectGateway`'s composed public name and `spec.public.port` as
  `--rgw-endpoint`, so ODF external mode also provisions S3/object storage;
  omitted, the export covers RBD and CephFS only. The step reads that name from
  the resolved `objectGateway.publicFQDN` key the step-ref carries alongside the
  serialized gateway — the spec itself holds only the label.
- For external `StorageCluster`s, `spec.dataFoundation` must be empty and
  `spec.externalDetails` is required.
- `spec.externalDetails`, when set, requires `fromSecretRef` (its only arm),
  which must resolve to a declared `Secret` holding the operator-supplied
  external-cluster-details JSON. A managed-`StorageCluster` export may omit
  `externalDetails` entirely: the consuming add-on then produces the payload
  itself — its step runs the exporter on a Ceph node of the export's cluster
  and captures the JSON as a step output.

## ClusterAddon

`ClusterAddon` owns one post-install bootstrap component.

Rules:

- `spec.type` is required and must be `olm` or `manifestSet`; the two
  arms are mutually exclusive.
- `olm` requires `spec.olm` and must not set `manifestSet`.
  `olm.namespace.name` is required; optional `olm.namespace.create` and
  `olm.namespace.labels` control namespace creation and labels. Optional
  `olm.operatorGroup` sets the OperatorGroup `name` and `targetNamespaces[]`.
  Optional `olm.catalogSource` ships an operator catalog with the add-on:
  `name` and `image` are required, `displayName`/`publisher`/`pollInterval` are
  optional (`pollInterval` is a Go duration), and optional
  `grpcPodConfig.securityContextConfig` must be `legacy` or `restricted`.
  `subscription.source` must match `catalogSource.name` (normalize defaults it
  when omitted).
  `olm.subscription` requires `name`, `package`, `channel`, and `source`;
  `sourceNamespace` defaults to `openshift-marketplace` and `installPlanApproval`
  defaults to `Automatic` (both normalize-materialized; `installPlanApproval`
  accepts `Automatic` or `Manual`), and optional `startingCSV` pins the initial
  CSV. Each `olm.customResources[]` entry requires `apiVersion`, `kind`, and
  `metadata.name`; `metadata.namespace` is optional (omitted for cluster-scoped
  resources). Apply installs the shipped CatalogSource first (waiting for its
  registry to report a READY connection), then the
  namespace/OperatorGroup/Subscription, waits for the operator's CSV to reach
  `Succeeded`, then applies the custom resources.
- `manifestSet` requires `spec.manifestSet.manifests[]` (at least one) and must
  not set `olm`. Each `manifests[].path` is relative to the `ClusterAddon` file,
  ends in `.yaml`/`.yml`, must stay within the file directory, must not be a
  symlink, and must exist.
- `spec.provides[]` accepts open capability tokens matching
  `^[A-Za-z0-9][A-Za-z0-9._-]*$`; declaring any `provides` value requires at
  least one `spec.readiness.checks[]` entry.
- `spec.requires[]` accepts the same token grammar as `provides[]`. Each
  requirement must be provided by another add-on in the same binding (ordering is
  resolved per binding), and add-ons are applied after the add-ons providing their
  required capabilities (a per-binding stable topological order). Unsatisfied
  requirements and `requires`/`provides` cycles are rejected.
- `spec.readiness.timeout` is a Go duration. Each
  `spec.readiness.checks[]` entry is a presence union with exactly one of
  `csvSucceeded` (requires `namespace`, `subscription`), `condition` (requires
  `apiVersion`, `kind`, `name`, `condition.{type,status}`), or
  `resourceExists` (requires `apiVersion`, `kind`, `name`).
- `spec.accepts.inputs[]` declare binding-scoped scalar inputs. Each input has a
  `name`, an optional `required` marker (when `true`, every binding of the add-on
  must supply the input), exactly one of `resourceRef.kind` (a known Bootwright
  kind) or `secretRef: {}`, and optional `effects[]`. Each effect is a presence
  union with exactly one of `storageExportAttachment: {}` or
  `globalPullSecretMerge`.
- A `storageExportAttachment` effect requires the add-on to provide
  `dataFoundation` and the input to declare `resourceRef.kind: StorageExport` —
  the scope machinery reads the scalar input value to pull the referenced Ceph
  cluster into the add-on's task state. The attachment itself (the
  external-details payload and consumer manifests) is applied by the add-on's own
  steps.
- A `globalPullSecretMerge` effect requires `registry` and `username` under the
  `globalPullSecretMerge` arm, and the input must declare `secretRef: {}`. Before
  the add-on's resources apply, the referenced secret's value is merged into the
  bound cluster's global pull secret as the `auths[<registry>]` credential.
- `spec.steps[]` ship add-on-owned imperative integration run at a lifecycle
  point of the add-on apply — an Ansible playbook, templated Kubernetes
  manifests, or both — so the logic travels with the add-on instead of being
  compiled into Bootwright. `playbook`, `rolesPath`, `collectionsPath`, and each
  `manifests[].path` are relative paths resolved against the `ClusterAddon` file
  (the `manifestSet.path` rules); the loader skips the `playbooks/`, `roles/`,
  and `collections/` subtrees as Ansible content.
  - A `steps[].manifests[].path` must contain a `manifests/` path segment, and a
    `steps[].playbook` must contain a `playbooks/` segment unless the step sets
    `spec.source.path`, whose external directory the loader never walks
    (`source.git` is rejected on add-on steps). Both segments keep the loader
    from parsing the content as desired state; a path outside them fails
    validation.
  - `steps[].name` is required and unique within the add-on. Exactly one of
    `steps[].gates` or `steps[].follows` is required and anchors the step to a
    lifecycle point: `gates: apply` (before the operator install, blocking it
    until the step succeeds), `follows: operatorReady` (after the operator CSV
    reaches `Succeeded`, before `olm.customResources`; olm add-ons only), or
    `follows: ready` (after the readiness checks pass). Steps run in that
    lifecycle order. `gates` cannot be combined with `onFailure: continue` — a
    gate that lets the add-on proceed on failure is not a gate.
  - A step ships a `playbook`, `manifests[]`, or both. A manifest-only step (no
    `playbook`) applies templated manifests from values already available to the
    add-on and ignores `target`/`outputs`; a playbook step additionally runs
    imperative work against resolved machines and captures outputs.
  - `steps[].target` selects the machines a playbook runs against and is required
    for a playbook step. It is a presence union with exactly one arm:
    `target.boundCluster: {}` (the bound `ContainerCluster`'s nodes),
    `target.fromInput.input: <accepted input name>` (a `resourceRef` input
    dereferenced to its object, then mapped to nodes: `StorageExport` → its
    `storageClusterRef` Ceph nodes, `StorageCluster` → its Ceph nodes,
    `ContainerCluster` → its agent nodes, `Machine` → the machine), or
    `target.static` with `clusters[]` and/or `machines[]`. A step carries no
    `hostGroups` and can never resolve to the controller/localhost.
    `target.limit` is `firstReachable`
    (default: run against the first machine that answers) or `all` (run against
    every resolved machine). Storage-cluster targets connect through the
    cluster's post-install `cephadm.clusterSSH` user and key; container-cluster
    and direct Machine targets use the Machine's `access.ssh` identity.
  - `steps[].secretRefs[]` name `Secret`s materialized into the step's scoped
    per-run secrets directory (`bootwright_step_secrets_dir`) — only the declared
    secrets, never the whole store. `steps[].extraVars` is a free-form map handed
    to the playbook as one JSON `-e` value. It MUST NOT carry a connection or
    privilege-escalation key (`ansible_user`, `ansible_ssh_user`, `ansible_host`,
    `ansible_ssh_host`, `ansible_port`, `ansible_ssh_port`, `ansible_connection`,
    `ansible_password`, `ansible_ssh_pass`, `ansible_private_key_file`,
    `ansible_ssh_private_key_file`, `ansible_ssh_common_args`,
    `ansible_ssh_extra_args`, `ansible_become`, `ansible_become_user`,
    `ansible_become_method`, `ansible_become_pass`, `ansible_become_password`):
    an extra var outranks every inventory value, so one would silently repoint
    the identity Bootwright connects and escalates with for every host in the
    run, past the declared access, the recorded host-key trust, and the
    `--ssh-user` override. The same restriction applies to
    `CustomPlaybook.spec.extraVars`. Validation rejects it.
  - `steps[].timeout` bounds the playbook run (a Go duration; default `10m`).
    `steps[].run` is `onChange` (default: skip a step whose content and resolved
    inputs are unchanged since the last reconcile) or `always`.
    `steps[].onFailure` is `fail` (default: a failing step blocks the add-on
    apply) or `continue` (record the failure and proceed); a step whose manifests
    consume its outputs must be `fail`.
  - `steps[].outputs[]` declare files the playbook writes under
    `{{ bootwright_step_outputs_dir }}`, captured after the run: `name` (the
    manifest token), `file` (relative to the outputs directory), optional
    `secret` (persisted `0600` under `clusters/<cluster>/secrets/addons/...` and
    reclaimed from run history; non-secret outputs persist under
    `runtime/addons/...`), and `format` `text` (default) or `json` (validates the
    captured bytes parse as JSON). A declared output the playbook did not write
    fails the step; `outputs` require a `playbook`.
  - `steps[].manifests[]` are templated manifests applied to the bound cluster
    (`oc apply --server-side`, in declared order) after the step succeeds. Each
    entry requires `path`; `reclaimRendered` removes the rendered plaintext once
    applied (for manifests embedding secret outputs). Manifest tokens —
    `{{ cluster }}`, `{{ output <name> }}`, `{{ input <in>.<prop> }}`,
    `{{ secret <name> }}`, and `{{ exportDetails <in>.<prop> }}` (the
    operator-supplied external-cluster-details payload of a referenced
    `StorageExport`) — must each be a whole YAML scalar value.

## ClusterAddonProfile

`ClusterAddonProfile` owns a reusable, ordered group of add-ons.

Rules:

- A profile must include at least one of `spec.profileRefs[]` or
  `spec.addonRefs[]`.
- `spec.profileRefs[]` must reference `ClusterAddonProfile`s; nesting must be
  acyclic.
- `spec.addonRefs[]` must reference `ClusterAddon`s.

## ClusterAddonBinding

`ClusterAddonBinding` owns the per-cluster binding of add-ons and binding-scoped
input values.

Rules:

- `spec.clusterRef` is required and references a `ContainerCluster`.
- A binding must include at least one of `spec.profileRefs[]` or
  `spec.addonRefs[]`.
- `spec.profileRefs[]` must reference `ClusterAddonProfile`s;
  `spec.addonRefs[]` must reference `ClusterAddon`s.
- `spec.addonConfigs[].addonRef` must reference a `ClusterAddon` already
  selected by `spec.profileRefs[]` or `spec.addonRefs[]`.
- A given `ClusterAddon` may be applied to one `ContainerCluster` only once
  across all bindings.
- `spec.addonConfigs[].inputs[]` must be declared by the add-on's
  `spec.accepts.inputs`, have unique names, and carry a non-empty scalar
  `value`. Every accepted input the add-on marks `required: true` must be
  supplied.
- Input values for `resourceRef` inputs must name a loaded object of that kind;
  values for `secretRef` inputs must resolve to a declared `Secret`.

## Native add-on catalog store

Bootwright compiles a built-in catalog of ready-made `ClusterAddon` directories
into the binary. `bootwright add-ons add` registers a catalog release into a
machine-local store at `/var/lib/bootwright/add-ons/<name>/` — one registered
version per add-on name, sibling to `contexts/` and `media/` under the
Bootwright root — and `add-ons delete` removes it.

Rules:

- The store is a fallback resolution source, not a load path of its own. When a
  `ClusterAddonBinding` `spec.addonRefs[]` or `ClusterAddonProfile`
  `spec.addonRefs[]` names no authored `ClusterAddon` in the loaded input, the
  loader resolves it from the store directory of that name (which loads like any
  authored add-on directory, its `SourcePath` anchoring the shipped
  playbooks/manifests). An authored add-on with the same name always wins.
- `context init`/`update` snapshot each referenced registered add-on into the
  context input tree, after which the in-tree copy resolves the reference and
  the store is not consulted; the context is self-contained, so deleting or
  re-registering a store add-on never changes an existing context.
- A store that is absent or unreadable (a rootless run cannot traverse the
  root-owned Bootwright directory) falls through to the normal
  unresolved-reference validation error, whose remedy names `add-ons add` when
  the built-in catalog ships the referenced name.

## CustomPlaybook

`CustomPlaybook` owns one operator-supplied Ansible playbook run against
machines at a chosen provisioning stage. It is the imperative escape hatch
sibling of `ClusterAddon`: where an add-on applies declarative Kubernetes objects
into an installed cluster, a `CustomPlaybook` injects an operator playbook
(and optional vendored roles/collections) into the provisioning DAG at any of the
five sub-phases, before or after that phase's built-in work.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: CustomPlaybook
metadata:
  name: harden-storage-nodes
spec:
  follows: machines        # fabric | machines | deps | base | add-ons
  target:
    clusters:
      - nprd-ceph
  playbook: playbooks/harden.yml
  rolesPath: roles
  collectionsPath: collections
  tags:                    # optional --tags
    - tuning
  skipTags:                # optional --skip-tags
    - reboot
  extraVars:
    tuned_profile: throughput-performance
  secretRefs:
    - vault-token
  timeout: 10m             # optional Go duration (default 10m)
  run: onChange            # onChange (default) | always
  onFailure: fail          # fail (default) | continue
```

Rules:

- Exactly one of `spec.gates` or `spec.follows` is required, and its value is one
  of the five sub-phase names (`fabric`, `machines`, `deps`, `base`, `add-ons`) —
  the same vocabulary as `--stage`. `follows` runs the playbook once that phase's
  built-in work has completed; `gates` runs it first and makes that phase's own
  tasks hard-depend on it, so the phase does not start until the playbook
  succeeds. `gates` may not be combined with `onFailure: continue`: a gate that
  lets the phase proceed on failure is not a gate.
- `spec.source.git` optionally fetches the Ansible content from a git repository:
  `url` (an `https`, `ssh`, or `file://` URL, or an absolute local repository
  path), `ref` (commit, tag, or branch), optional `subdir`, and optional
  `secretRef`. Exactly one of `source.path` and `source.git` may be set. The
  fetch happens once per resolved commit under the run directory, only on the
  `apply` path — `plan`, `diff`, and `destroy` omit git-sourced playbooks rather
  than reaching the network. `secretRef` must name a `Secret` whose type matches
  the transport (`sshKeyPair` for `ssh`; `token` or `usernamePassword` for
  `https`); a local repository takes no secret. Authentication is explicit and is
  never inherited from the operator's ssh-agent or git configuration. A branch
  `ref` is allowed, and because `run: onChange` digests the fetched content, the
  playbook re-runs whenever that branch advances. `source.git` is rejected on
  `ClusterAddon.spec.steps[]`, whose content ships with its add-on package.
- `spec.source.path` optionally names an **absolute directory outside the input
  tree** holding the Ansible content. When set, `playbook`, `rolesPath`, and
  `collectionsPath` resolve against that directory instead of the object's own,
  and the `playbooks/` directory rule does not apply (the loader never walks an
  external directory, so there is nothing to mis-parse). The directory must
  exist, be a directory, and not be a symlink; the content paths must still stay
  within it. The same field is available on `ClusterAddon.spec.steps[]`.
  Content outside the context snapshot is **not** copied by `context init`, so
  it must remain present and readable on the controller at apply time.
- `spec.playbook` is required, a `.yml`/`.yaml` file path relative to the
  `CustomPlaybook` file (or to `spec.source.path`), contained within that directory (no
  absolute paths, `..`, or symlinks) — the `ClusterAddon` `manifestSet.path` rules. `rolesPath`
  and `collectionsPath` are optional relative directories under the same rules,
  and must not be named `vendor` or `node_modules` (directories `context init`'s
  tree copy skips, so the vendored content would silently vanish).
- Operator Ansible content lives under `playbooks/`, `roles/`, and `collections/`
  directories; the loader skips those subtrees (they are Ansible content, not
  authored Bootwright objects) while `context init` still copies them so
  `ansible-playbook` resolves them at run time. Vendored collections are the
  air-gap-safe delivery; a Galaxy `requirements.yml` install is not supported.
- `spec.target` selects the inventory hosts and must set at least one of
  `clusters` (a `ContainerCluster` → its agent-node group, a `StorageCluster` →
  its storage group), `machines` (a `Machine` → its node inventory host(s)), or
  `hostGroups` (raw inventory group names). These are selection lists, not
  references. A target may not name the bootwright controller / localhost
  (`localhost`, `127.0.0.1`, `bootwright_ocp_hosts`,
  `bootwright_controller_hosts`): a controller-targeted playbook would run
  operator code as root over every context's secrets.
- `spec.provides`/`spec.requires` order playbooks within the same
  `(gates/follows, phase)` bucket: every `requires` must be met by another enabled
  playbook's `provides` in the same bucket, `provides` are unique, and the graph
  is acyclic. `spec.order` tie-breaks within a bucket.
- `spec.tags` and `spec.skipTags` are optional lists rendered as
  `--tags`/`--skip-tags`. Each entry is a single token (`[A-Za-z0-9][A-Za-z0-9._-]*`)
  because Ansible joins them with commas; entries are unique within a list, and a
  tag may not appear in both, which would select and deselect the same work. Both
  are part of the `onChange` input hash, so changing them re-runs the playbook.
- `spec.secretRefs` must resolve to declared `Secret` objects; the playbook
  reads them from `{{ bootwright_secrets_dir }}/<name>` (never the argv).
- `spec.run` selects re-run behaviour: `onChange` (default) skips a run whose
  declared inputs — spec plus a content digest of the playbook and vendored trees
  — are unchanged since the last reconcile; `always` re-runs every apply.
- `spec.onFailure` is `fail` (default: a failed playbook blocks the anchor
  phase) or `continue` (the failure is recorded and the phase proceeds).
- `spec.timeout` caps a single playbook run, as a Go duration string
  (`90s`, `45m`, `2h`); it must parse and be greater than zero. It defaults to
  `10m`, the same shape and default as `ClusterAddon.spec.steps[].timeout`, so a
  hung playbook fails its task instead of wedging the apply forever. It is an
  execution bound, not declared input: changing it alone does not re-run an
  `onChange` playbook.
- `spec.enabled` defaults true; `enabled: false` keeps the object but skips it.

A playbook is planned only when its anchor phase is in the run's phase set (the
`--stage` filter) and its target resolves to at least one in-scope host (the
`--clusters` filter). A `follows` playbook waits for the anchor phase's core tasks
in scope; a `gates` playbook blocks every anchor-phase core task in scope and
itself lands after the previous phase. Custom playbooks flow through `apply`, `plan`,
and `diff --recorded` on the existing `--stage`/`--clusters` axes; there
is no dedicated CLI verb. Because a playbook is opaque, `diff --recorded` reports it
as `match` (declared inputs unchanged) or `drift` (changed, will re-run) from the
input hash only — it never observes node reality.

## Secret

`Secret` declares one named piece of sensitive material and how Bootwright
obtains it. It is the promoted, first-class form of the removed
`Environment.spec.secrets[]` entries: every `SecretRef` in the fleet resolves to
a `Secret` by `metadata.name`. A `Secret` never carries material bytes in desired
state, so it is safe to commit.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: proxy-credentials
spec:
  type: usernamePassword
  source:
    generated:
      username: proxy
```

Rules:

- `spec.type` is required and is one of `opaque`, `token`, `usernamePassword`,
  `dockerConfigJson`, `caBundle`, `tlsCertificate`, or `sshKeyPair`. There is no
  inference. The type fixes the material roles the secret carries, which source
  arms are legal, and the shape of any generated parameters.
- `spec.source` says how the material is obtained. Omitting it (or setting an
  empty block) selects `contextStore`; at most one of `contextStore`, `file`, or
  `generated` may be set.
  - `contextStore` keeps the material only in the per-context AES-256-GCM store,
    populated by `bootwright secret set`/`generate`. It carries no parameters.
  - `file` names operator-owned file(s), scoped by type: single-file types use
    `path`; `tlsCertificate` uses `cert`+`key`; `sshKeyPair` uses `privateKey`
    (+ optional `publicKey`). A file key the type does not consume is rejected.
    Paths resolve against the `Secret`'s own file, or are absolute or `~`-rooted.
    `Environment.spec.secretStorage.mode` governs whether file material is read
    in place (`source`) or copied into the context store (`context`).
  - `generated` has Bootwright mint the material and is legal only for `token`,
    `usernamePassword`, `tlsCertificate`, `caBundle`, and `sshKeyPair`. Its
    parameters are flat and scoped by type: `usernamePassword` takes `username`
    (default `admin`); `tlsCertificate`/`caBundle` take `commonName` (required),
    `dnsNames[]`, `ipAddresses[]`, `validityDays` (self-signed, default `3650`);
    `sshKeyPair`
    takes `keyType` (default `ed25519`, one of `ed25519`, `rsa`, `ecdsa-p256`,
    `ecdsa-p384`, `ecdsa-p521`) and `comment`; `token` takes `bytes`
    (default `32`). A parameter the type does not consume is rejected.
- `Secret` objects are the one-object-per-file carve-out (see
  [Kinds](#kinds)): a tree's `Secret`s are grouped into a single multi-document
  `secrets.yaml` beside `environment.yaml`, or into a fleet-global `secrets/`
  directory of multi-document files named for the group (for example
  `bmc-credentials.yaml`).

## Rendering Contract

- `install-config.yaml` is rendered from `ContainerCluster`, `Environment`,
  selected machines, machine `NetworkConfig` references, selected providers,
  endpoints, and platform render mode.
- `agent-config.yaml` hosts are rendered from `ContainerCluster.spec.nodes`,
  each referenced `Machine`, selected `NetworkConfig` templates, per-machine
  network overrides, and substrate MAC inventory.
- Machine boot variables are rendered from `Machine` substrate facts,
  `InfraProvider` capabilities, and cluster artifact access.
- Shared service variables are rendered from the machine-service graph built
  from `InfraComponent`, environment catalog selections, network DNS refs, and
  cluster endpoint sources.
- Storage tool inputs are rendered from `StorageCluster`, selected storage
  machines, placement resources, and storage exports.

## Validation Rules

These are the cross-kind rules; each kind's own validation rules live in its
section above.

- Unknown kinds and unknown fields are rejected at load time.
- A native input key that Bootwright relocates or renames — an
  `install-config.yaml`/`agent-config.yaml` key, a cephadm flag or spec key,
  or a kickstart directive whose authored home is elsewhere — produces a
  redirect diagnostic naming its owner (`field "<native>" is not authored
  here; <where>`), never a bare unknown-field error. The per-kind Native
  mapping tables under `docs/concepts/` enumerate the mapping.
- A field that keeps a native key name but shifts its value vocabulary (for
  example `provisioningNetwork`, authored lowercase and rendered
  `Disabled`/`Managed`/`Unmanaged`) states the native spellings in its
  `docs/concepts/` row and names them in its rejection message.
- Retired kinds and fields are not migrated.
- A default consumed by more than one pipeline stage is materialized by the
  normalize phase (for example, an omitted standard container endpoint
  `source.type` becomes `openshift`); validators and renderers read the
  normalized value instead of recomputing the default. A diagnostic on a
  normalize-injected reference the author never wrote (Environment-defaults
  copies, and the `ContainerCluster` pull-secret and node-SSH conventions) says
  the value was defaulted and how to override it.
- References must resolve to loaded resources selected by `Environment`, and
  object names are unique within a kind — see
  [References and Names](#references-and-names).
- Exactly one `Environment` must be loaded.
- Machines must declare `spec.os.provided`.
- Machines with `os.provided: false` must have `spec.substrate.providerRef`.
- Machines reached over SSH must resolve `spec.access.ssh.addressRef` to a
  declared `spec.addresses[]` entry (the implicit `fqdn` address counts) and set
  exactly one `spec.access.ssh.auth` arm — see the `Machine` access rules for
  the union and the `addressRef` default. Container-cluster node access is the
  exception because `ContainerCluster.spec.install.nodeSSH` owns the installed
  RHCOS identity.
- Provider network attachment refs must exist and match the provider arm used
  by the machine.
- A `Machine` is node-bound by at most one cluster across `ContainerCluster`
  `spec.nodes[].machineRef` and `StorageCluster`
  `spec.ceph.topology.nodes[].machineRef`, and by at most one host entry
  within that cluster.
- `ContainerCluster` and `StorageCluster` names share one cluster selection
  namespace: `--clusters` and the `Environment` cluster lists resolve bare
  names against both kinds, so a name is declared by at most one cluster root
  across the two kinds (in addition to the per-kind duplicate rules).
- The name `artifact-server` is reserved: no `ContainerCluster` or
  `StorageCluster` may use it, because `destroy --stage infra --clusters
  artifact-server` accepts that literal to remove only the generated artifact
  publication service, and a cluster of that name would make the destructive
  selection ambiguous.
- Container cluster endpoints must resolve to valid addresses or valid
  InfraComponent bind addresses. When the cluster selects a machine network, a
  resolved endpoint address — the direct `address` or the `infraComponent`
  source's bind address — must fall inside a selected `NetworkConfig`
  `machineNetwork[].cidr`; an out-of-network endpoint fails validation naming the
  slot and value.
- **Single-stack is the `v1alpha1` scope**: one `ContainerCluster` carries one IP
  address family. The effective networking of a cluster is the union of the
  `machineNetwork[].cidr` entries of the `NetworkConfig`s its nodes consume (plus
  any inline `spec.network.config.spec.machineNetwork[]`),
  `spec.networking.clusterNetwork[].cidr`, `spec.networking.serviceNetwork[]`,
  and the resolved `api`, `api-int`, and `ingress` endpoint addresses. A second
  address family anywhere in that set fails validation naming the cluster, both
  conflicting values with their families, and the scope. IPv6-only stays legal —
  the rule refuses **mixing**, not IPv6; `clusterNetwork` and `serviceNetwork`
  defaults already follow the machine-network family. Node install addresses are
  not collected separately because an `interfaceAddresses`-resolved install IP
  must already fall inside a selected machine network. The refusal exists
  because `endpoints.<slot>.address` is a single address where the native
  `apiVIPs`/`ingressVIPs` are lists precisely to carry one VIP per family, so a
  dual-stack fleet would otherwise render a silent single-stack install-config
  (ADR 0043).
- A machine's `interfaceAddresses`-resolved install IP must fall inside a
  `machineNetwork[].cidr` of its selected `NetworkConfig`; an address outside
  every machine network fails validation naming the `Machine`, the
  `interfaceAddresses` entry, and the resolved IP.
- A `Machine` Bootwright installs an OS on — `os.provided: false` with a
  `MachineInstallProfile` — must have an install network the kickstart can
  express. Anaconda takes exactly one `network` directive, so the expressible
  subset is a single primary interface of type `ethernet`, `vlan`, or `bond`
  carrying one IPv4 address or taking one by DHCP, plus an optional default
  gateway and DNS servers.
  Three postures fail validation, each naming the `Machine`, the interface, the
  reason, and `spec.os.provided: true` as the alternative that hands the
  install to the operator: the install interface carries neither an IPv4
  address nor IPv4 DHCP (`ipv4.dhcp: true` with `ipv4.enabled` not `false`) —
  a machine with no static address at all already renders the same
  `network --device=link --bootproto=dhcp`, so refusing the DHCP-plus-IPv6 one
  would contradict it; the primary is an interface type the directive cannot
  create (a bridge, a team, a VRF); or `access.ssh.addressRef` resolves to a
  literal IP that is not on the interface the directive brings up. A reference
  that resolves to the machine's implicit `fqdn` entry names no install-time
  IP — there is nothing to move onto an interface — so that arm does not apply;
  which interface answers the name is a DNS question `preflight` checks, and
  the `fqdn` record must resolve to the `access.ssh` address in any case. A
  machine that addresses a second
  interface is **not** refused for that alone — the install brings up the
  primary and `nmstatectl apply` adds the rest afterwards, which is how a
  storage node reaches its cluster network. The rule exists because the
  alternative is silent: the kickstart otherwise falls back to
  `network --device=link --bootproto=dhcp`, a posture nobody authored, and the
  machine is unreachable only after its disk is wiped. A machine with
  `os.provided: false` and no install profile is out of scope — it boots the
  agent image from the full `NetworkConfig`, where bridges and IPv6 are
  expressible. The subset is enumerated in `docs/advanced/managed-os.md`.
- A `Machine` whose install profile selects `installer.templateClone` is bound to
  the substrate that can clone. Four rules apply, each naming the machine, the
  profile and the remedy: the machine's provider must be `vsphere` (no other
  adapter clones a machine from a template today); its
  `machineProfiles[].template` must be set; an `installer.anaconda` profile on a
  machine whose profile sets `template` is refused; and the machine's effective
  network config must put a **static IPv4 address on an `ethernet` interface
  carrying the default route**. The addressing rule exists because the seed is
  the only thing that brings a clone onto the network before SSH answers — a
  bond or VLAN primary is applied afterwards by nmstate, which cannot run before
  Bootwright can log in, and a DHCP or IPv6-only primary leaves nothing to seed.
  The refusal offers `installer.anaconda` as the arm without that restriction.
- Bare-metal boot requires BMC details and artifact access suitable for the
  configured boot method.
- KubeVirt `hostClusterRef` dependencies must be acyclic. A cluster cannot
  host itself directly or indirectly.
- Secret references must resolve to a declared `Secret` object.

## CLI Contract

This contract binds the whole `bootwright` verb surface: the setup verbs
(`context`, `example init`, `secret`, `media`, `add-ons`), the inspect verbs
(`validate`, `preflight`, `plan`, `diff`, `status`, `render`), the lifecycle
verbs (`apply`, `destroy`), and the resource verbs (`machine`, `bastion
setup`, `cluster`, `container-cluster`, `storage-cluster`). The `secret`
verbs' material and encryption semantics are delegated to `security.md`
(Secret Ownership and Lifecycle); everything else is normative here.
Subsections: [Diagnostics and refusals](#diagnostics-and-refusals) ·
[Machine-readable output](#machine-readable-output) ·
[Global flags](#global-flags) ·
[Contexts, runs, and read-only posture](#contexts-runs-and-read-only-posture) ·
[Selection and stages](#selection-and-stages) · [Destroy](#destroy) ·
[Authorizations](#authorizations) ·
[Teardown safeguards, cleanup, and history](#teardown-safeguards-cleanup-and-history) ·
[Apply modes and drift](#apply-modes-and-drift) ·
[Validate, machine trust, add-ons, and diff](#validate-machine-trust-add-ons-and-diff).

- Human CLI output goes through `internal/cli/output` except JSON output, shell
  exports, Cobra help, prompts, and external process passthrough.
- Exit codes are contract: `0` success, `1` a run or load failure, `2` a usage
  error. `diff` adds `3` for out-of-sync selected state (see the `diff`
  bullets).
  The interactive passthrough verbs — `container-cluster oc`/`container-cluster
  kubectl` and `machine`/`cluster` `rsh`/`exec` — propagate the child process's
  exit status verbatim and are outside this contract.
- Cluster verbs split by how much they need to know about the cluster kind.
  `cluster` holds what reads the same for every cluster root — `list`, `info`,
  and the `rsh`/`exec` node shells — and its `--name` accepts a
  `ContainerCluster` or a `StorageCluster`. A verb that only means something for
  one kind lives under that kind's own command: `container-cluster` (`oc`,
  `kubectl`, `kubeconfig` — the Kubernetes admin-credential surface) and
  `storage-cluster`. Their `--name` accepts that kind only and refuses the other
  by name. A kind-specific verb is never added under `cluster`.
- `storage-cluster replace-arbiter --name <cluster>` moves a managed Ceph
  cluster's stretch tiebreaker onto another machine, running only the work that
  change needs. It is the one operation `apply` cannot express: `apply` converges
  the authored topology, but a live cluster already in stretch mode reaches a new
  arbiter only through `ceph mon set_new_tiebreaker`, and a re-apply neither
  issues it nor retires the mon it replaces.
  - **Desired state stays the source of truth.** The verb reconciles the live
    tiebreaker onto `spec.ceph.topology.stretch.tiebreaker`. Optional
    `--new-arbiter-machine <machine>` authors that intent first: it rewrites the
    context input so the tiebreaker node names the given `Machine` (snapshotting
    the prior input to `input-history/` through the one mutation component), then
    reconciles. Omitted, the verb reconciles the arbiter the input already
    declares.
  - **The candidate is authored, not discovered.** `--new-arbiter-machine` is
    refused unless the `Machine` declares the `ceph-arbiter` capability (which
    requires `ceph-node`) and is node-bound by no other `ContainerCluster` or
    `StorageCluster`.
  - **Order, and what a failure leaves behind.** The run prepares and installs
    the replacement machine and Ceph on it (the `apply --through deps` graph for
    the cluster), deploys its mon with the stretch CRUSH location, waits for that
    mon to be in the monmap *and* in quorum *and* located, only then moves the
    tiebreaker, and only then retires the replaced mon and its orchestrator host.
    Nothing is removed before the replacement is proved, so every failure ahead
    of the swap leaves the original arbiter holding the tiebreaker with quorum
    intact. Every step is idempotent and a re-run resumes; a run whose desired
    arbiter already answers as `tiebreaker_mon` is a reported no-op.
  - **Refusals.** A cluster that is not managed, declares no `spec.ceph`, or
    authors no stretch tiebreaker fails closed before the cluster is contacted.
    A cluster whose live monmap reports `stretch_mode: false`, or that answers no
    live read at all, fails closed rather than guess which mon to retire.
    A replacement arbiter sharing a failure domain with the data-site mons needs
    `--authorize same-site-arbiter`; declared mons outside quorum need
    `--authorize degraded-quorum`; a replaced arbiter host the run proves it
    cannot contact needs `--authorize unreachable-nodes`, which retires it with
    `ceph orch host rm --offline --force` and no host-local cleanup.
  - **The replaced machine keeps running.** Only its Ceph membership is removed;
    its OS and substrate are untouched, and tearing the machine down stays a
    separate `destroy` decision.
  - **What the run records.** A successful replacement refreshes the cluster's
    recorded desired and structural state exactly as a successful `apply` writes
    it. Otherwise the next plain `apply` would refuse on drift this verb itself
    authored, and the operator's only exits from a supported day-2 operation
    would be a destructive rebuild or a hand-edit of recorded state.

### Diagnostics and refusals

Every fail-closed refusal, on every verb, names: the object — and the field
path when a field is at fault; the consequence the refusal prevents, in the
kind's own vocabulary (a disk wipe, OSD data loss, an orphaned record); exactly
one way forward — the literal `bootwright …` invocation that proceeds
intentionally, carrying any required token and the run's own scope and stage
flags, or, when no Bootwright command can proceed, the sanctioned external
remedy and why (the fsid-scoped `cephadm rm-cluster --force --fsid <fsid>` and
"widen `--clusters`" are the two shapes of that clause); and, for
authorization refusals, the token spelled exactly as it is typed. This clause
states once what ADR 0007 (guidance-first refusals), ADR 0030 (a refusal names
the exact token that unblocks it), and ADR 0010 (failure summaries preserve
the exact remedy command through shortening) each record; the per-case bullets
below add only case-specific content.

### Machine-readable output

`--output json` payloads split into contract and non-contract. Contract —
automation may pin their shape, and changing one is a breaking change:
`validate` (the census under
[Validate, machine trust, add-ons, and diff](#validate-machine-trust-add-ons-and-diff))
the `--dry-run` plan reports of `apply`, `destroy`, and `plan`; and the
executed check results of `preflight`.
`diff --output json` joins the contract set when its key layout is specified;
until then it — like every remaining verb's JSON — is a rendering of the human
report and may change between releases. `diff`'s exit `3` means **not proven
in sync**: a live difference, drift, foreign ownership, a degraded probe, or a
never-applied selection — deliberately one code, so automation gates without
parsing the report; a consumer that must distinguish real drift from a probe
that could not run reads the report body, which separates classified
differences from degraded probes.

`preflight` answers in JSON without `--dry-run`, because the question it exists
to answer — is this fleet ready to apply? — is answered by running the checks,
not by planning them. Every target answers in one shape: `context`, `target`,
`ok`, a `checks[]` of `{name, target, status, detail, impact, fix}`, a
`summary` of `{total, failed}`, and the run `log` where one was written.
`status` is `ok`, `warn`, `info`, `skip`, or `fail`, and only `fail` counts
toward `summary.failed` and the exit code — a warning is disclosed without
failing a pipeline. Controller-side checks come first, and a failure among them
ends the run before any host is contacted; otherwise the Ansible preflight's
own outcome is the last entry. Requiring `--dry-run` before JSON binds the
**mutating** verbs only, where a plan is the one thing safe to emit without
acting; `preflight --dry-run --output json` still reports the plan it would
have run. Exit codes do not change with the output format, and machine-readable
output suppresses every prompt — the interactive host-key trust prompt included
— so a check that would have prompted reports its status instead of stalling a
pipeline.

### Global flags

`--context`, `--ssh-id-file`, and `--ssh-user` are persistent root
flags, accepted on every command. The rest are registered per command, on the
verbs that reach machines.

- `--context <name>` selects the context the invocation operates in, overriding
  the current-context selection for that invocation only. It must name an
  existing context, and it is the selector deciding which fleet `apply` and
  `destroy` mutate.
- `--ssh-id-file <path>` offers the named private key ahead of the
  declared credentials; the declared credentials remain the fallback. It is a
  per-invocation preference: it never enters desired state, the converge hash,
  or an install marker.
- `--ssh-user <name>` names the account only for machines whose resolved auth
  arm is `operatorIdentity` — the inventory `ansible_user`, the add-on step
  targets, and the connection identity of the Ceph node-access role. It never
  moves a login Bootwright created nor one a `Secret` names, never renames a
  cluster's orchestration account, and `apply`, `plan`, and `destroy`
  **refuse** when no machine in the run declares that arm rather than silently
  changing nothing. It is refused unless the value is a valid POSIX user name,
  and is likewise never recorded. On `machine rsh`/`exec` and
  `cluster rsh`/`exec` it keeps `ssh(1)` semantics and reaches any account: a
  value naming an identity Bootwright holds — the `Machine`'s own login, or the
  orchestration account of a cluster listing that machine — is opened with that
  identity's credential; any other value offers no stored credential, leaving
  the operator's own identities to authenticate. A name two clusters own with
  different credentials is refused, naming both (ADR 0033).
- Neither SSH flag reaches an ownership record. A record captures the
  **declared** `ansible_user` and `ansible_ssh_common_args`, so replaying it to
  reach a host that has left desired state cannot inherit one operator's account
  or key path from the run that wrote it.
- `--trust-on-first-use` (default **true**) governs the interactive host-key
  recording described below; `--trust-on-first-use=false` never records trust.
- `--verbose`/`-v` prints full Ansible task output **including values normally
  censored by `no_log`** — secrets, BMC/registry/RHSM/proxy credentials, tokens,
  generated Ceph keys — to the terminal and the run log. It is the redaction
  escape hatch specified in `security.md`.
- `--ask-become-pass` prompts for the Ansible become password (default: false
  when running as root, true otherwise).

### Contexts, runs, and read-only posture

- `context init --name <name> -f <dir>` (`--name` and `-f` required, exactly one directory) creates
  a context by copying the whole source directory tree into the context's input
  directory at `/var/lib/bootwright/contexts/<name>/input/`. The context is
  self-contained: every command reads the copy, so the context keeps working
  even if the source is later moved or deleted, and editing the source has no
  effect until the next `context update`. `init` fails if the context already
  exists; `--yes` drops the existing context entirely and recreates it from the
  source. The whole tree is copied (not only YAML) so `file:`-sourced secrets and
  SSH keys, resolved relative to the loaded YAML, remain available.
- `context update --name <name> -f <dir>` (`--name` and `-f` required, exactly one
  directory) replaces the named context's input directory with a fresh copy of the
  source and preserves all other context state (secrets, runs, rendered output,
  clusters, ownership, provider state). It does not change the current-context
  selection.
- `context delete --name <name>` requires `--purge` (contexts live in shared
  root state, so there is no partial delete) and fails closed while the context
  still owns ownership records or installed clusters. `--abandon-resources`
  authorizes deleting it anyway: the infrastructure keeps running, and the
  ownership records plus install-captured credentials — kubeconfigs, the
  kubeadmin password — are permanently lost. It is also what proceeds when
  ownership records cannot be read at all. `destroy --context <name>` first is
  the non-abandoning path. The flag is deliberately **not** spelled `--force`:
  that word would read as *destroy these resources*, while this flag means
  *walk away from them*, and one spelling for two opposite outcomes is a
  foot-gun. No Bootwright verb carries a `--force` flag anywhere — destructive
  risks are named one at a time with `--authorize`.
- An input directory that is missing, unreadable, or not a directory is a named
  failure at context resolution/readiness time that names the context and the
  input directory and points at `context update --name <name> -f` (or `context init
  --name <name> … --yes`) to repopulate it; there is no silent degradation.
- A mutating `apply` records the loaded input YAML files as a forensic output
  under the run's history directory (`runs/history/<run-id>/input/`); a
  mutating `destroy` records them under `runs/last-destroy-input/`. The
  snapshot is what was applied, written at the start of the mutating run;
  nothing reads it back, and plan/`--dry-run` never write it.
- A command that mutates the context's desired-state input — `context update`
  replacing the tree, `diff --adopt` editing objects, and
  `storage-cluster replace-arbiter --new-arbiter-machine` rebinding the stretch
  tiebreaker — snapshots the current input to `input-history/<seq>-<reason>/`
  before writing, under a bounded retention. Every such mutation goes through
  one component so the snapshot-then-write guarantee holds uniformly and the
  pre-change input stays recoverable. Recovery from a snapshot is manual — no
  verb reads one back; the procedure is documented with the context model in
  `docs/concepts/`.
- Read-only verbs (`status`, `diff`, `render`, `plan`, `apply --dry-run`,
  `validate`, help, and discovery) must not write runtime records
  (convergence-safety, install, ownership, ledger) or acquire a mutating run
  lease, and must not mutate provider, BMC, cluster, or storage state. `status`,
  `render`, `plan`, `validate`, and `diff --recorded` must not contact provider
  hosts, BMCs, or clusters at all; the default live `diff` reads live cluster
  state read-only (managed Ceph seed reads and a container `ClusterVersion`
  check) and still writes no runtime records. `diff --adopt` additionally writes
  desired-state input YAML — folding discovered live state back into the context
  input tree, snapshotting the prior input to history first — but writes no
  runtime records and mutates no provider, BMC, cluster, or storage state.
  `preflight` may run an Ansible preflight but only
  with read-only or check-mode operations that converge nothing. As the one
  exception to that read-only posture, `preflight` (and `apply` before its host
  check) may record SSH server-key trust, but only for a host with no existing
  trust record and only after an explicit interactive per-host fingerprint
  confirmation; never under `--dry-run`, JSON output, or non-interactive
  execution (`--yes`). A changed key is never accepted interactively — it keeps
  failing closed until `bootwright machine trust --replace <machines>` records
  it deliberately. Trust is recorded only when `--trust-on-first-use` (default
  true) is left on.
- The `media` verbs (`add`, `list`, `delete`) manage the root-owned ISO store
  that `MachineImage` `local-media:` keys resolve against (see
  [MachineImage](#machineimage)); ADR 0010 decides their flag conventions —
  root escalation, `--name` targeting, and the one `--yes` gate on replace and
  delete. `media delete` checks only the store: it does not cross-check loaded
  `MachineImage` objects, so a key a declared image still names is caught
  later, by the installer-media preflight check, which fails naming the missing
  source and the `media add --name <filename.iso>` remediation — the same
  check that catches a `MachineImage` naming a key never added.
- `secret encryption init|status` complete the `migrate`/`rotate` pair whose
  semantics `security.md` (Secret Lifecycle) owns: `init` initializes the
  current context's encrypted secret store, `status` reports the store's
  encryption state without reading material. `status` is read-only; the other
  three mutate only context-local secret state.

### Selection and stages

- Any command that narrows to cluster roots spells it `--clusters`; no command
  uses another spelling. It is accepted by `apply`, `plan`, `diff`, `destroy`
  (with or without `--stage`), the `preflight` scope subcommands, `render`, and
  `machine list`. `render installer` and `render storage` accept only the one
  cluster kind they render. `preflight all` is the deliberate exception: it is
  context-wide by design, and narrowing means running `preflight bastion`,
  `preflight infra --clusters <name>`, and `preflight clusters --clusters
  <name>` instead.
- `--clusters` accepts a comma-separated list
  of `ContainerCluster` and `StorageCluster` names; validation keeps the
  names unique across both kinds, so a name selects exactly one cluster
  root. `destroy --stage infra
  --clusters` additionally accepts the literal `artifact-server` to remove only
  the generated artifact publication service.
- An `apply --clusters` whose scope reaches the shared machine layer (the
  `fabric`/`machines` sub-phases, the `infra` family, or the full graph) fails
  closed when the narrowed run would re-render a shared machine service that
  degrades under the narrowing — its configuration would be rebuilt from the
  selected clusters alone and lose the unselected consumers. Services
  classified as self-contained are exempt; a service with no classification is
  treated as degrading, so a newly added one fails safe. The remedy is to widen
  `--clusters` to cover every consumer. The classification is part of this
  contract, keyed by the service slot:

  | class | slots | why |
  | --- | --- | --- |
  | self-contained (exempt) | the `artifactServer`, `proxy`, and `registry` `InfraComponent` types; the provider BMC service | its rendered configuration is a function of the service's own declaration alone, so a narrowed re-render cannot lose a consumer |
  | degrading (fails closed) | the `loadBalancer`, `nameResolution`, and `ntp` `InfraComponent` types | its rendered configuration is a function of the set of consuming clusters, so re-rendering from a narrowed selection would drop the unselected ones |

  A slot absent from the self-contained row degrades. The "why" column is the
  test: a new service slot classifies itself by which sentence is true of it.
- A scoped `destroy` fails closed the same way on shared provider-service
  conflicts. No `--authorize` token widens the selection there; the remedy is
  to widen or narrow `--clusters`.
- `--machines`, on `apply`/`plan`/`destroy`, is the per-machine alternative to
  `--clusters` and is mutually exclusive with it: a comma-separated list of
  `Machine` names. On `apply`/`plan` the scope is limited to the `fabric` and
  `machines` sub-phases (a `--stage`/`--through` outside that machine layer is
  rejected); on `destroy` it is limited to the machine-substrate teardown (a
  `--stage` other than `infra` is rejected). A named machine must have
  provisioning work — be a cluster node or host a shared service or provider —
  so teardown (or provisioning) of a standalone managed-OS machine that belongs
  to no cluster is fail-closed (Bootwright installs a managed OS only on
  cluster-member machines). Destroying a machine that is a node of an installed
  cluster fails closed unless `--authorize installed-cluster-node`, for both
  cluster kinds: an installed `ContainerCluster` (proved by its install record)
  and a provisioned managed `StorageCluster` (proved by its Bootwright-owned
  `storage-cluster` ownership record).
- `--mode`, on `apply`/`plan`, is the single-valued intent axis:
  `create` asserts a greenfield run and fails if any selected object already
  exists; `reconcile` (the default) creates what is missing, skips what matches,
  and fails closed on drift; `rebuild` authorizes Bootwright-owned destructive
  re-convergence of drifted owned objects and never adopts a foreign one. The
  value is stamped verbatim into the `bootwright_apply_mode` extra-var, so the
  CLI, plan composition, and the per-role Ansible gates share one vocabulary.
  An unrecognized value is a usage error (exit 2) listing the three.
  `destroy` has no `--mode`: teardown has one intent.
- `--stage` accepts two family names, `infra` and `clusters`, which decompose
  into five ordered sub-phases; `--stage`/`--through` accept a sub-phase name as
  well as a family name. The `infra` family is `fabric` then `machines`; the
  `clusters` family is `deps`, `base`, then `add-ons`:
  - `fabric` converges provider hosts (BMC services) and machine-bound shared
    services (proxy, registry, NTP, boot artifacts, DNS, load balancers).
  - `machines` makes machines exist with an OS: per-cluster substrate,
    instantiation, managed-OS install, machine networks, name resolution, VIPs.
  - `deps` installs per-cluster prerequisites: cephadm on storage nodes; build
    the `openshift-install` agent ISO.
  - `base` brings control planes up: bootstrap Ceph and apply OSDs; boot nodes
    and wait for `openshift-install`.
  - `add-ons` applies declarative cluster add-ons and attaches storage.

  `apply`, `plan`, and `diff` accept sub-phase `--stage` values.
  `destroy --stage` accepts only the two families (`infra`, `clusters`);
  sub-phases are apply-only and rejected on `destroy`.
- `--stage` and `--through`, on `apply`, `plan`, and `diff`, together select an
  inclusive range of ordered stages: `--stage <s>` is the first stage to run and
  `--through <t>` is the last, so combined they run `<s>` through `<t>` inclusive.
  `--stage <s>` alone runs exactly that stage (the range collapses to one);
  `--through <t>` alone runs from the first stage up to and including `<t>` (a
  cumulative prefix from the beginning); with neither, the full graph runs.
  `--through end` runs through the final stage regardless of how the sub-phase
  list grows. A `--stage` that orders after its `--through` is a usage error, and
  a range whose first stage is not the graph's first assumes the omitted earlier
  stages already applied and says so.
  A family name used as a range endpoint resolves to that family's boundary
  sub-phase: as `--stage` it starts at the family's first sub-phase, as
  `--through` it ends at the family's last. So `--through infra` equals
  `--through machines`, `--stage clusters` starts at `deps`, and
  `--through clusters` is the full graph.
### Destroy

- `destroy --stage infra` tears down infrastructure for the current context.
  It uses current desired state plus root-managed ownership records. Without
  `--clusters`, it must also remove all context-owned VMs that provider
  adapters can identify. With `--clusters`, it is limited to selected
  `ContainerCluster` or `StorageCluster` roots and must not run context-wide VM
  cleanup. Infra-component services are removed after machine infrastructure,
  because apply makes every machines-phase task depend on the fabric services
  and teardown is the inverse of build-up; the managed name-resolution and proxy
  components serve the addresses machine teardown itself connects through. The
  exception is the infra-component placement closure: a machine infrastructure
  step whose cluster hosts an infra component, or is a transitive KubeVirt host
  of one, runs after infra-component removal so that placement machine remains
  reachable for its own teardown. Managed machine
  disk cleanup is limited to provider-owned disks or declared
  Bootwright-managed devices; Bootwright must not wipe arbitrary visible disks.
  A Ceph OSD host whose devices are selected by a filter rather than by path
  (`data_devices.all`, or a `model`/`size`/`rotational` selection) declares no
  path for that wipe to target, so an `all`-devices host is additionally
  reclaimed by Ceph signature: a device is wiped only when it is a whole disk,
  is not the disk backing the root filesystem, carries no mountpoint anywhere in
  its tree, and bears a `ceph_bluestore` filesystem or an LVM physical volume in
  a `ceph-*` volume group. A host whose local disk scan fails must fail the
  teardown rather than guess. Without this the bluestore signatures outlive the
  cluster and the next `apply` observes every declared device as unavailable to
  ceph-volume.
- `destroy --stage clusters` removes cluster-stage runtime for selected or all
  `ContainerCluster` and `StorageCluster` names: OpenShift install runtime,
  add-on records, generated storage attachment records, managed storage
  cluster services/runtime, and the storage nodes' orchestration account and its
  sudoers drop-in — the inverse of the `clusterSSH.user` provisioning in ADR
  0019, restoring the node's prior root-login posture. A clusters-stage destroy
  therefore mutates machine OS state even though it retains the machine
  substrate. It does not destroy provider infrastructure. On a
  node that also carries a foreign (non-Bootwright) Ceph cluster, local Ceph
  state removal is scoped to the owned cluster's fsid directories; the shared
  `/etc/ceph`, `/var/lib/ceph`, and `/var/log/ceph` trees are removed
  wholesale only when no foreign fsid remains on the node.
- `destroy` with `--stage` omitted tears down the full lifecycle of its work set
  as the inverse of build-up (ADR 0023). Container-cluster installer and add-on
  runtime and storage-cluster runtime are graph roots and start together. A
  container cluster's machine infrastructure waits on that cluster's runtime
  teardown; a storage cluster's waits additionally on its machine registration
  and its storage node access. Machine infrastructure removes guests strictly
  before their KubeVirt hosts. Container-cluster records and captured
  credentials wait on the whole machine-infrastructure set, and the exclusively
  owned infra-component services and provider services go last.
  Steps that name a single cluster are planned per cluster, so one cluster's
  failure blocks only its own dependents rather than the whole fleet. Without
  `--clusters`, the work set is the whole context and the infra teardown also
  sweeps context-owned VM artifacts and orphan ownership records exactly as
  unscoped `destroy --stage infra` does. With `--clusters`, the work set is
  limited to the selected roots, machine infrastructure includes only their
  machines, and no context-wide sweep runs.
  Positively owned libvirt, vSphere, and KubeVirt machines and their owned disks
  are deleted; bare-metal hardware and its installed OS are retained while
  Bootwright-local install state is released. `destroy --stage clusters
  --clusters <names>` is the explicit retain-machine-substrate operation.
  Container-cluster teardown splits in two: the runtime half — installer
  runtime, add-on runtime, generated add-on secrets — is a graph root with no
  predecessors, while the records half — install record, connection record,
  captured `kubeconfig` and `kubeadmin-password` — has a hard dependency on the
  whole machine-infrastructure set, so failed virtual-machine deletion preserves
  credentials and ownership evidence for retry. When a selected KubeVirt host
  and its nested cluster are both destroyed, nested guest machines are removed
  through the still-live host before either the host cluster's own machine
  substrate or its kubeconfig and runtime are removed. Machine teardown orders
  KubeVirt tenants before their host clusters and rejects a host-reference cycle.
- Destroy must remove host packages only when ownership records prove
  Bootwright installed them and no remaining ownership record on that host
  still requires the package.
### Authorizations

- The destructive surface is exactly two orthogonal axes (ADR 0030): `--mode`
  states intent on `apply`/`plan`, and `--authorize <token>` states which named
  risk the operator accepts. Three verbs accept `--authorize`: `apply`/`plan`,
  `destroy`, and `storage-cluster replace-arbiter` (ADR 0042); `plan` aliases
  `apply` throughout the table. `--authorize` is repeatable and
  comma-separated. An unknown token is a usage error (exit 2) listing the
  tokens the verb accepts; so is a token the verb has no gate for at all — the
  `accepted by` column below is normative, and passing a token to a verb
  outside its cell is refused with the guidance that resolves it there rather
  than silently ignored, so an operator can never believe a gate was cleared. A
  token the verb accepts but this particular run never consumed is a non-fatal
  warning naming it. Under `--dry-run` no token is consumed and the human
  report says so for every one (JSON output carries the plan, not the
  warnings). Exactly these tokens exist. Every one but `all` unblocks exactly
  one refusal; `all` is the blanket authorization and unblocks exactly the
  tokens whose `accepted by` cell names the invoked verb, nothing wider
  (ADR 0040):

  | token | authorizes | accepted by |
  | --- | --- | --- |
  | `all` | every other token the invoked verb accepts, in one word. The set is exactly the tokens this table marks as reachable by the verb being run, so a token added by a later ADR is inside it from the day it lands and `apply --authorize all` never gains a destroy-only one. It grants no refusal that has no token of its own, and never answers a confirmation prompt | `apply`, `destroy`, `storage-cluster replace-arbiter` |
  | `data-loss` | any disk wipe or Ceph OSD zap, on `apply` and on `destroy` | `apply`, `destroy` |
  | `protected` | acting on state whose Environment sets `spec.safety.destroyProtection: protected`, or whose scope-filtered teardown covers a kind listed in `spec.safety.protectedKinds` (the granular gate — a protected kind absent from the scope needs no token) | `destroy` |
  | `installed-cluster-node` | `destroy --machines` naming a node of an installed `ContainerCluster` (its install record) or of a provisioned managed `StorageCluster` (its Bootwright-owned `storage-cluster` ownership record) | `destroy` |
  | `unowned-vms` | tearing down a libvirt domain, KubeVirt VirtualMachine, or vSphere VM that matches the Bootwright `<cluster>-<machine>` naming but carries a missing or mismatched ownership marker | `destroy` |
  | `unowned-networks` | removing the cluster's libvirt network or its KubeVirt DataVolumes when unowned — the wider blast radius, because an unowned network may still carry another context's VMs | `destroy` |
  | `unowned-devices` | wiping a declared OSD device that carries data signatures or LVM/dm-crypt holders while this node holds no Bootwright OSD ownership record for it — the orphan a destroyed or foreign Ceph install leaves behind, which no `ceph orch osd rm` can reach. On `apply` the gate runs only under `--reclaim-devices`; on `destroy` it is the declared-device wipe gate. It authorizes only the *unowned* refusal: the wipe itself still needs `data-loss`, and a mounted, in-use, or unprobeable device still fails closed | `apply`, `destroy` |
  | `foreign-daemons` | removing the cephadm daemons, systemd units and `/var/lib/ceph` state of a Ceph cluster this apply does not own from a storage node it enrolls, with the fsid-scoped `cephadm rm-cluster --force --fsid <fsid>` the refusal names. `apply` only: it is consumed where an apply converges a storage cluster, and it zaps no disk, so the other cluster's OSD data survives. The gate re-probes the node after the removal and refuses again if any of its units outlive it | `apply` |
  | `unreachable-nodes` | acting on a node the run *proves* it could not contact: on `destroy` skipping it and leaving the cluster partially destroyed, on `storage-cluster replace-arbiter` retiring the replaced arbiter offline with no host-local cleanup. Absence is matched positively from the probe evidence — no route, unreachable network, host down, a connection that timed out or was refused, a probe the timeout wrapper killed. Every other refusal fails closed and prints what the probes reported: a rejected identity (an unauthorized key, an untrusted host key, a refused sudo escalation), an address that does not resolve, an empty or unreadable diagnostic. None of those prove the node is gone, and no token skips them, because skipping a node that is in fact running leaves its Ceph daemons up and its OSD devices holding cluster data while the run reports the cluster destroyed | `destroy`, `storage-cluster replace-arbiter` |
  | `same-site-arbiter` | promoting a mon to tiebreaker while another mon already sits in its stretch failure domain, on `storage-cluster replace-arbiter` — Ceph's own `--yes-i-really-mean-it` path. It is the emergency fallback for a lost third site: an arbiter that shares a site with a voting mon cannot independently break a tie, so losing that site drops two votes at once and the survivor is left without quorum. The usual shape is an arbiter moved inside a data site, but the gate keys on the shared domain, not on the site's role | `storage-cluster replace-arbiter` |
  | `degraded-quorum` | moving a stretch tiebreaker while declared mons sit outside quorum, on `storage-cluster replace-arbiter`. `ceph mon set_new_tiebreaker` needs a quorum to commit, and swapping the arbiter during a site outage removes the vote holding the remaining quorum together | `storage-cluster replace-arbiter` |
  | `unreadable-records` | proceeding when ownership records under the context's ownership directory cannot be read, whose resources would silently be left standing | `destroy` |
  | `shared-infra` | a `--stage infra` teardown of a storage cluster still consumed by a `ContainerCluster` outside the selection, and infra components owned or referenced by another context (including the case where that cross-context check cannot be evaluated at all) | `destroy` |
  | `stale-input` | planning a teardown from the context's stored input when one or more documents no longer decode or validate against the running build, skipping exactly those documents; whatever they declared is absent from the work set and is reported as left standing | `destroy` |

  No token widens `--clusters`, relaxes the shared-provider-service scope
  conflict, or relaxes the KubeVirt tenant gate — `all` included, because it is
  defined as the union of the tokens above and not as a general override.
  `unowned-vms`/`unowned-networks` apply only where those refusals run (the
  machine layer); outside it they are reported as having had no effect. Neither
  relaxes the Ceph cluster or OSD-device ownership gates.

  `all` is resolved per verb at the point each gate is consulted, so it can
  never authorize a risk the verb has no gate for, and it is reported rather
  than assumed: a real run that used it prints which unnamed tokens it stood in
  for, and a run in which it answered nothing reports that it had no effect like
  any other token. A token named alongside `all` is credited to that name, not
  to `all`.

  Device data-safety splits in two, and only the ownership half is
  authorizable (ADR 0034). `unowned-devices` relaxes exactly one refusal — that
  a device carries signatures or LVM/dm-crypt holders this node has no
  Bootwright OSD ownership record for — because that refusal has no
  self-service remedy when the cluster that wrote them is gone. The physical
  half is not authorizable by any token: a mounted or in-use device, and a
  device whose probe failed for any reason other than "not present", still fail
  closed. A device that is simply absent is neither refused nor wiped — it is
  skipped, reported as a declaration that does not match the hardware, and left
  to fail at OSD readiness where the count is the diagnosis.
- Every verb that accepts `--authorize` publishes its own accepted tokens in
  its help: the flag usage names the tokens and nothing more, and an
  `Authorizations:` block in the command's long help lists one token per line
  with a gloss of at most 120 characters. A token's full consequence stays in
  the table above, in `docs/advanced/operations.md`, and in the refusal that
  names it — a flag usage that inlines every token's prose teaches the
  vocabulary by refusal instead of by reading.
- Every preview of an authorizing verb — `plan`, `apply --dry-run`,
  `destroy --dry-run`, and `storage-cluster replace-arbiter --dry-run` —
  reports the authorizations the real run will demand, under a **Required
  authorizations** heading, so a blast radius is learnable without triggering
  the refusal that teaches it. It prints one line per token the run would
  consult, decided by the same predicate the real gate reads, marks a token the
  invocation already supplied as satisfied, and, when a token's predicate
  cannot be settled without contacting a host the preview may not contact,
  names it as *may be required* with that as the reason rather than omitting
  it. JSON previews carry the same set as `requiredAuthorizations`, always
  present and empty rather than absent when no gate is reached. A preview that
  names no token is the positive statement that the real run needs none.
  `destroy --purge-history` discloses in the preview the state tree a real run
  would delete, for the same reason: the highest-consequence part of a run must
  be legible before it happens, not only after.
  This is the preview half of the one-predicate rule: the gate, the refusal,
  the prompt choice, and this block read one consequence predicate, so a
  preview cannot under-report what the run then refuses on.
- `--yes` has one meaning on both verbs: it answers the ordinary confirmation
  prompt and authorizes no named risk. On `destroy`, a teardown that destroys a
  managed storage cluster's OSD data requires `--authorize data-loss`; without
  it, a `--yes` run fails closed naming the token, and an interactive run gets
  the data-loss confirmation ("accept data loss", naming the storage clusters
  whose data goes) instead of the routine one. This is the same contract `apply`
  enforces.
  The gate follows the data, not the stage: it fires both where the cluster
  layer runs `cephadm rm-cluster --zap-osds` and where the machine layer
  (`--stage infra`, `--machines`, or a full teardown) deletes the provider-owned
  machines backing a selected managed `StorageCluster`'s OSDs, because deleting a
  libvirt/KubeVirt/vSphere VM deletes its disks. A storage cluster whose OSD
  hosts are bare-metal hardware Bootwright retains loses nothing in the machine
  layer — a bare-metal destroy never wipes in place — so it does not trip the
  gate, and the teardown preview states that retention rather than claiming a
  wipe. One predicate decides both the gate and the preview, so they cannot
  disagree.
- A storage teardown takes the LVM stack down before it wipes a device, on both
  wipe paths (the declared-device wipe and the `all`-devices filter reclaim).
  `wipefs` cannot clear a device whose volume group is still active — it fails
  with `probing initialization failed: Device or resource busy` — so the
  teardown deactivates the volume groups standing on the devices it is about to
  wipe, removes them, removes the physical volume labels, and only then wipes
  signatures and zaps the partition table. A volume group that refuses to
  deactivate still has an open logical volume, which on a storage node is a Ceph
  OSD daemon still running: the teardown fails closed naming that volume group,
  the `vgchange` exit status, and the fsid-scoped
  `cephadm rm-cluster --force --fsid <fsid>` remedy, and no token relaxes it.
  Before the deactivation the teardown releases the Ceph cluster the node is
  still running when the seed no longer names it — the leftover a previous
  teardown that skipped this node or failed partway through it leaves behind.
  The cluster identity is read from the `ceph.cluster_fsid` tag of the bluestore
  logical volumes standing on the devices this node's own OSD ownership marker
  records (or, on an `all`-devices host, on the Ceph-signed disks the filter
  reclaim selected), so the teardown releases only the cluster that owns the
  disks it is already authorized to wipe. It is the same fsid-scoped
  `cephadm rm-cluster --force --fsid <fsid>`, without `--zap-osds`: the teardown
  wipes the devices itself. A device wiped under `--authorize unowned-devices`
  alone vouches for no cluster identity and releases nothing (ADR 0039).
- `destroy --recover-ceph-ownership
  <StorageCluster>=<fsid>[,...]` is the narrow recovery path for a managed Ceph
  seed whose controller ownership record or
  `/etc/ceph/.bootwright-owned` marker is missing or mismatched.
  Every named cluster must be a selected, declared, managed `StorageCluster`;
  any existing controller owner record must agree with the declared cluster and
  seed; and the supplied UUID must exactly equal the fsid parsed from that
  seed's `/etc/ceph/ceph.conf`. The mapping is the operator's explicit
  ownership attestation for that exact cluster identity. Only after the remote
  match does Bootwright reconstruct a missing controller record and re-stamp
  the host marker, then re-read both through the normal ownership decision
  before `cephadm rm-cluster`. A reachable live `ceph fsid` response must agree
  with the on-disk fsid; it is a contradiction check, never the source of
  authorization. Contradictory controller or live evidence is never
  overwritten or acted on. The flag does not relax the OSD-device ownership or
  data-safety gates, is accepted only when the clusters stage runs, and
  authorizes no `--authorize` token and no `--yes`.
- `destroy --authorize unreachable-nodes` tolerates powered-off or unreachable
  nodes during teardown: it skips them — their devices are NOT wiped and their
  local state remains — and continues, leaving the cluster partially destroyed.
  It stands alone and needs no second flag. Absence is decided per node from the
  teardown identity probes and must be proven, not assumed: the token skips a
  node only when the probe evidence positively says it could not be contacted.
  Any other refusal — a rejected identity, an unresolvable address, an empty or
  unreadable diagnostic — fails the teardown closed and reports the probe exit
  status and message, instead of the power-off reading, because an unreadable
  refusal is not proof that a node is gone. The skipped-node warning and the
  controller-side partial-destroy result carry each skipped node's diagnostic,
  so a skip always says on what evidence, and the message states that a skipped
  node keeps serving the cluster the run reported destroyed. Storage teardown still fails closed
  when a cluster's Ceph seed host is unreachable, so ownership stays proven
  before any device wipe. A KubeVirt host-cluster API holding a recorded guest
  is not a skippable node: when it is unreachable Bootwright cannot prove the
  guest VM and DataVolumes absent, so destroy fails closed and retains their
  ownership and cluster runtime records even with the token.
### Teardown safeguards, cleanup, and history

- Destroying a KubeVirt host cluster while an installed nested cluster is left
  outside the selected work set fails before mutation. No `--authorize` token
  widens `--clusters`; the nested cluster must be selected in the same
  full-lifecycle destroy or destroyed first. The same gate binds a *scoped*
  `apply` that would rebuild a host cluster's machine substrate — a
  `--mode rebuild` rebuild or
  a release-authorized reinstall — and it keys on the run's selected work set,
  so a `--machines` selection is gated exactly like a `--clusters` one: the
  nested cluster must be inside the selection or already destroyed. An unscoped
  apply covers every cluster, so nothing is left stranded and the gate does not
  apply.
- Post-teardown record cleanup is part of the destroy contract, not advisory
  bookkeeping. When a destroy completes its remote work but cannot remove a
  convergence record, an install record, or write a substrate-release record, it
  reports each problem and exits non-zero: a surviving convergence record makes
  the next `apply` classify the destroyed resource as already converged and skip
  re-provisioning it, and a missing release record leaves its reinstall
  unauthorized. The refusal names the files to remove or the destroy to re-run.
- A destroy that tears down machine substrate writes a substrate-release
  record (`runs/substrate-release/`) — the positive authorization the next
  `apply` needs before it may reinstall the released substrate. The record is
  machine-granular: `destroy --machines` writes (or merges into) the cluster's
  record the names of the released machines, while a cluster-scoped destroy
  releases the whole cluster (no machine list); the managed-OS install probe
  honors the release only for covered machines. A release is consumed only for
  the machines an apply actually covered: a `--machines`-scoped apply shrinks
  the record to the still-released remainder and clears it when none remain.
  Consumption happens at the step that completes the rebuild, not merely at the
  step that re-creates the substrate: the managed-OS install (`managedMachineOS`,
  a machines-phase task) for a storage machine, and `wait-for install-complete`
  (`installWait`, a base-phase task) for a `ContainerCluster`. A `ContainerCluster`
  plans no `managedMachineOS` task, so `apply --stage infra` and
  `apply --through machines` re-create its machines without consuming its
  release — by design, because the cluster is not yet reinstalled — and it keeps
  re-arming the destroyed-substrate warning until an apply reaches the base
  phase. With `destroy --authorize unreachable-nodes`, a managed
  storage cluster receives the release only when its teardown completion report
  proves that no topology node was skipped. A partially destroyed storage
  cluster withholds it, as do infra-only, machine-scoped, and non-storage
  teardowns under this flag because they have no equivalent per-node completion
  proof. Those machines keep failing closed on apply until a destroy without
  skips finishes the teardown.
- A bare-metal destroy never wipes the OS disk in place; the disk is reclaimed
  by the release-authorized reinstall on the next `apply`. That apply is
  therefore the moment data is actually lost, and a release-authorized apply
  covering bare-metal managed-OS machines counts as a destructive action in
  apply's data-loss gate: an interactive run confirms it at the destructive
  prompt, and a non-interactive run (`--yes`) must pass `--authorize data-loss`.
- A destroy leaves no directory skeleton behind. Teardown removes state file by
  file, so the directories that held it are pruned after the run: any directory
  under `clusters/<name>/` the teardown emptied is removed, down to
  `clusters/<name>` itself when nothing of the component remains, and the
  per-cluster managed-OS install tree under `provider-state/os-install/<name>/`
  is removed once its last machine is torn down. Pruning removes only
  directories that hold nothing — a directory that still contains state is
  always kept, and pruning it away is never a substitute for removing the state
  itself.
- `destroy --purge-history` deletes retained per-component history once a
  cluster's or machine's teardown actually succeeds: the destroyed cluster's
  whole state tree under `clusters/<name>/` — installer working directory,
  install and connection records, kubeconfig and kubeadmin-password, and
  captured cluster secrets such as a Ceph `dashboard-password` — and its per-run
  task and flow logs under `runs/history/<run-id>/`. The state tree is purged
  for every cluster kind the run tore down, `StorageCluster` and
  `ContainerCluster` alike. Scope tracks `--clusters`/`--machines` exactly (the
  whole context on an unscoped destroy) and never a component outside it: a
  teardown that leaves the machine layer standing (`--stage clusters`) keeps
  that layer's provider state under
  `clusters/<name>/runtime/provider-state/` and purges only the cluster's own
  history. A cluster left partially destroyed by `--authorize unreachable-nodes`
  keeps both its history and its state tree so the operator can still diagnose
  and retry. A run whose tasks span both a purged and a still-live component
  keeps its ledger and shared run log — only the purged component's task
  directories and per-cluster log are removed — so history for the surviving
  component is never lost. It never removes the destroy-authorization
  substrate-release record (`runs/substrate-release/`, needed so a later `apply`
  can reinstall the released substrate) or unrelated context state (the
  ownership store, the input-history rollback snapshots). Rejected with the
  artifact-server literal (`--stage infra --clusters artifact-server`), which
  has no per-component history to remove.
### Apply modes and drift

- `apply` reconciles by default: it creates missing objects, skips objects
  whose recorded desired state matches the current desired state, converges
  drift that is reconcilable in place, and fails closed on structural
  (destructive-identity) drift or foreign ownership before any mutation.
  Reconcilable-in-place drift is drift that converges without a destructive
  rebuild: a StorageCluster OSD-device add; a storage sub-object edit confined
  to `ceph ... set`-reconcilable fields (pool replicas/quota/compression/crush,
  MDS placement, an added data pool) rather than an immutable identity (pool
  type / EC profile, CephFS metadata or default-data pool); a ContainerCluster
  edit confined to day-2-owned intent (node labels/taints, cluster add-ons)
  rather than install-config/agent-config identity; and any drift on a
  reconfigure-only kind (whose re-apply is idempotent and non-destructive).
  `apply --mode create` additionally refuses to proceed when any selected
  object already exists. `--mode` is single-valued, so intent cannot be
  self-contradictory.
  Every selected object is classified independently against the recorded last
  apply by the same classification that powers `diff --recorded`.
- A structural drift whose only difference is a managed Ceph cluster's
  `spec.ceph.topology.stretch.tiebreaker.node` routes the operator to
  `bootwright storage-cluster replace-arbiter --name <cluster>`, not to
  `--mode rebuild`. Moving one vote is a `ceph mon set_new_tiebreaker`
  operation the dedicated verb performs online; rebuilding the cluster to
  express it would destroy every OSD's data to change an arbiter. A drift set
  that reaches anything beyond the tiebreaker keeps the rebuild remedy, because
  the verb converges only the arbiter.
- A recorded desired hash covers only the desired state that reaches a host, so
  editing controller-side authorization policy never reads as drift.
  `Environment.spec.safety` (`destroyProtection`, `protectedKinds`) is consumed
  only by validation and by the destroy/rebuild gates, renders into nothing, and
  is therefore excluded from every recorded hash and from the structural
  projection. Turning protection on must not make the fleet look structurally
  drifted: that would refuse the next `apply`, and the protection gate would then
  refuse the `--mode rebuild` the refusal asked for, leaving `destroy` as the only
  exit from a change that mutated nothing.
- A recorded hash is also independent of the run's selection: a task's hash is
  computed from the unscoped desired state, so the same task hashes identically
  in a whole-fleet run and in a `--clusters`/`--machines` run. Otherwise a scoped
  run after a clean whole-fleet apply reports drift on a converged fleet and
  `diff --recorded` exits `3` on it.
- Before a managed-OS install mutates a host, the install probe classifies the
  live host by presence evidence, independent of recorded state. An
  SSH-unreachable host is absent — greenfield — and the install proceeds. A
  reachable host that authenticates against the pinned host key with a probe
  identity is present: the on-host install-marker ownership and match checks
  apply. A reachable host that rejects every probe identity — or one with no
  host-key trust configured to probe with — is unverifiable: Bootwright cannot
  read its marker to prove ownership, and unprovable is not absent, so the
  install fails closed in every apply mode, naming the
  remedies: restore key access for the machine's `access.ssh` identity;
  `bootwright machine trust --replace <machine>` when an authorized
  out-of-band rebuild changed the host key; `bootwright destroy --stage infra
  --machines <machine>` (or `--clusters <cluster>`) then re-apply to
  rebuild it; power off a truly unused host. Only coverage by the machine's
  substrate-release record authorizes reclaiming the host — destroy remains
  the consent moment. A changed host key fails the same way: the probe never
  re-accepts a changed key (the recorded key is replaced only after an install
  this run actually performed), so key rotation is always the deliberate
  `machine trust --replace` step.
- The probe needs no fallback identity. An installed machine's login is the
  `bootwright` service account for the whole of its life: no posture change and
  no cluster binding removes it, so the account the probe authenticates as is
  always the account the install created.
- `apply --mode create` is also enforced against live state at the managed-OS
  probe: a reachable host that already runs an OS — Bootwright-owned or not —
  fails closed under `--mode create` unless the machine's substrate release
  covers it, so recorded state alone can never make `--mode create` treat a
  live host as greenfield.
- The rename signature fails closed for both cluster kinds. When an apply would
  provision a new `ContainerCluster` or `StorageCluster` while a provisioned
  cluster of the same kind is no longer declared, apply refuses before any
  mutation and names the restore-then-`destroy --clusters` sequence. The
  "provisioned" evidence is the per-cluster install record for a
  `ContainerCluster` and the Bootwright-owned `storage-cluster` ownership record
  for a `StorageCluster`; a record owned by another manager is never rename
  evidence.
- `apply` is additive for every kind: it never removes, deprovisions, or
  unconfigures a live resource whose declaration was deleted from desired state.
  Deletion is not a plannable apply action; removal crosses the destroy
  authorization boundary (`destroy`) or is performed out of band. The storage
  additive-only rules above are the Ceph instance of this product-wide rule.
- Convergence-safety records are written per object when its apply task completes
  successfully; a failed or interrupted task writes no new record. So an object
  with no prior successful apply is `missing` — meaning "no completed apply is
  recorded", not "nothing exists on the substrate" (a half-provisioned VM whose
  install task failed still reports `missing`) — and a bare `apply` re-runs it,
  while an object with a prior successful record keeps classifying against that
  record.
- A bare `apply` resumes a partially-completed container install from its
  recorded phase: `creating-iso` (or no phase) restarts from the agent ISO;
  `iso-created` skips the ISO and resumes from node boot; `nodes-booted` and
  `waiting` skip the ISO and boot and resume the install wait; `complete` is a
  no-op. The `booting` phase fails closed — node-boot completion is uncertain, so
  Bootwright refuses to reboot without `--mode rebuild` (which recreates the agent
  ISO and reboots the nodes; no completed cluster is destroyed). An unrecognized
  phase also fails closed.
- A cluster whose availability cannot be probed is never treated as a rebuild
  candidate. When a `ContainerCluster` whose recorded install inputs match
  desired state cannot be probed at all — no reachable API, no usable
  kubeconfig, no `oc` — `apply --mode rebuild` fails closed before any
  mutation, naming each unprovable cluster, the probe error, and the remedies
  (restore reachability and re-run; exclude it with `--clusters`; or
  `destroy --clusters <name>` then re-apply to rebuild it deliberately). A
  probe that succeeds and reports `Available=False` is different evidence — the
  cluster answered — and still authorizes the `--mode rebuild` under
  the data-loss acknowledgment.
- `apply --mode rebuild` authorizes every destructive rebuild in the run's
  selected scope; it cannot be narrowed to an individual object. Narrow the
  destructive set with `--clusters` or `--machines` before using it.
  It may continue past Bootwright-owned
  unsafe mismatch checks that have an explicit override path: it bypasses the
  skip-if-already-complete install check, reinstalls a managed-OS machine (the
  substrate VM is undefined and its disks wiped, then rebuilt), and cleanly
  rebuilds a managed Ceph cluster (`cephadm rm-cluster --zap-osds`), allowed only
  when a Bootwright ownership marker proves the live cluster is the one Bootwright
  created — a foreign or co-resident cluster fails closed. It must not bypass
  active-run leases, validation, secret checks, or foreign-resource ownership
  failures.
- `--mode rebuild`'s consequence depends on the object kind, and this split gates
  destroy protection (below). For the reconfigure-only kinds — provider host
  services, infra-component services, node-config apply, per-host `virtctl`
  provisioning, cluster add-ons, machine RHSM registration, machine repository
  reconciliation, and provisioning-playbook re-runs — it is an idempotent, non-destructive
  re-apply that touches no data, OS, or VM. For every
  other kind — a managed-OS or substrate machine (reinstall; disks wiped) and a
  container or storage cluster (reinstall / `cephadm rm-cluster --zap-osds`) — it
  is a destructive rebuild. A kind is destructive unless it is on the
  reconfigure-only allowlist, so a newly added kind fails safe.
- When selected state contains `Environment.spec.safety.destroyProtection:
  protected`, `apply --mode rebuild` fails closed before any mutation when the
  drift it would resolve is a destructive rebuild (a machine or cluster), rather
  than rebuilding protected resources: that destruction must cross the destroy
  authorization boundary, so the operator runs `destroy --authorize protected`
  for the affected scope and then re-applies. Drift confined to reconfigure-only kinds is
  an in-place re-apply and does not trip the protection gate. `protectedKinds`
  narrows this to specific kinds: on an `allow`-default environment, a destructive
  `apply --mode rebuild` of a protected kind still fails closed the same way,
  while an unprotected kind rebuilds. Dry-run/plan still previews the override plan.
- `apply --mode rebuild` never reimages, in place, a machine that is a node
  of a managed-RHSM `StorageCluster`: an in-place reinstall would strand the
  Satellite consumer, and the reused host DMI UUID would then block
  re-registration. Such a rebuild fails closed naming the affected clusters and
  the remedy — `destroy --stage infra --clusters <cluster> --authorize protected`, then
  re-apply — because destroy unregisters the node from RHSM before wiping it.
  This refusal is independent of `destroyProtection` and `protectedKinds`: it
  fires on an unprotected environment too.
- Independent of `destroyProtection`, a destructive `apply --mode rebuild` (a
  managed-OS or substrate machine reinstall with disks wiped, or a container/Ceph
  cluster wipe-and-rebuild) requires an explicit data-loss acknowledgment even on an
  unprotected environment, so a mis-scoped `--mode rebuild` never silently destroys. An
  interactive run confirms it at a distinct data-loss prompt naming the objects; a
  non-interactive run must pass `--authorize data-loss`. `--yes` answers the routine apply
  confirmation but never authorizes data loss (mirroring how `--yes` never implies
  `--mode rebuild`). A reconfigure-only or reconcilable-in-place rebuild touches nothing
  destructive and reaches neither gate.
- `apply --reclaim-devices <paths>` takes a comma-separated list of block-device
  paths to WIPE in-band before a managed-Ceph apply — the recovery path for an
  owned OSD disk whose on-node marker was lost (for example by a managed-OS
  reinstall). It is irreversible and fail-closed: it wipes only a named device
  that is a declared OSD device of a Bootwright-owned cluster, is not mounted or a
  system disk, and is on a host whose OSD marker does not already record it; a
  path that matches no declared OSD device is rejected, and if no selected
  cluster is recorded Bootwright-owned nothing is reclaimed. The wipe runs in the
  `deps` phase, so the run scope must include it (`--stage deps`, `--through
  base`, or the full graph), and it is gated by the same data-loss acknowledgment
  (`--authorize data-loss` or an interactive confirm). On a protected environment
  (or with `StorageCluster` in `protectedKinds`) `--authorize data-loss` is also
  the explicit protected-data-loss authorization. That
  is a deliberate exception to the rule that a protected destructive rebuild
  crosses the `destroy` boundary: reclaim recovers named devices of a cluster
  that stays up, so routing it through `destroy` would demand a strictly larger
  destruction (the whole Ceph cluster) to recover a smaller one. The refusal
  names the flag that authorizes it.
### Validate, machine trust, add-ons, and diff

- `bootwright validate --output json` reports a declared-state census: one count
  key per authored kind, named as the lower-camel form of the kind's plural
  (`environments`, `entitlements`, `machines`, `machineImages`,
  `machineInstallProfiles`, `networkConfigs`, `infraProviders`,
  `infraComponents`, `containerClusters`, `storageClusters`,
  `storagePlacementPolicies`, `storagePools`, `storageFilesystems`,
  `storageObjectGateways`, `storageNFSExports`, `storageExports`,
  `clusterAddons`, `clusterAddonProfiles`, `clusterAddonBindings`,
  `customPlaybooks`, `secrets`). The census covers **every** kind in the Kinds
  table — no authored kind may be missing from it — and each key is always
  present, including at zero. The human `Objects` block of `validate`, `status`,
  and `render effective` reports the same census, omitting the zero counts.
- `bootwright machine trust` records SSH server-key trust for declared machines.
  It remains the scriptable pre-recording path for automation: non-interactive
  runs never record trust on first use, so pipelines record it with `machine
  trust` before `preflight`/`apply`, and only `machine trust --replace <machines>`
  may accept a changed key.
- `bootwright add-ons list|add|delete` manage the machine-local store of native
  add-ons vended from the binary's built-in catalog (see Native add-on catalog
  store). Registering an add-on only makes it available; binding it to a cluster
  with a `ClusterAddonBinding` is the separate, explicit step.
  - `add-ons list` prints each catalog entry with its available versions and
    default version, and — for a registered entry — the registered version and
    whether the on-disk content is locally modified. `--output json` emits the
    machine-readable report.
  - `add-ons add --name <name>[:<version>]` registers a catalog release into the
    store, replacing any prior version of the same add-on after a `--yes` or
    interactive confirm. `--version` is an alternative to the `<name>:<version>`
    shorthand (the two conflict) and defaults to the entry's default version.
  - `add-ons delete --name <name>[:<version>]` removes a registered add-on after
    a `--yes` or interactive confirm; an inline `<version>` is asserted against
    the registered one, not a selector. It refuses a directory the store did not
    register. Existing contexts keep their snapshotted copy.
- `bootwright diff` compares the selected desired state against the live clusters
  and prints the differences as a git-style diff (`-` desired, `+` real). For each
  managed Ceph `StorageCluster` it discovers live state read-only on the seed —
  hosts, services and their placements, OSD device layout (which physical devices
  back OSDs on each host, from `ceph osd metadata`), CRUSH rules, pools and
  replication, ceph config, mgr modules, and health — and diffs it field by
  field. The `osd-devices` facet compares only hosts that pin explicit plain
  `/dev/<name>` devices, where a real mismatch is genuine drift; a host that
  selects devices by filter/`all` (or a stable `/dev/disk/by-*` alias) is not
  drift — the filter intent is satisfied — but is reported as a reconstruction
  advisory naming the devices it currently consumes, so the operator can pin
  `osd.dataDevices.paths` for a byte-exact rebuild. For each
  `ContainerCluster` it runs a shallow reachability/`ClusterVersion` Available
  check (a container cluster carries no declared quantitative expectation to diff
  deeper against, so this is the meaningful floor). Live discovery is read-only
  and best-effort: an unreachable seed or a never-applied cluster degrades to a
  note, never a fatal error. Under the storage additive-only rule an object on the
  cluster but not declared is reported as `real-only` (an `--adopt` candidate), not
  a deletion. `diff` accepts `--stage`, `--clusters`, and `--output` like the other
  selection commands and rejects `--mode rebuild` (it neither mutates cluster state nor
  suppresses its report).
- In **both** modes, `diff` also reports `undeclared` ("Owned but no longer
  declared") resources: Bootwright ownership records — at `Machine`, cluster,
  `InfraProvider`, and `InfraComponent` granularity — that correlate to no
  object in the FULL desired state (never the `--clusters`-scoped subset), i.e.
  objects deleted from desired state without being destroyed. `undeclared` is
  report-only: it does not affect the exit code, which gates on selected-state
  sync only. Reclaim an orphan with `destroy` (see the removal lifecycle in
  `docs/advanced/operations.md`).
- `bootwright diff --recorded` skips all cluster contact and instead produces the
  fast offline desired-vs-recorded report for automation. It compares desired
  state against the convergence-safety evidence recorded by the last apply only;
  it does not probe live hosts, BMCs, or clusters, so a change made out of band
  after a matching apply (a wiped disk, an undefined VM, a deleted namespace) is
  invisible to `--recorded` but is exactly what the default live `diff` surfaces.
  - **Classification vocabulary.** It classifies each selected resource against
    the durable convergence-safety evidence recorded by the last apply: `missing`
    (no completed apply recorded), `match` (applied with the current desired
    state), `drift` (desired state changed since it was applied), or `foreign` (a
    non-Bootwright owner). The report-only `undeclared` list described above is
    reported alongside these four.
  - **Evidence granularity.** A root whose resources are all `missing` is reported
    as one absence; a present root reports only the resources that are not in sync.
    Drift is reported per apply task: each selected apply task is one reported
    resource. Classification granularity is per task, and how finely it isolates a
    single desired-state edit depends on the task's recorded hash scope. Two scopes
    classify at true object granularity: a managed `StorageCluster`'s pools,
    filesystems, object gateways, NFS exports, and exports are each classified
    against their own recorded apply (the report names the individual pool or
    export that drifted or is not yet applied — the same object granularity
    `apply` acts on), and each `infrastructure` host task hashes a host-scoped
    projection so an unrelated edit does not flip it. The ContainerCluster install
    tasks (agent ISO, node boot, install wait, per-machine provisioning) hash the
    cluster-filtered desired state for their full-drift signal but also carry a
    structural projection that excludes day-2-owned intent (cluster add-ons, node
    labels/taints), so an edit confined to that intent is reconcilable-in-place
    drift on the install object rather than a destructive reinstall; a change to
    install-config/agent-config identity still moves the structural hash and stays
    a rebuild. A present `StorageCluster` lists its out-of-sync sub-objects under
    the cluster root, while a never-applied cluster still collapses to one absence.
    The `infrastructure` root aggregates the provider and infra-component host
    tasks.
  - **Display classes.** Any change within a present cluster still reports every
    one of that cluster's install tasks as drift (the display class), not only the
    task that consumes the changed object. The `--recorded` text report summarizes
    a present root's out-of-sync resources by class (drifted, foreign-owned,
    not-yet-applied) so opposite remediations are never conflated. It is distinct
    from `status` (context setup checks, local readiness, and next-step spine),
    `preflight` (Ansible preflight), and `plan`/`apply --dry-run` (the intended
    task graph).
  - **Remediation hints.** The report names which resource drifted, not which
    field, and annotates each drifted resource with whether its drift reconciles
    in place (a day-2 reconfigure, a storage `set-*` edit, an OSD-device add) or
    needs a destructive rebuild, so a safe reconcile is never mistaken for a wipe;
    use `render effective` and diff, or `plan`, to see which object changed, and
    the reconcilable/structural split to tell a day-2 reconcile from a reinstall.
  - **Exit code 3.** On top of the product-wide exit codes above, `diff` exits
    `3` in both modes when the selected state is out of sync — a live
    difference, drift, foreign ownership, a degraded probe, or never-applied —
    so automation can gate without parsing the report.
- `bootwright diff --adopt` folds the discovered live state back into desired-state
  YAML so a subsequent `apply` reproduces the cluster as it actually runs. It is the
  one mutating form of `diff`, requires live discovery (rejected with `--recorded`),
  and writes only inside the context input tree — snapshotting the prior input to
  history first, so the pre-adopt state is recoverable. It edits declared objects in
  place (preserving comments) and creates a one-object file for a pool that exists
  only on the cluster; a difference it cannot safely fold in (a structural pool
  change, host/service/CRUSH topology) is reported as detected-but-not-adopted for a
  deliberate manual edit rather than silently dropped.
  Once the context has a recorded apply, the `status` next-step spine surfaces
  `diff` ahead of `plan`/`apply`. When the last run failed, the spine's
  next step is instead the exact scoped retry command (`apply` with the failed
  run's `--stage`/`--clusters` selection) alongside the failed tasks' log paths
  under `runs/history/<run-id>/`, so an interrupted apply resumes without an
  operator reaching for `--mode rebuild` or `destroy`.
  Every spine entry is either a command the CLI accepts verbatim — it resolves
  to a registered command path and carries only flags that command registers —
  or command-free prose. A spine that ends on a verb the CLI no longer has
  turns the moment of first success into an unknown-command error, so the
  entries are exercised against the registered command tree rather than
  written by hand.
- `example init --name <cluster>` writes a scaffolded desired-state tree,
  contacting no host and no context. `--kind` selects the shape and defaults to
  `container-cluster`: that kind writes the OpenShift core set for the
  substrate `--provider` names, and `storage-cluster` writes the smallest Ceph
  input — an `Environment`, a `Secret`, the storage-node `Machine`s, and a
  `StorageCluster` under `clusters/storage/<cluster>/`. An unknown `--kind` is
  a usage error naming the known kinds. `--provider` applies to
  `container-cluster` only: the storage scaffold's machines are `os.provided`,
  so it provisions no substrate, and passing `--provider` with it is a usage
  error naming the flag to drop rather than a silently ignored flag. Both kinds
  share one command shape — `--output-dir` defaults to the `--name` value, a
  non-empty output directory is refused unless `--yes` is passed, and the run
  reports what was written. Scaffolded output carries no secret material, only
  named `Secret` objects and placeholders, and every scaffolded tree validates
  as written: `bootwright validate -f <output-dir>` passes on fresh output for
  every `--kind`, and for every `--provider` the container kind accepts.
- Rendered effective state must not include secret bytes.
