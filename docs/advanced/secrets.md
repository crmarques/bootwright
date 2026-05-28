---
title: Secrets
description: How secret bytes stay out of YAML and Git.
---

# Secrets

Desired-state YAML references secret material **by name only**. Bytes
live outside the repo. Three sources are supported per name:

- Empty entry - context-local bytes written with `bootwright secret set`
  under the current context secrets directory.
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
  secretStorage:
    mode: context

  secrets:
    openshift-pull-secret:
    cluster-admin-ssh-key:
      generated:
        sshKeyPair:
          type: ed25519
          comment: bootwright-cluster-admin
    provider-host-ssh:
      file: ~/.ssh/bootwright-ssh-key
    bmc-credentials:
      generated:
        credentials:
          username: admin
    proxy-credentials:
      generated:
        credentials:
          username: proxy
    mirror-registry-trust-bundle:
      generated:
        selfSignedCertificate:
          commonName: registry.lab.bootwright.test
    api-serving-tls:
      file: ../secrets/api-serving.crt
      keyFile: ../secrets/api-serving.key
    ingress-serving-tls:
```

Every entry has **at most one** of `file:` or `generated:`. An entry
with neither field resolves to
`/var/lib/bootwright/contexts/<context>/secrets/<name>`. TLS pair consumers
read that file and `<name>.key`, unless `file:` and `keyFile:` point at
operator-owned files. Generated SSH key pairs write the private key to
`<name>` and the public key to `<name>.pub`. Each kind
references the secret by name: `keyRef.name`, `credentialRef.name`,
`trustBundleRef.name`, `caBundleRefs[].name`, `proxyAuthRef.name`,
`secretRef.name`, `defaultCertificateRef.name`, or
`nodeSSH.keyPairRef.name`.

For node SSH, use `install.nodeSSH.keyPairRef` when one
secret owns both halves. Use `publicKeyRef` plus optional `privateKeyRef` when
the public key authorized in `install-config.yaml` and the private key used for
local post-install probes are stored under different secret names.

## Local secrets directory

Bytes live under the root-managed Bootwright context directory:

| Path | Location |
| --- | --- |
| Context registry | `~/.bootwright/contexts.yaml` |
| Context dir | `/var/lib/bootwright/contexts/<context-name>` |
| Secrets dir | `/var/lib/bootwright/contexts/<context-name>/secrets` |

The secrets directory must be host-local, unversioned, mode `0700`, and
individual files mode `0600`.

## Writing secrets

| Form | Command |
| --- | --- |
| Pull secret or context-local secret | `bootwright secret set openshift-pull-secret --pull-secret ~/pull-secret.json` |
| TLS certificate and key | `bootwright secret set ingress-serving-tls --tls-cert ./tls.crt --tls-key ./tls.key` |
| Credentials | `bootwright secret set proxy-credentials --username proxy --password-stdin` |
| Credentials from protected env vars | `bootwright secret set proxy-credentials --username "$PROXY_USER" --password "$PROXY_PASS"` |
| Materialize every `generated:` entry | `bootwright secret generate` |
| Generate and copy context-storage entries | `bootwright secret materialize` |
| Inspect required material | `bootwright secret list` |
| Print one local secret as raw bytes | `bootwright secret show --name <name>` |
| Print generated SSH public key | `bootwright secret show --name <name> --part public` |
| Delete local material | `bootwright secret delete <name> --yes` |

`bootwright secret set` writes into the current context secrets
directory, so context-local entries can be declared as empty keys.
`bootwright secret generate` only materializes entries declared as `generated:`.
`bootwright secret materialize` runs generated materialization and, when
`secretStorage.mode: context`, copies external `file:` entries into the context
secrets directory. External `file:` entries, such as provider-host SSH keys
under `~/.ssh`, must already exist at their declared paths before they can be
copied.

`bootwright secret show` reads only context-local secret files. It does not
read external `file:` sources.

`bootwright print-env` exports `BOOTWRIGHT_CONTEXT` and proxy environment variables.
When proxy credentials would be embedded in those exports, rerun it as
`bootwright print-env --sensitive` and avoid recording the shell output.

## Format on disk

| Secret kind | File contents |
| --- | --- |
| Pull secret | JSON as downloaded from console.redhat.com |
| Node SSH public key | OpenSSH public key |
| Provider host SSH key | OpenSSH private key |
| Generated SSH key pair | OpenSSH private key in `<name>` and public key in `<name>.pub` |
| BMC / proxy credentials | One `username:password\n` line — sushy-emulator and Squid htpasswd files are derived from this at apply time and never committed |
| Self-signed cert + key | PEM cert and PEM key written by `bootwright secret generate` |
| TLS pair | PEM certificate chain in `<name>` and unencrypted PEM private key in `<name>.key` |

## What never appears in YAML or logs

- Plaintext credentials, kubeconfigs, pull secrets, private keys,
  tokens.
- Effective install / agent configs and `openshift/` manifests with resolved
  secrets (these live under
  `/var/lib/bootwright/contexts/<context>/runtime/installer/<cluster>/` with
  mode `0600` and are never committed).
- Generated self-signed cert/key material outside the local secrets
  directory.

The binding rules live in
[`/specs/security.md`](https://github.com/crmarques/bootwright/blob/main/specs/security.md).
