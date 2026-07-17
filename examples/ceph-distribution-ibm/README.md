# IBM Storage Ceph distribution

Selects the IBM Storage Ceph distribution with `spec.ceph.distribution: ibm`.
Two `Entitlement` objects split the access by concern: a `redhat-rhel`
entitlement (`rhel`) holds the RHEL subscription, and an `ibm-storage-ceph`
entitlement (`ibm-storage-ceph`) holds the IBM registry and license and names
the `rhel` entitlement via `rhelEntitlementRef`. The `StorageCluster` references
the IBM entitlement with `spec.ceph.entitlementRef`.

Bootwright registers each provided RHEL node with RHSM (from the `rhel` entry)
in the machines phase, before any Ceph work; the clusters-stage Ceph work then
enables the RHEL base/appstream repositories, installs the IBM Storage Ceph
`.repo` definition, installs and accepts the `ibm-storage-ceph-license`, and
logs in to the IBM container registry (from the `ibm-storage-ceph` entry).
Setting the `rhel` entitlement's `rhsm.management: external` instead delegates
registration to a corporate `ProvisioningPlaybook` (see
`examples/ceph-external-rhsm`). The secret names below are declarations only;
the bytes are supplied out of band.

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

## Node hostnames

These are **provided-OS** nodes (`spec.os.provided: true`), so Bootwright cannot
set an FQDN on them: `spec.ceph.topology.hosts[].hostname` defaults to the
**bare machine name** (here `ceph-0`), which cephadm renders verbatim and must
equal the node's real hostname. Name your host to match, or author
`spec.ceph.topology.hosts[].hostname` explicitly.
