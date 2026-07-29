# Community Ceph distribution

Selects the community Ceph distribution with `spec.ceph.distribution: oss`. The
exact `20.2.2` release pins both build axes — the community package repository
and the `v20.2.2` daemon image. No `Entitlement` is declared or referenced.

## Edit First

- `environment.yaml`: the fleet name and `domains.base`.
- `machine.yaml`: the provided storage node — its `ssh` address, login user, and
  `access.ssh.auth.privateKeyRef`.
- `secrets.yaml`: `ceph-cluster-ssh` (generated — the cephadm cluster key) and
  `ceph-node-ssh` (points at a local private-key file).
- `storage.yaml`: `spec.ceph.release`, the community `mirror`, the cephadm
  bootstrap node, and the node topology.
