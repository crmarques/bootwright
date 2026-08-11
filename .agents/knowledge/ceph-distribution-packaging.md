# Ceph distribution packaging: registries, repos, EPEL, and RHSM gotchas

**Constraint:** Distribution facts live in one Go table (`distributionDef` in
`internal/storage/cephprovider`) — capability flags (`requiresRHSM`,
`requiresRegistry`, `requiresLicense`), repo sets, vendor image namespace/stream
templates, and `ArtifactPolicy`. The policy states whether Ceph-runtime and
cephadm-ansible package pins, full RPM release coordinates, image bases, and
image pins are optional, required, or forbidden and selects optional native
preparation and parity modes. The Ansible role dispatches on rendered flags and
policy, never on distribution names (ADR 0002): a new
distribution is a table entry plus its api/v1alpha1 constant and validation,
not a new product-name `when:` clause.

**Constraint:** Bootwright carries NO Ceph release list and NO vendor support
matrix, and must never acquire one — not as a validation gate, not as a warning,
not as a `check` advisory, not as a doc table presented as authoritative. It does
not know or report which releases exist, which are current or ended, or which
RHEL versions a release supports. Those facts move on the vendor's schedule, so
any copy inside Bootwright is wrong the moment the vendor moves, and enforcing or
even flagging a stale copy turns a correct operator declaration into noise or a
failure. `ResolveRelease` (`internal/storage/cephprovider/release.go`) fails only
when a string cannot yield artifact coordinates at all; the leading dot-component
of a product version is the stream, and the stream plus the node's own
`ansible_distribution_major_version` build each package repo id and `.repo`
URL. Every managed release is required and carried verbatim — no compiled
default or normalization to a Bootwright-preferred value. `ResolvedRelease`
holds exactly `Value` and `Stream`;
adding a support-matrix field to it is the mistake this constraint exists to
prevent. Two earlier attempts got this wrong: first a closed release catalog that
hard-failed unknown releases, then an advisory catalog that warned on them. The
same rule governs `spec.ceph.packageVersion`,
`spec.ceph.cephadm.ansible.packageVersion`, and `spec.ceph.image.version`: all
are coordinates the operator reads from vendor sources, validated syntactically
and never cross-checked against `release` or a static catalog. Artifact
requirements and live proofs come from provider policy. The current IBM row
requires all three pins and selects the generic `cephadm-ansible-local`
preparation plus `ceph-version` parity adapters. A future rule changes policy
data or adds an isolated capability-named adapter, never a release-number
branch.

**Constraint:** `spec.ceph.image` is a `{base, version}` block, not a reference
string. `version` is the build (tag or `sha256:` digest), and the renderer joins
it to `base` with `@` for a digest and `:` for a tag. Subscription-backed
providers require an authored base: its trailing OS stream is an independent
vendor coordinate, so Bootwright must not compile one into future combinations.
Validation still welds that base to the provider's vendor namespace and product
stream. `oss` alone defaults the stable repository `quay.io/ceph/ceph`; it
derives `v<x.y.z>` only from an exact release version. A base without a version
is valid whenever provider policy makes image pinning optional.

**Constraint:** `spec.ceph.packageVersion` renders exact artifacts for
`cephadm` and the provider's native-preparation runtime names (`ceph-common` in
the current subscription-backed rows).
`spec.ceph.cephadm.ansible.packageVersion` independently renders the
`cephadm-ansible` artifact. Each artifact carries a bare name, composed package
spec, and desired-state path; the generic install, repository probe, post-native
proof, and failure all consume that same list. Bare names remain the
ownership-record keys and the static destroy list contains `cephadm`,
`ceph-common`, and `cephadm-ansible`: keying a record on a versioned string
orphans it forever. Exact installs use `ansible.builtin.dnf` with downgrades
disabled, then `rpm -q` emits version, version-release, epoch-version, and
epoch-version-release forms; one must equal the declaration both before and
after native preparation. This closes DNF's possible no-op against a newer
installed package and any native-artifact change. IBM supports upgrades, not
in-place software downgrades, so a lower declaration is refused and must be
handled as a new cluster or through the vendor procedure. Package facts are
gathered before the first exact transaction because ownership computes
`preexisting` from them; otherwise destroy could remove an operator package.
Both package coordinates are reconcile-only desired state and are excluded
from storage and managed-OS structural hashes.

**Constraint:** Package-mode daemon RPMs and cephadm container daemons cannot
share a storage node safely. The fixed conflict set is `ceph-mds`, `ceph-mgr`,
`ceph-mon`, `ceph-osd`, `ceph-radosgw`, and `rbd-mirror`: these packages can own
the ports, systemd units, or udev behavior the containers require. The shared
`check_storage_preflight/tasks/package_mode_daemons.yml` gate gathers live
`package_facts` on every selected node, fails closed if the inventory is not a
mapping, and runs from standalone storage preflight, before the storage-node
orchestration account mutates the host, and again at the head of the cephadm
apply role before hostname, repository, package, rebuild, or bootstrap mutation.
It reports the installed subset and the exact external `dnf remove` command;
Bootwright never removes a package it did not install. Apply-time retry text
consumes `bootwright_mutating_invocation`, while standalone preflight stays
command-free and asks for the same preflight to be re-run.

**Constraint:** `sos` is part of every managed Ceph provider's prerequisite
set. Vendor readiness checks expect it and an incident must not depend on a new
package transaction before support evidence can be collected. It reaches the
node through `Provider.PrerequisitePackages` and the ownership-record apply
path; `bootwright_ceph_default_prerequisite_packages` is the Ansible fallback,
and `bootwright_ceph_managed_packages` is the matching destroy release list.
All three must retain the bare package name. Package ownership keeps a
pre-existing copy and removes only a Bootwright-installed copy with no remaining
owner.

**Constraint:** The only storage-node OS check is `ansible_os_family == 'RedHat'`
(mirrored by a `family: rhel` validation error). That is a Bootwright capability
statement — the subscription provider implements RHEL-family package sources only
— not a vendor compatibility claim. No OS *version* is examined anywhere, in Go or
in Ansible. The rendered `runtimeOS` var carries `family` and nothing else.

**Constraint:** A FIPS-enabled storage cluster has both an install-time and a
live-host gate. Validation requires FIPS in every managed node install profile;
the rendered cluster `fips` and per-host `fipsRequired` facts additionally make
standalone storage preflight, storage-node access apply, and cephadm apply read
`/proc/sys/crypto/fips_enabled` on every selected topology host before their
first mutation. The kernel flag must be exactly `1`, including on an
`os.provided: true` arbiter. That proves current kernel mode only: it neither
enables FIPS nor proves that an externally installed host or vendor image has a
particular certification provenance. IBM's compatibility matrix directs
customers to their IBM representative for access to FIPS-enabled builds; no
Bootwright gate proves that the pulled image is that entitled artifact. Likewise,
Bootwright's Data Foundation export wiring is not a support certification: the
exact consumer/external-Ceph combination must be checked in Red Hat's
authenticated ODF Supportability and Interoperability Checker, and public
component-version parity is insufficient.

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

**Symptom:** A cluster with `spec.ceph.image.version` pinned runs its daemons
from the vendor's floating tag anyway — `ceph orch` pulls
`cp.icr.io/cp/ibm-ceph/ceph-9-rhel9:latest` — and re-applying never corrects it.

**Root cause:** `--image` is an argument to `cephadm bootstrap`, and bootstrap is
gated on `/etc/ceph/ceph.conf` being absent, so it runs once in a cluster's life.
mgr/cephadm resolves every `CEPH_IMAGE_TYPES` daemon (mon, mgr, osd, mds, rgw,
nfs — ganesha included) from the `container_image` config option, while
`mgr/cephadm/container_image_base` is read only by `ceph orch upgrade`; pinning
the base therefore binds no daemon. A cluster bootstrapped before the pin was
authored — or by an earlier run that bootstrapped and then failed, leaving no
converge record — keeps the ceph package's compiled-in `container_image`
default, which on a vendor build is that vendor's repository at a floating tag.

**Fix:** `bootstrap_steps/container_image_base.yml` reads and sets
`global container_image` on every apply, in the same read-then-set shape as the
base pin and in the same step (5 of 20), which is before the first
`ceph orch apply` at step 8. It is skipped when no version is pinned: an unset
`image.version` must never clear a value an out-of-band `ceph orch upgrade`
wrote. Two alternatives are wrong and must not replace it — seeding
`container_image` into the bootstrap ceph.conf (cephadm's own
`prepare_bootstrap_config` overwrites that key from `--image`), and declaring it
in `spec.ceph.config[global]` (those operations run in the topology phase, after
the service specs, so daemons deployed by the same run still use the old value;
validation rejects the key for that reason). Setting the option binds the
daemons cephadm creates or redeploys afterwards; daemons already running move
only on `ceph orch upgrade start --image <ref>`, which stays out of band.

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

**Symptom:** Installing community `ceph-common` on a FIPS-enabled RHEL node
fails a transaction test because CentOS Stream `openssl-fips-provider`
conflicts with RHEL `openssl-fips-provider-so`.

**Root cause:** Upstream community `ceph-common` RPMs are built against the
moving CentOS Stream ABI. A current Squid RPM requires
`libcrypto.so.3(OPENSSL_3.4.0)`, which RHEL 9.5 does not provide. Enabling
unrestricted CentOS Stream BaseOS/AppStream/CRB repositories on RHEL lets DNF
attempt a cross-distribution replacement of OpenSSL and other core packages;
repository filters cannot repair the package's direct ABI requirement.

**Fix:** Bootwright installs only `cephadm` on storage hosts and runs `ceph` and
`radosgw-admin` commands through `cephadm shell`. Community setup does not add
CentOS Stream repositories or install host `ceph-common`. This keeps the Ceph
client ABI inside the selected Ceph daemon image and leaves the host RHEL
package set under its own repositories. On rerun, the role removes its obsolete
`/etc/yum.repos.d/bootwright-ceph-dependencies.repo` before a DNF transaction,
but only after its three Bootwright section IDs prove ownership; an unrecognized
file fails closed for operator review.

**Constraint:** The renderer emits exactly one of `community.version` (a full
x.y.z, resolving `rpm-<x.y.z>/` + `cephadm add-repo --version`, and deriving
`quay.io/ceph/ceph:vX.Y.Z` when `image` is unset) or `community.release` (a
codename, resolving `rpm-<name>/` + `--release`; the image floats). Both paths
are fully name-agnostic — any parseable codename or `x.y.z` resolves, and no
upstream series is treated as active or ended.
`community.checksum` is normalized to bare sha256 hex and re-prefixed into
`get_url`'s checksum so the fetched-and-executed cephadm bootstrap binary is
content-verified.

**Constraint:** The licensed IBM packages refuse to operate until the
acceptance marker `/usr/share/ibm-storage-ceph-license/accept` exists, so it is
written in the repository stage — before the install stage pulls `cephadm`/
daemon tooling from the vendor sources
(`https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/ibm-storage-ceph-<N>-rhel-9.repo`).
IBM Storage Ceph 9.9.1.0 also pauses `cephadm bootstrap` for interactive license
acceptance unless `--automatically-accept-license` is present. That acceptance
enables the `call_home_agent` mgr module by default, so the StorageCluster must
declare `ibm.callHome: enabled|disabled`; apply acknowledges the enabled state
or denies it to turn the module off.

**Symptom:** `ceph orch deny call-home-enabled` prints `Call home agent module
disabled and health warning for call home enablement cleared`, but the client
does not exit; the 300-second wrapper terminates it with rc 124. A separate
read-only form occurs at `Inspect IBM Call Home manager module state`: `ceph mgr
module ls --format json` emits a complete JSON document with
`call_home_agent` in `enabled_modules`, but still reaches the 120-second bound
and exits rc 124.

**Root cause:** IBM's `deny_call_home()` removes the
`CALL_HOME_ENABLED_AUTOMATICALLY` warning, sends a nested `mgr module disable`
monitor command for `call_home_agent`, clears
`mgr/cephadm/call_home_needs_acceptance`, and then returns that exact text. The
Ceph CLI prints and flushes its command response before shutting down its
cluster handle, so a complete response and a later client timeout can coexist.
The module-list JSON includes the full option metadata for every disabled
module even though this reconcile needs one enabled-module bit; the observed
process remained live after all 34,039 response bytes arrived. Source ordering
makes post-response CLI/RADOS or cephadm cleanup the leading inference, but the
precise stage and trigger are not vendor-confirmed and may affect any CLI
command. The denial's nested module unload is an additional risky path that the
direct-disable ordering avoids, not a proven common cause. Both timeouts remain
unknown outcomes under Bootwright's fail-closed command contract and stdout
must never override rc 124 or 137. This symptom is not evidence of an HTTP(S)
proxy failure: the Ceph messenger monitor request is not HTTP, and the complete
monitor response proves the request itself succeeded.

**Fix:** Do not pre-read the module state. Run the bounded, monitor-targeted
`ceph mgr module enable call_home_agent` or `ceph mgr module disable
call_home_agent` directly for the explicit intent; Ceph returns rc 0 and an
`already enabled` or `already disabled` response when the state is already
converged. For disabled intent, run that direct disable before the bounded
`ceph orch deny call-home-enabled` so IBM still clears the warning and persists
the denial without unloading a module inside the orchestrator request. Declared
`mgrModules[]` use the same native idempotency instead of repeating the detailed
module-list probe later in the operation plan. Read-only discovery retains its
direct-host module catalog because diff needs both explicitly enabled and
always-on modules; the failing reconcile used the separate `cephadm shell`
path. The generated native apply script keeps the same direct operations and
order. Live diagnosis requires all three conditions: `call_home_agent` absent
from the MgrMap,
`mgr/cephadm/call_home_needs_acceptance` false, and
`CALL_HOME_ENABLED_AUTOMATICALLY` absent from `ceph health detail`; none is a
reason to reinterpret a timed-out run as successful.

**Constraint:** IBM Storage Ceph 9 uses IBM's four-component V.R.M.F product
version. The trailing R.M.F retains the prior Ceph release, modification, and
fix meaning, so IBM's equivalent of release 9.1 is `9.9.1.0`; the daemon
container tag remains independently versioned as `v9.9.1-<build>`. Only the
leading component is interpreted (as the stream), so vendor product-version
syntax accepts any number of dot-separated numeric components and the extra
components need no Bootwright knowledge.

**Constraint:** An entitlement registry override changes credentials and trust
and acts as a mirror root, not permission to select an arbitrary repository. A
subscription-backed cluster always authors `spec.ceph.image.base` at the
selected registry root plus the distribution's namespace and stream:
`rhceph/rhceph-<stream>-rhel` for Red Hat or `ibm-ceph/ceph-<stream>-rhel` for
IBM. The same prefix check applies on the default vendor registry, preventing a
Red Hat cluster from accepting an IBM or unrelated image merely because it is
below the authenticated registry. It is a cross-vendor guard only — the trailing
build base is never compared against the release, because which base a vendor
compiled a release against is a vendor fact Bootwright does not track. There is
no compiled `rhel9` or other OS-stream default; the authored base is also the
value asserted as `container_image_base`.
Registry addresses are scheme-less `host[:port][/path]`; community package
mirrors remain HTTPS URLs because cephadm refuses insecure repository
transport.

**Constraint:** RHSM is converged declaratively: `redhat_subscription`
(machines-phase `machine_registration_rhsm` role) registers only when needed
(re-registering with `subscription-manager register --force` mints a fresh
consumer record and rotates
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

**Constraint:** Bootstrap, live health, and destroy use `cephadm shell` for all
Ceph client commands; no phase assumes a host `ceph` or `radosgw-admin` binary.
Subscription-backed native preparation nevertheless installs `ceph-common`
because the provider artifact requires it. That package belongs to the
preflight/runtime artifact set and is not a license to bypass `cephadm shell` in
later tasks.

**Constraint:** IBM's release table has two similarly named package columns.
`StorageCluster.spec.ceph.packageVersion` takes the **IBM Storage Package
Version**; `StorageCluster.spec.ceph.cephadm.ansible.packageVersion` takes the
**Cephadm Ansible Package Version**. IBM's public Ceph RPM metadata confirms
that `cephadm` and `ceph-common` share the Ceph build NVR (with epoch `2`),
whereas the separate `cephadm-ansible` RPM carries epoch `1`. For IBM Storage
Ceph 9.9.0.3, the July 2026 CVE row maps to
`packageVersion: 20.1.0-221.el9cp`,
`cephadm.ansible.packageVersion: 5.0.2-1.el9cp`, and image tag
`v9.0-20201`. The public table abbreviates the Ansible package to `5.0.2-1`,
but exact DNF installation and verification use the RPM's full version-release
`5.0.2-1.el9cp`. Swapping the two fields would request nonexistent package
builds. IBM repository primary metadata recorded SHA-256
`af0e7053c765de766abb3c81991cfa31af425c1cfca31985320b4623afe75a45`
for `cephadm-20.1.0-221.el9cp.noarch.rpm` and
`40541d9e3e10b8df3a0fa7e4be27f6a872f3a369dbbac1b794b7fee6c39876f8`
for the matching
`ceph-common-20.1.0-221.el9cp.x86_64.rpm` when inspected.

**Constraint:** Exact-artifact enforcement consumes rendered provider policy,
not `distribution == ibm`. The current IBM row requires `packageVersion`
(including the RPM release component),
`cephadm.ansible.packageVersion` (also full version-release), `image.base`, and
`image.version`. It selects native preparation mode `cephadm-ansible-local` and
parity mode `ceph-version`. The package artifact list drives subscription
repoquery, exact DNF installation, bare-name ownership, and exact post-native
proof for `cephadm`, `ceph-common`, and `cephadm-ansible` on every node. After
runtime readiness and before cluster work, the generic dispatcher includes
`phases/native_parity/ceph-version.yml`; its installed `cephadm version` and
`ceph --version` image probes must succeed, and their native version tokens must
equal the epoch-free package declaration and each other. This proves nothing
about the authored product `release`, RHEL compatibility, or certification. A
future provider selects another policy or isolated parity adapter without a
release-number branch.

**Constraint:** Bootwright executes the installed provider preflight; it does
not reproduce that playbook's checks. The inspected
`cephadm-ansible-5.0.2-1.el9cp.noarch.rpm` from IBM's public Ceph 9 RHEL 9
repository has SHA-256
`e3174a043c3273177d73ee73881870ad1ae3a37347d844ef083d28a3dc0bd6be`;
that checksum matched the repository's primary metadata. The RPM reports epoch
`1`, source RPM `cephadm-ansible-5.0.2-1.el9cp.src.rpm`, and a Red Hat signature
under key ID `1e1850467eaa5b6e`; the inspection did not import that key and
therefore did not establish trust. In
`roles/ceph_defaults/defaults/main.yml:12-24` it selects the moving IBM stream,
defaults `upgrade_ceph_packages: false`, and names unversioned `cephadm` and
`ceph-common`; `roles/ceph_defaults/tasks/main.yml:57-70` maps the legacy
variables into those defaults. In the extracted `cephadm-preflight.yml:193-214`,
package state is `latest` only when that upgrade switch is true and otherwise
`present`; the earlier `ceph-common` install is also unversioned.
Consequently `present` retains an already-installed package but, on an absent
host, resolves the best candidate visible in the configured repository.

The `cephadm-ansible-local` adapter closes that moving-source gap without
replacing the provider artifact. Bootwright converges the repository first,
preinstalls authored exact artifacts with downgrades disabled, then runs the
RPM-owned playbook on every node with `-i localhost, -c local --limit
localhost`. Both origin variables are `custom`,
`ceph_custom_repositories: []` is explicitly defined, and the audited 5.0.2
validation/setup accepts that empty list and adds no repository. Both upgrade
spellings are false, `packages_to_uninstall` is empty, Ceph package lists carry
the rendered exact requests, client lists are empty, and prerequisites retain
the provider's rendered list. The playbook therefore uses Bootwright's already
selected vendor or subscription repository and `present` skips every exact
package already installed. A post-run RPM proof detects any change. Red Hat's
optional pins use the same adapter but install an omitted coordinate by bare
name with `present`; frozen repository content is then the reproducibility
boundary.

The nested Ansible run uses the installed `ansible.cfg` for the artifact's role
and module paths, but redirects its log, local/remote temp paths, and fact cache
into a root-only transient directory and uses memory fact caching. Its report is
first written in that fresh directory, proved current, then copied into the
Bootwright ownership directory; destroy removes the preserved copy. This avoids
stale `localhost` facts, operator-home residue, and an old report satisfying a
new run. Provider report findings remain advisory because the stock playbook
records several failures without necessarily failing the play; Bootwright's
package-conflict, FIPS, runtime, network, and ownership gates remain
authoritative.

`cephadm-ansible-local` is a capability/ABI adapter, not a promise that every
future RPM understands the audited variable surface. An artifact that renames
or removes origin, repository, upgrade, uninstall, package-list, client-list, or
report variables needs a new capability-named adapter selected by provider
policy. It must never be selected through a concrete product release branch;
Ansible silently accepts unknown extra vars, so syntax-check alone cannot prove
that an incompatible artifact consumed the safety controls.

## Vendor references

- Upstream release lifecycle: <https://docs.ceph.com/en/latest/releases/>
- Red Hat Ceph Storage 9 compatibility guide: <https://docs.redhat.com/en/documentation/red_hat_ceph_storage/9/pdf/compatibility_guide/Red_Hat_Ceph_Storage-9-Compatibility_Guide-en-US.pdf>
- IBM Storage Ceph release/package mapping: <https://www.ibm.com/support/pages/what-are-red-hat-and-ibm-storage-ceph-releases-and-corresponding-ceph-package-versions>
- IBM Storage Ceph 9 public RPM repository: <https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel9/x86_64/>
- Inspected IBM-hosted cephadm 20.1.0-221 RPM: <https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel9/x86_64/cephadm-20.1.0-221.el9cp.noarch.rpm>
- Inspected IBM-hosted ceph-common 20.1.0-221 RPM: <https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel9/x86_64/ceph-common-20.1.0-221.el9cp.x86_64.rpm>
- Inspected IBM-hosted cephadm-ansible 5.0.2 RPM: <https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel9/x86_64/cephadm-ansible-5.0.2-1.el9cp.noarch.rpm>
- IBM 9.9.0 compatibility matrix and FIPS-build access note: <https://www.ibm.com/docs/en/storage-ceph/9.9.0?topic=compatibility-matrix>
- IBM connected preflight procedure: <https://www.ibm.com/docs/en/storage-ceph/9.9.0?topic=installation-running-preflight-playbook>
- IBM disconnected/custom-repository preflight procedure: <https://www.ibm.com/docs/en/storage-ceph/9.9.0?topic=installation-running-preflight-playbook-disconnected>
- IBM upgrade FAQ (software downgrades unsupported): <https://www.ibm.com/docs/en/storage-ceph/9.9.0?topic=faqs-upgrade-questions>
- Red Hat ODF Supportability and Interoperability Checker (authentication required): <https://access.redhat.com/labs/odfsi/>
- IBM Storage Ceph 9.9.1.0 versioning scheme: <https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=whats-new-in-storage-ceph-991>
- IBM Storage Ceph 9.9.1.0 node prerequisites: <https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=installation-registering-storage-ceph-nodes>
- IBM 9.9.1.0 bootstrap license and Call Home behavior: <https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=installation-bootstrapping-new-storage-cluster>
- IBM 9.9.1.0 Call Home disable procedure: <https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=interface-disabling-call-home>
- IBM Storage Ceph 9 source RPM: <https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel9/source/ceph-20.2.1-324.el9cp.src.rpm>
