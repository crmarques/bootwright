# Ceph distribution packaging: registries, repos, EPEL, and RHSM gotchas

**Constraint:** Distribution facts live in one Go table (`distributionDef` in
`internal/storage/cephprovider`) — capability flags (`requiresRHSM`,
`requiresRegistry`, `requiresLicense`), repo sets, and image templates. The
Ansible role dispatches on the rendered flags (a `community` block vs
`requiresRHSM`), never on distribution names (ADR 0002): a new distribution is
a table entry plus its api/v1alpha1 constant and validation, not a new `when:`
clause or task file.

**Symptom:** On an IBM Storage Ceph cluster, `ceph orch upgrade ls` and the
dashboard "Upgrade" page fail with 401 against `registry.redhat.io`.

**Root cause:** cephadm resolves upgrade targets from
`mgr/cephadm/container_image_base`, NOT the running daemon image, and its
compiled-in default base is the Red Hat Ceph Storage image even on IBM Storage
Ceph — but an IBM cluster is entitled only to `cp.icr.io`.

**Fix:** The apply pins the base to the distribution's own repository
(`cp.icr.io/cp/ibm-ceph/ceph-<N>-rhel9`,
`registry.redhat.io/rhceph/rhceph-<N>-rhel9`, or `quay.io/ceph/ceph`). An
explicit `spec.ceph.image` wins with its tag/digest stripped (a registry
`host:port` is preserved). Read-then-set keeps it idempotent and re-asserts on
every apply; it runs on the seed only and is skipped when no base is known (an
oss release-name image that floats).

**Constraint:** `cephadm bootstrap --registry-json` seeds the registry login
once, but day-2 daemon pulls authenticate from the **mgr cephadm registry
store**, not from a node-level podman login. Rotated credentials make every
new daemon pull 401 — masked on nodes that still hold a podman login. The
apply re-pushes the resolved credentials to the mgr store every run
(idempotent: same credentials are a store no-op).

**Constraint:** `cephadm add-repo` unconditionally enables EPEL via
`dnf install -y epel-release` and has no flag to skip it; an unregistered
managed RHEL node has no enabled repo providing that package. Bootwright
pre-installs `epel-release` from Fedora's canonical RPM
(`bootwright_ceph_community_epel_release_url`, overridable to an internal
mirror) with `disable_gpg_check: true` — epel-release is the bootstrap package
that itself ships the EPEL signing key, so no key is yet trusted.

**Constraint:** Community `ceph-common` depsolves against distribution base
repos that `cephadm add-repo` never configures: `librabbitmq`, `librdkafka`,
`python3-prettytable` (AppStream) and `libbabeltrace` (CodeReady Builder). An
unregistered RHEL node exposes none, so depsolve fails. Bootwright adds the
CentOS Stream BaseOS/AppStream/CRB repositories with the CentOS Official
signing key fingerprint-pinned (`99DB70FAE1D7CE227FB6488205B555B38483C65D`).
The release track is `<EL major>-stream` (e.g. `9-stream`), NOT dnf's
`$releasever`, which on RHEL resolves to a point release with no matching
CentOS Stream path. Override the mirror for disconnected sites, or set
`spec.ceph.community.enableDependencyRepos: false` where BaseOS/AppStream/CRB
are entitled another way.

**Constraint:** The renderer emits exactly one of `community.version` (a full
x.y.z, resolving `rpm-<x.y.z>/` + `cephadm add-repo --version`, and deriving
`quay.io/ceph/ceph:vX.Y.Z` when `image` is unset) or `community.release` (a
codename, resolving `rpm-<name>/` + `--release`; the image floats).
`community.checksum` is normalized to bare sha256 hex and re-prefixed into
`get_url`'s checksum so the fetched-and-executed cephadm bootstrap binary is
content-verified.

**Constraint:** The licensed IBM packages refuse to operate until the
acceptance marker `/usr/share/ibm-storage-ceph-license/accept` exists, so it is
written in the repository stage — before the install stage pulls `cephadm`/
`ceph` from the vendor repo
(`https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/ibm-storage-ceph-<N>-rhel-9.repo`).

**Constraint:** RHSM is converged declaratively: `redhat_subscription`
(machines-phase `machine_registration_rhsm` role) registers only when needed
(re-registering with `--force` mints a fresh consumer record and rotates
entitlement certs every run), and the storage role's `rhsm_repository` with
`purge: true` enables exactly the named repos and disables the rest, with
honest change reporting — skipped under `rhsm.management: external` so
operator-enabled repo sets are never purged. Proxy and TLS-inspection CA
handling for the RHSM repos (rhsm.conf `[server]` proxy, `[rhsm]
repo_ca_cert`, the three distinct CA anchor files) is covered in
[rhsm-proxy-and-repo-ca.md](rhsm-proxy-and-repo-ca.md).

**Constraint:** Subscription-backed repos embed
`{{ ansible_distribution_major_version }}`, and ansible-core finalizes that
template the moment `bootwright_ceph_provider` is `set_fact`'d — so OS facts
must be gathered first (context phase, before the repository phase) or the
set_fact fails with `ansible_distribution_major_version is undefined`.

**Constraint:** The bootstrap (storage) stage runs with `skip_prereqs` and is
the actual consumer of the ceph CLI, so it ensures `cephCommonPackage`
(default `ceph-common`) itself rather than trusting the prereq stage's
possibly older tooling set.
