# Multi-datacenter platform

This synthetic example follows the [Bootwright API](../../specs/api.md).
It contains 100 Bootwright objects across 22 of the 26 kinds, plus four native
add-on manifests. The graph has four container clusters, one storage cluster,
32 Machines, and three reusable NMState configurations. `Registry`, `LoadBalancer`,
`StorageNFSExport`, and `CustomPlaybook` are not needed for this topology.

The examples explicitly show applicable defaults to make the schemas easier
to review. Optional fields remain optional; absent features and inactive union
arms are not filled merely for completeness. Existing nondefault ports,
release pins, devices, encryption choices, RSA keys, and service IDs remain
visible. The Kubernetes-style `apiVersion`, `kind`, and `metadata` envelope is
contiguous, with blank lines separating subsequent logical blocks.

All identities, domains, addresses, MACs, devices, versions, digests, and topology
are synthetic. Infrastructure IPv4 uses documentation ranges and domains use
`example.com`. Some software coordinates are deliberately fictional. This is
an API example, not a runnable deployment or evidence of release compatibility.

Desired-state validation, effective rendering, durable contexts and local
Secret storage are implemented. Native rendering and platform lifecycle
operations remain [future implementation work](../../specs/milestones.md).
Admission does not establish executable service support, certificate validity,
or installation success.

## Follow the graph

Start with [Environment](environment.yaml), a
[physical Machine](infra/machines/ocp-01/machine-01.yaml), a
[virtual Machine](infra/machines/hub-01/machine-01.yaml), and its
[shared network](infra/networkconfigs/ocp-bare-metal-network.yaml).
Then follow the [storage cluster](clusters/ceph/cluster.yaml),
[pool](clusters/ceph/pools/odf-hub-multidc-01-rbd.yaml),
[add-on definition](add-ons/fusion-data-foundation/add-on.yaml), and
[binding](clusters/hub-01/add-on-binding.yaml).

| Kind | Role in the API |
| --- | --- |
| `Environment` | Domains, sites, graph selection, kind defaults, and required controller selection. |
| `Entitlement` | Product-specific subscription and license intent with Secret references. |
| `Machine` | One physical or virtual machine, including named contacts and static assignments. |
| `MachineImage` | Installation media identity and an optional or required content pin. |
| `MachineInstallProfile` | OS installation, packages, device choices, and security configuration. |
| `NetworkConfig` | Reusable native NMState, installation networks, and resolver selections. |
| `InfraProvider` | One substrate implementation and its local VM/attachment profiles. |
| `Proxy` | Managed or external proxy connection; consumers own bypass policy. |
| `DNSServer` | Managed or external DNS service selected by network configuration. |
| `NTPServer` | Managed or external time source selected by installation policy. |
| `ArtifactServer` | Managed or external artifact endpoints; installation selects managed serving. |
| `Registry` | Managed or external registry connection and trust; not used here. |
| `LoadBalancer` | Managed or external endpoint address provider; not used here. |
| `ContainerCluster` | Container installation, node roles, endpoints, and cluster security. |
| `StorageCluster` | Storage engine and cluster-owned topology, roles, bootstrap, and devices. |
| `StoragePlacementPolicy` | An explicitly owned storage placement/protection policy. |
| `StoragePool` | An owned pool with its policy, role, and application. |
| `StorageFilesystem` | Pool composition and MDS configuration. |
| `StorageObjectGateway` | RGW service and its shared public endpoint/ingresses. |
| `StorageNFSExport` | An NFS service with export entries; not used here. |
| `StorageExport` | Consumer integration with explicitly referenced storage resources. |
| `ClusterAddon` | Reusable implementation, typed inputs, capabilities, and readiness. |
| `ClusterAddonProfile` | Ordered add-on composition. |
| `ClusterAddonBinding` | Cluster selection and values for selected add-ons' inputs. |
| `CustomPlaybook` | Reserved custom automation declaration; not used here. |
| `Secret` | Typed source or generation declaration without material values. |

## Defaults and service selection

[Kind defaults](../../specs/api/environment.md#kind-defaults) mirror the target
kind's spec. For example:

```yaml
spec:
  defaults:
    ContainerCluster:
      install:
        pullSecretRef: openshift-pull-secret
```

A cluster's explicit value wins; otherwise the Environment supplies the value
before the kind's remaining defaults. The schema defines nested-record,
collection, variant, and identity/source rules. The example retains explicit
cluster values so their resolved choices are visible.

Environment's `controller.machineRef` selects the existing
[provided local Machine](infra/machines/controller.yaml). That Machine's `proxy`
references the [external Proxy](infra/components/proxy-default.yaml) directly.
Kind defaults select the same Proxy for ContainerCluster installation and
MachineInstallProfile. Each consumer owns its `noProxy` list; `direct: {}`
replaces the complete inherited proxy choice. Adding another service cannot
change a route.

The controller is retained independently of cluster selection and remains
outside cluster node membership. The artifact server explicitly places itself
on this Machine, whose `container-runtime` capability is declared. Controller
selection does not supply service placement or install that runtime. Ordinary
Machines keep their own applicable proxy choices; Bootwright-installed
Machines may inherit their install profile's proxy, while downstream-installer
Machines use the cluster's separate installation policy.

The three DNSServer objects are selected by `NetworkConfig.spec.dns`. The
three NTPServer objects are selected by MachineInstallProfile and
ContainerCluster installation defaults. Explicit empty lists clear inherited
selections, without requesting that OS time synchronization be disabled.
Artifact consumers name [ArtifactServer](infra/components/artifact-server.yaml)
and an explicit endpoint directly. Service declarations contain no default
flags or catalog aliases. [Infrastructure services](../../specs/api/infrastructure-services.md)
own the shared selection rules. Download mirrors still belong to Environment
`downloads`, separately from object defaults.

Each cluster's `install.additionalTrustBundleRefs` directly selects its
additional installation CA bundles. All four clusters explicitly select the
same synthetic CA. A kind default can share the same field, but an authored list replaces
that default and an explicit `[]` clears it. This selection adds no controller,
Machine OS or service connection trust; Proxy and Registry trust remain on
the service objects. See
[additional installation trust](../../specs/api/container-clusters.md#additional-installation-trust).

## Machines and native networks

`NetworkConfig.spec.nmstate` keeps native bond, VLAN, bridge, DNS, and route
settings. Machines select it with `network.configRef` and declare each static
address once in `network.addresses`. An entry with `interface` assigns the
IP/prefix; one without it is a contact. Host bits are preserved, and consumers
of an address reference receive the host address without the prefix.

`installAddressRef` selects an eligible installation address independently of
secondary network assignments. Explicit NIC bindings show the exact-name
mapping that would otherwise be derived. VM profile names describe sizing;
cluster roles remain on cluster nodes. CUDN references are cluster-scoped and
therefore have no namespace. The [machine contract](../../specs/api/machines.md)
owns the constraints and also supports inline networks when reuse is unnecessary.

`ContainerCluster.security.diskEncryption.roles` selects encryption targets,
not access permissions. Omission selects all represented roles. The examples
show `master` for physical clusters and `master`, `worker`, and `infra` for hubs.
Infra uses the native worker pool, so it is not an independent encryption
boundary. See [container security](../../specs/api/container-clusters.md#security).

## Storage and add-ons

Storage children use `clusterRef` and keep explicit placement-policy and pool
references. The cluster owns Ceph topology, OSD devices, bootstrap identity,
and roles. Explicit `cephadm` and six-host OSD placements show their defaults;
the MON-only witness is excluded. The stretch placement policy shows its
four replicas and minimum of two.

The gateway's `endpoint` groups DNS, transport, TLS, and ingresses. Its HTTPS
endpoint uses one TLS Secret for certificate and key; ingress VIPs share that
transport. Ingress remains a nested service configuration, with ownership and
constraints in the [storage contract](../../specs/api/storage.md). The independent
RGW-role pool is not implicitly bound to RGW's native pool layout.

Add-on input and binding names are generic named lists. Each input declares a
resource kind or Secret type. `effects` describes how the input is consumed:
`storageExportAttachment` declares the external-storage relationship, while
`globalPullSecretMerge` describes registry-credential consumption. Effect names
are a closed vocabulary; they are not arbitrary scripts or execution authority.
Global pull-secret merging remains unsupported until its lifecycle contract is
implemented with ownership and a safe inverse.

All three OLM add-ons show their own `csvSucceeded` check and preserve additional
checks. The [add-on schema](../../specs/api/addons.md) defines readiness defaults,
explicit-list replacement, input typing, and namespace/OperatorGroup behavior.

## Secret declarations and verification

The 20 [Secret descriptors](secret-descriptors/) include 12 explicit
`source.contextStore: {}` choices and eight generated sources. Six SSH keys
retain RSA generation, the gateway certificate retains its parameters, and the
recovery token retains its entropy setting. No values are included, read,
imported, generated, encrypted, or populated by this example. The eventual
store is local to a selected Bootwright context and must be populated through
explicit Secret operations. [Secret source ownership](../../specs/api/secrets.md)
replaces the former Environment custody selector.

The artifact server's TLS descriptor is connected to its HTTPS listeners.
Its eventual certificate must cover IP `192.0.2.127` and DNS name
`utility-01.example.com`; no certificate bytes are available to verify this.
The image checksum is a quoted synthetic sentinel, not a verified hash.

The example contains 104 YAML files: 100 Bootwright declarations and four
native add-on payloads. Validate the complete graph without reading payload
or Secret bytes:

```sh
bootwright validate -f examples/multidc-platform
```

The compiler checks identities, references, address/prefix and NIC mappings,
cluster membership, storage placement, add-on inputs, and Secret source
declarations. Native network and installer release qualification remains part
of the corresponding future renderer milestone.
