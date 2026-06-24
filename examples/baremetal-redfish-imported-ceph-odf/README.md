# Bare-Metal Redfish Imported Ceph ODF

Bare-metal OpenShift consuming an imported Ceph cluster through Red Hat OpenShift
Data Foundation external mode.

## Edit First

- `environment.yaml`: base domain, `resources`, secret names, artifact server
  catalog, and the imported Ceph external-details secret reference.
- `infra/`: service host, bare-metal provider, artifact server, and network
  definitions.
- `clusters/container/metal-ocp/cluster-machines.yaml`: VIPs, artifact endpoint,
  machine selection, and platform render mode.
- `clusters/container/metal-ocp/cluster.yaml`: release, cluster networking, and
  node bindings.
- `clusters/storage/imported-ceph/*.yaml`: imported storage cluster and
  Data Foundation export surface.
- `add-ons/openshift-data-foundation.yaml` and
  `clusters/container/metal-ocp/add-on-binding.yaml`: Data Foundation channel,
  readiness, storage-export input, and external-details secret binding.

## Validate And Apply

Before importing, provide
`<input-dir>/secrets/imported-ceph-odf-details.json` as a local unversioned
file matching the `file:` secret source in `environment.yaml`.

```text
bootwright validate -f <input-dir>
bootwright context init --name lab -f <input-dir>
bootwright secret generate
bootwright bastion setup --yes
bootwright preflight all
bootwright plan
bootwright apply --yes
bootwright status --watch
```

Use `bootwright apply --stage clusters --clusters metal-ocp,imported-ceph --yes`
for focused recovery after the full graph has been reviewed.
