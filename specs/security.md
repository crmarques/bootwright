# Security

Bootwright desired state is safe to commit. It references secrets by name and
path, but never carries secret bytes.

## Secret Ownership

`Environment.spec.secrets` declares every secret source used by the loaded
state. A scalar list item is context-local material written under the current
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

KubeVirt child-cluster profiles follow the same boundary. `hostContainerClusterRef`
resolves to the host cluster kubeconfig already produced under Bootwright
cluster secrets state, and `kubeconfigRef` resolves through `Environment.spec.secrets`
for external virtualization clusters. Desired state records only the reference
name, never kubeconfig bytes.

Storage provisioning follows the same boundary. `StorageCluster` SSH
identities reference `Environment.spec.secrets`; `nodeSSH` is the
bastion-to-RHEL-node identity and `clusterSSH` is the identity passed to
cephadm for ongoing orchestration. Data Foundation external-cluster details
render with placeholders for Ceph client secrets; generated Ceph keys are
created or read during apply and must not be committed. Imported Ceph
connection JSON is declared through a Data Foundation add-on input value named
`externalDetailsRef`; normal render output uses a placeholder, while sensitive
render and apply-time artifacts inline the secret JSON only in local
restrictive-mode output. Managed Ceph saves generated connection JSON under
`clusters/<cluster>/secrets/addons/<addon>/inputs/<input>/external-cluster-details.json`
with restrictive permissions.

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
`Environment.spec.proxyFor.containerClusterInstall` select which proxy each consumer
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
  `~/.bootwright/contexts.yaml`, containing only that user's current context
  selection.
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
  source reads, external `Environment.spec.secrets` object-item `file` and `keyFile`
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
  `/var/lib/bootwright/contexts/<context>/clusters/<cluster>/rendered/installer/`.
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
  `/var/lib/bootwright/contexts/<context>/clusters/<cluster>/runtime/installer/`,
  with restrictive file modes, and must never be versioned.
- Rendered storage tool inputs live under
  `/var/lib/bootwright/contexts/<context>/rendered/storage/<storageCluster>/`.
  They may contain non-secret Ceph monitor endpoints, RGW endpoints, pool
  names, and placeholder external-cluster details, but must not contain Ceph
  client keys, SSH private keys, kubeconfigs, or tokens.
- Kubeconfigs produced for installed host clusters live at
  `/var/lib/bootwright/contexts/<context>/clusters/<cluster>/secrets/kubeconfig`.
  They may be consumed by KubeVirt child-cluster operations through
  `hostContainerClusterRef`, but must never be copied into authored desired state.
- Bootwright-managed apply logs that can include external tool output live under
  `/var/lib/bootwright/contexts/<context>/runs/` and
  `/var/lib/bootwright/contexts/<context>/clusters/<cluster>/runs/`, with
  restrictive file modes, and must never be versioned.
- Per-cluster install records live at
  `/var/lib/bootwright/contexts/<context>/clusters/<cluster>/runtime/install-record.json`.
  They may contain cluster names, non-secret desired-input fingerprints,
  install phases, run IDs, timestamps, and node boot markers, but must not
  contain kubeconfigs, pull secrets, tokens, private keys, or other secret
  bytes.
- The generated OpenShift kubeadmin password is copied to
  `/var/lib/bootwright/contexts/<context>/clusters/<cluster>/secrets/kubeadmin-password`
  with mode `0600` after a successful agent install.
- Cluster access inventory commands may print cluster API and console URLs,
  local kubeconfig paths, local password file paths, and retrieval commands,
  but must not print kubeconfigs, kubeadmin
  passwords, tokens, or other secret bytes.
- `bootwright render --output-dir <dir> --sensitive` writes
  operator-requested external tool inputs, including secret-inlined
  `openshift-install` configs, optional `openshift/` manifests, and rendered
  storage tool inputs, under `<dir>` with restrictive file modes. The command
  must fail without
  `--sensitive`. Operators must keep that directory local and unversioned.

## Code Surface Hygiene

Unused code and duplicated implementations are security and maintenance risks.
They preserve stale paths for privileges, filesystem access, command
execution, TLS, trust material, redaction, generated-output boundaries, and
secret handling after the maintained path has moved on.

Implementation and review work must search for unused packages, files,
functions, roles, tasks, templates, scripts, tests, and examples in the touched
scope. Confirmed unused code must be removed rather than kept for speculative
future use.

Duplicated behavior must be treated as responsibility drift. Validation,
normalization, rendering, path safety, redaction, command construction,
privilege boundaries, and provider or BMC capability handling should have one
domain owner. Other components must call that owner instead of reimplementing
the rule locally.

Code should stay clean, lean, and direct. Domain-driven design boundaries are
part of the safety model: domain concepts and invariants live in the package
or role that owns the domain responsibility, adapters translate external
systems into that model, and CLI or orchestration layers coordinate rather
than duplicate domain decisions. When duplication is found, refactor toward one
centralized implementation that current workflows use and tests cover.

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
