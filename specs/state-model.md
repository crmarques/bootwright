# Desired-State Model

Bootwright desired state uses `apiVersion: bootwright.io/v1alpha1` and sixteen
user-authored kinds. The provisioning schema intentionally tracks the inputs
consumed by `openshift-install` for agent installs and `cephadm` for external
Ceph storage:

- `ContainerCluster` owns install intent and the fields that render mostly to
  `install-config.yaml`.
- `ClusterAddon` owns reusable post-install bootstrap components applied
  inside installed OpenShift or OKD clusters, including optional input APIs
  and native effects.
- `ClusterAddonProfile` owns ordered reusable platform profiles made from
  add-ons and nested profiles.
- `ClusterAddonBinding` owns one cluster's post-install add-ons, profiles, and
  binding-scoped add-on input values.
- `StorageCluster` owns external storage cluster provisioning intent.
- `StoragePlacementPolicy` owns storage placement and CRUSH policy intent.
- `StoragePool` owns Ceph pool desired state.
- `StorageFilesystem` owns CephFS metadata/data pool wiring and MDS placement.
- `StorageObjectGateway` owns RGW service and refs to public and cephadm
  ingress endpoints.
- `StorageExport` owns the exported storage surface. Consuming clusters select
  it through add-on inputs that declare storage-export attachment effects.
- `ClusterInfra` owns the selected machines, endpoint definitions, platform
  render mode, and logical-to-substrate network bindings.
- `NetworkConfig` owns reusable `machineNetwork[]`, name-resolution service
  selections, and the NMState template rendered into `agent-config.yaml`.
- `InfraProvider` owns substrate inventory, network attachments, and
  capabilities.
- `InfraComponent` owns host-bound shared infra services and their routable
  endpoints.
- `Environment` owns fleet-wide defaults, context resource selection, secret
  sources, proxy defaults, registry mirrors, and Bootwright component image
  pins.
- `Host` owns SSH reachability and named addresses for durable machines
  Bootwright or managed tools SSH into, including provider hosts, service
  hosts, external Ceph admin hosts, and managed Ceph nodes.

Post-install components are deliberately not embedded under
`ContainerCluster.spec.install`. `ContainerCluster` remains focused on
provisioning an installed cluster; add-ons are selected by `Environment`,
bound to clusters, and applied after the cluster is available.
External storage is also a peer object family, not a child of
`ContainerCluster`. Storage clusters reuse `ClusterInfra`, `InfraProvider`, and
`NetworkConfig` for machine facts while keeping Ceph pools, filesystems,
gateways, exports, and add-on input effects in their owning resources.

There is no compatibility layer for the pre-refactor shape. Old fields are
decode or validation errors.

## Environment

`Environment` is fleet-wide. It contains defaults, optional context input
resource selection, and secret references, never secret bytes.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: lab
spec:
  baseDomain: example.test

  resources:
    - hosts.yaml
    - networks.yaml
    - provider.yaml
    - infra-component.yaml
    - cluster-infra.yaml
    - cluster.yaml
    - add-ons/openshift-virtualization.yaml
    - add-ons/platform-profile.yaml
    - clusters/container/demo/add-on-binding.yaml

  secrets:
    - openshift-pull-secret
    - prod-3node-cluster-admin-ssh-key:
        generated:
          sshKeyPair:
            comment: bootwright-prod-3node-cluster-admin
    - provider-host-ssh:
        file: ~/.ssh/bootwright-ssh-key
    - bmc-credentials
    - proxy-credentials:
        generated:
          credentials:
            username: proxy
    - mirror-registry-credentials
    - mirror-registry-ca:
        file: ../secrets/mirror-registry-ca.crt
    - corp-root-ca:
        file: ../secrets/corp-root-ca.crt
    - prod-api-tls:
        file: ../secrets/prod-api-tls.crt
        keyFile: ../secrets/prod-api-tls.key
    - prod-apps-wildcard-tls

  containerClusters:
    - prod-3node

  infraComponents:
    proxies:
      - name: default
        type: external
        connection:
          httpProxy: http://proxy.example.test:3128
          httpsProxy: http://proxy.example.test:3128
          noProxy:
            - .example.test
            - 192.168.133.0/24
          auth:
            proxyAuthRef:
              name: proxy-credentials
    nameResolution:
      - name: default-01
        type: external
        ip: 192.168.133.53
    artifactServers:
      - name: default
        type: managed
        componentRef:
          name: artifact-server
    ntpSources:
      - name: default-01
        type: external
        address: ntp.example.test
      - name: lab-ntp
        type: managed
        componentRef:
          name: ntp-server
        endpoint: cluster

  proxyFor:
    bootwright: default
    containerClusterInstall: default

  registries:
    mirror:
      url: registry.example.test:5000
      credentialsRef:
        name: mirror-registry-credentials
      trustBundleRef:
        name: mirror-registry-ca

  installTrust:
    caBundleRefs:
      - name: corp-root-ca

```

Rules:

- Authored desired-state YAML uses block-style collections. Do not use
  flow-style mapping braces, inline lists, or empty inline maps in examples,
  e2e inputs, fixtures, or scaffold output.
- `resources[]`, when set, is a YAML file or directory allow-list relative to the
  `Environment` file directory. The `Environment` file itself is always
  loaded.
- When `resources[]` is omitted, the current context input directory loads
  every discovered YAML file.
- A listed file is loaded as a complete YAML file. A listed directory is walked
  deterministically for YAML files. Every Bootwright resource referenced by any
  selected resource must also be selected.
- When a selected `ClusterAddonBinding` references addon profiles or
  add-ons, those referenced resource files must also be selected. When a
  selected `ClusterAddonProfile` references child profiles or add-ons, those
  files must also be selected.
- When a selected storage object references a `StorageCluster`,
  `StoragePlacementPolicy`, `StoragePool`, `StorageFilesystem`,
  `StorageObjectGateway`, `StorageExport`, or storage
  `ClusterInfra`, those referenced resource files must also be selected.
- `containerClusters[]`, when set, is the effective fleet selection list for
  render, apply, status, destroy, and check flows. Omitted means every loaded
  `ContainerCluster`.
- Cluster selection filters add-on bindings to selected clusters and omits
  add-on resources that are reachable only from removed bindings.
- `defaults.install.pullSecretRef`, when set, is copied into selected
  OpenShift `ContainerCluster` install specs that omit `pullSecretRef`.
  Omitted means the conventional `openshift-pull-secret` secret name.
- `defaults.install.nodeSSH`, when set, is copied into selected
  `ContainerCluster` install specs that omit `nodeSSH`. Omitted means
  `keyPairRef.name: <container-cluster-name>-cluster-admin-ssh-key`.
- `defaults.artifactAccess`, when set, is copied into selected
  `ClusterInfra.spec.artifactAccess` fields only for active artifact
  consumers. Redfish virtual-media defaults apply only to cluster
  infrastructures used by bare-metal `ContainerCluster` machines.
  `containerClusterInstall` defaults apply only to cluster infrastructures used
  by disconnected `ContainerCluster` installs. `serverRef` is copied only when
  an explicit or defaulted artifact consumer endpoint exists. Explicit
  `ClusterInfra.spec.artifactAccess` values always win.
- `defaults` is a typed fleet-default surface. Defaults are normalized into the
  owning resource before validation and rendering, fill omitted values only,
  and must not contain provider inventory, machine facts, endpoint addresses,
  or secret bytes.
- Bootwright and OpenShift installer actions run on the bastion host where the
  CLI is invoked. Desired state does not select that execution host.
- `infraComponents.proxies[]`, `infraComponents.nameResolution[]`,
  `infraComponents.artifactServers[]`, `infraComponents.registries[]`, and
  `infraComponents.ntpSources[]` are the environment service access catalog.
  Component entries are either `external` with direct access configuration, or
  `managed` with `componentRef.name` pointing at an `InfraComponent` arm of
  the matching kind.
- External artifact server entries use named `endpoints[]` with `url`.
  Managed artifact server entries use `componentRef.name` to reference
  `InfraComponent.spec.artifactServer`.
- External NTP source entries use `address`. Managed NTP source entries use
  `componentRef.name` plus an optional `endpoint` selector. Resolved values
  feed installer `additionalNTPSources` in authored order with duplicates
  removed.
- `infraComponents.nameResolution[].additionalIngressHosts[]` is optional.
  Values from environment entries and managed
  `InfraComponent.spec.nameResolution.additionalIngressHosts[]` merge into DNS
  host records that point at each consuming cluster's ingress VIP.
- `proxyFor.bootwright` and `proxyFor.containerClusterInstall` select proxy
  catalog entries by name. Proxy entries do not accept `default`. Omitted
  values default to `none`; `none` is a reserved value that disables proxy use
  for that consumer.
- A `proxyFor.bootwright` selection may point at an external proxy for every
  command phase. If it points at a managed proxy, Bootwright must not depend on
  that proxy during `apply bastion` because the proxy component does not exist
  until infrastructure convergence. Later checks, renders, applies, and status
  views may use the managed proxy after its selected `InfraComponent` has been
  converged; pre-infra commands should report that bootstrap limitation rather
  than silently pretending the proxy is active.
- `NetworkConfig.spec.dnsRefs[]` selects name-resolution catalog entries by
  name. Name-resolution entries do not accept `default`.
- `infraComponents.artifactServers[]` is a catalog. Clusters select an artifact
  server through `ClusterInfra.spec.artifactAccess.serverRef.name`.
- `infraComponents.registries[]` infers its single entry as selected. When the
  list has multiple entries, at most one entry may set `default: true`.
- `secretStorage.mode` defaults to `source`. `source` preserves each declared
  secret source at runtime: `file:` resolves to the declared local path and
  context-local/generated material resolves under the context secrets directory.
  `context` makes workflows resolve declared material from
  `<context>/secrets/` after `bootwright secret materialize` copies file-sourced
  entries into context storage.
- `secrets` declares names, not bytes. It is a list where each item is either
  a scalar secret name or a single-key object whose key is the secret name.
  Scalar items resolve to `<context>/secrets/<name>` and must be populated
  with `bootwright secret set` before the consuming workflow runs. `generated:`
  resolves under the context secrets directory; generated credentials may be
  populated with either `bootwright secret set` or
  `bootwright secret generate`.
- `generated.sshKeyPair` creates an SSH private key at
  `<context>/secrets/<name>` and the matching OpenSSH public key at
  `<context>/secrets/<name>.pub`; only `type: ed25519` is accepted.
- `generated.credentials` creates one `username:password` line at
  `<context>/secrets/<name>`. `username` defaults to `admin` and must not
  contain whitespace, newlines, or `:`.
- `generated.selfSignedCertificate` creates a PEM certificate at
  `<context>/secrets/<name>` and a PEM key at `<context>/secrets/<name>.key`.
  `commonName` is required; `dnsNames[]`, `ipAddresses[]`, and
  `validityDays` are optional, and `validityDays` defaults to `3650`.
- TLS-pair consumers use the certificate at `file:` plus the private key at
  `keyFile:` for file-sourced secrets. Context-local and generated TLS pairs
  resolve as `<context>/secrets/<name>` and `<context>/secrets/<name>.key`.
- `installTrust.caBundleRefs[]` is optional fleet-wide CA trust rendered into
  every selected cluster install. Entries reference PEM CA bundle secrets.
- External proxy `connection` entries use installer field names:
  `httpProxy`, `httpsProxy`, and `noProxy`.
- `install.mode: disconnected` on any `ContainerCluster` requires mirror trust
  material and either an external mirror URL or a managed registry component.
- `registries.imageDigestSources[]`, when set, renders into installer
  `imageDigestSources`. Each entry requires `source` and at least one
  `mirrors[]` value. `sourcePolicy`, when set, must be `NeverContactSource`
  or `AllowContactingSource`.
- `componentImages` is a closed override map for Bootwright-managed component
  images. Each `local` or `public` reference must be pinned to an explicit
  version tag or digest; omitted tags, non-version tags, and `:latest` are
  invalid.
- `infraComponents.ntpSources[]` is optional. Each entry must have a unique
  `name` and `type: external` or `type: managed`. External entries require
  `address`; managed entries require a matching `InfraComponent.spec.ntp`
  reference and may select one of that component's endpoints.

## ContainerCluster

`ContainerCluster` is the provider-neutral install request. It owns
distribution, release, install mode, pools, cluster networking, and the node to
machine binding.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: ContainerCluster
metadata:
  name: prod-3node
spec:
  distribution:
    release:
      version: 4.20.15

  install:
    endpointRefs:
      api:
        name: api
      apiInt:
        name: api-int
      ingress:
        name: apps
    additionalTrustBundleRefs:
      - name: cluster-extra-ca
    nodeSSH:
      keyPairRef:
        name: prod-3node-cluster-admin-ssh-key
    servingCertificates:
      apiServer:
        namedCertificates:
          - names:
              - api.prod-3node.example.test
            secretRef:
              name: prod-api-tls
      ingress:
        defaultCertificateRef:
          name: prod-apps-wildcard-tls

  networking:
    networkType: OVNKubernetes
    clusterNetwork:
      - cidr: 10.128.0.0/14
        hostPrefix: 23
    serviceNetwork:
      - 172.30.0.0/16

  nodes:
    - hostname: master-0
      role: master
      infraNodeRef:
        clusterInfra: prod-3node-infra
        name: master-0
    - hostname: master-1
      role: master
      infraNodeRef:
        clusterInfra: prod-3node-infra
        name: master-1
    - hostname: master-2
      role: master
      infraNodeRef:
        clusterInfra: prod-3node-infra
        name: master-2
```

Rules:

- `install.mode` defaults to `connected`.
- `install.method` defaults to `agent`; other methods are not accepted yet.
- `install.endpointRefs.api`, `install.endpointRefs.apiInt`, and
  `install.endpointRefs.ingress` explicitly bind OpenShift install roles to
  named entries in the referenced `ClusterInfra.spec.endpoints` map.
  The endpoint source defaults to `openshift` for these refs, so multi-node
  bare-metal installs can omit `source.type` when OpenShift owns the VIP.
- `install.nodeSSH` selects the SSH key material authorized during
  install and, when available, the private key Bootwright uses for
  post-install node SSH probes. `keyPairRef` names one `SecretRef`
  that owns both halves. For split ownership, use `publicKeyRef` for
  `install-config.yaml` and optional `privateKeyRef` for local probe access.
  A node SSH spec must use either `keyPairRef` or
  `publicKeyRef`/`privateKeyRef`, not both.
- `install.baseDomain`, `install.imageDigestSources`,
  `install.installConfigOverrides`, and `install.agentConfigOverrides` are not
  user API. Base domain belongs to `Environment.spec.baseDomain`; registry
  mirror sources belong to `Environment.spec.registries.imageDigestSources`;
  rendered installer files are Bootwright-owned generated output.
- `distribution.type` is `openshift` or `okd`; omitted means `openshift`.
- OpenShift requires either `release.version` or `release.image`.
- `release.image`, when set, must be pinned to an explicit version tag or
  digest; omitted tags, non-version tags, and `:latest` are invalid.
- For OpenShift only, omitted `release.channel` is derived as
  `stable-<major>.<minor>` when an exact version is supplied and no explicit
  release image is set.
- OKD does not derive an OpenShift channel and does not require a Red Hat pull
  secret by default. Prefer explicit OKD release images for exact installs.
- `additionalTrustBundleRefs[]` is cluster-scoped CA trust. Effective
  `additionalTrustBundle` order is environment `installTrust`, mirror registry
  trust, then cluster refs, de-duplicated by secret name.
- `servingCertificates.apiServer.namedCertificates[]` renders OpenShift API
  serving certificate Secrets plus `APIServer/cluster`. `names[]` is required
  and must not target the internal `api-int.<cluster>.<baseDomain>` endpoint.
- `servingCertificates.ingress.defaultCertificateRef` renders the default
  ingress certificate Secret plus `IngressController/default`. The certificate
  must cover `*.apps.<cluster>.<baseDomain>`.
- Every OpenShift node binds to a provider-sourced `ClusterInfra` node by
  `nodes[].infraNodeRef.clusterInfra` and `nodes[].infraNodeRef.name`.
- In v1, all nodes in one cluster must reference the same `ClusterInfra`.
- `networking` is required. `clusterNetwork[]` and `serviceNetwork[]` must
  each contain at least one valid CIDR. Each `clusterNetwork[].hostPrefix` is
  required and must be greater than the CIDR prefix length and no larger than
  the address width. `networkType` is optional and, when set, must not contain
  leading or trailing whitespace; Bootwright does not enumerate CNI names so
  future OpenShift network types remain possible.

## Storage

The first managed storage implementation provisions Ceph with `cephadm` on
OS-installed hosts. Storage nodes are named in a storage-only `ClusterInfra`
and each matching infra node must use `source.hostRef` to point at a `Host`.
`Host` owns durable SSH connection details and named addresses. Storage nodes
are assumed to already run RHEL and be reachable by the Ansible storage layer
from the bastion over SSH.

Full-managed storage, where Bootwright customizes a RHEL ISO/kickstart, boots
bare metal through BMCs, installs RHEL, and then runs the managed Ceph flow, is
recognized as `spec.management: fullManaged` but apply and plan fail clearly
because that infrastructure path is not available yet.

`StorageCluster.spec.management` defaults to `managed`. `external` declares a
previously provisioned Ceph cluster and omits `clusterInfraRef` and `ceph`;
Bootwright renders and applies only Data Foundation attachment manifests for
that cluster. For imported Ceph, the user declares the Data Foundation
external-cluster-details JSON as a secret and references it from
`StorageExport.spec.externalDetails.fromSecret`. For managed Ceph, Bootwright
generates the same JSON during storage provisioning and stores it as
restrictive runtime secret material.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: StorageCluster
metadata:
  name: ceph-stretch
spec:
  type: ceph
  clusterInfraRef:
    name: ceph-stretch-infra

  ceph:
    cephadm:
      addressRef:
        name: public
      bootstrap:
        seedNode: ceph-dc1-0
        monIP:
          nodeRef:
            name: ceph-dc1-0
          addressRef:
            name: public
      registry:
        url: registry.redhat.io
        credentialsRef:
          name: ceph-registry-credentials
      nodeSSH:
        keyPairRef:
          name: ceph-stretch-cluster-admin-ssh-key

    topology:
      stretch:
        enabled: true
        failureDomain: datacenter
        dataSites:
          - dc1
          - dc2
        tiebreaker:
          site: dc3
          node: ceph-arbiter
        replicatedPoolDefaults:
          size: 4
          minSize: 2
        ruleName: stretch-replicated
      nodes:
        - name: ceph-dc1-0
          site: dc1
          roles:
            - mon
            - mgr
            - osd
            - mds
            - rgw
            - ingress
```

Rules:

- `StorageCluster.spec.ceph.cephadm.bootstrap.monIP` is singular because
  `cephadm bootstrap` creates the first monitor on one seed host. `nodeRef`
  names a `StorageCluster.spec.ceph.topology.nodes[]` entry; `addressRef`
  names a `Host.spec.addresses[]` entry on the Host behind the matching
  `ClusterInfra.spec.components.nodes[].source.hostRef`. When omitted,
  `bootstrap.monIP.addressRef.name` defaults from `cephadm.addressRef.name`.
  When `cephadm.addressRef.name` is omitted, Bootwright uses each Host's SSH
  address for Ceph host `addr` resolution. HA monitor placement is declared
  through the rendered cephadm service specs.
- `StorageCluster.spec.ceph.cephadm.registry` is required for managed Ceph.
  `url` names the image registry, `credentialsRef` points at credential
  material, and optional `trustBundleRef` points at a registry CA bundle.
  Registry credentials are never embedded in the URL.
- `StorageCluster.spec.management: external` disables Bootwright-managed Ceph
  provisioning. `StoragePlacementPolicy`, `StoragePool`, `StorageFilesystem`,
  and `StorageObjectGateway` are not declared for imported Ceph.
- Each managed `StorageCluster.spec.ceph.topology.nodes[].name` must match a
  `ClusterInfra.spec.components.nodes[].name` whose `source.hostRef.name`
  references a `Host` with capability `ceph-node`. The referenced
  `Host.spec.ssh` owns the bastion-to-node Ansible SSH identity and the SSH
  identity copied to the seed host for cephadm orchestration. All storage-node
  Hosts in one managed StorageCluster must use the same `ssh.user` and
  `ssh.keyRef.name`, because cephadm bootstrap accepts one SSH user/key for
  cluster host orchestration. Their key material must provide both private and
  public halves so Bootwright can connect and cephadm can authorize the same
  key. When `Host.spec.ssh.knownHostsRef` is omitted, Bootwright uses
  context-managed SSH trust recorded by `bootwright host trust` for strict
  host-key checking.
- A storage-only `ClusterInfra` can omit OpenShift API, API-int, and ingress
  endpoints. Explicit bare-metal BMC fields are required only for
  `ContainerCluster` boot targets, not for storage-only preinstalled nodes.
- In stretch mode, exactly two data sites and one distinct tiebreaker site are
  accepted. The tiebreaker node must be monitor-only. Monitor placement must be
  two monitors in each data site plus one tiebreaker monitor.
- Stretch-mode replicated pools must use `size: 4` and `minSize: 2`.
  Erasure-coded pools are rejected for stretch clusters.
- `StorageFilesystem` makes CephFS metadata and data pools distinct:
  `metadataPoolRef` and the default `dataPoolRefs[]` entry render to
  `ceph fs new <fs> <metadataPool> <dataPool>`.
- `StorageObjectGateway` uses cephadm `ingress` services for RGW HA. Cephadm
  deploys HAProxy and keepalived; Bootwright does not install a separate load
  balancer. Each site-local ingress can place a VIP on nodes in that site, and
  the aggregate ingress placement must cover at least two ingress-capable
  nodes per data site.
- `StorageObjectGateway.spec.publicEndpointRef.name` points to a
  `ClusterInfra.spec.endpoints` entry that describes the public RGW DNS name,
  scheme, and port. Its source defaults to `external`.
- `StorageObjectGateway.spec.ceph.ingresses[].endpointRef.name` points to a
  `ClusterInfra.spec.endpoints` entry for the cephadm-managed ingress VIP.
  Its source defaults to `cephadm`. For example, an endpoint named `rgw-dc1`
  should set `address: 192.168.141.80` for the specific VIP,
  `prefixLength: 24` for the keepalived virtual IP CIDR suffix, and
  `interfaceNetworks: [192.168.141.0/24]` so cephadm can select the site-local
  interface network.
- `ClusterAddon.spec.accepts.inputs[]` can declare an input effect of
  `type: storage-export-attachment` and `provider: data-foundation`.
  Binding values for that input connect a `StorageExport` to the binding's
  `ContainerCluster`. The cluster must have a bound `ClusterAddon` that
  provides `data-foundation`; storage attachment manifests are applied after
  that add-on reports readiness.
- Data Foundation exports render per-consuming-cluster Ceph auth operations in
  `ceph/operations.yaml` with capture metadata for generated RBD, CephFS,
  healthchecker, and RGW keys. Create-style storage operations declare
  explicit `idempotency.kind` and `idempotency.name` fields for Ansible
  preflight checks; the storage role must not infer skip logic from operation
  names or positional command arguments. Rendered manifests carry
  generated-at-apply placeholders for secret keys; authored examples must not
  contain generated external-cluster secret bytes.
- Imported Data Foundation attachments render a placeholder in normal output
  and inline the `StorageExport.spec.externalDetails.fromSecret` JSON only for
  sensitive render output and apply-time task artifacts. Managed Ceph
  attachments read generated details from
  `clusters/<cluster>/secrets/addons/<addon>/inputs/<input>/external-cluster-details.json`.

Rendered storage files are deterministic and are the same files used during
apply:

```text
storage/<storageCluster>/cephadm/bootstrap-spec.yaml
storage/<storageCluster>/cephadm/core-services.yaml
storage/<storageCluster>/cephadm/late-services.yaml
storage/<storageCluster>/ceph/operations.yaml
storage/<storageCluster>/data-foundation/<cluster>/<addon>/<input>/rook-ceph-external-cluster-details.yaml
storage/<storageCluster>/data-foundation/<cluster>/<addon>/<input>/ocs-external-storagecluster.yaml
storage/<storageCluster>/data-foundation/<cluster>/<addon>/<input>/ocs-external-storagesystem.yaml
```

The `cephadm/` and `ceph/` files are omitted for imported storage clusters.

## Cluster Add-Ons

Cluster add-ons model initial post-install bootstrap components. They are
for early platform setup, not for replacing long-term day-2 GitOps management.

`ClusterAddon` declares one reusable component. MVP types are
`olm-operator` and `manifest-set`.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: ClusterAddon
metadata:
  name: openshift-virtualization
spec:
  type: olm-operator
  provides:
    - kubevirt

  olm:
    namespace:
      name: openshift-cnv
      create: true
      labels:
        openshift.io/cluster-monitoring: "true"

    operatorGroup:
      name: kubevirt-hyperconverged-group
      targetNamespaces:
        - openshift-cnv

    subscription:
      name: hco-operatorhub
      package: kubevirt-hyperconverged
      channel: stable
      startingCSV: kubevirt-hyperconverged-operator.v4.21.8
      source: redhat-operators

    customResources:
      - apiVersion: hco.kubevirt.io/v1beta1
        kind: HyperConverged
        metadata:
          name: kubevirt-hyperconverged
          namespace: openshift-cnv

  readiness:
    checks:
      - type: csvSucceeded
        namespace: openshift-cnv
        subscription: hco-operatorhub
      - type: condition
        apiVersion: hco.kubevirt.io/v1beta1
        kind: HyperConverged
        name: kubevirt-hyperconverged
        namespace: openshift-cnv
        condition:
          type: Available
          status: "True"
```

`provides[]` advertises add-on-provided cluster capabilities consumed by
cross-cluster substrates and storage attachments. Accepted values are `kubevirt`
and `data-foundation`. An add-on that provides a capability must declare
readiness checks so dependent work waits for the actual platform capability,
not just resource submission.

`accepts.inputs[]` declares binding-scoped input APIs accepted by an add-on.
The supported schema subset is intentionally small: `type: object`,
`required[]`, and `properties.*` entries that are either `refKind` references
to Bootwright resources or `secretRef: true`. `effects[]` declares built-in
Bootwright behavior driven by the input values. The initial built-in effect is
`storage-export-attachment` with `provider: data-foundation`.

```yaml
spec:
  provides:
    - data-foundation
  accepts:
    inputs:
      - name: external-storage
        schema:
          type: object
          required:
            - exportRef
          properties:
            exportRef:
              refKind: StorageExport
        effects:
          - type: storage-export-attachment
            provider: data-foundation
```

`manifest-set` add-ons apply existing YAML files in declared order. Paths
are relative to the `ClusterAddon` file directory. Non-Bootwright YAML under
an add-on `manifests/` directory is treated as add-on payload, not desired
state to decode.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: ClusterAddon
metadata:
  name: console-customization
spec:
  type: manifest-set
  manifestSet:
    manifests:
      - path: manifests/console-banner.yaml
      - path: manifests/console-links.yaml
  readiness:
    timeout: 10m
    checks:
      - type: resourceExists
        apiVersion: operator.openshift.io/v1
        kind: Console
        name: cluster
```

`ClusterAddonProfile` declares an ordered reusable group:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: ClusterAddonProfile
metadata:
  name: virtualization-platform
spec:
  profiles:
    - name: base-platform
  addons:
    - name: openshift-virtualization
```

Expansion is deterministic: expand `profiles` in declared order, then
append direct `addons` in declared order. Duplicate add-on names after
expansion are allowed and de-duplicated by first occurrence. Cycles are
rejected.

`ClusterAddonBinding` attaches profiles, direct add-ons, and add-on input
values to one container cluster:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: ClusterAddonBinding
metadata:
  name: demo-ocp-addons
spec:
  clusterRef:
    name: demo-ocp

  addonProfiles:
    - name: virtualization-platform

  addons:
    - name: console-customization
    - name: openshift-data-foundation
      inputs:
        - name: external-storage
          values:
            exportRef:
              name: ceph-stretch-data-foundation
```

Binding expansion follows the same order as profiles: referenced
`addonProfiles`, then direct `addons`, with first occurrence
de-duplication. The expanded order becomes the apply order for
`clusterRef.name`. Direct `addons[]` entries can supply inputs for add-ons
selected directly or through a profile. Addon-only bindings are valid. Native
effects wait for the required addon capability, such as `data-foundation`,
plus managed storage provisioning when applicable. Apply uses fixed
Bootwright server-side apply defaults.

Rules:

- `olm-operator` requires `olm.namespace.name` and a complete subscription:
  `name`, `package`, `channel`, `source`, `sourceNamespace`, and
  `installPlanApproval`.
- `subscription.startingCSV`, when set, is rendered to
  `Subscription.spec.startingCSV` to request a specific catalog CSV while still
  declaring the channel.
- When `subscription.startingCSV` is omitted, the add-on intentionally tracks
  the declared catalog channel. This is accepted for bootstrap add-ons because
  catalog selection is authored cluster intent, not a Bootwright-managed
  component image pin. Operators that need a specific CSV must set
  `startingCSV`.
- `installPlanApproval` is `Automatic` or `Manual`.
- If `operatorGroup` is set, `operatorGroup.name` is required.
- `customResources[]` may be empty. When present, each custom resource must
  set `apiVersion`, `kind`, `metadata.name`, and `metadata.namespace` in the
  MVP.
- Generated OLM resources apply in this order: Namespace when
  `namespace.create` is true, OperatorGroup when set, Subscription, then
  `customResources[]`.
- `manifest-set.manifests[]` must not be empty. Each path must be relative,
  remain under the add-on file directory, name an existing non-symlink
  `.yaml` or `.yml` file, and apply in declared order.
- Readiness timeout uses Go duration strings and defaults to `30m`.
- Readiness checks are `csvSucceeded`, `condition`, and `resourceExists`.
- `csvSucceeded` requires `namespace` and `subscription`; it waits for the
  Subscription `status.installedCSV`, then for that CSV `status.phase` to be
  `Succeeded`.
- `condition` requires `apiVersion`, `kind`, `name`, `condition.type`, and
  `condition.status`; namespace is optional for cluster-scoped resources.
- `resourceExists` requires `apiVersion`, `kind`, and `name`; namespace is
  optional.
- `ClusterAddonBinding.spec.clusterRef.name` must name exactly one existing
  `ContainerCluster`.
- `ClusterAddonBinding.spec.addonProfiles[]` and `spec.addons[]` references
  must resolve.
- `ClusterAddon.spec.accepts.inputs[].name` values must be unique. Schemas
  must use the supported object subset, and effects must be supported by
  Bootwright.
- `ClusterAddonBinding.spec.addons[].inputs[].name` values must be declared by
  the target `ClusterAddon`, unique after profile expansion, and valid against
  that accepted input schema.
- The `storage-export-attachment` effect requires `values.exportRef.name` to
  resolve to a `StorageExport` of type `data-foundation`.
- Imported Ceph requires `StorageExport.spec.externalDetails` with exactly one
  source. `fromSecret` references declared external-cluster-details JSON.
  `generated` is rejected for imported Ceph.
- Managed and full-managed Ceph may omit
  `StorageExport.spec.externalDetails`; Bootwright treats it as `generated`.
  If set explicitly, it must still choose exactly one supported source.
- `StorageExport.spec.externalDetails.fromSecret` names secret material
  declared in `Environment.spec.secrets`. `generated` means Bootwright creates
  Data Foundation Ceph auth and assembles exporter-compatible JSON from the
  managed storage result. `sshExecution` runs the official Data Foundation
  exporter through Ansible over SSH to each referenced Host using that Host's
  `spec.ssh` key, user, address, and explicit or context-managed SSH host
  trust, becomes root through Ansible `become`, writes the validated JSON into
  the attachment Secret manifest, and does not log exporter stdout. Imported
  Ceph requires explicit `ceph-admin` host refs. Managed and full-managed Ceph
  may omit host refs; Bootwright uses the storage cluster's
  `cephadm.bootstrap.seedNode` Host ref.
- Future add-on types may include `kustomize` and `helm`; they are not
  accepted by the MVP schema.

## NetworkConfig

`NetworkConfig` is a reusable machine-network template. It carries the
installer `networking.machineNetwork[]` data and the NMState document used by
agent-config hosts.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: NetworkConfig
metadata:
  name: rack1-bonded-machine
spec:
  machineNetwork:
    - cidr: 192.168.133.0/24

  dnsRefs:
    - default

  template:
    networkConfig:
      interfaces:
        - name: eno1
          type: ethernet
          state: up
        - name: eno2
          type: ethernet
          state: up
        - name: bond0
          type: bond
          state: up
          link-aggregation:
            mode: active-backup
            options:
              miimon: "100"
            port:
              - eno1
              - eno2
          ipv4:
            enabled: true
            dhcp: false
          ipv6:
            enabled: false
      dns-resolver:
        config:
          server:
            - 192.168.133.1
      routes:
        config:
          - destination: 0.0.0.0/0
            next-hop-address: 192.168.133.1
            next-hop-interface: bond0
            table-id: 254
```

Rules:

- `spec.machineNetwork[]` renders to
  `install-config.yaml networking.machineNetwork[]`.
- A given `NetworkConfig` must not repeat the same
  `spec.machineNetwork[].cidr`. The same CIDR may appear on separate
  `NetworkConfig` objects when different reusable NMState templates share one
  L3 machine network.
- `spec.template.networkConfig` renders to
  `agent-config.yaml hosts[].networkConfig` after per-machine overrides.
- `spec.dnsRefs[]` selects entries from
  `Environment.spec.infraComponents.nameResolution[].name`. These refs are
  Bootwright service-selection intent and must stay outside the raw NMState
  template. Resolved IPs are appended to generated
  `dns-resolver.config.server` entries.
- `ClusterInfra.spec.components.nodes[].network` accepts either a reusable
  `networkConfigRef` plus raw NMState `overrides`, or a complete inline
  `spec`. `networkConfigRef` and `spec` are mutually exclusive. `network` is
  valid only for provider-sourced nodes.
- When `networkConfigRef` is used, `overrides` is merged into the referenced
  `NetworkConfig.spec.template.networkConfig` by updating the declared nested
  fields instead of replacing the whole original tree. Interface lists are
  matched by `name` before nested attributes are merged.
- Static per-machine IPs belong in
  `overrides.interfaces[].ipv4.address[]` or
  `overrides.interfaces[].ipv6.address[]`, and must fit at least one
  referenced machine network CIDR. Host-sourced nodes use
  `Host.spec.addresses[]` and never use `NetworkConfig`.
- Substrate attachments such as libvirt bridges, vSphere portgroups,
  KubeVirt NADs, and bare-metal VLANs belong to
  `InfraProvider.spec.networkAttachments[]` and are selected from
  `ClusterInfra.spec.networkBindings[]`.

## Host

`Host` means an OS-installed, reachable execution target with SSH and named
addresses. It owns how Bootwright reaches durable provider, service, external
Ceph admin, or managed Ceph node actions. Raw or provider-owned substrate
inventory remains under `InfraProvider.spec.machines[]`. OpenShift install
nodes are still declared through `ContainerCluster.spec.nodes[]` and
provider-sourced nodes in `ClusterInfra`; their installer SSH trust remains
`ContainerCluster.spec.install.nodeSSH`.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Host
metadata:
  name: provider-01
spec:
  addresses:
    - name: ssh
      address: 192.168.133.2
    - name: lab-lan
      address: 192.168.133.2

  ssh:
    addressName: ssh
    user: core
    keyRef:
      name: provider-host-ssh

  capabilities:
    - libvirt
    - container-runtime
```

Rules:

- `spec.addresses[]` declares neutral named host addresses. Names are scoped to
  one `Host` and may be referenced by same-host consumers.
- `spec.ssh.addressName` references one `spec.addresses[].name` and is the
  bastion-facing SSH endpoint. When `spec.ssh.user` is omitted, Bootwright
  does not render `ansible_user`; SSH chooses the local account or configured
  host-specific user. Set `spec.ssh.user` only when Bootwright must force a
  provider-host SSH login name.
- When the resolved SSH endpoint is `localhost`, a loopback address, the
  current controller hostname, or a local interface address, Bootwright treats
  the host as controller-local, uses Ansible local connection, and does not
  require host SSH key material for preflight.
- `spec.ssh.keyRef.name` is required and references
  `Environment.spec.secrets`. Controller-local hosts still do not require host
  SSH key material during preflight.
- `spec.ssh.knownHostsRef.name` is optional. When set, it references
  `Environment.spec.secrets` and Bootwright uses that known_hosts material.
  When omitted on a non-local Host, `bootwright host trust` must record the
  Host server key under the current context before check or apply workflows
  can run. Non-local durable Host SSH always renders with
  `StrictHostKeyChecking=yes` and `UserKnownHostsFile` pointing at the explicit
  or context-managed known_hosts material.
- `spec.capabilities[]` is required. The current canonical tags are `libvirt`,
  `container-runtime`, `ceph-admin`, and `ceph-node`; provider, service,
  external Ceph, and managed Ceph workflows use them to select hosts for
  substrate, service, or Ceph-side work.

## InfraProvider

`InfraProvider` keeps named capability lists. Capability internals use shapes
close to installer platform and host inputs.

Bare metal inventory keeps physical facts:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata:
  name: rack1-baremetal
spec:
  machines:
    - name: rack1-srv1
      labels:
        datacenter: dc1
      baremetal:
        bootMACAddress: 52:54:00:33:11:10
        interfaces:
          - name: eno1
            macAddress: 52:54:00:33:11:10
          - name: eno2
            macAddress: 52:54:00:33:11:20
        rootDeviceHints:
          deviceName: /dev/sda
        bmc:
          address: redfish-virtualmedia+https://bmc-rack1-srv1.example.test/redfish/v1/Systems/1
          credentialsRef:
            name: bmc-credentials
          disableCertificateVerification: true  # lab-only; use trusted BMC TLS in production
```

Libvirt profiles require emulated Redfish BMC settings for current apply
support:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata:
  name: lab-libvirt
spec:
  machineProfiles:
    - name: sno
      cpu: 8
      memoryMiB: 16384
      diskGiB: 120
      libvirt:
        hostRef:
          name: provider-01
        uri: qemu:///system
        bmcEmulationDefaults:
          enabled: true
          protocol: redfish
          bindAddress: 0.0.0.0
          port: 8000
          vmediaPort: 8001
          auth:
            credentialRef:
              name: bmc-credentials
```

vSphere profiles keep vCenter and failure-domain facts:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata:
  name: dc1-vsphere
spec:
  machineProfiles:
    - name: vsphere-control-plane
      cpu: 8
      memoryMiB: 16384
      diskGiB: 120
      vsphere:
        vcenters:
          - server: vcenter.example.test
            port: 443
            datacenters:
              - dc1
            credentialsRef:
              name: vcenter-credentials
        failureDomains:
          - name: dc1-zone-a
            region: dc1
            zone: zone-a
            server: vcenter.example.test
            topology:
              datacenter: dc1
              computeCluster: /dc1/host/cluster1
              datastore: /dc1/datastore/datastore1
              folder: /dc1/vm/bootwright
              resourcePool: /dc1/host/cluster1/Resources/bootwright
              networks:
                - VM_Network_1
                - VM_Network_2
        nodeNetworking:
          external:
            networkSubnetCidr:
              - 192.168.133.0/24
```

KubeVirt profiles keep the host virtualization cluster and namespace facts:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata:
  name: child-kubevirt
spec:
  machineProfiles:
    - name: child-sno
      cpu: 8
      memoryMiB: 16384
      diskGiB: 120
      kubevirt:
        hostContainerClusterRef:
          name: metal-ocp
        namespace: bootwright-child-ocp
        storageClassRef:
          name: lvms-vg1
```

`hostContainerClusterRef` names a Bootwright `ContainerCluster` whose generated
kubeconfig is used at runtime. Use `kubeconfigRef` instead when the KubeVirt
host cluster is external to Bootwright:

```yaml
kubevirt:
  kubeconfigRef:
    name: external-virt-cluster-kubeconfig
  namespace: bootwright-child-ocp
```

Provider network attachments name substrate-specific network surfaces that a
cluster may bind to a logical `NetworkConfig`:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata:
  name: child-kubevirt
spec:
  networkAttachments:
    - name: child-machine-net
      kubevirt:
        nadRef:
          name: child-machine-net
          namespace: bootwright-child-ocp
    - name: lab-bridge
      libvirt:
        bridge: vbr-lab
    - name: vm-network
      vsphere:
        portgroup: VM_Network_1
    - name: rack-vlan730
      baremetal:
        vlan: 730
```

Rules:

- Provider credentials are always `SecretRef`s.
- `machines[].labels` is optional non-secret metadata for selection,
  reporting, and operator grouping. Label keys and values use Kubernetes label
  syntax.
- Bare metal physical facts stay provider-owned: BMC, boot MAC, physical NIC
  MACs, and root device hints.
- `machineProfiles[].libvirt.bmcEmulationDefaults` is required for current
  libvirt apply support. `enabled: false` is invalid until a non-Redfish
  libvirt boot path exists.
- `bmcEmulationDefaults.port` defaults to `8000`; `vmediaPort` defaults to
  `port + 1`. Ports must be in range, differ from each other, and be unique per
  provider host across libvirt BMC emulator services.
- vSphere platform facts stay provider-owned: vCenter refs, datacenters,
  failure domains, topology, datastore, folder, resource pool, and networks.
- vSphere failure domains must include the installer-required `region`, `zone`,
  `server`, `topology.datacenter`, `topology.computeCluster`,
  `topology.datastore`, and `topology.networks`.
- KubeVirt profiles must set exactly one of `hostContainerClusterRef` or
  `kubeconfigRef`.
- `hostContainerClusterRef.name` references a loaded Bootwright `ContainerCluster`. The
  host cluster must have a selected `ClusterAddonBinding` that applies an
  add-on with `provides: [kubevirt]`.
- `kubeconfigRef.name` references `Environment.spec.secrets`; the secret stores
  a kubeconfig path or context-local kubeconfig material and never stores bytes
  in desired state.
- `kubevirt.namespace` is required. `storageClassRef.name` is optional.
- `networkAttachments[]` names are unique within one `InfraProvider` and each
  attachment sets exactly one of `libvirt`, `vsphere`, `kubevirt`, or
  `baremetal`.
- KubeVirt `networkAttachments[].kubevirt.nadRef` requires `name` and
  `namespace`. The referenced NAD is supplied by the operator or by a
  parent-cluster `manifest-set` add-on.
- Bare-metal `networkAttachments[].baremetal.vlan` is optional and must be in
  range `0..4094` when set.
- KubeVirt, libvirt, and vSphere-backed machines must have a matching
  `ClusterInfra.spec.networkBindings[]` entry for the selected provider and
  `NetworkConfig`.
- KubeVirt `hostContainerClusterRef` dependencies must be acyclic. A cluster cannot host
  itself directly or through another child cluster.
- Capability names are scoped by capability kind.

## InfraComponent

`InfraComponent` declares reusable host-bound services that are not cluster
intent and are not substrate inventory.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: InfraComponent
metadata:
  name: artifact-server
spec:
  artifactServer:
    hostRef:
      name: services-host
    bindAddress: 0.0.0.0
    listeners:
      - name: https
        protocol: https
        port: 8443
    endpoints:
      - name: bmc
        listener: https
        hostAddress: lab-lan
      - name: cluster
        listener: https
        hostAddress: lab-lan
```

Rules:

- `spec` must set exactly one component arm: `artifactServer`,
  `loadBalancer`, `proxy`, `nameResolution`, `ntp`, or `registry`.
- `artifactServer.hostRef.name` references a `Host` with `container-runtime`.
- `artifactServer.bindAddress` defaults to `0.0.0.0`.
- `artifactServer.listeners[]` declares the ports the container listens on.
  Supported protocols are `http` and `https`. If omitted, Bootwright defaults
  to one HTTPS listener named `https` on port `8443`.
- `artifactServer.endpoints[]` names reachable service addresses. Each endpoint
  chooses a listener, and `hostAddress` must match a
  `Host.spec.addresses[].name` on `artifactServer.hostRef`; Bootwright uses
  that address object's `address` value in endpoint URLs and TLS names.
- Artifact endpoint names are opaque endpoint selectors, not DNS labels. They are
  the stable binding surface used by
  `ClusterInfra.spec.artifactAccess`.
- The artifact server is implemented as a containerized static file service
  that serves generated ISOs and disconnected boot artifacts. HTTPS listeners
  use a self-signed certificate generated on the component host.
- `loadBalancer`, `proxy`, `nameResolution`, `ntp`, and `registry` arms declare
  their host placement and component-specific bind surface. Environment
  catalog entries decide which consumers use proxy, DNS, NTP, registry, or
  artifact services.
- `proxy.endpoints[]`, `nameResolution.endpoints[]`, `ntp.endpoints[]`, and
  `registry.endpoints[]`, when set, use `hostAddress` values that must match
  `Host.spec.addresses[].name` on their component `hostRef`.
- `nameResolution.additionalIngressHosts[]` adds explicit host records that
  point at the consuming cluster's ingress VIP. Entries from the component and
  matching environment service catalog entry merge without mutating authored
  desired state.
- `ntp.type` must be `chrony`. `ntp.bindAddress` defaults to `0.0.0.0`,
  `ntp.port` defaults to `123`, and `ntp.upstreamSources[]` lists optional
  upstream IP or DNS time sources used by the managed host service.

## ClusterInfra

`ClusterInfra` owns the infrastructure selected for one cluster: platform
render mode, endpoints, and selected machines.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: ClusterInfra
metadata:
  name: prod-3node-infra
spec:
  platform:
    type: baremetal
    baremetal:
      provisioningNetwork: disabled

  endpoints:
    api:
      address: 192.168.133.10
      source:
        type: external
    api-int:
      address: 192.168.133.10
      source:
        type: external
    apps:
      address: 192.168.133.11
      source:
        type: external

  artifactAccess:
    serverRef:
      name: default
    redfishVirtualMedia:
      endpointRef:
        name: bmc

  networkBindings:
    - networkConfigRef:
        name: rack1-bonded-machine
      providerRef:
        name: rack1-baremetal
      attachmentRef:
        name: rack1-vlan

  components:
    nodes:
      - name: master-0
        source:
          providerRef:
            name: rack1-baremetal
          machineRef:
            name: rack1-srv1
        network:
          networkConfigRef:
            name: rack1-bonded-machine
          overrides:
            interfaces:
              - name: bond0
                ipv4:
                  address:
                    - ip: 192.168.133.20
                      prefix-length: 24
      - name: master-1
        source:
          providerRef:
            name: rack1-baremetal
          machineRef:
            name: rack1-srv2
        network:
          networkConfigRef:
            name: rack1-bonded-machine
          overrides:
            interfaces:
              - name: bond0
                ipv4:
                  address:
                    - ip: 192.168.133.21
                      prefix-length: 24
      - name: master-2
        source:
          providerRef:
            name: rack1-baremetal
          machineRef:
            name: rack1-srv3
        network:
          networkConfigRef:
            name: rack1-bonded-machine
          overrides:
            interfaces:
              - name: bond0
                ipv4:
                  address:
                    - ip: 192.168.133.22
                      prefix-length: 24

```

Rules:

- `platform.type` is required when a `ContainerCluster` installs against the
  `ClusterInfra`: `baremetal`, `vsphere`, `none`, or `external`. This is the
  installer platform render mode, not the substrate type; substrate ownership
  remains with selected `InfraProvider` machines or profiles.
- Bare-metal `provisioningNetwork`, when set, is `disabled`, `managed`, or
  `unmanaged`. `disabled` renders OpenShift bare metal in "no dedicated
  provisioning network" mode and is appropriate for agent installs using
  Redfish virtual media on the existing machine network.
- `components.nodes[]` is the neutral cluster-member list for OpenShift and
  storage workflows. `source` must set exactly one of `hostRef` or
  `providerRef`. When `providerRef` is set, exactly one of `machineRef` or
  `profileRef` is required.
- Provider-sourced nodes select provider machines or profiles and may apply
  per-node install network overrides through `network`.
- Host-sourced nodes reference OS-installed reachable `Host` targets and must
  not set `network` or `rootDeviceHints`.
- `networkBindings[]` maps a logical `NetworkConfig` and selected provider to
  one `InfraProvider.spec.networkAttachments[]` entry. Bindings are unique per
  `(providerRef.name, networkConfigRef.name)` pair.
- Binding attachment kind must match the machine substrate: libvirt machines
  bind to libvirt attachments, vSphere machines to vSphere attachments,
  KubeVirt machines to KubeVirt attachments, and bare-metal machines to
  bare-metal attachments when a binding is declared.
- `endpoints` is an arbitrary map of named endpoint objects. Endpoint names
  must be DNS labels. Consumers decide which endpoint names they use through
  explicit refs, for example `ContainerCluster.spec.install.endpointRefs` or
  `StorageObjectGateway.spec.publicEndpointRef`.
- `address` is the concrete VIP or service IP. `dnsName`, `scheme`, and
  `port` describe externally consumed endpoints. `prefixLength` and
  `interfaceNetworks[]` are optional consumer inputs; cephadm RGW ingress uses
  them to render keepalived virtual IP and interface network settings.
- `source.type` records the endpoint owner. It may be `openshift`, `external`,
  `cephadm`, or `infraComponent`. Consumers provide defaults: OpenShift install
  endpoint refs default to `openshift`, `StorageObjectGateway.publicEndpointRef`
  defaults to `external`, and RGW ingress endpoint refs default to `cephadm`.
- `source.type=infraComponent` references an `InfraComponent` with
  `spec.loadBalancer`. `source.bindAddress` is the name of an entry in
  `spec.loadBalancer.bindAddresses[]`; it is required when the load balancer
  has more than one bind address.
- `artifactAccess.serverRef.name` selects an environment artifact server
  catalog entry for this cluster infrastructure.
- `artifactAccess.redfishVirtualMedia.endpointRef.name` selects the artifact
  endpoint used in BMC ISO fetch URLs.
- `artifactAccess.containerClusterInstall.endpointRef.name` selects the
  artifact endpoint used for disconnected agent-install boot artifacts.
- `Environment.spec.defaults.artifactAccess` can provide fleet-wide defaults
  for omitted artifact access fields. Normalized effective state still records
  the selected values on `ClusterInfra.spec.artifactAccess`.
- Bare-metal Redfish virtual-media boot and disconnected agent installs derive
  generated artifact publication from `artifactAccess`.
- For vSphere multi-NIC installs, the first adapter network must correspond to
  the machine network unless `platform.vsphere.nodeNetworking` or profile
  `nodeNetworking` says otherwise.

Bootwright-provisioned load balancer endpoints reference named component bind
addresses:

```yaml
endpoints:
  api:
    source:
      type: infraComponent
      componentRef:
        name: control-plane
      bindAddress: control-plane-ip
  api-int:
    source:
      type: infraComponent
      componentRef:
        name: control-plane
      bindAddress: control-plane-ip
  apps:
    source:
      type: infraComponent
      componentRef:
        name: apps
      bindAddress: apps-ip
```

Here `control-plane-ip` and `apps-ip` are not literal IP addresses. They are
the names of `InfraComponent.spec.loadBalancer.bindAddresses[]` entries; the
effective IP comes from those bind-address records.

## Render Behavior

Bootwright renders:

- `install-config.yaml` metadata, base domain, pull secret, SSH key, pools,
  cluster/service networking, proxy, trust, mirrors, and release inputs from
  `ContainerCluster` plus `Environment`.
- `install-config.yaml networking.machineNetwork[]` from
  `NetworkConfig.spec.machineNetwork[]` referenced by selected machines.
- `install-config.yaml` API and ingress VIPs from
  `ContainerCluster.spec.install.endpointRefs`, resolving endpoint `address`
  or the referenced load balancer bind-address IP.
- `install-config.yaml platform.<type>` from `ClusterInfra.spec.platform`,
  endpoints, and provider capabilities. Single-node clusters render
  `platform.none` unless `ClusterInfra.spec.platform.type` is `external`,
  matching the upstream agent installer constraint for one control-plane and
  zero compute nodes.
- `agent-config.yaml hosts[]` by matching `ContainerCluster.spec.nodes[]` to
  provider-sourced `ClusterInfra.spec.components.nodes[]`.
- `agent-config.yaml hosts[].networkConfig` from inline
  `network.spec` definitions or referenced `NetworkConfig` templates plus
  node overrides.
- `agent-config.yaml hosts[].networkConfig.dns-resolver.config.server` from
  static NMState servers plus `NetworkConfig.spec.dnsRefs[]`, de-duplicated in
  that order.
- `agent-config.yaml hosts[].interfaces[]` from provider MAC inventory or
  deterministic generated MACs for virtual substrates that Bootwright creates.
- Provider or generated machine MACs into matching NMState interfaces when
  present.
- `agent-config.yaml minimalISO` and `bootArtifactsBaseURL` for disconnected
  installs that set `ClusterInfra.spec.artifactAccess.containerClusterInstall`.
- `agent-config.yaml additionalNTPSources` from resolved
  `Environment.spec.infraComponents.ntpSources[]`: external entries contribute
  `address`, and managed entries contribute the selected endpoint host address
  or a concrete non-unspecified `spec.ntp.bindAddress`.
- Install-time OpenShift extra manifests under `openshift/` for declared API
  and ingress serving certificates. Placeholder render output redacts Secret
  data; runtime and `--sensitive` output include TLS material.
- Managed non-machine components from `InfraComponent` objects selected by
  endpoints, environment catalog entries, and DNS refs.
- Shared infra component service identities, consumers, host placement, conflict
  fields, and mergeable overlays from the resolved service graph. Mergeable
  overlays are rendered into generated Ansible vars without mutating authored
  desired state.
- Generated artifact server components when a cluster needs agent ISO or
  boot-artifact publication.
- Storage tool inputs from selected storage resources: cephadm host, core
  service, and late service specs; phased Ceph operations for topology, pools,
  CephFS, RGW users, and Data Foundation keys with explicit idempotency
  metadata; and external-mode manifests for each selected storage attachment.
- Add-on apply plans from selected `ClusterAddonBinding` resources.
  OLM add-ons generate Namespace, OperatorGroup, Subscription, and custom
  resources. Manifest-set add-ons reference declared files and include file
  contents in desired-input hashes.

## Validation Rules

Validation rejects:

- Unknown fields and unsupported kinds.
- Any old network reference field under providers, machines, or endpoints.
- Any old cluster-to-infra top-level reference on `ContainerCluster`.
- Removed `ContainerCluster.spec.install` fields: `baseDomain`,
  `imageDigestSources`, `installConfigOverrides`, `agentConfigOverrides`,
  `sshKeyRef`, and `clusterAdminSSH`.
- Multiple `ClusterInfra` references inside one `ContainerCluster`.
- `ContainerCluster` node references that do not resolve to a selected machine.
- OpenShift clusters without a pull secret reference after normalization.
- OKD clusters that set an OpenShift release channel.
- Missing or invalid `ContainerCluster.spec.networking`, including malformed
  cluster or service CIDRs, missing `clusterNetwork[].hostPrefix`, or
  `hostPrefix` values outside the selected CIDR.
- Missing, unknown, or incomplete endpoint refs from a consumer.
- Endpoint entries without `address`, `dnsName`, or
  `source.type=infraComponent`.
- `source.componentRef.name` or `source.bindAddress` references that do not
  resolve to declared load balancer components and bind addresses.
- Unreferenced load balancers or named bind addresses.
- Endpoint VIPs or machine override IPs outside selected machine networks.
- Bare-metal machines selected by a non-bare-metal platform.
- vSphere platform selections backed by a non-vSphere machine profile.
- Invalid environment proxy or registry catalog entries, unresolved managed
  component refs, or conflicting service defaults.
- Clusters that need generated artifact publication unless
  `ClusterInfra.spec.artifactAccess` selects an artifact server entry and the
  required endpoint resolves.
- Shared infra component service consumers with the same rendered service identity
  but incompatible host, role, realisation, bind address, port, or selected
  capability.
- Missing `ClusterAddon`, `ClusterAddonProfile`, or
  `ClusterAddonBinding` references.
- `ClusterAddonProfile` reference cycles.
- Unsupported cluster add-on types, readiness check types, apply phases, and
  install plan approval values.
- Unsupported `ClusterAddon.spec.provides[]` capabilities. Current values
  are `kubevirt` and `data-foundation`; duplicate values and capabilities
  without readiness checks are invalid.
- Invalid storage references, add-on input storage effects, stretch-mode
  topology, monitor placement, tiebreaker roles, replicated pool defaults,
  erasure-coded stretch pools, CephFS metadata/data pool wiring, RGW/MDS
  placement, or Data Foundation capability dependencies.
- Unsafe `manifest-set` paths, including absolute paths, directory escapes,
  symlinks, missing files, and non-YAML add-on manifests.
- KubeVirt profiles missing exactly one host reference, referencing a missing
  host cluster, referencing an undeclared kubeconfig secret, missing
  `namespace`, using a non-KubeVirt network config, or creating a cluster
  dependency cycle.
- `ClusterAddonBinding.spec.clusterRef.name` values that name missing
  clusters.
- Imported Ceph `StorageExport` objects without
  `spec.externalDetails.fromSecret` or another accepted source.
- `StorageExport.spec.externalDetails` values that set multiple sources or
  reference undeclared secrets or hosts.

## CLI Contract

The CLI resolves desired state from the current named context. Context storage
is shared across sudo-capable administrators under
`/var/lib/bootwright/contexts/<context>/`; each OS user keeps only their
private current-context selection in `~/.bootwright/contexts.yaml`:

```yaml
current: lab
```

Context names are discovered from the fixed root-managed path
`/var/lib/bootwright/contexts/<context>/`; `context init` imports one or more
YAML files or directories into that context's `input/` directory. Input
directories are walked for YAML files unless exactly one discovered
`Environment` sets `spec.resources`, in which case only that environment file
plus the listed files are loaded.
Legacy map-shaped registries are rejected with an actionable reset message.
Retired list-shaped `contexts:` registry entries are ignored and removed on
the next registry write. Operators may remove `~/.bootwright/contexts.yaml`
and select or recreate contexts with `bootwright context use <name>` or
`bootwright context init <name> -f <path> --yes`.
Unknown fields are rejected at decode time, and all loaded objects are
normalized and validated before any render or apply step.
Context-backed commands fail before doing work when the selected context is
missing from shared storage or is not structurally ready. Bootwright never
auto-switches or auto-clears a stale private current selection; `context list`
reports it and context-backed commands block until the user chooses an existing
context or recreates it. Controller-local actions run on localhost;
`bootwright context validate` reports each checked aspect as `OK` or
`MISSING`, reports missing declared secret material and missing managed SSH
host trust as `WARN`, and supports `--output json` for automation. `WARN` does
not block structurally ready contexts; `MISSING` and `FAIL` remain blocking.

Primary commands:

```text
bootwright example init lab --output ./lab-input
bootwright validate -f ./lab-input
bootwright validate --output json
bootwright context init lab -f ./lab-input
bootwright context update lab -f ./lab-input
bootwright context validate
bootwright context validate --output json
bootwright context list
bootwright context use lab
bootwright context current [--short]
bootwright context delete lab --purge --yes
bootwright host trust
bootwright host trust --hosts provider-01,ceph-dc1-0
bootwright host trust --replace provider-01
bootwright host trust --dry-run
bootwright host trust --output json
bootwright cluster list
bootwright cluster list --output json
bootwright cluster access-info
bootwright cluster access-info --cluster managed-01
bootwright cluster access-info --output json
bootwright print-env [--sensitive]
bootwright secret list
bootwright secret list --output json
bootwright secret show --name <secret-name>
bootwright secret show --name <secret-name> --part public
bootwright secret set openshift-pull-secret --pull-secret <path>
bootwright secret generate
bootwright secret materialize
bootwright secret delete <secret-name> --yes
bootwright check syntax
bootwright check syntax -f ./lab-input
bootwright check syntax --output json
bootwright check bastion
bootwright check all [--dry-run]
bootwright apply bastion --yes
bootwright check infra
bootwright check infra --dry-run --output json
bootwright render effective
bootwright render effective --output json
bootwright render installer --scope <cluster>
bootwright render installer --sensitive
bootwright render installer --output json
bootwright render storage --scope <storage-cluster>
bootwright render --output-dir ./rendered --sensitive
bootwright apply --stage infra --dry-run
bootwright apply --stage infra --dry-run --output json
bootwright apply --stage infra --yes
bootwright apply --stage infra --parallelism 4 --yes
bootwright apply --stage infra --clusters managed-01 --yes
bootwright check clusters
bootwright check clusters --dry-run --output json
bootwright check addons
bootwright apply --stage clusters --dry-run
bootwright apply --stage clusters --dry-run --output json
bootwright apply --stage clusters --yes
bootwright apply --stage clusters --clusters managed-01,ceph-storage --yes
bootwright apply --stage clusters --override --yes
bootwright plan
bootwright plan --stage infra --clusters managed-01
bootwright plan --stage clusters --clusters managed-01,ceph-storage
bootwright plan --output json
bootwright apply --dry-run
bootwright apply --dry-run --output json
bootwright apply --yes
bootwright status
bootwright status --watch
bootwright status --output json
bootwright destroy container-cluster --yes
bootwright destroy container-cluster --dry-run --output json
bootwright destroy infra --yes
bootwright destroy infra --dry-run --output json
bootwright destroy infra --scope artifact-server --yes
```

Human text output is for operators and is not a parse-stable interface. It is
grouped into sections, status labels, artifact groups, summaries, and
actionable check remediation.
Commands that support `--output json` expose the stable automation surface and
must emit only JSON on stdout. Shell-export commands such as
`bootwright print-env` intentionally emit only `export ...` lines. `secret
show` intentionally emits raw secret bytes on stdout and is a sensitive
raw-output exception.
`bootwright cluster list` and `bootwright cluster access-info` read only local
context state. They print cluster API and console URLs, local kubeconfig paths,
the shell `KUBECONFIG=...` prefix, local password file paths, and the command
operators can run when they need the password. They must not print kubeconfig contents,
kubeadmin password bytes, tokens, or other cluster credential material.
`validate -f <path>` and `check syntax -f <path>` load YAML files or directories
directly and must not require or mutate the current context. `validate` is the
primary pre-import validation path for generated examples, copied examples, and
CI jobs that review authored desired state before `context init`.
`render effective` writes only the normalized desired-state snapshot with
defaults applied to `<context>/rendered/effective-state.yaml`, and supports
JSON output for the rendered path and object counts.
Ansible-backed apply commands may execute independent tasks concurrently.
Operators can tune task scheduling with `--parallelism`,
`--parallelism-per-host`, and `--parallelism-redfish`; `0` for any of those
flags means Bootwright uses the maximum safe automatic value. Explicit limits
only reduce automatic concurrency; provider-host and Redfish safety locks still
apply. `apply --stage clusters` uses direct local executors for add-on tasks
and the normal Ansible runner internally for storage and install tasks.
`apply --dry-run` is a plan-only action preview. It does
not run host, tool, secret, BMC, or cluster readiness checks and does not
mutate provider hosts, nodes, or clusters; operators must run
`bootwright check <target>` for readiness.
`plan` is equivalent to a read-only full `apply --dry-run` preview and is the
primary plan command for the end-to-end path. `apply --yes` is the primary
happy path after `apply bastion`, `check all`, and `plan`.
`apply --stage infra` prepares providers, infra components, selected machines,
networking, VIPs, artifact publication, DNS/NTP/proxy/registry support, and
managed storage-node prerequisites. It does not install OpenShift or OKD, run
cephadm bootstrap, or apply add-ons. `StorageCluster.spec.management:
fullManaged` is schema-valid, but apply and plan fail with unsupported
full-managed storage infrastructure.
`apply --stage clusters` installs or provisions selected `ContainerCluster` and
`StorageCluster` resources. Container cluster work creates the agent ISO, boots
nodes, and waits for install completion. Storage cluster work runs cephadm
bootstrap, service application, Ceph operations, and generated Data Foundation
export records. Bound add-ons and storage-export attachment effects run after
their dependencies are ready. `--clusters` accepts comma-separated
`ContainerCluster` and `StorageCluster` names; unknown names fail before
rendering. Omitting `--stage` runs the full graph: `infra`, then `clusters`.
Virtualized child clusters that use a Bootwright-managed host cluster wait for
the parent install and its `provides: [kubevirt]` add-on when parent and child
are selected together. Child-only selection is accepted only when the parent
cluster is already installed and the KubeVirt-providing add-on has a ready
runtime record; otherwise apply and plan fail before mutation and require the
operator to name both parent and child in `--clusters`.
Apply terminal output is a ledger-backed fleet dashboard for both single-cluster
and multi-cluster runs. It shows run metadata, run and cluster log paths,
task-count progress, per-cluster lifecycle phases, running work, and concise
failures. Bootwright does not stream live Ansible, `oc`, SSH, SCP, Ceph, or
installer process output to the terminal; that output stays in the run log,
task logs, and owning cluster log.
`bootwright apply --stage clusters --override` forces OpenShift agent install tasks to
run even when local cluster secrets kubeconfig state reports that the target cluster is
already available. It is for reinstalling after the operator has reset or
replaced the target machines; it does not wipe disks, destroy substrate
machines, power off nodes, or remove host services.
Without `--override`, `apply --stage clusters` must not regenerate the agent ISO or
reboot nodes when Bootwright can prove that the selected cluster is already
installed for the same rendered desired inputs. Completed installs are proven by
the per-cluster install record, the non-secret desired-input fingerprint, and a
local kubeconfig probe that reports `ClusterVersion Available=True`. If the
stored fingerprint differs from the current rendered inputs, apply must stop and
require either `destroy container-cluster` or `apply --stage clusters --override` after the
operator has reset or replaced target machines. If an interrupted run already
booted nodes, apply resumes at `openshift-install agent wait-for
install-complete` instead of creating a new ISO or rebooting machines. Add-on
tasks still run after skipped or completed install tasks and use their own
per-add-on desired hashes and readiness records for idempotency.
Every apply writes `<runs-dir>/current.json` atomically. The
ledger records the run ID, target, scope, selected concurrency limits, task
IDs, task dependencies, task statuses, timestamps, and per-task
`ansible-output.log` paths under
`<runs-dir>/history/<run>/tasks/<task>/`.
Cluster-owned tasks also record
`/var/lib/bootwright/contexts/<context>/clusters/<cluster>/runs/<run>/bootwright.log`.
Per-cluster install state is stored under
`<clusters-dir>/<cluster>/runtime/install-record.json`; it records the desired
input fingerprint, install status, last safe phase, run ID, timestamps, and
node boot markers, but not secret bytes.
Per-add-on state is stored under
`<clusters-dir>/<cluster>/runtime/addons/<addon>.json`; it records the
desired hash, status, phase, run ID, timestamps, observed resources, and last
observed readiness state, but not kubeconfig or secret bytes.
Storage task inputs are rendered under each task artifact directory and the
context rendered storage tree; they may include non-secret Ceph endpoints and
placeholder external-cluster details, but not generated Ceph client keys.
Task statuses are `pending`, `ready`, `running`, `blocked`, `skipped`, `ok`,
`failed`, and `cancelled`.
While an apply process is active, Bootwright also refreshes
`<runs-dir>/current.lease.json`. Mutating workflow commands
must block when the current apply ledger has a fresh lease whose local process
is still running. If the ledger still says `running` but its lease is missing,
stale, or points at a local process that has exited, the next `apply` or
`destroy` marks that previous run `cancelled` before continuing.
`bootwright status` reads local context state without contacting provider
hosts, BMCs, or clusters. Text output summarizes desired-state load status,
declared secret material presence, installer freshness, the current apply,
progress counts, running work, cluster task state, blocked work, failures, and
next steps. It reports whether a running apply lease is fresh or stale.
`bootwright status --watch` refreshes the same readable view until the current
apply reaches a terminal state or its lease is stale.
`bootwright status --output json` includes the full current apply ledger and
activity state when present.
`destroy infra --scope artifact-server` is a reserved destroy-only scope that
removes only the generated artifact publication service used for BMC agent ISO
fetches, including HTTPS listeners when configured.
Filtered `apply --stage infra --clusters` and full `apply --clusters` fail before rendering
when the selected clusters share host services with unselected clusters;
include every consumer or run without `--clusters`.

Fixed storage layout:

- The only user-home state is `~/.bootwright/contexts.yaml`, containing that
  user's current context selection only. Direct `sudo bootwright ...` uses
  root's own private selection; normal `bootwright ...` commands preserve the
  caller's registry across internal sudo re-exec.
- The root-managed tree is `/var/lib/bootwright/`, mode `0700`.
- `/var/lib/bootwright/` contains only shared cache/tooling and named
  contexts:
  - `cache/ansible-venv/`
  - `cache/ansible-bundles/<version-or-digest>/`
  - `contexts/<context>/`
- Each context has `input/`, `secrets/`, `rendered/`, `runs/`,
  `managed-services/`, `provider-state/`, `trust/`, and `clusters/`.
- Rendered reviewable output lives under `rendered/`, including
  `effective-state.yaml`, `bootwright.lock.yaml`,
  `ansible/{inventory,vars}.yaml`, and
  `storage/<storageCluster>/` tool inputs, while installer files live under
  `clusters/<cluster>/rendered/installer/`.
- Secret-inlined installer inputs and install records live under
  `clusters/<cluster>/runtime/`.
- Generated cluster access material lives under `clusters/<cluster>/secrets/`.
- Context-managed SSH host trust lives under `trust/ssh/`; `hosts.json`
  records Host name, address, key type, public key, and fingerprint, while
  `known_hosts` is the OpenSSH file used by strict host-key checking.
- Apply ledgers, leases, task logs, run logs, and artifacts live under `runs/`;
  per-cluster apply logs live under
  `clusters/<cluster>/runs/<run>/bootwright.log`.
- Managed service files live under `managed-services/<component-name>/`.
  Artifact server web roots mount only `managed-services/<component-name>/public/`;
  TLS keys and generated config stay outside the served root.
- Provider, substrate, and BMC emulator state not owned by one cluster lives
  under `provider-state/`. Cluster-owned provider state lives under
  `clusters/<cluster>/runtime/provider-state/`.
- `context init <name> -f <path> --yes` replaces the entire context directory
  after validating staged input.
- `context update <name> -f <path>` requires an existing context and replaces
  only `input/`, preserving secrets, rendered output, cluster state, run
  history, managed service files, and provider state.

Generated output boundaries:

- User-authored YAML lives under
  `/var/lib/bootwright/contexts/<context>/input/`.
- Placeholder installer output lives under
  `/var/lib/bootwright/contexts/<context>/clusters/<cluster>/rendered/installer/`.
- Bootwright-managed secret-inlined runtime installer output lives under
  `/var/lib/bootwright/contexts/<context>/clusters/<cluster>/runtime/installer/`.
- External tool input exports written by
  `bootwright render --output-dir <dir> --sensitive` live under the
  requested output directory and include
  `openshift-install/<cluster>/{install,agent}-config.yaml`,
  optional `openshift-install/<cluster>/openshift/` manifests, and Ansible
  inventory and vars files. The command must fail before writing files when
  `--sensitive` is omitted, because the OpenShift install input export
  contains secret material and must be treated as local runtime output.
