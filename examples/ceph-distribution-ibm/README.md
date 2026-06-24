# IBM Storage Ceph distribution

Selects the IBM Storage Ceph distribution with `spec.ceph.distribution: ibm`.
Two `Environment.spec.entitlements[]` entries split the access by concern: a
`redhat/rhel` entry (`rhel`) holds the RHEL subscription, and an
`ibm/ibm-storage-ceph` entry (`ibm-storage-ceph`) holds the IBM registry and
license and names the `rhel` entry via `rhelEntitlementRef`. The `StorageCluster`
references the IBM entry with `spec.ceph.entitlementRef`.

Bootwright prepares each storage node by registering it with RHSM (from the
`rhel` entry), enabling the RHEL base/appstream repositories, installing the IBM
Storage Ceph `.repo` definition, installing and accepting the
`ibm-storage-ceph-license`, and logging in to the IBM container registry (from
the `ibm-storage-ceph` entry). The secret names below are declarations only; the
bytes are supplied out of band.

## Credentials to supply

| Entitlement field | Secret | What it holds | How to set it |
| --- | --- | --- | --- |
| `rhel` → `rhsm.organizationRef` | `ibm-rhsm-org` | RHSM organization ID | `bootwright secret set ibm-rhsm-org --raw-file ./org.txt` |
| `rhel` → `rhsm.activationKeyRef` | `ibm-rhsm-activation-key` | RHSM activation key | `bootwright secret set ibm-rhsm-activation-key --raw-file ./key.txt` |
| `ibm-storage-ceph` → `registry.credentialsRef` | `ibm-registry-credentials` | IBM container registry login | `bootwright secret set ibm-registry-credentials --username cp --password-stdin` |

The registry credential is the non-obvious one: IBM Storage Ceph images are
pulled from `cp.icr.io/cp` using the fixed username **`cp`** and your **IBM
entitlement key** as the password. Obtain the key from the *Access your container
software* page in My IBM and pipe it to the command above:

```sh
printf '%s' "$IBM_ENTITLEMENT_KEY" | bootwright secret set ibm-registry-credentials --username cp --password-stdin
```

`license.accept: true` in the `ibm-storage-ceph` entitlement records that you
accept the IBM Storage Ceph license; Bootwright refuses to install the licensed
packages without it.
