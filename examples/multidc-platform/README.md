# Multi-datacenter platform

This synthetic example follows the [Bootwright API](../../specs/api.md).
It contains 93 Bootwright objects across 19 of the 21 kinds, plus four native
add-on manifests. The graph has four container clusters, one storage cluster,
32 Machines, and three reusable NMState configurations. `StorageNFSExport` and
`CustomPlaybook` are not needed for this topology.

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

The CLI currently exposes a compiler stub. Full desired-state validation,
native rendering, local Secret storage, and platform lifecycle operations
remain [future implementation work](../../specs/milestones.md). Static example
checks do not establish executable API support, certificate validity, or
installation success.

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
| `Environment` | Domains, sites, graph selection, kind defaults, and shared-service policy. |
| `Entitlement` | Product-specific subscription and license intent with Secret references. |
| `Machine` | One physical or virtual machine, including named contacts and static assignments. |
| `MachineImage` | Installation media identity and an optional or required content pin. |
| `MachineInstallProfile` | OS installation, packages, device choices, and security configuration. |
| `NetworkConfig` | Reusable native NMState, installation networks, and resolver selections. |
| `InfraProvider` | One substrate implementation and its local VM/attachment profiles. |
| `InfraComponent` | One managed shared-service implementation. |
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

Environment uses `proxy.defaultRef` and explicit per-consumer `proxyRef`
choices. `direct: {}` is the opt-out. The three example consumers use the same
external proxy; adding another catalog entry cannot change that route.
Download mirrors, when needed, belong to `downloads`, separately from object
defaults. [Environment](../../specs/api/environment.md) owns the exact fields,
precedence, and service constraints.

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

Review checks cover all 97 YAML files with duplicate-key rejection; identities,
references, address/prefix and NIC mappings, native network topology, 18 VM
sizes, cluster membership, storage placement, add-on inputs, and Secret source
declarations. These are static checks while the compiler remains unimplemented.
