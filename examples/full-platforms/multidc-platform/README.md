# Multi-datacenter platform

This synthetic graph is an authoring reference for a future platform: four
OpenShift clusters, one stretch Ceph cluster, physical and KubeVirt Machines,
shared networks and storage-dependent add-ons. It deliberately contains
unsupported features and fictional software coordinates. **It does not pass
current validation and cannot be deployed as a complete Bootwright platform.**
All addresses, identities, credential references, devices and topology are
synthetic; consult the [API owners](../../../specs/index.md) and
[milestone status](../../../specs/milestones.md) for current contracts and
availability.

## Follow the graph

Start at [Environment](environment.yaml), then follow:

| Area | Representative inputs |
| --- | --- |
| Controller and substrate | [Local controller](infra/machines/controller.yaml), [physical provider](infra/providers/physical.yaml), [KubeVirt provider](infra/providers/kubevirt-ocp-01.yaml). |
| Machine and network | [Physical node](infra/machines/ocp-01/machine-01.yaml), [virtual node](infra/machines/hub-01/machine-01.yaml), [shared NMState network](infra/networkconfigs/ocp-bare-metal-network.yaml). |
| Container cluster | [Physical OpenShift cluster](clusters/ocp-01/cluster.yaml), [virtual hub cluster](clusters/hub-01/cluster.yaml). |
| Storage | [Cluster](clusters/ceph/cluster.yaml), [placement policy](clusters/ceph/placement-policy.yaml), [pool](clusters/ceph/pools/odf-hub-multidc-01-rbd.yaml), [consumer](clusters/ceph/consumers/odf-hub-multidc-01.yaml). |
| Add-ons | [Built-in package contract](../../../specs/api/addons.md#package-fdf), [profile](add-ons/profiles/physical-cluster-addons.yaml), [binding](clusters/hub-01/add-on-binding.yaml). |

The native Kubernetes files under `add-ons/virtual-cluster-network/manifests/`
are referenced payloads, rather than Bootwright objects. Input discovery excludes
them from the Bootwright object stream.

## Defaults and service selection

Environment defaults share installation pull-secret, proxy and NTP choices.
An explicit object value wins; an explicit list replaces its inherited list,
and `direct: {}` replaces an inherited proxy choice. See
[kind defaults](../../../specs/api/environment.md#kind-defaults).

`controller.machineRef` selects the provided local controller independently of
cluster membership. Managed artifact serving explicitly selects that Machine;
controller selection does not imply placement. Machine, cluster and profile
proxy choices remain separate. NetworkConfig selects DNS; installation policy
selects NTP; media consumers select an ArtifactServer endpoint. These selections
are illustrated by the [service declarations](infra/components/) and governed
by [infrastructure services](../../../specs/api/infrastructure-services.md).

Each cluster selects its own `additionalTrustBundleRefs`. Those bundles supply
installation trust only; they do not configure controller, Machine or service
trust. The current cluster installer also refuses the proxy, FIPS and disk
encryption choices shown here, even where schema admission accepts them.

## Machines and native networks

Machines select reusable `NetworkConfig.spec.nmstate` and declare their static
IP/prefix once under `network.addresses`. `installAddressRef` chooses the
installation contact. NIC bindings map native interfaces to substrate NICs;
keep assignments, routes and interface names consistent when authoring a copy.
The [Machine contract](../../../specs/api/machines.md) owns these rules.

KubeVirt network references name cluster-scoped CUDNs without namespaces. The
associated add-on payloads illustrate intended attachment topology; they do not
establish a currently realized KubeVirt substrate. Physical OpenShift node
installation also remains refused by the
[cluster implementation](../../../specs/openshift-clusters.md#selection-and-refusal).

## Storage and add-ons

The tracked `entitlements/ceph.yaml` sets `license.accept: true` as a
placeholder; review the IBM license terms and accept them yourself in a copy.

Storage children select `clusterRef` explicitly and reference their placement
policies and pools. The cluster's native MON/MGR/OSD services use registered
hostnames; the witness belongs to MON placement without OSD devices. The stretch
policy expresses four replicas with a minimum of two. The
[filesystem](clusters/ceph/filesystems/odf-cephfs.yaml) declares its rank and
standby policy and pools, bound by `mdsServiceRef` to its native MDS service.
The [object gateway](clusters/ceph/object-gateways/odf-rgw-all.yaml) binds the
cluster's native RGW service through `serviceRef`; its endpoint is the HTTPS
ingress whose backend is that service. The gateway, RGW and ingress services have a native lifecycle; this
reference's live qualification remains unrun. Admission also accepts the native observability services and the
Grafana Secret binding; planning refuses each service that needs an auxiliary
image until that image can be selected. Managed CephConsumers now use the built-in consumer lifecycle;
see [storage API](../../../specs/api/ceph.md) and
[native service qualification](../../../specs/ceph-native-api.md).

The four Data Foundation bindings select the built-in `fdf` package and
separate managed CephConsumers. Each declares an owned catalog, channel and exact
starting CSV; replace the synthetic digest and versions with verified vendor
choices. Include IBM registry credentials in the cluster installation pull
secret. Bootwright does not merge the global pull secret. Each cluster selects
its CA bundle under `Always` for the TLS gateway. Other authored add-ons remain
future lifecycle intent; see the [add-on contract](../../../specs/api/addons.md).

## Secret declarations and verification

[Secret descriptors](secret-descriptors/) contain source/generation declarations
only. Context-store sources require operator-supplied material; generated
sources retain their explicit key and certificate parameters. This authoring
example needs no real Secret material, context, media import or controller
setup to inspect its expected validation refusals. Never populate tracked
inputs with keys, certificates, registry credentials or pull secrets. Image
hashes and package coordinates here are illustrative, not verified release
pins.

Build and validate from the repository root:

```sh
make build
./bin/bootwright validate -f examples/full-platforms/multidc-platform
```

The expected result is failure with explicit diagnostics for:

- External endpoint sources on the two baremetal OpenShiftClusters.
- Stretch topology and manager-module intent.
- Unqualified global Ceph options, including missing members of the currently
  qualified pool-default pair.
- A FIPS Ceph node without a managed FIPS installation profile.
- Agent-install bridge interfaces on the two physical OpenShiftClusters.
- Unrealized-declaration warnings for the KubeVirt providers and the authored
  add-on profile selections.

The exact diagnostic set is pinned by `TestActualCLIRefusesFutureReferenceFeatures`
in [admission acceptance tests](../../../cmd/bootwright/admission_acceptance_test.go).
Resolving these admission errors alone would not make the graph deployable:
physical OpenShift, KubeVirt, security choices and add-on realization have
further lifecycle limits. Keep this graph for schema and dependency review;
use the [single-node OpenShift](../../openshift-clusters/lab-sno/README.md) or
[three-node Ceph](../../ceph-clusters/libvirt-3-nodes-ceph/README.md)
runbooks for the implemented example journeys.
