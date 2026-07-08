---
title: Secrets and entitlements
description: How desired state names secret material without carrying bytes — the Secret kind and its type/source axes, consumer references, storage modes, node SSH, the secret CLI, and vendor entitlements.
---

# Secrets and entitlements

Desired state references secret material **by name only** — it never carries
bytes, so it is safe to commit. A reference names a
[`kind: Secret`](#the-secret-object) object; the bytes live in the per-context
secret store or in operator-owned local files. The same is true of vendor
entitlements: an entitlement declares *named* access to a subscription or
registry, and the credentials behind it are themselves `Secret` objects.

This page is both the concept and the field reference for secrets and
entitlements. See [conventions](index.md) for the object envelope and the
Required/Default field-table convention.

## The Secret object

A `Secret` is a first-class authored object — the promoted form of the removed
`Environment.spec.secrets[]` list. Every `...Ref` in the fleet (a
[SecretRef](#how-consumers-reference-names)) resolves to a `Secret` by
`metadata.name`, which must be a DNS label. A `Secret` never carries material
bytes, so it is safe to commit; it declares two axes:

- **`spec.type`** (required) says what the material *is*. It fixes the material
  roles the secret carries, which source arms are legal, and the shape of any
  generated parameters. There is no inference — every `Secret` states its type.
- **`spec.source`** says *how* the material is obtained: the per-context
  encrypted store (`contextStore`, the default), operator-owned files (`file`),
  or generation (`generated`). Omit `spec.source` entirely for the common
  context-local case; at most one arm may be set, and the legal arms are scoped
  by `type`.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: openshift-pull-secret
spec:
  type: dockerConfigJson       # context-local: bytes set via `bootwright secret set`
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: lab-ocp-cluster-admin-ssh-key
spec:
  type: sshKeyPair
  source:
    generated:
      comment: bootwright-lab-ocp-cluster-admin
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: bastion-host-ssh
spec:
  type: sshKeyPair
  source:
    file:
      privateKey: ~/.ssh/bootwright-ssh-key
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: proxy-credentials
spec:
  type: usernamePassword
  source:
    generated:
      username: proxy
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: mirror-registry-trust-bundle
spec:
  type: caBundle
  source:
    generated:
      commonName: registry.lab.bootwright.test
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: api-serving-tls
spec:
  type: tlsCertificate
  source:
    file:
      cert: ../secrets/api-serving.crt
      key: ../secrets/api-serving.key
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: ingress-serving-tls
spec:
  type: tlsCertificate         # context-local: `bootwright secret set --tls-cert … --tls-key …`
```

Author one `Secret` per file, named for its `metadata.name`, the same
one-object-per-file layout as every other kind. Paths under `source.file`
resolve against the `Secret`'s own file.

### The seven secret types

`spec.type` is required and is one of:

| `type` | Material | Typical consumers |
| --- | --- | --- |
| `opaque` | Arbitrary externally-supplied blob the system never mints | kubeconfig, `known_hosts`, RHSM org ID / activation key, external Ceph details, boot-media headers, playbook secrets |
| `token` | A single secret string the system may mint | Ceph mgmt-gateway oauth2 client / cookie secrets |
| `usernamePassword` | One-line `username:password` credential | BMC, vCenter, registry, mirror, proxy credentials |
| `dockerConfigJson` | A docker `config.json` with an `.auths` object | the OpenShift pull secret |
| `caBundle` | `CERTIFICATE`-only PEM trust-anchor set | every trust bundle / CA |
| `tlsCertificate` | Serving identity: cert PEM + private key PEM | API-server named certs, ingress default, Ceph mgmt-gateway |
| `sshKeyPair` | An SSH key pair | cluster node SSH, machine host access, cephadm cluster identity |

### The source union

Omit `spec.source` (or set an empty `source: {}`) for context-local material —
the default. Otherwise set exactly **one** arm:

| `source` arm | Meaning |
| --- | --- |
| `contextStore: {}` | Bytes live only in the encrypted per-context store, written by `bootwright secret set` or minted by `bootwright secret generate`. This is the default; usually just omit `source`. |
| `file:` | Operator-owned file(s) that already exist. With the default `secretStorage.mode: source` they are read in place; `mode: context` copies them into the context store. |
| `generated:` | Bootwright mints the material during `bootwright secret generate`. Legal only for `token`, `usernamePassword`, `tlsCertificate`, `caBundle`, and `sshKeyPair`. |

#### `source.file` keys by type

The populated file keys are scoped by `spec.type`; a key the type does not
consume is rejected. Paths are relative to the `Secret`'s file, absolute, or
`~`-rooted.

| Type | File keys |
| --- | --- |
| `opaque`, `token`, `usernamePassword`, `dockerConfigJson`, `caBundle` | `path` — the single material file. |
| `tlsCertificate` | `cert` + `key` — both required. |
| `sshKeyPair` | `privateKey` (required) + optional `publicKey` (derived from the private key when omitted). |

#### `source.generated` parameters by type

The generated parameters are flat and scoped by `spec.type`; a parameter the
type does not consume is rejected. `caBundle` and `tlsCertificate` both mint a
self-signed certificate (a `caBundle` acts as its own trust anchor).

| Field | Type(s) | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `username` | `usernamePassword` | No | `admin` | Generated credential username (no whitespace, colon, or newlines). The password is random. |
| `commonName` | `tlsCertificate`, `caBundle` | Yes | — | Certificate common name. |
| `dnsNames[]` | `tlsCertificate`, `caBundle` | No | — | DNS SANs. |
| `ipAddresses[]` | `tlsCertificate`, `caBundle` | No | — | IP SANs. |
| `validityDays` | `tlsCertificate`, `caBundle` | No | — | Validity period; must not be negative. |
| `keyType` | `sshKeyPair` | No | `ed25519` | Key algorithm: `ed25519`, `rsa`, `ecdsa-p256`, `ecdsa-p384`, or `ecdsa-p521`. Use `rsa` or ECDSA on FIPS-enforced control nodes. |
| `comment` | `sshKeyPair` | No | — | Public key comment (no leading/trailing whitespace or newlines). |
| `bytes` | `token` | No | `32` | Entropy of the generated token, in bytes; must not be negative. |

!!! note "`secret set --generate` is for test fixtures only"
    `bootwright secret set --generate` mints a random password (default username
    `admin`) and is intended for test fixtures, not production credential
    provisioning. Production generated material comes from `bootwright secret
    generate` materializing the `source.generated` declarations.

## How consumers reference names

Every kind references a secret by a bare name — a `SecretRef` — that resolves to
the `Secret` of that `metadata.name`. A context-local (`contextStore`) secret's
bytes resolve to `/var/lib/bootwright/contexts/<context>/secrets/<name>`. The
reference fields across the API include:

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
`bootwright machine trust`; `Machine.spec.access.ssh.knownHostsRef` is available
when an operator needs to point at explicit known_hosts material. Bootwright
records each non-local Machine server key under
`/var/lib/bootwright/contexts/<context>/trust/ssh/` and uses that known_hosts
file with strict host-key checking.

For interactive runs, recording trust up front is optional: `preflight` and
`apply` show each unknown host's key fingerprint and ask before recording it,
for hosts with no existing record only (opt out with
`--trust-on-first-use=false`). Automation must still pre-record trust by running
`bootwright machine trust` after importing or updating a context — non-interactive
runs (`--yes`, `--output json`, `--dry-run`) never prompt and fail closed on
missing trust. Verify the displayed fingerprints out of band before accepting
first-use trust; a *changed* server key is never accepted interactively and
requires `bootwright machine trust --replace` after you verify the new fingerprint.

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

1. Declare one `kind: Secret` object per secret.
2. `bootwright secret set` writes context-local bytes through the local user
   (it does **not** re-exec as root; you need write access to the context
   secrets directory). Replacing an existing context-local secret prompts unless
   `--yes` is passed.
3. `bootwright secret generate` creates missing `source.generated` secrets and,
   when `secretStorage.mode: context`, copies `source.file` material into the
   context store; `--renew` regenerates every generated secret.
4. `bootwright secret check` is the read-only gate: it reports declared secrets
   that still need `bootwright secret set` and exits non-zero while any remain
   missing. `source.file` material must already exist at its declared paths.

The other secret subcommands (`secret generate`, `secret check`, `secret
delete`, and the `secret encryption` family) operate on the root-managed context
store. `bootwright secret show` reads only context-local secret files (never
`source.file` material) and decrypts only the requested material.

!!! note "Install secrets and access"
    After a successful cluster install, the kubeadmin password is stored at
    `clusters/<cluster>/secrets/kubeadmin-password`; `bootwright cluster info`
    prints the API and console URLs, kubeconfig path, password file path, and
    the retrieval command without printing bytes unless you pass `--secrets`. A managed Ceph
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

An entitlement declares *named* vendor-controlled access for one product. It is
its own first-class `Entitlement` kind — one object per file, shared fleet-wide,
and in a tree layout it lives under `infra/entitlements/<name>.yaml`. It is
referenced by name from storage and OS install inputs — for example
`StorageCluster.spec.ceph.entitlementRef` names one. `metadata.name` and
`spec.type` are always required; `spec.type` is the discriminator, and the
`rhsm`, `registry`, `license`, and `rhelEntitlementRef` arms become required per
type. The secrets it names live on `Environment.spec.secrets`.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `metadata.name` | Yes | — | Entitlement name referenced by storage or OS install inputs. |
| `spec.type` | Yes | — | Discriminator: `redhat-rhel`, `redhat-ceph`, or `ibm-storage-ceph`. |
| `spec.rhsm.organizationRef` | Conditional | — | Secret for the Red Hat organization ID. Required wherever `rhsm` is required. |
| `spec.rhsm.activationKeyRef` | Conditional | — | Secret for the Red Hat activation key. Required wherever `rhsm` is required. |
| `spec.rhsm.connectToInsights` | No | `false` | Whether managed RHEL installs connect to Insights. |
| `spec.rhsm.satellite.hostname` | Conditional | — | Corporate Red Hat Satellite/Capsule FQDN (bare host, no scheme). Required when the `satellite` block is set. |
| `spec.rhsm.satellite.trustBundleRef` | No | — | Secret with the Satellite's PEM CA bundle, trusted before registration. Required in practice for private/self-signed Satellite CAs. |
| `spec.rhsm.satellite.contentBaseURL` | No | `https://<hostname>/pulp/content` | Override for the Satellite content (Pulp) base URL; derived from `hostname` when omitted. |
| `spec.registry.url` | No | — | Vendor registry URL; must not embed credentials (use `credentialsRef`). Defaults to `registry.redhat.io` (`redhat-ceph`) or `cp.icr.io/cp` (`ibm-storage-ceph`). |
| `spec.registry.credentialsRef` | Conditional | — | Registry entitlement credentials. Required for `redhat-ceph` and `ibm-storage-ceph`. |
| `spec.registry.trustBundleRef` | No | — | Registry trust bundle. |
| `spec.license.accept` | Conditional | `false` | Must be `true` for `ibm-storage-ceph`. |
| `spec.rhelEntitlementRef` | Conditional | — | Names a `redhat-rhel` entitlement supplying the RHEL subscription. Required for `ibm-storage-ceph`; rejected on every other type (which carry `rhsm` inline). |

### Types

Exactly one of three `spec.type` values is accepted; any other value is rejected.

| `spec.type` | Meaning |
| --- | --- |
| `redhat-rhel` | A Red Hat RHEL subscription (RHSM) — the RHEL BaseOS/AppStream repos. |
| `redhat-ceph` | A single Red Hat subscription covering both RHEL and the `rhceph` tools repo, plus `registry.redhat.io` access. |
| `ibm-storage-ceph` | IBM Storage Ceph product access (registry + license), running on RHEL entitled by a separate `redhat-rhel` entitlement. |

The required arms follow from `spec.type`:

| `spec.type` | Required arms |
| --- | --- |
| `redhat-rhel` | `rhsm` (`organizationRef` + `activationKeyRef`) |
| `redhat-ceph` | `rhsm` + `registry.credentialsRef` |
| `ibm-storage-ceph` | `registry.credentialsRef` + `license.accept: true` + `rhelEntitlementRef` (no inline `rhsm`) |

IBM Storage Ceph ships its own image registry (`cp.icr.io`) and product license
but runs on RHEL it does not itself entitle, so its RHEL subscription is a
separate `redhat-rhel` entitlement named via `rhelEntitlementRef` — an inline
`rhsm` arm on an `ibm-storage-ceph` entitlement is rejected. (`redhat-ceph`
stays bundled: a single Red Hat subscription entitles both RHEL and the `rhceph`
tools repo, so its own `rhsm` arm covers both.)

A Red Hat Ceph entitlement, with the `Secret` objects it names:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: ceph-distribution-redhat
spec:
  baseDomain: bootwright.test
---
apiVersion: bootwright.io/v1alpha1
kind: Entitlement
metadata:
  name: rhcs
spec:
  type: redhat-ceph
  rhsm:
    organizationRef: redhat-org
    activationKeyRef: redhat-activation-key
  registry:
    credentialsRef: redhat-registry-credentials
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: ceph-node-ssh
spec:
  type: sshKeyPair
  source:
    generated:
      comment: bootwright-ceph-node
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: redhat-org
spec:
  type: opaque
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: redhat-activation-key
spec:
  type: opaque
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: redhat-registry-credentials
spec:
  type: usernamePassword
```

### Corporate Satellite

By default an `rhsm` arm registers against the public Red Hat CDN
(`subscription.redhat.io`). Add an optional `rhsm.satellite` block to redirect
registration to a corporate Red Hat Satellite (or Capsule): the same
`organizationRef` and `activationKeyRef` are interpreted against the Satellite,
and the CA named by `trustBundleRef` is trusted before registration. One block
covers both the install-time Anaconda kickstart and the day-2 cephadm
`subscription-manager register`, so nodes never fall back to the CDN. Because the
redirect lives on the entitlement, a `MachineInstallProfile` Red Hat CDN package
source or a Ceph cluster that already references the entitlement inherits
Satellite with no other changes.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: corp
spec:
  baseDomain: corp.example.com
---
apiVersion: bootwright.io/v1alpha1
kind: Entitlement
metadata:
  name: rhel
spec:
  type: redhat-rhel
  rhsm:
    organizationRef: rhel-org
    activationKeyRef: rhel-activation-key
    connectToInsights: true
    satellite:
      hostname: satellite.corp.example.com
      trustBundleRef: corp-satellite-ca
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: corp-satellite-ca
spec:
  type: caBundle
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: rhel-org
spec:
  type: opaque
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: rhel-activation-key
spec:
  type: opaque
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
- [Environment](environment.md) — the `spec.secretStorage`, proxy, and registry
  surface; the `Secret` and `Entitlement` kinds are their own field references.
