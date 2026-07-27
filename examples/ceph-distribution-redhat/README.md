# Red Hat Ceph Storage distribution

Selects Red Hat Ceph Storage with `spec.ceph.distribution: redhat` and resolves
all subscription and registry material through one `Entitlement` object
(`rhcs`, type `redhat-ceph`), referenced from the `StorageCluster` with
`spec.ceph.entitlementRef`.

The StorageCluster selects Red Hat Ceph Storage 9.1 and pins both build axes:
`spec.ceph.packageVersion` fixes the `cephadm` RPM the nodes install, and
`spec.ceph.image.version` fixes the daemon container build. Read both off Red
Hat's own release-to-package-version table — Bootwright keeps no such table and
takes whatever you write verbatim. `spec.ceph.image.base` is left unset, so the
vendor repository is derived from the distribution and release and cannot drift
from them. `examples/ceph-external-rhsm` uses the 9.0 stream instead, to
demonstrate delegated registration.

Bootwright registers each provided RHEL node with RHSM in the machines phase,
before any Ceph work; the clusters-stage Ceph work then enables the RHEL
base/appstream and `rhceph-*-tools` repositories and logs in to
`registry.redhat.io` — all from this entitlement. Setting
`rhsm.management: external` instead delegates registration to a corporate
`Playbook` (see `examples/ceph-external-rhsm`). The secret names
below are declarations only; the bytes are supplied out of band.

## Credentials to supply

| Entitlement field | Secret | What it holds | How to set it |
| --- | --- | --- | --- |
| `rhsm.organizationRef` | `redhat-org` | RHSM organization ID | `bootwright secret set --name redhat-org --raw-file ./org.txt` |
| `rhsm.activationKeyRef` | `redhat-activation-key` | RHSM activation key | `bootwright secret set --name redhat-activation-key --raw-file ./key.txt` |
| `registry.credentialsRef` | `redhat-registry-credentials` | `registry.redhat.io` login | `bootwright secret set --name redhat-registry-credentials --username '<sa-user>' --password-stdin` |

Use a **registry service account** for the registry credential rather than a
personal login: create one at <https://access.redhat.com/terms-based-registry/>,
then supply its username (`<numeric>|<name>`) and token:

```sh
printf '%s' "$RH_REGISTRY_TOKEN" | bootwright secret set --name redhat-registry-credentials --username '12345678|ceph-node' --password-stdin
```

Generate the RHSM activation key and find your organization ID under
*Activation Keys* at <https://console.redhat.com/insights/connector/activation-keys>.
