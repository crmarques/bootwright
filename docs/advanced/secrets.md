---
title: Secrets
description: How secret bytes stay out of YAML and Git.
---

# Secrets

Desired-state YAML references secret material **by name only**. Bytes
live outside the repo. Three sources are supported per name:

- Scalar list item, or a single-key list item with an omitted/null value -
  context-local bytes written with `bootwright secret set` into the encrypted
  current context secret store.
- `file:` — operator-supplied bytes that already exist at a declared
  path. The path is local to the bastion.
- `generated:` — bytes Bootwright produces via `bootwright secret
  generate`. Operators may also pre-populate generated credentials with
  `bootwright secret set`.

By default, file-sourced secrets stay at their declared source paths. Set
`secretStorage.mode: context` and run `bootwright secret materialize` when
the active context should carry context-local copies of every file-sourced
secret.

## Declaration

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

Each object item has **at most one** of `file:` or `generated:`. A scalar item,
or an object item with an omitted/null value, resolves to
`/var/lib/bootwright/contexts/<context>/secrets/<name>`. TLS pair consumers
read that file and `<name>.key`, unless `file:` and `keyFile:` point at
operator-owned files. Generated SSH key pairs write the private key to
`<name>` and the public key to `<name>.pub`. Each kind
references the secret by name: `keyRef.name`, `credentialRef.name`,
`trustBundleRef.name`, `installTrust.caBundleRefs[].name`,
`proxyAuthRef.name`, `secretRef.name`, `defaultCertificateRef.name`, or
`nodeSSH.keyPairRef.name`. Durable SSH targets normally use context-managed
host trust recorded by `bootwright host trust`; `Machine.spec.access.ssh.knownHostsRef`
is available when an operator needs to point at explicit known_hosts material.

For node SSH, use `install.nodeSSH.keyPairRef` when one
secret owns both halves. Use `publicKeyRef` plus optional `privateKeyRef` when
the public key authorized in `install-config.yaml` and the private key used for
local post-install probes are stored under different secret names. When
`install.nodeSSH` is omitted, Bootwright uses
`<cluster-name>-cluster-admin-ssh-key`; examples still declare and reference
that name explicitly.

For durable machines Bootwright or managed tools SSH into, put SSH connection
material on `Machine.spec.access.ssh`. `keyRef.name` supplies private SSH key material;
managed Ceph node Hosts also require the public half at `<name>.pub` so
Bootwright can pass it to cephadm. Run `bootwright host trust` after importing
or updating a context so Bootwright records each non-local Machine server key
under `/var/lib/bootwright/contexts/<context>/trust/ssh/` and uses that
known_hosts file with strict host-key checking. Verify the displayed
fingerprints out of band before accepting first-use trust.

## Local secrets directory

Bytes live under the root-managed Bootwright context directory:

| Path | Location |
| --- | --- |
| Current context selection | `~/.bootwright/contexts.yaml` |
| Context dir | `/var/lib/bootwright/contexts/<context-name>` |
| Secrets dir | `/var/lib/bootwright/contexts/<context-name>/secrets` |

The secrets directory must be host-local, unversioned, mode `0700`, and
individual files mode `0600`. Context-local material is encrypted at rest with
AES-256-GCM envelopes. Bootwright auto-initializes a `root-owned-file` keyring
under `secrets/.bootwright/` on the first context-local write; the key files
are host-local, unversioned, non-symlink regular files with mode `0600`.

## Writing secrets

| Form | Command |
| --- | --- |
| Pull secret or context-local secret | `bootwright secret set openshift-pull-secret --pull-secret ~/pull-secret.json` |
| TLS certificate and key | `bootwright secret set ingress-serving-tls --tls-cert ./tls.crt --tls-key ./tls.key` |
| Credentials | `bootwright secret set proxy-credentials --username proxy --password-stdin` |
| Credentials from protected env vars | `printf '%s\n' "$PROXY_PASS" \| bootwright secret set proxy-credentials --username "$PROXY_USER" --password-stdin` |
| Materialize every `generated:` entry | `bootwright secret generate` |
| Generate and copy context-storage entries | `bootwright secret materialize` |
| Inspect required material | `bootwright secret list` |
| Print one local secret as raw bytes | `bootwright secret show --name <name>` |
| Print generated SSH public key | `bootwright secret show --name <name> --part public` |
| Delete local material | `bootwright secret delete <name> --yes` |
| Initialize encryption explicitly | `bootwright secret encryption init` |
| Inspect encryption status | `bootwright secret encryption status` |
| Migrate old plaintext context files | `bootwright secret encryption migrate --yes` |
| Rotate the active context key | `bootwright secret encryption rotate --yes` |

After a successful cluster install, Bootwright stores the kubeadmin password at
`clusters/<cluster>/secrets/kubeadmin-password`.
`bootwright cluster access-info` shows the API and console URLs, kubeconfig path,
password file path, and the command to retrieve the password without printing
secret bytes by default.

A managed Ceph `StorageCluster` is treated the same way: the dashboard `admin`
password generated by `cephadm bootstrap` is captured at install and stored at
`clusters/<storage-cluster>/secrets/dashboard-password`, with the same path-only,
`sudo cat` access protection. See
[Ceph Storage Clusters](storage-ceph.md) for access and recovery.

`bootwright secret set` writes into the current context secrets directory, so
context-local entries can be declared as scalar list items or single-key list
items with omitted/null values. Replacing an existing context-local secret
prompts for confirmation unless the command includes `--yes`.
`bootwright secret generate` only materializes entries declared as `generated:`.
`bootwright secret materialize` runs generated materialization and, when
`secretStorage.mode: context`, copies external `file:` entries into the context
secrets directory. External `file:` entries, such as provider-host SSH keys
under `~/.ssh`, must already exist at their declared paths before they can be
copied.

`bootwright secret show` reads only context-local secret files. It does not
read external `file:` sources. It decrypts only the requested material.

`bootwright print-env` exports `BOOTWRIGHT_CONTEXT` and proxy environment variables.
When proxy credentials would be embedded in those exports, rerun it as
`bootwright print-env --sensitive` and avoid recording the shell output.

## Logical Material

The same logical paths are used for encrypted context material:

| Secret kind | Logical path |
| --- | --- |
| Pull secret | `<name>` |
| Node SSH public key | `<name>.pub` |
| Provider host SSH key | `<name>` |
| Generated SSH key pair | private key in `<name>` and public key in `<name>.pub` |
| BMC / proxy credentials | `<name>` |
| Self-signed cert + key | cert in `<name>` and key in `<name>.key` |
| TLS pair | certificate chain in `<name>` and private key in `<name>.key` |

On disk, those files contain JSON encryption envelopes with version,
algorithm, key provider, key ID, context, secret name, material role, nonce,
ciphertext, and `kdf: none`. Plaintext context files are blocked during normal
reads; run `bootwright secret encryption migrate --yes` once to replace old
plaintext files with encrypted envelopes. External `file:` sources remain
operator-owned files at their declared paths.

## What never appears in YAML or logs

- Plaintext credentials, kubeconfigs, pull secrets, private keys,
  tokens.
- Effective install / agent configs and `openshift/` manifests with resolved
  secrets (these live under
  `/var/lib/bootwright/contexts/<context>/clusters/<cluster>/runtime/installer/` with
  mode `0600` and are never committed).
- Generated self-signed cert/key material outside the local secrets
  directory.

The binding rules live in
[`/specs/security.md`](https://github.com/crmarques/bootwright/blob/main/specs/security.md).
