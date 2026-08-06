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
recover it. Upstream Ceph userspace implements only `CEPH_CRYPTO_NONE` and
`CEPH_CRYPTO_AES`; the AES256KRB5 type is a downstream/kernel addition.

Bootwright is a faithful pipe here: the exporter's stdout reaches the Secret
byte for byte. Since 2026-08-05 the exporter playbook refuses the export when
any entry's `data.userKey` does not start with `AQ`, naming the offending
entries, rather than attaching keys that cannot work.

Repairing it is a Ceph-side action on the five Data Foundation entities
(`client.healthchecker`, `client.csi-rbd-node`, `client.csi-rbd-provisioner`,
`client.csi-cephfs-node`, `client.csi-cephfs-provisioner`).

IBM Storage Ceph `20.2.1-324.el9cp` accepts a per-entity key type — verified
from `ceph auth get-or-create -h` on ceph-prd-01, which advertises
`auth get-or-create <entity> [<caps>...] [--key_type <value>]` and the same
flag on `get-or-create-key`. Note the **underscore**: `--key_type`, not
`--key-type`. The only cipher-related config option this build exposes is
`mon_auth_emergency_allowed_ciphers`; the `auth_preferred_cipher` /
`auth_allowed_ciphers` / `auth_service_cipher` names that circulate in
downstream rook source do **not** exist here, and rook sets its own via
`ceph mon set`, which `spec.ceph.config` (`ceph config set`) cannot reach.

Traps:

- `ceph auth rotate` re-mints with the build default. So does a bare
  `get-or-create` after `auth rm` — the key type only changes if `--key_type`
  is passed explicitly.
- The exporter calls `get-or-create` and adopts an existing key verbatim
  (`check_user_exist`), so a repaired entity survives re-export — and a bad one
  is never re-minted away by re-running the export. Repair, or delete and let
  the exporter recreate; re-running alone changes nothing.
- Re-creating by hand means re-supplying the exact caps the exporter would set.
  Capturing `ceph auth get <entity>` first, or deleting the entity and letting
  the exporter recreate it, avoids transcribing them.
- Never flip the type byte on an existing 44-byte blob: `CryptoAES` validates
  only `length >= 16`, so a 32-byte secret relabelled type 1 loads silently and
  uses the first 16 bytes — a silently wrong credential.
- Bootwright passes no `--restricted-auth-permission`, so every attached
  OpenShift cluster shares these five entities: one repair fixes all of them,
  and one mistake breaks all of them.
