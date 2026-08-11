# IBM Storage Ceph distribution

Selects the IBM Storage Ceph distribution with `spec.ceph.distribution: ibm`.
Two `Entitlement` objects split the access by concern: a `redhat-rhel`
entitlement (`rhel`) holds the RHEL subscription, and an `ibm-storage-ceph`
entitlement (`ibm-storage-ceph`) holds only the IBM registry and license. The
`StorageCluster` references the IBM entitlement with `spec.ceph.entitlementRef`
and names the `rhel` entitlement for node registration with
`spec.ceph.osSubscriptionRef` (managed-OS nodes would instead name it via
`MachineInstallProfile.spec.subscription`).

Bootwright registers each provided RHEL node with RHSM (from the `rhel` entry)
in the machines phase, before any Ceph work; the clusters-stage Ceph work then
enables the RHEL base/appstream repositories, installs the IBM Storage Ceph
`.repo` definition, installs and accepts the `ibm-storage-ceph-license`, and
logs in to the IBM container registry (from the `ibm-storage-ceph` entry).
Setting the `rhel` entitlement's `rhsm.management: external` instead delegates
registration to a corporate `CustomPlaybook` (see
`examples/ceph-external-rhsm`). The secret names below are declarations only;
the bytes are supplied out of band.

`storage.yaml` pins the three coordinates from the IBM release table as one
unit: IBM Storage Ceph `9.9.0.3`, package build `20.1.0-221.el9cp`, and daemon
image tag `v9.0-20201`. Keep those values on the same vendor-table row when
selecting another release. The provided-OS node in `machine.yaml` is expected
to run the compatible RHEL `9.7` release.

## Edit First

- `environment.yaml`: the fleet name and `domains.base`.
- `machine.yaml`: the provided storage node — its `ssh` address, login user, and
  `access.ssh.auth.privateKeyRef`.
- `secrets.yaml`: `ceph-cluster-ssh` (generated — the cephadm cluster key),
  `ceph-node-ssh` (points at a local private-key file), and the three
  entitlement secrets in the table below.
- `rhel.yaml`: the `redhat-rhel` `Entitlement` — RHSM organization and
  activation-key references.
- `ibm-storage-ceph.yaml`: the `ibm-storage-ceph` `Entitlement` — registry
  credential reference and `license.accept`.
- `storage.yaml`: `spec.ceph.release`, `packageVersion`, `image.version`,
  `ibm.callHome`, the cephadm bootstrap node, and the node topology.

## Credentials to supply

| Entitlement field | Secret | What it holds | How to set it |
| --- | --- | --- | --- |
| `rhel` → `rhsm.organizationRef` | `ibm-rhsm-org` | RHSM organization ID | `bootwright secret set --name ibm-rhsm-org --raw-file ./org.txt` |
| `rhel` → `rhsm.activationKeyRef` | `ibm-rhsm-activation-key` | RHSM activation key | `bootwright secret set --name ibm-rhsm-activation-key --raw-file ./key.txt` |
| `ibm-storage-ceph` → `registry.credentialsRef` | `ibm-registry-credentials` | IBM container registry login | `bootwright secret set --name ibm-registry-credentials --username cp --password-stdin` |

The registry credential is the non-obvious one: IBM Storage Ceph images are
pulled from `cp.icr.io/cp` using the fixed username **`cp`** and your **IBM
entitlement key** as the password. Obtain the key from the *Access your container
software* page in My IBM and pipe it to the command above:

```sh
printf '%s' "$IBM_ENTITLEMENT_KEY" | bootwright secret set --name ibm-registry-credentials --username cp --password-stdin
```

`license.accept: true` in the `ibm-storage-ceph` entitlement records that you
accept the IBM Storage Ceph license; Bootwright refuses to install the licensed
packages without it.

`spec.ceph.ibm.callHome: disabled` makes the IBM Call Home outbound-communication
choice explicit and keeps it disabled after bootstrap.

## Node names

These are **provided-OS** nodes (`spec.os.provided: true`), so Bootwright cannot
manage their OS hostname. `spec.ceph.topology.nodes[].name` is **required**;
Bootwright composes it into the node FQDN
(`<name>.<cluster>.<domains.storageClusters>`) that cephadm renders verbatim, so
it must resolve to and match the node's real hostname. Give each node a `name`
that matches its host (or set the host's hostname to match), or set
`nodes[].fqdn` to the node's real FQDN when it does not follow that shape.
