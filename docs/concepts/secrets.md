---
title: Secrets and entitlements
description: How desired state names secret material without carrying bytes — declaration grammar, consumer references, storage modes, node SSH, the secret CLI, and vendor entitlements.
---

# Secrets and entitlements

Desired state references secret material **by name only** — it never carries
bytes, so it is safe to commit. A reference names an entry in
[`Environment.spec.secrets`](environment.md); the bytes live in the per-context
secret store or in operator-owned local files. The same is true of vendor
entitlements: an entitlement declares *named* access to a subscription or
registry, and the credentials behind it are themselves secrets.

This page is both the concept and the field reference for secrets and
entitlements. See [conventions](index.md) for the object envelope and the
Required/Default field-table convention.

## Declaration grammar

`Environment.spec.secrets[]` is the API's one bespoke collection codec: it is
*authored as a list* of scalar names or single-key objects, and decodes into a
name-keyed map. It is neither a plain list nor a plain map. Entry names must be
DNS labels. Each name resolves to exactly one of three source arms:

- **Context-local** — a scalar list item, or a single-key item with an
  omitted/null value. Bytes are written with `bootwright secret set` into the
  encrypted current-context secret store, or minted by `bootwright secret
  generate`.
- **`file:`** — operator-owned local material that already exists at a declared
  path. The path is resolved on the machine running Bootwright (the control
  node); with the default `secretStorage.mode: source` it is read in place.
- **`generated:`** — bytes Bootwright produces during `bootwright secret
  generate` (context-local credentials, self-signed certificates, and SSH key
  pairs).

An object item has **at most one** of `file:` or `generated:`; `keyFile:`
requires `file:`; and a `generated:` block sets exactly one of `credentials`,
`selfSignedCertificate`, or `sshKeyPair`.

```yaml
spec:
  secrets:
    - openshift-pull-secret
    - lab-ocp-cluster-admin-ssh-key:
        generated:
          sshKeyPair:
            comment: bootwright-lab-ocp-cluster-admin
    - bastion-host-ssh:
        file: ~/.ssh/bootwright-ssh-key
    - bmc-credentials:
    - proxy-credentials:
        generated:
          credentials:
            username: proxy
    - mirror-registry-trust-bundle:
        generated:
          selfSignedCertificate:
            commonName: registry.lab.bootwright.test
    - api-serving-tls:
        file: ../secrets/api-serving.crt
        keyFile: ../secrets/api-serving.key
    - ingress-serving-tls
```

### Source-arm fields

| Shape | Required parts | Meaning |
| --- | --- | --- |
| `- name` | name | Context-local material (encrypted context store). |
| `- name:` | name | Same as scalar context-local material. |
| `- name: {file: <path>}` | `file` | Operator-owned local source file. |
| `- name: {file: <path>, keyFile: <path>}` | `file` (and `keyFile`) | TLS or paired material with a key file; `keyFile` requires `file`. |
| `- name: {generated: {credentials: ...}}` | `generated.credentials` | Generated username/password-style credentials. |
| `- name: {generated: {selfSignedCertificate: ...}}` | `generated.selfSignedCertificate` | Generated cert/key pair. |
| `- name: {generated: {sshKeyPair: ...}}` | `generated.sshKeyPair` | Generated SSH key pair. |

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `generated.credentials.username` | No | — | Generated credential username (no whitespace, colon, or newlines). |
| `generated.selfSignedCertificate.commonName` | Yes | — | Certificate common name. Required when `selfSignedCertificate` is used. |
| `generated.selfSignedCertificate.dnsNames[]` | No | — | DNS SANs. |
| `generated.selfSignedCertificate.ipAddresses[]` | No | — | IP SANs. |
| `generated.selfSignedCertificate.validityDays` | No | — | Validity period; must not be negative. |
| `generated.sshKeyPair.type` | No | `ed25519` | Key type; currently only `ed25519`. |
| `generated.sshKeyPair.comment` | No | — | Public key comment (no leading/trailing whitespace or newlines). |

!!! note "`secret set --generate` is for test fixtures only"
    `bootwright secret set --generate` mints a random password (default username
    `admin`) and is intended for test fixtures, not production credential
    provisioning. Production generated material comes from `bootwright secret
    generate` materializing the `generated:` declarations.

## How consumers reference names

Every kind references a secret by name. A scalar item, or an object item with an
omitted/null value, resolves to
`/var/lib/bootwright/contexts/<context>/secrets/<name>`. The reference fields
across the API include:

| Reference | Consumer |
| --- | --- |
| `install.pullSecretRef` | [`ContainerCluster`](container-clusters.md) pull secret. |
| `install.nodeSSH.keyPairRef` (or `publicKeyRef`/`privateKeyRef`) | Cluster node SSH; see [Node SSH keys](#node-ssh-keys). |
| `credentialsRef` | BMC, proxy, mirror-registry, and entitlement credentials. |
| `trustBundleRef` / `installTrust.caBundleRefs[]` / `additionalTrustBundleRefs[]` | CA trust bundles. |
| `keyRef` | `Machine.spec.access.ssh` private SSH key. |
| `secretRef` / `defaultCertificateRef` | Serving certificates. |
| `proxyAuthRef` | Proxy credentials. |

### Node SSH keys

For cluster node SSH, use `install.nodeSSH.keyPairRef` when one secret owns both
halves. Use `publicKeyRef` plus optional `privateKeyRef` when the public key
authorized in `install-config.yaml` and the private key used for local
post-install probes are stored under different secret names. When
`install.nodeSSH` is omitted, Bootwright uses
`<cluster-name>-cluster-admin-ssh-key`; examples still declare and reference
that name explicitly.

!!! warning "Renaming a cluster re-derives the default node-SSH secret"
    Because `<cluster-name>-cluster-admin-ssh-key` derives from the cluster
    name, renaming a `ContainerCluster` that relies on the omission default
    re-derives it: validation reports the new name as a defaulted, undeclared
    secret, and generated key bytes are keyed by secret name, so declaring the
    new name produces a fresh key pair instead of reusing the one nodes were
    installed with. When renaming a cluster, keep `install.nodeSSH.keyPairRef`
    pinned to the existing secret name (or migrate the key material
    deliberately).

For durable machines Bootwright or managed tools SSH into, put SSH connection
material on `Machine.spec.access.ssh`. `keyRef` supplies private SSH key
material; managed Ceph node hosts also require the public half at `<name>.pub`
so Bootwright can pass it to cephadm. Generated SSH key pairs write the private
key to `<name>` and the public key to `<name>.pub`.

### SSH host trust

Durable SSH targets normally use context-managed host trust recorded by
`bootwright host trust`; `Machine.spec.access.ssh.knownHostsRef` is available
when an operator needs to point at explicit known_hosts material. Bootwright
records each non-local Machine server key under
`/var/lib/bootwright/contexts/<context>/trust/ssh/` and uses that known_hosts
file with strict host-key checking.

For interactive runs, recording trust up front is optional: `preflight` and
`apply` show each unknown host's key fingerprint and ask before recording it,
for hosts with no existing record only (opt out with
`--trust-on-first-use=false`). Automation must still pre-record trust by running
`bootwright host trust` after importing or updating a context — non-interactive
runs (`--yes`, `--output json`, `--dry-run`) never prompt and fail closed on
missing trust. Verify the displayed fingerprints out of band before accepting
first-use trust; a *changed* server key is never accepted interactively and
requires `bootwright host trust --replace` after you verify the new fingerprint.

## Storage modes and encryption at rest

Secret bytes live under the root-managed Bootwright context directory:

| Path | Location |
| --- | --- |
| Current context selection | `~/.bootwright/contexts.yaml` |
| Context dir | `/var/lib/bootwright/contexts/<context>` |
| Secrets dir | `/var/lib/bootwright/contexts/<context>/secrets` |

`Environment.spec.secretStorage.mode` selects how `file:`-sourced material is
handled:

- **`source`** (default) — file-sourced secrets stay at their declared source
  paths and are read in place.
- **`context`** — the active context carries context-local copies of every
  file-sourced secret; run `bootwright secret generate` to copy `file:`-sourced
  material into the encrypted store before workflows read it.

The secrets directory must be host-local, unversioned, mode `0700`, with
individual files mode `0600`. Context-local material is encrypted at rest with
AES-256-GCM envelopes. Bootwright auto-initializes a `root-owned-file` keyring
under `secrets/.bootwright/` on the first context-local write; the key files are
host-local, unversioned, non-symlink regular files with mode `0600`.

!!! note "Enforcing the mode requirement"
    `bootwright apply` and `bootwright bastion setup` warn on secrets-dir or
    secret-file mode violations by default. Pass `--strict-secrets` to either
    command to abort instead when the secrets directory is not `0700` or any
    secret file is not `0600`.

On disk, context-local files contain JSON encryption envelopes (version,
algorithm, key provider, key ID, context, secret name, material role, nonce,
ciphertext, and `kdf: none`). Plaintext context files are blocked during normal
reads; run `bootwright secret encryption migrate --yes` once to replace old
plaintext files with encrypted envelopes. External `file:` sources remain
operator-owned files at their declared paths.

### Logical material layout

| Secret kind | Logical path |
| --- | --- |
| Pull secret | `<name>` |
| Node SSH public key | `<name>.pub` |
| Provider host SSH key | `<name>` |
| Generated SSH key pair | private key in `<name>` and public key in `<name>.pub` |
| BMC / proxy credentials | `<name>` |
| Self-signed cert + key | cert in `<name>` and key in `<name>.key` |
| TLS pair | certificate chain in `<name>` and private key in `<name>.key` |

## The secret CLI workflow

| Goal | Command |
| --- | --- |
| Pull secret or context-local secret | `bootwright secret set --name openshift-pull-secret --pull-secret ~/pull-secret.json` |
| TLS certificate and key | `bootwright secret set --name ingress-serving-tls --tls-cert ./tls.crt --tls-key ./tls.key` |
| CA / trust bundle (PEM) | `bootwright secret set --name corporate-ca --raw-file ./ca.pem` |
| Credentials | `bootwright secret set --name proxy-credentials --username proxy --password-stdin` |
| Credentials from a `username:password` file | `bootwright secret set --name bmc-credentials --from-file ./bmc.txt` |
| Converge `generated:` and context-storage entries | `bootwright secret generate` |
| Verify every declared secret has local material | `bootwright secret check` |
| Inspect required material | `bootwright secret list` |
| Print one local secret as raw bytes | `bootwright secret show --name <name>` |
| Print generated SSH public key | `bootwright secret show --name <name> --part public` |
| Delete local material | `bootwright secret delete --name <name> --yes` |
| Initialize / inspect / migrate / rotate encryption | `bootwright secret encryption {init,status,migrate,rotate}` |

The flow is:

1. Declare secret names in `Environment.spec.secrets`.
2. `bootwright secret set` writes context-local bytes through the local user
   (it does **not** re-exec as root; you need write access to the context
   secrets directory). Replacing an existing context-local secret prompts unless
   `--yes` is passed.
3. `bootwright secret generate` creates missing `generated:` entries and, when
   `secretStorage.mode: context`, copies external `file:` entries into the
   context store; `--renew` regenerates every `generated:` secret.
4. `bootwright secret check` is the read-only gate: it reports declared secrets
   that still need `bootwright secret set` and exits non-zero while any remain
   missing. External `file:` entries must already exist at their declared paths.

The other secret subcommands (`secret generate`, `secret check`, `secret
delete`, and the `secret encryption` family) operate on the root-managed context
store. `bootwright secret show` reads only context-local secret files (never
external `file:` sources) and decrypts only the requested material.

!!! note "Install secrets and access"
    After a successful cluster install, the kubeadmin password is stored at
    `clusters/<cluster>/secrets/kubeadmin-password`; `bootwright cluster access`
    prints the API and console URLs, kubeconfig path, password file path, and
    the retrieval command without printing bytes by default. A managed Ceph
    cluster's dashboard `admin` password is captured at install under
    `clusters/<storage-cluster>/secrets/dashboard-password` the same way.

Effective install/agent configs and `openshift/` manifests with resolved secrets
are runtime outputs under
`/var/lib/bootwright/contexts/<context>/clusters/<cluster>/runtime/installer/`
with mode `0600`; they are never committed. Plaintext credentials, kubeconfigs,
pull secrets, private keys, and tokens never appear in YAML or logs.

See [Installation](../getting-started/installation.md) for the end-to-end
secret-loading workflow as part of a first apply.

## Entitlements

Vendor entitlements declare *named* access for one provider/product pair. They
live on `Environment.spec.entitlements[]` and are referenced by storage and OS
install inputs — for example `StorageCluster.spec.ceph.entitlementRef` names one.
`name`, `provider`, and `product` are always required; the `rhsm`, `registry`,
`license`, and `rhelEntitlementRef` arms become required per pair.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `entitlements[].name` | Yes | — | Local entitlement name referenced by storage or OS install inputs. |
| `entitlements[].provider` | Yes | — | `community`, `redhat`, or `ibm`. |
| `entitlements[].product` | Yes | — | `ceph`, `openshift`, `rhel`, or `ibm-storage-ceph`, depending on provider. |
| `entitlements[].rhsm.organizationRef` | Conditional | — | Secret for the Red Hat organization ID. Required wherever `rhsm` is required. |
| `entitlements[].rhsm.activationKeyRef` | Conditional | — | Secret for the Red Hat activation key. Required wherever `rhsm` is required. |
| `entitlements[].rhsm.connectToInsights` | No | `false` | Whether managed RHEL installs connect to Insights. |
| `entitlements[].rhsm.satellite.hostname` | Conditional | — | Corporate Red Hat Satellite/Capsule FQDN (bare host, no scheme). Required when the `satellite` block is set. |
| `entitlements[].rhsm.satellite.trustBundleRef` | No | — | Secret with the Satellite's PEM CA bundle, trusted before registration. Required in practice for private/self-signed Satellite CAs. |
| `entitlements[].rhsm.satellite.contentBaseURL` | No | `https://<hostname>/pulp/content` | Override for the Satellite content (Pulp) base URL; derived from `hostname` when omitted. |
| `entitlements[].registry.url` | No | — | Vendor registry URL; must not embed credentials (use `credentialsRef`). |
| `entitlements[].registry.credentialsRef` | Conditional | — | Registry entitlement credentials. Required for `redhat/ceph` and `ibm/ibm-storage-ceph`. |
| `entitlements[].registry.trustBundleRef` | No | — | Registry trust bundle. |
| `entitlements[].license.accept` | Conditional | `false` | Must be `true` for `ibm/ibm-storage-ceph`. |
| `entitlements[].rhelEntitlementRef` | Conditional | — | Names a `redhat/rhel` entitlement supplying the RHEL subscription. Required for `ibm/ibm-storage-ceph`; rejected on every other pair (which carry `rhsm` inline). |

### Provider and product pairs

Only the following pairs are accepted; any other combination is rejected.

| Provider | Products |
| --- | --- |
| `community` | `ceph`, `openshift` |
| `redhat` | `ceph`, `rhel`, `openshift` |
| `ibm` | `ibm-storage-ceph` |

The required arms follow from the pair rather than a discriminator field:

| Provider / product | Required arms |
| --- | --- |
| `community/ceph` | none |
| `community/openshift` | none |
| `redhat/openshift` | none |
| `redhat/rhel` | `rhsm` (`organizationRef` + `activationKeyRef`) |
| `redhat/ceph` | `rhsm` + `registry.credentialsRef` |
| `ibm/ibm-storage-ceph` | `registry.credentialsRef` + `license.accept: true` + `rhelEntitlementRef` (no inline `rhsm`) |

IBM Storage Ceph ships its own image registry (`cp.icr.io`) and product license
but runs on RHEL it does not itself entitle, so its RHEL subscription is a
separate `redhat/rhel` entitlement named via `rhelEntitlementRef` — an inline
`rhsm` arm on an `ibm/ibm-storage-ceph` entitlement is rejected. (`redhat/ceph`
stays bundled: a single Red Hat subscription entitles both RHEL and the `rhceph`
tools repo, so its own `rhsm` arm covers both.)

A Red Hat Ceph entitlement, with the secrets it names:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: ceph-distribution-redhat
spec:
  baseDomain: bootwright.test
  secrets:
    - ceph-node-ssh:
        generated:
          sshKeyPair:
            comment: bootwright-ceph-node
    - redhat-org
    - redhat-activation-key
    - redhat-registry-credentials
  entitlements:
    - name: rhcs
      provider: redhat
      product: ceph
      rhsm:
        organizationRef: redhat-org
        activationKeyRef: redhat-activation-key
      registry:
        credentialsRef: redhat-registry-credentials
```

### Corporate Satellite

By default an `rhsm` arm registers against the public Red Hat CDN
(`subscription.redhat.io`). Add an optional `rhsm.satellite` block to redirect
registration to a corporate Red Hat Satellite (or Capsule): the same
`organizationRef` and `activationKeyRef` are interpreted against the Satellite,
and the CA named by `trustBundleRef` is trusted before registration. One block
covers both the install-time Anaconda kickstart and the day-2 cephadm
`subscription-manager register`, so nodes never fall back to the CDN. Because the
redirect lives on the entitlement, a `MachineImage` boot ISO or a Ceph cluster
that already references the entitlement inherits Satellite with no other changes.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: corp
spec:
  baseDomain: corp.example.com
  secrets:
    - corp-satellite-ca
    - rhel-org
    - rhel-activation-key
  entitlements:
    - name: rhel
      provider: redhat
      product: rhel
      rhsm:
        organizationRef: rhel-org
        activationKeyRef: rhel-activation-key
        connectToInsights: true
        satellite:
          hostname: satellite.corp.example.com
          trustBundleRef: corp-satellite-ca
```

The `rhsm` kickstart command is supported on Red Hat Enterprise Linux only
(Anaconda disables it on rebuilds such as AlmaLinux, Rocky, and CentOS Stream),
so Satellite registration applies to `family: rhel` installs.
OpenShift/RHCOS agent-install nodes do not use Satellite.

## Where to go next

- [Installation](../getting-started/installation.md) — the secret-loading
  workflow inside a first apply.
- [Disconnected and proxied installs](../advanced/disconnected-proxy.md) — trust
  bundles, mirror credentials, and RHSM in disconnected and proxied environments.
- [Environment](environment.md) — the full `spec.secrets`, `spec.entitlements`,
  and proxy/registry surface.
