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

Sensitive material includes pull secrets, SSH private keys, TLS private keys,
BMC credentials, vCenter credentials, proxy credentials, mirror credentials,
CA bundles, tokens, and kubeconfigs. These values must stay outside versioned
desired state.

Machine SSH follows the same boundary. Durable SSH connection details live on
`Machine.spec.access.ssh`. `keyRef` and `knownHostsRef` reference `Secret`
objects by name; when `knownHostsRef` is omitted, Bootwright records
server keys under context-managed SSH trust state. Non-local durable SSH uses
strict checking against explicit or context-managed known-hosts material.
Trust is recorded either by `bootwright host trust` or on first use during an
interactive `preflight`/`apply`, and first-use recording is allowed only for a
host with no existing trust record and only after an explicit interactive
per-host confirmation of the displayed key fingerprint. It never happens under
`--dry-run`, JSON output, or non-interactive execution, and a changed server
key is never accepted interactively: it fails closed until the operator
verifies the new fingerprint and records it deliberately with
`bootwright host trust --replace`.

KubeVirt child-cluster profiles also follow the same boundary. `hostClusterRef`
resolves to a generated kubeconfig already stored under Bootwright cluster
secrets state, and `kubeconfigRef` resolves through a `Secret` object
for external virtualization clusters. Desired state records only reference
names, never kubeconfig bytes.

Data Foundation external-cluster details render with placeholders for Ceph
client secrets. Generated Ceph keys are created or read during apply and must
not be committed.

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

### BMC virtual-media certificate verification

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

- By default (no authored `virtualMedia.tls`), for an HTTPS fetch the
  `container_cluster_boot_redfish` role temporarily sets the BMC
  `SecurityService.HttpsTransferCertVerification` and the VirtualMedia
  `VerifyCertificate` to `false` for the fetch, then restores the probed
  original values in an `always` cleanup
  (`media/restore_certificate_verification.yml`). TLS encryption is retained —
  only server authentication is dropped — and the fetch is further bounded by
  the unguessable per-run publish token in the ISO URL.
- `virtualMedia.tls.verify: false` asks the BMC to skip verification and does
  not restore it afterward (best-effort; some firmware ignores it).
- `virtualMedia.tls.importServerCertificate: true` is the trust path: it uploads
  the artifact server certificate into the BMC trust store before the fetch, and
  `removeServerCertificateAfterBoot: true` removes it once the ISO is mounted.

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

Commands that print proxy shell exports containing referenced credentials must
fail unless the operator passes `--sensitive`.

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
  `MachineInstallProfile.spec.installer.anaconda.packageSource.redhatCDN`
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
both the terminal and the persisted `0600` run log. `apply` and `destroy` accept
`--verbose`/`-v`, which sets `bootwright_no_log` to `false`. This is a deliberate,
opt-in operator escape hatch for debugging: with it set, the secret bytes those
tasks handle (BMC, registry, RHSM, and proxy credentials, tokens, and generated
Ceph keys) reach both the terminal and the `0600` run log in full. Default runs
remain redacted.

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
