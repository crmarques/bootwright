# Data Foundation external-cluster attach: two failure modes

The `attach-external-storage` step of `fusion-data-foundation` (and its ADR 0013
twin `openshift-data-foundation`) exports an external Ceph cluster's connection
details with rook's `create-external-cluster-resources.py`, writes them into the
`rook-ceph-external-cluster-details` Secret, and applies a `StorageCluster` with
`externalStorage.enable: true`. Two independent things break it. Both were
observed on the same prd apply on 2026-08-05, one per OpenShift cluster.

## `no matches for kind "StorageCluster" in version "ocs.openshift.io/v1"`

The step's manifests applied before the CRD existed. `follows: operatorReady`
only proves the add-on's own Subscription (`odf-operator`) reached `Succeeded`;
`storageclusters.ocs.openshift.io` is owned by `ocs-operator`, which
`odf-operator` subscribes *from its own running pod* once it starts. The kind is
therefore unresolvable for as long as that second install takes, and the apply
loses unless something burns the time first — the exporter playbook's own
5-minute ConfigMap poll is what masked it on one cluster and not the other.

Fixed by declaring the API in `spec.steps[].requires[]`; see
[addon-apply-engine-gates.md](addon-apply-engine-gates.md). Gating harder on the
add-on's own Subscription cannot close it, and waiting for `ocs-operator` to run
is unsatisfiable: `odf-operator` holds its Deployment at zero replicas until a
`StorageCluster` exists.

## `error setting modifier for [client.healthchecker] type=key ...: Malformed input [buffer:3]`

Seen in the `StorageCluster` status as `ExternalClusterConnecting=False`, reason
`ExternalClusterStateError`, wrapping `failed to get external ceph mon version`,
`monclient: keyring not found`, and `[errno 5] RADOS I/O error`. The
`StorageCluster` sits `Progressing` until the readiness budget expires.

This is a **cephx key-type skew, not a corrupted or truncated key**. Decode the
blob the error quotes: a cephx `CryptoKey` is a little-endian `u16` type, an
8-byte `utime_t`, a `u16` secret length, then the secret. A legacy key is
type 1 with a 16-byte secret — 28 bytes, 40 base64 chars, always starting `AQ`.
The failing keys are type 2 (`CEPH_CRYPTO_AES256KRB5`) with a 32-byte secret —
44 bytes, 60 base64 chars, starting `Ag`.

IBM Storage Ceph mints AES256KRB5 by default from build `20.2.1-297` onwards
(ceph-prd-01 runs `20.2.1-324.el9cp`). Data Foundation 4.21 bundles a Ceph
`20.2.0` client, whose keyring parser has no handler for type 2 and rejects it
outright before authentication is ever attempted — no client-side config can
recover it.

**The discriminator is the vendor build, NOT the version number.** Upstream Ceph
userspace — checked at the `tentacle` branch, the `v20.2.1` tag, and `main` —
implements only `CEPH_CRYPTO_NONE` and `CEPH_CRYPTO_AES`, and every key-minting
call site hardcodes `CEPH_CRYPTO_AES`; AES256KRB5 is a downstream/kernel
addition an upstream mon cannot produce. So community Ceph `20.2.2` attaches to
Data Foundation 4.21 fine (confirmed on a lab cluster 2026-07-31) while IBM's
`20.2.1-324.el9cp` does not, even though 20.2.2 is the higher number. Do not
chase the Ceph version when triaging this; ask whether the provider is an IBM
build. Two fingerprints, both absent upstream and present on IBM builds:
`ceph auth get-or-create -h` advertising `--key_type`, and
`ceph config help mon_auth_emergency_allowed_ciphers` resolving. The fastest
test on any cluster is `ceph auth get-key client.healthchecker | head -c 2` —
`AQ` parses, `Ag` does not.

Bootwright is a faithful pipe here: the exporter's stdout reaches the Secret
byte for byte. Since 2026-08-05 the exporter playbook refuses the export when
any entry's `data.userKey` does not start with `AQ`, naming the offending
entries, rather than attaching keys that cannot work.

**The mon-map cipher policy, verified on ceph-prd-01 (`20.2.1-324.el9cp`),
2026-08-05.** `ceph mon dump` carries three fields, and `ceph -h` shows exactly
one command that writes them — `ceph mon set <auth_service_cipher |
auth_allowed_ciphers | auth_preferred_cipher> <value>`:

```
"auth_service_cipher":   { "name": "aes256k", "value": 2 }
"auth_allowed_ciphers": [ { "name": "aes256k", "value": 2 } ]
"auth_preferred_cipher": { "name": "aes256k", "value": 2 }
```

`auth_allowed_ciphers` holding only `aes256k` is why
`ceph auth get-or-create ... --key_type aes` answers
`Error EINVAL: creating key with insecure key type ("aes") not allowed` — the
flag exists (note the **underscore**) and `aes` is the right value; it is
policy, not vocabulary, that refuses. These are **mon map** fields:
`ceph config set` cannot write them, and the only related config option,
`mon_auth_emergency_allowed_ciphers`, is mon-only, `Can update at runtime:
false`, and rejected by the config store — "is special and cannot be stored by
the mon". It is a startup-only lockout escape hatch, not fleet configuration.

`spec.ceph.security.cephx.keyType` declares this (see
`docs/advanced/ceph-topologies.md`). Bootwright renders `auth_allowed_ciphers`
then `auth_preferred_cipher`, in that order — a cipher cannot be preferred
before it is allowed — and **never** `auth_service_cipher`, because rotating the
monitors' own service cipher invalidates every client's service key. `aes`
widens the allowed set rather than replacing it, so keys in use keep working.

Declaring `aes` is a cluster-wide cephx downgrade, so the alternative remains
worth taking first: pair the cluster with a Data Foundation build whose client
parses AES256KRB5. Check the vendor support matrix — an ODF/FDF 4.21 consumer
against an IBM Storage Ceph 9.x provider may be outside it, making a version
realignment the real answer.

Other traps:

- `ceph auth rotate` re-mints with the build default. So does a bare
  `get-or-create` after `auth rm` — the type only changes with `--key_type`, or
  once `auth_preferred_cipher` has been moved.
- The exporter calls `get-or-create` and adopts an existing entity verbatim
  (`check_user_exist`), so re-running the export changes nothing on its own: a
  bad key is never re-minted away. This is why declaring a key type also makes
  the export step delete the mistyped `client.healthchecker` / `client.csi-*`
  entities first — a whitelist, so `client.admin`, mon, mgr and OSD keys are
  never in scope even though they carry the same `Ag` prefix.
- Never flip the type byte on an existing 44-byte blob: `CryptoAES` validates
  only `length >= 16`, so a 32-byte secret relabelled type 1 loads silently and
  uses the first 16 bytes — a silently wrong credential.
- Bootwright passes no `--restricted-auth-permission`, so every attached
  OpenShift cluster shares the five entities (`client.healthchecker`,
  `client.csi-rbd-node`, `client.csi-rbd-provisioner`,
  `client.csi-cephfs-node`, `client.csi-cephfs-provisioner`, all at plain names
  with no generation suffix): one repair fixes all of them, one mistake breaks
  all of them.
