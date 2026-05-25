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

## Declaration

```yaml
spec:
  secrets:
    openshift-pull-secret:
    cluster-admin-pub-key:
      file: ~/.ssh/bootwright-ssh-key.pub
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
with neither field resolves to `<context>/secrets/<name>`. TLS pair consumers
read `<context>/secrets/<name>` and `<context>/secrets/<name>.key`, unless
`file:` and `keyFile:` point at operator-owned files. Each kind
references the secret by name: `keyRef.name`, `credentialRef.name`,
`trustBundleRef.name`, `caBundleRefs[].name`, `proxyAuthRef.name`,
`secretRef.name`, or `defaultCertificateRef.name`.

## Local secrets directory

Bytes live under the Bootwright base directory:

| Path | Default | Override |
| --- | --- | --- |
| Context registry | `~/.bootwright/contexts.yaml` | none |
| Context base dir | `~/bootwright/<context-name>` | `bootwright context init --base-dir <dir>` |
| Secrets dir | `<base-dir>/secrets` | context selection |

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
| Inspect required material | `bootwright secret list` |
| Delete local material | `bootwright secret delete <name> --yes` |

`bootwright secret set` writes into the current context secrets
directory, so context-local entries can be declared as empty keys.
`bootwright secret generate` only materializes entries declared as `generated:`.
External `file:` entries, such as SSH keys under `~/.ssh`, must already
exist at their declared paths.

`bootwright print-env` exports context paths and proxy environment variables.
When proxy credentials would be embedded in those exports, rerun it as
`bootwright print-env --sensitive` and avoid recording the shell output.

## Format on disk

| Secret kind | File contents |
| --- | --- |
| Pull secret | JSON as downloaded from console.redhat.com |
| Cluster admin public key | OpenSSH public key |
| Provider host SSH key | OpenSSH private key |
| BMC / proxy credentials | One `username:password\n` line — sushy-emulator and Squid htpasswd files are derived from this at apply time and never committed |
| Self-signed cert + key | PEM cert and PEM key written by `bootwright secret generate` |
| TLS pair | PEM certificate chain in `<name>` and unencrypted PEM private key in `<name>.key` |

## What never appears in YAML or logs

- Plaintext credentials, kubeconfigs, pull secrets, private keys,
  tokens.
- Effective install / agent configs and `openshift/` manifests with resolved
  secrets (these live under
  `/var/lib/bootwright/contexts/<context>/runtime/<cluster>/installer/` with
  mode `0600` and are never committed).
- Generated self-signed cert/key material outside the local secrets
  directory.

The binding rules live in
[`/specs/security.md`](https://github.com/crmarques/bootwright/blob/main/specs/security.md).
