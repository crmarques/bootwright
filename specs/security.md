# Security

Bootwright desired state is safe to commit. It references secrets by name and
path, but never carries secret bytes.

## Secret Ownership

`Environment.spec.secrets` declares every secret source used by the loaded
state. A scalar list item, or a single-key list item with an omitted/null
value, is context-local material written through the encrypted context secret
store under the current context secrets directory. `file:` points at
operator-owned local material, and `generated:` describes material Bootwright
can create under the encrypted context store. `Environment.spec.secretStorage.mode`
defaults to `source`; `context` requires `bootwright secret materialize` before
workflows read encrypted context-local copies of file-sourced entries.

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
`Machine.spec.access.ssh`. `keyRef` and `knownHostsRef` reference
`Environment.spec.secrets`; when `knownHostsRef` is omitted, Bootwright records
server keys under context-managed SSH trust state. Non-local durable SSH uses
strict checking against explicit or context-managed known-hosts material.

KubeVirt child-cluster profiles also follow the same boundary. `hostClusterRef`
resolves to a generated kubeconfig already stored under Bootwright cluster
secrets state, and `kubeconfigRef` resolves through `Environment.spec.secrets`
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

## Proxy Boundaries

`Environment.spec.infraComponents.proxies[]` declares proxy access entries.
`Environment.spec.proxyFor.bootwright` and
`Environment.spec.proxyFor.containerClusterInstall` select which proxy each
consumer uses; omitted values and `none` disable proxy use.

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
  `/var/lib/bootwright/contexts/<context>/`.
- User-authored YAML lives under
  `/var/lib/bootwright/contexts/<context>/input/`.
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
  `MachineImage.spec.installSource.type: redhatCDN` is used. They are runtime
  artifacts only and must never be versioned.
- Rendered storage tool inputs live under
  `/var/lib/bootwright/contexts/<context>/rendered/storage/<storageCluster>/`.
- Kubeconfigs produced for installed clusters live at
  `/var/lib/bootwright/contexts/<context>/clusters/<cluster>/secrets/kubeconfig`.
- Apply logs live under `/var/lib/bootwright/contexts/<context>/runs/` and
  `/var/lib/bootwright/contexts/<context>/clusters/<cluster>/runs/` with
  restrictive file modes.
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
