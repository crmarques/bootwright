# Bare-Metal Multi-DC Virtualized ODF Ceph

This reference example shows the full canonical layout with:

- two three-node bare-metal OpenShift parent clusters in separate data centers;
- one three-master plus three-worker KubeVirt-hosted OpenShift child cluster on each parent;
- one managed stretched Ceph cluster spanning two data sites plus a tiebreaker;
- OpenShift Data Foundation bound to all four container clusters for block, file, and object storage;
- OpenShift Virtualization bound only to the two parent clusters;
- child cluster namespace and NAD manifests delivered as a cluster add-on.

Defaulted fields are intentionally present with short comments so authors can see the available surface and omit those values in smaller input sets.

## Edit First

- `environment.yaml`: base domain, resource directories, secret names, and
  shared service catalog entries.
- `infra/`: provider hosts, bare-metal and KubeVirt providers, network
  attachments, artifact services, and network templates.
- `clusters/container/*/cluster-infra.yaml`: VIPs, artifact endpoints, machine
  selections, KubeVirt network bindings, and platform render mode.
- `clusters/container/*/cluster.yaml`: OpenShift release, networking, and node
  bindings for parent and child clusters.
- `clusters/storage/ceph-storage/*.yaml`: Ceph topology, pools, filesystems,
  RGW, exports, and placement policy.
- `add-ons/*.yaml` and `clusters/container/*/add-on-binding.yaml`: bootstrap
  add-ons, capability providers, storage inputs, and profile bindings.

## Validate And Apply

```text
bootwright validate -f <input-dir>
bootwright context init lab -f <input-dir>
bootwright secret generate
bootwright apply bastion --yes
bootwright check all
bootwright plan
bootwright apply --yes
bootwright status --watch
```

Use `--clusters` only for focused recovery. Child KubeVirt clusters must be
selected with their parent cluster unless parent install and Virtualization
readiness records already exist.
