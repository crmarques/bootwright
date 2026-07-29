# Security

Bootwright desired state is safe to commit. It references secrets by name and
path, but never carries secret bytes.

## Secret Ownership

A `Secret` object declares every secret source used by the loaded state — each
with a `spec.type` (what the material is) and an optional `spec.source` (how it
is obtained). Omitting `spec.source` selects context-local material written
through the encrypted context secret store under the current context secrets
directory. `source.file` points at operator-owned local material, and
`source.generated` describes material Bootwright can create under the encrypted
context store. `Environment.spec.secretStorage.mode` defaults to `source`;
`context` requires `bootwright secret generate` before workflows read encrypted
context-local copies of file-sourced entries.

The context secret store preserves the SecretRef/name UX and logical material
paths (`<name>`, `<name>.key`, `<name>.pub`) but stores AES-256-GCM envelopes
instead of plaintext bytes. The initial key provider is `root-owned-file`:
Bootwright creates a hidden keyring under `secrets/.bootwright/` on the first
context-local secret write, stores 32-byte keys in `keys/<key-id>.key`, and
requires root-owned non-symlink regular files/directories with modes `0600`
for files and `0700` for directories. Envelopes bind authenticated data to
context name, secret name, material role, algorithm, key provider, and key ID.
Normal reads must never fall back to plaintext; only
`bootwright secret encryption migrate --yes` may consume existing plaintext
context secret files and replace them with encrypted envelopes.

That keyring is co-located with the material it protects, so root on the
controller host owns both and is inside the trust boundary: it can decrypt
every envelope. Encryption at rest defends against offline media, backups, and
non-root readers on the controller — not against a root reader.

Sensitive material includes pull secrets, SSH private keys, TLS private keys,
BMC credentials, vCenter credentials, proxy credentials, mirror credentials,
CA bundles, tokens, and kubeconfigs. These values must stay outside versioned
desired state.

Machine SSH follows the same boundary. A machine Bootwright installs carries a
`bootwright` service account whose key is named once for the fleet by
`Environment.spec.machineAccess.keyRef`; that key opens every such machine, so
it may not double as a `StorageCluster`'s `cephadm.clusterSSH.keyRef`, which
`cephadm bootstrap` copies into the mon config-key store. Durable SSH
connection details for a machine Bootwright did **not** install live on
`Machine.spec.access.ssh`. Its `auth` union names material by reference —
`privateKeyRef` and `passwordRef` resolve to `Secret` objects, never to bytes in
desired state — and `sudoPasswordRef` and `knownHostsRef` likewise; when
`knownHostsRef` is omitted, Bootwright records server keys under
context-managed SSH trust state.

Three surfaces deliberately hold no reference and are per-operator ambient
authority: the `auth.operatorIdentity` arm and the `--ssh-preferred-id-key`
and `--ssh-user` per-invocation flags. `auth.operatorIdentity` authenticates as
the operator running Bootwright, using that operator's own agent and default
identities: no key material enters the context, and the effective credential is
whatever that operator already holds. The
`--ssh-preferred-id-key` flag likewise names a controller-local private key
offered ahead of the declared credentials, with the declared credentials as
fallback; it is refused unless the file is a regular file with no group or other
permissions, and it is never recorded in desired state, the converge hash, or
an install marker. `--ssh-user` names the account for `operatorIdentity`
machines only — never a login Bootwright created or one a `Secret` names — and
is refused both when the value is not a valid POSIX user name and, on the
converging commands, when no machine in the run declares that arm; it is
likewise never recorded. Neither flag reaches
an ownership record: records carry the declared connection facts, so a replayed
record cannot inherit one operator's account or key path. All three are ambient
by construction and must be described as such — they trade reproducibility for
the ability to reach a machine the operator already administers. Non-local
durable SSH uses strict checking against explicit or context-managed
known-hosts material.
Trust is recorded by `bootwright machine trust`, on first use during an
interactive `preflight`/`apply`, or through OpenSSH's prompt during interactive
direct SSH. First-use recording is allowed only for a host with no existing
trust record and only after an explicit interactive per-host confirmation of
the displayed key fingerprint. It never happens under `--dry-run`, JSON output,
or non-interactive execution, and a changed server key is never accepted
interactively: it fails closed pending out-of-band verification. Machine-owned
endpoints are then recorded deliberately with `bootwright machine trust
--replace`; container-node replacement follows the Direct SSH rules below.

KubeVirt child-cluster profiles also follow the same boundary. `hostClusterRef`
resolves to a generated kubeconfig already stored under Bootwright cluster
secrets state, and `kubeconfigRef` resolves through a `Secret` object
for external virtualization clusters. Desired state records only reference
names, never kubeconfig bytes.

Data Foundation external-cluster details render with placeholders for Ceph
client secrets. Generated Ceph keys are created or read during apply and must
not be committed.

### Captured Secrets

An install captures credentials the cluster itself mints: `kubeconfig` and
`kubeadmin-password` for a `ContainerCluster`, `dashboard-password` for a
managed Ceph `StorageCluster`. They live in a per-cluster store at
`clusters/<cluster>/secrets/` with its own keyring, under the same envelope
rules as context-local material. Plaintext is accepted only at the post-capture
conversion boundary, where apply encrypts what the installer wrote; a
programmatic consumer gets a bounded `0600` scratch copy under a per-run
runtime directory. Destroy deletes captured material through the store.

### Secret Lifecycle

Every secret moves through the same stages, and each stage has one owner:

- **Authored** — a `Secret` object names the material; desired state carries
  the name, never the bytes.
- **At rest** — an AES-256-GCM envelope in the context store or a per-cluster
  store, readable only through that store's keyring.
- **In flight** — a per-run or per-task runtime directory with `0700`
  directories and `0600` files, removed after execution.
- **Captured** — the per-cluster store above.
- **Revealed** — `secret show`, `cluster info --secrets`, and
  `cluster kubeconfig` decrypt to memory or stdout; `cluster oc` and
  `cluster kubectl` materialize a bounded caller-owned `0600` temporary
  kubeconfig and remove it when the child exits.
- **Rotated** — `secret encryption rotate` re-encrypts every material in a
  store under a new key and drops the keys no envelope still references.
- **Destroyed** — `secret delete` removes context-local material; `destroy`
  removes captured material through the per-cluster store.

## Vendor Outbound Telemetry

Bootwright must not leave vendor outbound telemetry enabled through an implicit
default; the choice is authored.

IBM Storage Ceph license acceptance enables Call Home by default. An IBM
`StorageCluster` must therefore declare `spec.ceph.ibm.callHome` as `enabled`
or `disabled`; apply reconciles that choice after bootstrap.

## Node Login Identity and Privilege

A machine has two login identities, owned by two different objects, and they
must not be conflated (ADR 0019, ADR 0027).

A `Machine` a managed Ceph `StorageCluster` installs authors no access block:
it carries the `bootwright` account and the fleet key like every other
installed machine, and the cluster provisions `clusterSSH.user` on top of it
day-2. A topology node the cluster does not install authors its own
`access.ssh` and is reconciled, not created.

The **install-window identity** — the account Bootwright authenticates as to
install, probe, and take ownership of the machine — is the product constant
`bootwright` on every machine Bootwright installs. It is load-bearing beyond
connectivity: it feeds the managed-OS install-marker hash and it is the identity
the pre-install readiness probe authenticates as. Making it a constant rather
than an authored field removes a class of failure: no edit can repoint it, so no
edit can make an installed fleet read as not-installed. A machine that stops
answering as that account still fails the ownership probe closed rather than
being reinstalled. On a `spec.os.provided: true` machine there is no
install-window identity at all; `spec.access.ssh` describes a login the operator
prepared.

`StorageCluster.spec.ceph.cephadm.clusterSSH.user` is the **post-install
identity** — the account cephadm orchestrates every host as
(`cephadm --ssh-user`, reconciled day-2 by `ceph cephadm set-user`) and the
account Bootwright itself connects as, but only once that node's `Machine`
revokes root login — while root is kept it stays the install-window identity,
because a flow that does not provision the account first (destroy, a scoped
apply) must not connect as an account that may not exist yet. It is
cluster-scoped because cephadm holds exactly one such value per cluster. It
defaults to `cephadm` on a managed `StorageCluster` and to `root` on an
external one.

The two identities are layered, not merged. On a node Bootwright installs the
substrate creates `bootwright` and the cluster provisions `clusterSSH.user` on
top of it day-2; the node never accepts a root login at any point in its life.
On a `spec.os.provided: true` node the operator's own login is what Bootwright
connects with, and the cluster provisions its orchestration account through it.
`clusterSSH.user` resolving to `root` is refused when any topology node is one
Bootwright installs — such a node has no root login for cephadm to orchestrate
through — and likewise when a provided node's `access.ssh.user` is non-root.

`Machine.spec.access.rootLogin` is the machine's OS posture: `keep` (default)
or `revoke`. `revoke` writes `/etc/ssh/sshd_config.d/01-bootwright-access.conf`
with `PermitRootLogin no`, validated by `sshd -t` before the reload, and is
reversible — returning the field to `keep` removes the drop-in and
re-authorizes root. It is accepted only on a machine a managed Ceph
`StorageCluster` lists as a topology node under a non-root orchestration
account; revoking a machine no such cluster claims would leave no account able
to reach it and is refused. It is also refused on a machine Bootwright installs,
whose kickstart writes `PermitRootLogin no` unconditionally — there is no root
login there to keep or revoke.

The managed OS install ships no password of any kind. The `bootwright` account
is locked unless `MachineInstallProfile.spec.customizations.ssh.initialPassword`
names a `usernamePassword` `Secret`, whose value becomes that account's console
password only. A profile must never carry a built-in or derived default here:
a shared password compiled into the product is a fleet-wide credential no
operator can rotate. The `NOPASSWD` sudoers grant for `bootwright`, which also
carries `Defaults:bootwright !requiretty`, is written unconditionally and is
not configurable: Bootwright escalates with `become` throughout, so a machine
whose service account cannot escalate is a machine it cannot use. On a machine
Bootwright installs that exemption is why the identity it later borrows needs
no terminal; on a machine the operator owns nothing guarantees it, which is
what the differential probe below decides.

For a non-root orchestration account Bootwright provisions that account on
every topology node with a locked password, no `wheel` membership, the cluster
identity's public key (`clusterSSH.keyRef`) in its `authorized_keys`, and a
per-user sudoers drop-in at `/etc/sudoers.d/60-bootwright-<user>` containing
exactly `Defaults:<user> !requiretty` and `<user> ALL=(ALL) NOPASSWD: ALL`.

That drop-in is necessary but not sufficient, so Bootwright proves the grant
rather than assuming it. It is evaluated only if the node reads `/etc/sudoers`
and its `@includedir`, and only if nothing later overrides it: sudo applies
`Defaults` in parse order and the last one wins whether it is generic or
per-user, so any later-sorting file in the same directory overrides it, and a
`Defaults requiretty` placed after `@includedir` in `/etc/sudoers` cannot be
overridden by any drop-in. When an LDAP or SSSD `cn=defaults` carries
`ignore_local_sudoers`, sudo skips `/etc/sudoers` and every drop-in outright;
the grant must then come from the directory, and Bootwright fails closed rather
than proceeding on a file the node never reads.

The privilege grant is unrestricted by necessity and the security claim is
correspondingly narrow. cephadm's manager `sudo`-wraps every remote command it
issues when its SSH user is not `root`, and cephadm's own helper writes the
same `NOPASSWD: ALL` rule, so a command-scoped policy cannot orchestrate a
cluster. A keyed account with passwordless sudo is only marginally different in
reachable privilege from keyed root SSH. What the posture buys is precise: no
standing root SSH on the node, a named auditable principal in the audit and
sudo logs instead of anonymous `root`, key-only authentication, and a
credential that can be revoked without touching the root account. It is not
privilege separation and must not be described as such.

Neither SSH credential rotates today, and neither may be advertised as
rotatable. `Environment.spec.machineAccess.keyRef` is authorized exactly once,
by the kickstart, and no later task rewrites `authorized_keys`: replacing the
`Secret`'s bytes strands every installed machine and the ownership probe then
fails closed, and renaming the `Secret` reads as structural drift because the
install-marker hash carries the basename of the rendered public-key path. For
`cephadm.clusterSSH.keyRef` the node side re-authorizes a new public key, but
nothing reconciles the private half in the mon config-key store — apply issues
`ceph cephadm set-user` and never `set-priv-key`. Both are install-time
credentials; rotating either is an out-of-band operation.

`clusterSSH.keyRef` is **required** whenever the orchestration account is not
`root`, which is the managed default, and must resolve to a declared
`sshKeyPair` `Secret`. `cephadm bootstrap --ssh-private-key` persists it into
the Ceph mon config-key store, where the cluster's manager can read it, so what
that key opens bounds the blast radius of a compromised manager. It may
therefore name neither `Environment.spec.machineAccess.keyRef`, which opens the
`bootwright` account on **every** machine in the fleet, nor a `Secret` a
`Machine` authors as its own `access.ssh.auth.privateKeyRef`, which opens
machines outside the cluster. The key that store holds must open only that
cluster's own orchestration accounts.

Revocation is ordered verify-before-revoke: the orchestration account must be
proved to answer `sudo -n true` on every topology node before
`PermitRootLogin no` is written on any of them, and re-proved after the `sshd`
reload. That proof is made on a terminal-less SSH channel, deliberately: it is
the exact channel cephadm's manager issues its `sudo`-wrapped commands over, so
a node whose policy reaches the account only from an interactive session cannot
be orchestrated and must fail. A node whose account does not answer stops the
run with root still reachable.

A pseudo-terminal is therefore asymmetric by rule. It is permitted for the
identity Bootwright **borrows** — the machine's `access.ssh.user`, whose ability
to run one privileged command is probed differentially, without a terminal first
and once more with one, before any mutation — and forbidden for
the identity Bootwright **creates** and hands to cephadm, whose `sudo -n true`
acceptance test must never gain one. Bootwright writes sudo policy only for the
account it owns and never relaxes tty policy for the operator's account.

Node access state is recorded on the machine in an access marker, specified
under § Generated Artifacts with every other path Bootwright writes.

## Installer Trust

Cluster install trust is rendered only from explicit references:

- OpenShift pull secrets come from `ContainerCluster.spec.install.pullSecretRef`
  or the normalized environment default.
- Node SSH authorization comes from
  `ContainerCluster.spec.install.nodeSSH.keyPairRef` or `.publicKeyRef`, or the
  normalized environment default.
- OKD clusters may omit a Red Hat pull secret unless a private release or
  mirror requires credentials.
- Mirror credentials and trust bundles come from
  `Environment.spec.registries.mirror`.
- Fleet-wide installer trust comes from `Environment.spec.installTrust`.
- Cluster-scoped installer trust comes from
  `ContainerCluster.spec.install.additionalTrustBundleRefs`.
- API and ingress serving certificate material comes from
  `ContainerCluster.spec.install.servingCertificates`.

Disconnected installs are cluster-scoped through
`ContainerCluster.spec.install.mode`. They require mirror trust material and
either an external mirror URL or a managed registry component.

`cluster rsh` and `cluster exec` use the private half of the selected
`ContainerCluster.spec.install.nodeSSH` secret for container nodes. Bootwright
unlinks an owner-only temporary file before writing the decrypted key, keeps
its descriptor open while SSH runs, and gives OpenSSH the corresponding
`/proc/<bootwright-pid>/fd/<fd>` path. The parent closes the descriptor after
SSH exits. The encrypted context envelope is never passed to OpenSSH and no
named plaintext key remains after the command.

## Direct SSH

Every durable direct SSH connection follows one crypto and trust policy.
Direct SSH copies only allowlisted cryptographic directives from the Red Hat
OpenSSH crypto-policy backend, when present, into an anonymous configuration;
other platforms retain OpenSSH's compiled cryptographic defaults. It does not
read generic system host rules or the invoking root account's personal
configuration, and permits only the selected public-key identity. Its host-key
alias is the effective target address, with global and DNS host-key trust
disabled, so context or explicit known-hosts material remains the only
server-authentication source. Context-managed direct SSH uses OpenSSH's explicit
first-use prompt; unknown keys fail without an interactive terminal and changed
keys always fail closed. After out-of-band verification, container-node
replacement removes only the effective address's stale entry from the context
known-hosts file with `ssh-keygen -R`, then records the replacement through an
interactive direct connection. The child receives only terminal, locale, time
zone, and color environment values; caller-selected askpass, agent, loader, and
crypto-provider overrides do not cross the root process boundary.

## BMC and Virtual-Media Trust

A Machine boots from a Redfish BMC over two distinct TLS legs, each with its own
authored control (`state-model.md`, Machine section):

- **Controller → BMC** (the Redfish API leg) is governed by
  `spec.hardware.management.bmc.tls.verify`. It defaults to verify; set it
  `false` only for a lab/self-signed BMC certificate.
- **BMC → artifact server** (the virtual-media fetch of the agent ISO) is
  governed by `spec.hardware.management.bmc.virtualMedia.tls`.

The artifact server presents a self-signed certificate the BMC cannot trust
without distributing a CA to every BMC, so the fetch leg needs deliberate
handling:

- `virtualMedia.tls.trust: disable-verification` (the default when no
  `virtualMedia.tls` is authored): for an HTTPS fetch the
  `container_cluster_boot_redfish` role temporarily sets the BMC
  `SecurityService.HttpsTransferCertVerification` and the VirtualMedia
  `VerifyCertificate` to `false` for the fetch, then restores the probed
  original values in an `always` cleanup
  (`media/restore_certificate_verification.yml`) unless
  `restoreVerificationAfterBoot: false` leaves them off. TLS encryption is
  retained — only server authentication is dropped — and the fetch is further
  bounded by the unguessable per-run publish token in the ISO URL.
- `virtualMedia.tls.trust: import-certificate` is the trust path: it uploads
  the artifact server certificate into the BMC trust store before the fetch so
  verification can stay on, and `removeCertificateAfterBoot: true` removes it
  once the ISO is mounted.
- `virtualMedia.tls.trust: established` declares the trust exists out of band
  (CA-signed artifact certificate, root CA pre-loaded into the BMC trust store,
  or verification already disabled): the role performs **no** BMC security
  writes — no verification toggles, no import, no restore — and only issues the
  canonical `InsertMedia`. This is the only mode usable by a BMC account that
  lacks the vendor's security-configuration right on a verification-enforcing
  BMC.

This virtual-media fetch is the only place Bootwright relaxes artifact-server
TLS trust. `InfraProvider.spec.baremetal.defaults.bmc` supplies fleet-wide
defaults for both legs (a machine value wins; `credentialsRef` stays
per-machine).

The only other verification skips are narrowly-scoped reachability probes against
Bootwright's own managed self-signed artifact server: the staged-ISO `HEAD` and
byte-range fetch checks in `container_cluster_agent_install` and the
artifact-server HTTP readiness wait. They confirm the endpoint is serving and
read no response content, so no fetched bytes are ever consumed unverified.

The libvirt lab substrate's emulated Redfish BMC is a cleartext basic-auth
endpoint that binds all interfaces (`0.0.0.0`) by default. The bind address is an
authored knob (`bindAddress` on the provider's emulated-BMC defaults), so an
operator can narrow it to a management interface; even so it is a lab-only
convenience that must stay on a trusted management segment.

## Proxy Boundaries

`Environment.spec.infraComponents.proxies[]` declares proxy access entries; one
may set `default: true`. `Environment.spec.proxyFor.bootwright`,
`Environment.spec.proxyFor.containerClusterInstall`, and
`Environment.spec.proxyFor.machineOSInstall` override which proxy each consumer
uses: a name selects an entry, `none` opts the consumer out, and an empty slot
inherits the default proxy. `machineOSInstall` routes the managed-OS (Anaconda)
install fetch and takes effect only for an external proxy entry, since the node
installs before any managed proxy exists; a managed selection (direct or
inherited) is rejected. Each install fetch and the RHSM `no_proxy` honour the
proxy's `noProxy` list, including CIDR entries — CIDR-covered internal hosts are
pinned to concrete literals so bypass matchers that cannot parse a CIDR still
route them direct.

External proxy entries carry direct URLs and optional auth refs. Managed proxy
entries reference an `InfraComponent` with `spec.proxy`, and the runtime URL is
derived from the selected service machine and port.

Any command that prints proxy shell exports with embedded credentials must gate
that output behind `--sensitive`; without the flag the referenced credentials
must not be emitted.

## Generated Artifacts

Rendered installer files, inventories, lock files, and effective-state
snapshots must not inline secret bytes. They may contain references, public
keys, release images, mirror URLs, and non-secret cluster addresses.

Generated output boundaries are part of the safety contract:

- The context registry is the only user-home state:
  `~/.bootwright/contexts.yaml`.
- Context data is root-managed under
  `/var/lib/bootwright/contexts/<context>/`. `context init`/`context update`
  copy the operator's source directory tree into
  `/var/lib/bootwright/contexts/<context>/input/`, so the context owns its
  authored input and is self-contained. Mutating runs copy the loaded input YAML
  into the context as a forensic snapshot under `runs/`.
- The copied input tree at `input/` is root-owned with `0700` directories and
  `0600` files. Because `file:`-sourced secret material and SSH keys are
  resolved relative to the loaded YAML, any such operator-referenced files
  copied into `input/` are part of the authored input and are exempt from the
  ephemeral-only rule below — they live alongside the YAML under the same
  root-managed permissions.
- Input-tree snapshots under
  `/var/lib/bootwright/contexts/<context>/input-history/` inherit the same
  secret classification as `input/`, because the snapshotted tree may carry
  `file:`-sourced secret material. Retention and purge rules are in
  `state-model.md`, CLI Contract.
- Context SSH host trust lives under
  `/var/lib/bootwright/contexts/<context>/trust/ssh/` as `known_hosts` and
  `hosts.json`, with `0700` directories and `0600` files. The records are
  root-managed server public keys and fingerprints — non-secret, and never
  private key material.
- Context-local secrets live under
  `/var/lib/bootwright/contexts/<context>/secrets/` as encrypted envelopes.
  Short-lived plaintext copies for external tools may be materialized only
  under per-run/task runtime directories with `0700` directories and `0600`
  files, and must be removed after execution.
- Managed ISO media lives under `/var/lib/bootwright/media/`. These files are
  host-local, root-managed, non-secret, and not versioned; licensed media such
  as RHEL ISOs must be supplied by the operator.
- Runtime ownership records live under
  `/var/lib/bootwright/contexts/<context>/ownership/`. They are root-managed
  non-secret JSON records used to destroy resources Bootwright created or
  configured, including resources no longer present in the input YAML.
- Placeholder installer output lives under
  `/var/lib/bootwright/contexts/<context>/clusters/<cluster>/rendered/installer/`.
- Secret-inlined runtime installer output lives under
  `/var/lib/bootwright/contexts/<context>/clusters/<cluster>/runtime/installer/`
  with restrictive file modes and must never be versioned.
- Managed machine OS Kickstart files and remastered install ISOs may inline
  RHSM organization and activation-key material when
  `MachineInstallProfile.spec.installer.anaconda.packageSource.fromSubscription`
  references a Red Hat RHEL entitlement. They are runtime artifacts only and
  must never be versioned.
- Rendered storage tool inputs live under
  `/var/lib/bootwright/contexts/<context>/rendered/storage/<storageCluster>/`.
- Kubeconfigs produced for installed clusters live at
  `/var/lib/bootwright/contexts/<context>/clusters/<cluster>/secrets/kubeconfig`.
- Apply logs live under `/var/lib/bootwright/contexts/<context>/runs/` with
  restrictive file modes: the shared run log at `runs/history/<run-id>/
  bootwright.log` and each cluster's split-out flow log at
  `runs/history/<run-id>/bootwright-<cluster>.log`.
- Per-cluster install records live at
  `/var/lib/bootwright/contexts/<context>/clusters/<cluster>/runtime/install-record.json`.
- Per-resource convergence safety records live under
  `/var/lib/bootwright/contexts/<context>/runs/safety/`. They may contain
  owner identity, non-secret desired hashes, observed-state classifications,
  and task/resource identifiers, but never secret bytes.
- Managed machine OS install markers live on the installed machine at
  `/etc/bootwright/install-marker.json` by default. The marker contains
  Bootwright ownership metadata and a non-secret desired hash only.
- Node access markers live on the machine at
  `/etc/bootwright/access-marker.json` with mode `0644`. They record the
  orchestration account name, the root-login posture, and the paths Bootwright
  owns — non-secret, no key material.
- `bootwright render --output-dir <dir> --sensitive` writes
  operator-requested secret-inlined tool inputs under `<dir>` with restrictive
  file modes. The command must fail without `--sensitive`.
- `bootwright render --input-dir <dir>` renders context-free from an input
  directory with no configured context. Because no context secret store is
  available, every secret renders as a `{{ secret <name> }}` (or
  `{{ secret <name>.<role> }}`) placeholder rather than its bytes, so the output
  is safe to inspect and never inlines secret material; `--input-dir` is
  therefore incompatible with `--sensitive`.

### Redaction escape hatch

Ansible tasks that handle credentials gate `no_log` on `bootwright_no_log`, which
defaults to `true` so secret bytes are redacted as `censored due to no_log` in
both the terminal and the persisted `0600` run log. The commands that run
Ansible — `apply`, `destroy`, `check`, and `diff` — accept `--verbose`/`-v`,
which sets `bootwright_no_log` to `false`. This is a deliberate,
opt-in operator escape hatch for debugging: with it set, the secret bytes those
tasks handle (BMC, registry, RHSM, and proxy credentials, tokens, and generated
Ceph keys) reach both the terminal and the `0600` run log in full. Default runs
remain redacted.

A `--verbose` run therefore leaves credential plaintext in its persisted log —
`runs/history/<run-id>/bootwright*.log` for `apply`, `runs/destroy/` or
`runs/preflight/` for the others. Nothing expires it, and
`destroy --purge-history` removes an apply run's directory only when that whole
run is in its scope (`state-model.md`, CLI Contract). Purging or protecting
that run directory afterwards is the operator's obligation.

## Code Surface Hygiene

Unused code and duplicated implementations are security and maintenance risks.
Confirmed unused code must be removed rather than kept for speculative future
use. Validation, normalization, rendering, path safety, redaction, command
construction, privilege boundaries, and provider or BMC capability handling
should have one domain owner.

## Supply Chain

Every component Bootwright imports, installs, shells out to, renders, or
instantiates is part of the supply-chain contract. Direct Go modules are pinned
in `go.mod` and `go.sum`. Runtime tool and managed image pins are recorded in
the rendered lock. Component image overrides must use an explicit version tag
or digest. Mutable or floating references, including omitted image tags,
non-version tags, and `:latest`, are invalid unless an accepted spec decision
documents a temporary hold.
