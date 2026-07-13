# Bare-Metal Redfish Imported Ceph ODF

Bare-metal OpenShift consuming an imported Ceph cluster through Red Hat OpenShift
Data Foundation external mode.

## Edit First

- `environment.yaml`: base domain, `resources`, and artifact server catalog.
- `secrets.yaml`: the `Secret` objects (`openshift-pull-secret`,
  `bmc-credentials`, the cluster SSH key, and `imported-ceph-odf-details`, which
  supplies the imported Ceph external-cluster details from a local `file:`).
- `infra/`: service host, bare-metal provider, artifact server, and network
  definitions.
- `clusters/container/metal-ocp/cluster-machines.yaml`: the node `Machine` — NIC
  MAC, static IP, BMC Redfish address and credential reference, and root device
  hints.
- `clusters/container/metal-ocp/cluster.yaml`: OpenShift release,
  `install.endpoints` (API/ingress addresses), the agent artifact endpoint, and
  host→machine bindings.
- `clusters/storage/imported-ceph/cluster.yaml` and `export.yaml`: the imported
  `StorageCluster` and the `StorageExport` naming the external-details secret
  (`imported-ceph-odf-details`).
- `add-ons/openshift-data-foundation/add-on.yaml` (with its `manifests/`
  subtree) and `clusters/container/metal-ocp/add-on-binding.yaml`: Data
  Foundation channel, readiness, storage-export input, and external-details
  secret binding.

## Validate And Apply

Before importing, provide
`<input-dir>/secrets/imported-ceph-odf-details.json` as a local unversioned
file matching the `file:` secret source in `secrets.yaml`. Set the operator
secrets `openshift-pull-secret` and `bmc-credentials` before `secret generate`.

```text
bootwright validate -f <input-dir>
bootwright context init --name lab -f <input-dir>
bootwright secret set --name openshift-pull-secret --pull-secret <path>
printf '%s\n' "${BMC_PASS}" | bootwright secret set --name bmc-credentials --username "${BMC_USER}" --password-stdin
bootwright secret generate
bootwright bastion setup --yes
bootwright preflight all
bootwright plan
bootwright apply --yes
bootwright status --watch
```

Use `bootwright apply --stage clusters --clusters metal-ocp,imported-ceph --yes`
for focused recovery after the full graph has been reviewed.
