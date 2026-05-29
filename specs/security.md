# Security

Bootwright desired state is safe to commit. It references secrets by name and
path, but never carries secret bytes.

## Secret Ownership

`Environment.spec.secrets` declares every secret source used by the loaded
state. An empty entry is context-local material written under the current
context secrets directory, `file:` points at operator-owned local material, and
`generated:` describes material Bootwright can create under the context secrets
directory. `Environment.spec.secretStorage.mode` defaults to `source`; `context`
requires `bootwright secret materialize` to copy file-sourced entries into the
context secrets directory before workflows read them. Other kinds reference
those names with `SecretRef`.

Sensitive material includes pull secrets, SSH private keys, TLS private keys,
BMC credentials, vCenter credentials, proxy credentials, mirror credentials,
CA bundles, tokens, and kubeconfigs. These values must stay outside versioned
desired state.

KubeVirt child-cluster profiles follow the same boundary. `hostClusterRef`
resolves to the host cluster kubeconfig already produced under Bootwright
runtime state, and `kubeconfigRef` resolves through `Environment.spec.secrets`
for external virtualization clusters. Desired state records only the reference
name, never kubeconfig bytes.

## Installer Trust

Cluster install trust is rendered only from explicit references:

- OpenShift pull secrets come from `ContainerCluster.spec.install.pullSecretRef`
  or the normalized environment default.
- Node SSH authorization comes from
  `ContainerCluster.spec.install.nodeSSH.keyPairRef` or
  `.publicKeyRef`, or the normalized environment default.
- OKD clusters may omit a Red Hat pull secret unless a private release or
  mirror requires credentials.
- Mirror credentials and trust bundles come from
  `Environment.spec.registries.mirror`.
- Fleet-wide installer trust comes from `Environment.spec.installTrust`.
- Cluster-scoped installer trust comes from
  `ContainerCluster.spec.install.additionalTrustBundleRefs`.
- API and ingress serving certificate material comes from
  `ContainerCluster.spec.install.servingCertificates` references and is
  rendered as install-time OpenShift manifests.

Disconnected installs are cluster-scoped through
`ContainerCluster.spec.install.mode`.
They require mirror trust material and either an external mirror URL or a
managed registry component.

## Proxy Boundaries

`Environment.spec.infraComponents.proxies[]` declares proxy access entries.
`Environment.spec.proxyFor.bootwright` and
`Environment.spec.proxyFor.clusterInstall` select which proxy each consumer
uses; omitted values and the reserved value `none` disable proxy use.

External proxy `connection` entries carry direct URLs and optional auth refs.
Managed proxy entries reference an `InfraComponent` with `spec.proxy`, and the
runtime URL is derived from the selected service host and port.

Shell exports rendered from external proxy `connection.auth.proxyAuthRef`
include the referenced credentials in proxy URLs so downstream tools can
authenticate.
Commands that print those exports must fail unless the operator passes
`--sensitive`.

## Generated Artifacts

Rendered installer files, inventories, lock files, and effective-state snapshots
must not inline secret bytes. They may contain references, public keys, release
images, mirror URLs, and non-secret cluster addresses.

Generated output boundaries are part of the safety contract:

- The context registry is the only user-home state:
  `~/.bootwright/contexts.yaml`, containing only the current context name and a
  list of context names.
- Context data is root-managed under `/var/lib/bootwright/contexts/<context>/`.
  Commands that read or write that tree re-exec through `sudo` when not already
  running as root.
- Non-root commands that require local `sudo` must let Bootwright own the
  prompt. Bootwright checks each entered local sudo password immediately,
  retries invalid passwords up to three total attempts, sends passwords only
  over stdin to `sudo -S -v`, keeps them out of argv, environment, logs, and
  desired state, refreshes the validated sudo timestamp during long-running
  commands, and runs subsequent local sudo commands with `sudo -n`.
- Internal sudo re-exec has a strict local privilege boundary. User-owned
  inputs run as the original caller: context import source reads, `secret set`
  source reads, external `Environment.spec.secrets[].file` and `keyFile`
  checks/reads, `~` expansion for file-sourced secrets, caller PATH tool
  discovery, and other probes that do not need protected filesystem access.
  Root is used only for protected local mutations and reads: context state
  under `/var/lib/bootwright`, context-local/generated secret storage,
  workflow/runtime logs, the managed Ansible venv, package-manager installs,
  OCP CLI installation into `/usr/local/bin`, and context purges. An explicit
  `sudo bootwright ...` invocation is treated as root-owned and uses root's
  home, PATH, registry, and file permissions.
- User-authored YAML lives under
  `/var/lib/bootwright/contexts/<context>/input/`.
- Placeholder installer output lives under
  `/var/lib/bootwright/contexts/<context>/rendered/installer/<cluster>/`.
  Placeholder `openshift/` Secret manifests carry redacted data only.
- Context-local secrets live under
  `/var/lib/bootwright/contexts/<context>/secrets/`; `secret show` prints a
  named context-local secret as raw bytes and must not read external `file:`
  sources.
- Generated SSH key pairs live under the context secrets directory with the
  private key at `<name>` and public key at `<name>.pub`, both mode `0600`.
- External `file:` secret sources are operator-owned local material. When a
  non-root Bootwright invocation internally re-execs through `sudo`, checks and
  reads of those external files run as the original caller.
- Bootwright-managed secret-inlined runtime installer output lives under
  `/var/lib/bootwright/contexts/<context>/runtime/installer/<cluster>/`, with
  restrictive file modes, and must never be versioned.
- Kubeconfigs produced for installed host clusters live under
  `/var/lib/bootwright/contexts/<context>/runtime/installer/<cluster>/auth/`.
  They may be consumed by KubeVirt child-cluster operations through
  `hostClusterRef`, but must never be copied into authored desired state.
- Bootwright-managed apply logs that can include external tool output live under
  `/var/lib/bootwright/contexts/<context>/runs/`, with restrictive file
  modes, and must never be versioned.
- Per-cluster install records live under
  `/var/lib/bootwright/contexts/<context>/runtime/install-records/`.
  They may contain cluster names, non-secret desired-input fingerprints,
  install phases, run IDs, timestamps, and node boot markers, but must not
  contain kubeconfigs, pull secrets, tokens, private keys, or other secret
  bytes.
- The generated OpenShift kubeadmin password is copied into
  `/var/lib/bootwright/contexts/<context>/secrets/<cluster>-kubeadmin-password`
  with mode `0600` after a successful agent install.
- Cluster access inventory commands may print cluster API and console URLs,
  local kubeconfig paths, kubeadmin password secret names, local password file
  paths, and retrieval commands, but must not print kubeconfigs, kubeadmin
  passwords, tokens, or other secret bytes.
- `bootwright render --output-dir <dir> --sensitive` writes
  operator-requested external tool inputs, including secret-inlined
  `openshift-install` configs and optional `openshift/` manifests, under
  `<dir>` with restrictive file modes. The command must fail without
  `--sensitive`. Operators must keep that directory local and unversioned.

## Supply Chain

Every component Bootwright imports, installs, shells out to, renders, or
instantiates is part of the supply-chain contract. Additions and periodic
dependency reviews must check direct Go module dependencies and
Bootwright-instantiated runtime tools and container images against current
stable upstream releases.

All component references must be pinned. Direct Go modules are pinned in
`go.mod` and `go.sum`. Runtime tool and managed image pins are recorded in the
rendered lock. Component image overrides live in
`Environment.spec.componentImages`; each `local` or `public` image reference
must use an explicit version tag or digest. Mutable or floating references,
including omitted image tags, non-version tags, and the `:latest` container
tag, are invalid unless an accepted spec decision documents a temporary hold.

Disconnected mirror rendering must use the configured release image source for
OpenShift or OKD instead of hard-coded OpenShift-only sources.
