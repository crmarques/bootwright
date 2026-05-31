# Examples

Canonical examples are safe-to-commit desired-state input sets. They are meant
for reading, copying, and syntax validation; E2E cases under `test/e2e/` remain
the runnable host-specific test assets.

Current examples:

- `sno-libvirt-redfish`: minimal single-node OpenShift cluster on a libvirt
  provider with Redfish BMC emulation. Start here for the smallest safe
  input shape.
- `sno-libvirt-redfish-external-dns`: the minimal SNO shape with DNS provided
  by a name-resolution `InfraComponent` and selected from `NetworkConfig`.
- `sno-libvirt-redfish-external-proxy`: the minimal SNO shape with an
  operator-owned external proxy.
- `sno-libvirt-redfish-managed-proxy`: the minimal SNO shape with a
  Bootwright-managed Squid proxy.
- `sno-libvirt-redfish-disconnected-external-mirror`: disconnected install
  using an operator-owned external mirror registry.
- `sno-libvirt-redfish-managed-registry`: disconnected install using a
  Bootwright-managed mirror registry component.
- `libvirt-redfish-fleet`: compact three-node OpenShift cluster on a libvirt
  provider with Redfish BMC emulation and Bootwright-provisioned HAProxy VIPs.
- `baremetal-redfish`: the same `Environment` and `ContainerCluster`
  intent mapped to explicit bare-metal inventory with operator-owned external
  VIPs.
- `baremetal-redfish-postinstall`: the bare-metal Redfish shape plus
  declarative post-install OpenShift Virtualization resources.
- `baremetal-redfish-odf-external-ceph`: a three-node bare-metal Redfish
  cluster with Red Hat OpenShift Data Foundation bound to imported external
  Ceph details through `StorageClusterBinding`, plus OpenShift Virtualization.
- `baremetal-redfish-fusion-external-ceph`: a three-node bare-metal Redfish
  cluster with IBM Fusion Data Foundation bound to imported IBM Storage Ceph
  details through `StorageClusterBinding`, plus OpenShift Virtualization.
- `baremetal-redfish-fleet-stretched-ceph-data-foundation`: two compact
  bare-metal Redfish clusters bound through IBM Fusion Data Foundation to a
  Bootwright-provisioned seven-node Ceph stretch cluster.
- `baremetal-redfish-fleet-imported-ceph-data-foundation`: two compact
  bare-metal Redfish clusters bound through OpenShift Data Foundation to a
  previously provisioned external Ceph cluster by loading exporter JSON with
  `bootwright secret set shared-ceph-external-details --raw-file <path>`.
- `baremetal-redfish-virtualized-child`: a bare-metal SNO parent with
  OpenShift Virtualization and a KubeVirt-backed SNO child cluster.
- `baremetal-redfish-fleet`: two bare-metal Redfish clusters sharing common
  services and provider inventory through a subdirectory layout.
- `baremetal-redfish-fleet-postinstall`: the two-cluster bare-metal Redfish
  fleet shape plus declarative OpenShift Virtualization and OpenShift GitOps
  post-install extensions.

The two examples intentionally keep `environment.yaml` and
`clusters/<cluster>/container-cluster.yaml` byte-identical. Provider swaps should normally change
substrate-owned files only: `shared/hosts.yaml`, `shared/networks.yaml`, `shared/provider.yaml`, and
`clusters/<cluster>/cluster-infra.yaml`, plus `shared/infra-component.yaml` when service placement or
routable service endpoints change.

## Variant Deltas

Provider swap invariant:

| Variant | Files that change | Files that should not change |
| --- | --- | --- |
| `libvirt-redfish-fleet` to `baremetal-redfish` | `shared/hosts.yaml`, `shared/networks.yaml`, `shared/provider.yaml`, `clusters/<cluster>/cluster-infra.yaml`, `shared/infra-component.yaml` | `environment.yaml`, `clusters/<cluster>/container-cluster.yaml` |

Single-node mode variants:

| Variant | Changed files and owning fields |
| --- | --- |
| `sno-libvirt-redfish-external-dns` | `environment.yaml` selects name resolution; `shared/networks.yaml` uses `dnsRefs`; `shared/infra-component.yaml` defines the name-resolution service |
| `sno-libvirt-redfish-external-proxy` | `environment.yaml` adds an external proxy connection and `proxyFor` audiences |
| `sno-libvirt-redfish-managed-proxy` | `environment.yaml` selects a managed proxy; `shared/infra-component.yaml` places the Squid service |
| `sno-libvirt-redfish-disconnected-external-mirror` | `clusters/<cluster>/container-cluster.yaml` sets `install.mode: disconnected`; `environment.yaml` declares mirror registry URL, trust, and artifact cluster-install route |
| `sno-libvirt-redfish-managed-registry` | `clusters/<cluster>/container-cluster.yaml` sets `install.mode: disconnected`; `environment.yaml` selects managed registry and trust; `shared/infra-component.yaml` places the registry |

Copy an example to a working directory before editing it for a real
environment, then import that copy:

```text
cp -a examples/sno-libvirt-redfish ./my-sno-lab
bootwright check syntax -f ./my-sno-lab
bootwright context init lab -f ./my-sno-lab
bootwright secret list
bootwright render installer --scope <cluster-name>
bootwright render --output-dir ./rendered --scope <cluster-name> --sensitive
```

The external CLI export writes OpenShift installer files under
`./rendered/openshift-install/<cluster-name>/`.
