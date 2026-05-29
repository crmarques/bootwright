# Desired-State Model

Bootwright desired state uses `apiVersion: bootwright.io/v1alpha1` and ten
user-authored kinds. The provisioning schema intentionally tracks the inputs
consumed by `openshift-install` for agent installs:

- `ContainerCluster` owns install intent and the fields that render mostly to
  `install-config.yaml`.
- `ClusterExtension` owns reusable post-install bootstrap components applied
  inside installed OpenShift or OKD clusters.
- `ClusterExtensionSet` owns ordered reusable platform profiles made from
  extensions and other sets.
- `ClusterExtensionBinding` owns cluster-to-extension attachment after install.
- `ClusterInfra` owns the selected machines, install endpoints, and platform
  render mode.
- `NetworkConfig` owns reusable `machineNetwork[]`, name-resolution service
  selections, and the NMState template rendered into `agent-config.yaml`.
- `InfraProvider` owns substrate inventory and capabilities.
- `InfraComponent` owns host-bound shared infra services and their routable
  endpoints.
- `Environment` owns fleet-wide defaults, context resource selection, secret
  sources, proxy defaults, registry mirrors, and Bootwright component image
  pins.
- `Host` owns SSH reachability and named addresses for substrate or service
  hosts.

Post-install components are deliberately not embedded under
`ContainerCluster.spec.install`. `ContainerCluster` remains focused on
provisioning an installed cluster; extensions are selected by `Environment`,
bound to clusters, and applied after the cluster is available.

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
    - container-cluster.yaml
    - cluster-extension-virtualization.yaml
    - cluster-extension-set.yaml
    - cluster-extension-binding.yaml

  secretStorage:
    mode: context

  defaults:
    install:
      pullSecretRef:
        name: openshift-pull-secret
      nodeSSH:
        keyPairRef:
          name: cluster-admin-ssh-key

  secrets:
    openshift-pull-secret:
    cluster-admin-ssh-key:
      generated:
        sshKeyPair:
          type: ed25519
          comment: bootwright-cluster-admin
    provider-host-ssh:
      file: ~/.ssh/bootwright-ssh-key
    bmc-credentials:
      generated:
        credentials:
          username: admin
    proxy-credentials:
      generated:
        credentials:
          username: proxy
    mirror-registry-credentials:
    mirror-registry-ca:
      file: ../secrets/mirror-registry-ca.crt
    corp-root-ca:
      file: ../secrets/corp-root-ca.crt
    prod-api-tls:
      file: ../secrets/prod-api-tls.crt
      keyFile: ../secrets/prod-api-tls.key
    prod-apps-wildcard-tls:

  containerClusters:
    - prod-3node

  infraComponents:
    proxies:
      - name: default
        default: true
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
        default: true
        componentRef:
          name: artifact-server
        routes:
          redfishVirtualMedia:
            endpoint: bmc
          clusterInstall:
            endpoint: cluster
    ntpSources:
      - 0.pool.ntp.org
      - 192.168.133.1

  proxyFor:
    bootwright: default
    clusterInstall: default

  registries:
    mirror:
      url: registry.example.test:5000
      credentialsRef:
        name: mirror-registry-credentials
      trustBundleRef:
        name: mirror-registry-ca

  clusterTrust:
    caBundleRefs:
      - name: corp-root-ca

```

Rules:

- Authored desired-state YAML uses block-style collections. Do not use
  flow-style mapping braces, inline lists, or empty inline maps in examples,
  e2e inputs, fixtures, or scaffold output.
- `resources[]`, when set, is a YAML file allow-list relative to the
  `Environment` file directory. The `Environment` file itself is always
  loaded.
- When `resources[]` is omitted, the current context input directory loads
  every discovered YAML file.
- A listed file is loaded as a complete YAML file; every Bootwright resource
  referenced by any selected resource must also be selected.
- When a selected `ClusterExtensionBinding` references extension sets or
  extensions, those referenced resource files must also be selected. When a
  selected `ClusterExtensionSet` references child sets or extensions, those
  files must also be selected.
- `containerClusters[]`, when set, is the effective fleet selection list for
  render, apply, status, destroy, and check flows. Omitted means every loaded
  `ContainerCluster`.
- Cluster selection filters extension bindings to selected clusters and omits
  extension resources that are reachable only from removed bindings.
- `defaults.install.pullSecretRef`, when set, is copied into selected
  OpenShift `ContainerCluster` install specs that omit `pullSecretRef`.
  Omitted means the conventional `openshift-pull-secret` secret name.
- `defaults.install.nodeSSH`, when set, is copied into selected
  `ContainerCluster` install specs that omit `nodeSSH`. Omitted means
  `keyPairRef.name: cluster-admin-ssh-key`.
- Bootwright and OpenShift installer actions run on the bastion host where the
  CLI is invoked. Desired state does not select that execution host.
- `infraComponents.proxies[]`, `infraComponents.nameResolution[]`,
  `infraComponents.artifactServers[]`, `infraComponents.registries[]`, and
  `infraComponents.ntpSources[]` are the environment service access catalog.
  Component entries are either `external` with direct access configuration, or
  `managed` with `componentRef.name` pointing at an `InfraComponent` arm of
  the matching kind.
- External artifact server entries use `spec.redfishVirtualMediaURL` and/or
  `spec.clusterInstallURL`. Managed artifact server entries use
  `componentRef.name` plus `routes` endpoint selectors.
- `infraComponents.nameResolution[].additionalIngressHosts[]` is optional.
  Values from environment entries and managed
  `InfraComponent.spec.nameResolution.additionalIngressHosts[]` merge into DNS
  host records that point at each consuming cluster's ingress VIP.
- `proxyFor.bootwright` and `proxyFor.clusterInstall` select entries from
  `infraComponents.proxies[]`. Omitted values default to `none`; `none` is a
  reserved value that disables proxy use for that consumer.
- A `proxyFor.bootwright` selection may point at an external proxy for every
  command phase. If it points at a managed proxy, Bootwright must not depend on
  that proxy during `apply bastion` because the proxy component does not exist
  until infrastructure convergence. Later checks, renders, applies, and status
  views may use the managed proxy after its selected `InfraComponent` has been
  converged; pre-infra commands should report that bootstrap limitation rather
  than silently pretending the proxy is active.
- At most one entry per environment service list may set `default: true`.
- `infraComponents.artifactServers[].routes.redfishVirtualMedia.endpoint`
  selects the artifact server endpoint used in BMC ISO fetch URLs.
- `infraComponents.artifactServers[].routes.clusterInstall.endpoint` selects
  the artifact server endpoint used for disconnected agent-install boot
  artifacts.
- `secretStorage.mode` defaults to `source`. `source` preserves each declared
  secret source at runtime: `file:` resolves to the declared local path and
  context-local/generated material resolves under the context secrets directory.
  `context` makes workflows resolve declared material from
  `<context>/secrets/` after `bootwright secret materialize` copies file-sourced
  entries into context storage.
- `secrets` declares names, not bytes. An empty entry resolves to
  `<context>/secrets/<name>` and must be populated with `bootwright secret set`
  before the consuming workflow runs. `generated:` resolves under the context
  secrets directory; generated credentials may be populated with either
  `bootwright secret set` or `bootwright secret generate`.
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
- `clusterTrust.caBundleRefs[]` is optional fleet-wide CA trust rendered into
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
- `infraComponents.ntpSources[]` is optional. Each entry must be a parseable IP
  address or DNS hostname, and duplicate entries are rejected.

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
    type: openshift
    release:
      version: 4.20.15

  install:
    method: agent
    mode: connected
    pullSecretRef:
      name: openshift-pull-secret
    nodeSSH:
      keyPairRef:
        name: cluster-admin-ssh-key
    additionalTrustBundleRefs:
      - name: cluster-extra-ca
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

  controlPlane:
    name: master
    replicas: 3

  compute:
    - name: worker
      replicas: 0

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
      machineRef:
        clusterInfra: prod-3node-infra
        name: master-0
    - hostname: master-1
      role: master
      machineRef:
        clusterInfra: prod-3node-infra
        name: master-1
    - hostname: master-2
      role: master
      machineRef:
        clusterInfra: prod-3node-infra
        name: master-2
```

Rules:

- `install.mode` defaults to `connected`.
- `install.method` defaults to `agent`; other methods are not accepted yet.
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
  `additionalTrustBundle` order is environment `clusterTrust`, mirror registry
  trust, then cluster refs, de-duplicated by secret name.
- `servingCertificates.apiServer.namedCertificates[]` renders OpenShift API
  serving certificate Secrets plus `APIServer/cluster`. `names[]` is required
  and must not target the internal `api-int.<cluster>.<baseDomain>` endpoint.
- `servingCertificates.ingress.defaultCertificateRef` renders the default
  ingress certificate Secret plus `IngressController/default`. The certificate
  must cover `*.apps.<cluster>.<baseDomain>`.
- Every node binds to a `ClusterInfra` machine by
  `nodes[].machineRef.clusterInfra` and `nodes[].machineRef.name`.
- In v1, all nodes in one cluster must reference the same `ClusterInfra`.
- `networking` is required. `clusterNetwork[]` and `serviceNetwork[]` must
  each contain at least one valid CIDR. Each `clusterNetwork[].hostPrefix` is
  required and must be greater than the CIDR prefix length and no larger than
  the address width. `networkType` is optional and, when set, must not contain
  leading or trailing whitespace; Bootwright does not enumerate CNI names so
  future OpenShift network types remain possible.

## Cluster Extensions

Cluster extensions model initial post-install bootstrap components. They are
for early platform setup, not for replacing long-term day-2 GitOps management.

`ClusterExtension` declares one reusable component. MVP types are
`olm-operator` and `manifest-set`.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: ClusterExtension
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
      sourceNamespace: openshift-marketplace
      installPlanApproval: Automatic

    customResources:
      - apiVersion: hco.kubevirt.io/v1beta1
        kind: HyperConverged
        metadata:
          name: kubevirt-hyperconverged
          namespace: openshift-cnv

  readiness:
    timeout: 30m
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

`provides[]` advertises extension-provided cluster capabilities consumed by
cross-cluster substrates. The initial accepted value is `kubevirt`. An extension
that provides a capability must declare readiness checks so dependent work waits
for the actual platform capability, not just resource submission.

`manifest-set` extensions apply existing YAML files in declared order. Paths
are relative to the `ClusterExtension` file directory. Non-Bootwright YAML under
an extension `manifests/` directory is treated as extension payload, not desired
state to decode.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: ClusterExtension
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

`ClusterExtensionSet` declares an ordered reusable group:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: ClusterExtensionSet
metadata:
  name: virtualization-platform
spec:
  extensionSets:
    - name: base-platform
  extensions:
    - name: openshift-virtualization
```

Expansion is deterministic: expand `extensionSets` in declared order, then
append direct `extensions` in declared order. Duplicate extension names after
expansion are allowed and de-duplicated by first occurrence. Cycles are
rejected.

`ClusterExtensionBinding` attaches extension sets and direct extensions to
clusters selected by name:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: ClusterExtensionBinding
metadata:
  name: demo-ocp-extensions
spec:
  clusterSelector:
    names:
      - demo-ocp

  applyAfter:
    phase: clusterInstalled

  extensionSets:
    - name: virtualization-platform

  extensions:
    - name: console-customization

  policy:
    prune: false
    serverSideApply: true
    fieldManager: bootwright
    continueOnError: false
```

Binding expansion follows the same order as sets: referenced
`extensionSets`, then direct `extensions`, with first occurrence
de-duplication. The expanded order becomes the apply order for each selected
cluster. `applyAfter.phase` only supports `clusterInstalled` in the MVP.

Rules:

- `olm-operator` requires `olm.namespace.name` and a complete subscription:
  `name`, `package`, `channel`, `source`, `sourceNamespace`, and
  `installPlanApproval`.
- `subscription.startingCSV`, when set, is rendered to
  `Subscription.spec.startingCSV` to request a specific catalog CSV while still
  declaring the channel.
- `installPlanApproval` is `Automatic` or `Manual`.
- If `operatorGroup` is set, `operatorGroup.name` is required.
- `customResources[]` may be empty. When present, each custom resource must
  set `apiVersion`, `kind`, `metadata.name`, and `metadata.namespace` in the
  MVP.
- Generated OLM resources apply in this order: Namespace when
  `namespace.create` is true, OperatorGroup when set, Subscription, then
  `customResources[]`.
- `manifest-set.manifests[]` must not be empty. Each path must be relative,
  remain under the extension file directory, name an existing non-symlink
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
- `ClusterExtensionBinding.spec.clusterSelector.names[]` must name existing
  `ContainerCluster` objects. Label selectors are not part of the MVP.
- `policy.serverSideApply` defaults to `true`,
  `policy.fieldManager` defaults to `bootwright`, and
  `policy.continueOnError` defaults to `false`.
- `policy.prune: true` is rejected in the MVP.
- `policy.continueOnError: true` is rejected in the MVP.
- Future extension types may include `kustomize` and `helm`; they are not
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

  physical:
    vlan: 0
```

Rules:

- `spec.machineNetwork[]` renders to
  `install-config.yaml networking.machineNetwork[]`.
- A given `spec.machineNetwork[].cidr` may appear in exactly one
  `NetworkConfig`; duplicates across objects are invalid.
- `spec.template.networkConfig` renders to
  `agent-config.yaml hosts[].networkConfig` after per-machine overlays.
- `spec.dnsRefs[]` selects entries from
  `Environment.spec.infraComponents.nameResolution[].name`. These refs are
  Bootwright service-selection intent and must stay outside the raw NMState
  template. Resolved IPs are appended to generated
  `dns-resolver.config.server` entries.
- Common overlays should be limited to `addresses[]`; advanced users may set a
  full machine-level `networkConfig` override.
- Static overlay IPs must fit at least one referenced machine network CIDR.
- Optional substrate hints are `libvirt`, `vsphere`, `kubevirt`, or `physical`.
  They are Bootwright provisioning hints, not the OpenShift cluster network.

## Host

`Host` owns how Bootwright reaches a machine that runs provider or service
actions. It does not own cluster nodes; nodes are declared through
`ContainerCluster.spec.nodes[]` and selected machines in `ClusterInfra`.

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
- `spec.capabilities[]` is required. The current canonical tags are `libvirt`
  and `container-runtime`; provider and service capabilities use them to
  select hosts for substrate or service work.

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
        hostClusterRef:
          name: metal-ocp
        namespace: bootwright-child-ocp
        storageClassRef:
          name: lvms-vg1
```

`hostClusterRef` names a Bootwright `ContainerCluster` whose generated
kubeconfig is used at runtime. Use `kubeconfigRef` instead when the KubeVirt
host cluster is external to Bootwright:

```yaml
kubevirt:
  kubeconfigRef:
    name: external-virt-cluster-kubeconfig
  namespace: bootwright-child-ocp
```

Rules:

- Provider credentials are always `SecretRef`s.
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
- KubeVirt profiles must set exactly one of `hostClusterRef` or
  `kubeconfigRef`.
- `hostClusterRef.name` references a loaded Bootwright `ContainerCluster`. The
  host cluster must have a selected `ClusterExtensionBinding` that applies an
  extension with `provides: [kubevirt]`.
- `kubeconfigRef.name` references `Environment.spec.secrets`; the secret stores
  a kubeconfig path or context-local kubeconfig material and never stores bytes
  in desired state.
- `kubevirt.namespace` is required. `storageClassRef.name` is optional.
- KubeVirt-backed machines must use a `NetworkConfig` with `spec.kubevirt.nad`.
  The referenced NAD is supplied by the operator or by a parent-cluster
  `manifest-set` extension.
- KubeVirt `hostClusterRef` dependencies must be acyclic. A cluster cannot host
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
  `loadBalancer`, `proxy`, `nameResolution`, or `registry`.
- `artifactServer.hostRef.name` references a `Host` with `container-runtime`.
- `artifactServer.bindAddress` defaults to `0.0.0.0`.
- `artifactServer.listeners[]` declares the ports the container listens on.
  Supported protocols are `http` and `https`. If omitted, Bootwright defaults
  to one HTTPS listener named `https` on port `8443`.
- `artifactServer.endpoints[]` names routeable service addresses. Each endpoint
  chooses a listener, and `hostAddress` must match a
  `Host.spec.addresses[].name` on `artifactServer.hostRef`; Bootwright uses
  that address object's `address` value in routed URLs and TLS names.
- Endpoint names are opaque route selectors, not DNS labels. They are the
  stable binding surface used by
  `Environment.spec.infraComponents.artifactServers[].routes`.
- The artifact server is implemented as a containerized static file service
  that serves generated ISOs and disconnected boot artifacts. HTTPS listeners
  use a self-signed certificate generated on the component host.
- `loadBalancer`, `proxy`, `nameResolution`, and `registry` arms declare
  their host placement and component-specific bind surface. Environment
  catalog entries decide which consumers use proxy, DNS, registry, or artifact
  services.
- `proxy.endpoints[]`, `nameResolution.endpoints[]`, and
  `registry.endpoints[]`, when set, use `hostAddress` values that must match
  `Host.spec.addresses[].name` on their component `hostRef`.
- `nameResolution.additionalIngressHosts[]` adds explicit host records that
  point at the consuming cluster's ingress VIP. Entries from the component and
  matching environment service catalog entry merge without mutating authored
  desired state.

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
      externalVip: 192.168.133.10
    apiInt:
      externalVip: 192.168.133.10
    ingress:
      externalVip: 192.168.133.11

  components:
    machines:
      - name: master-0
        from:
          provider: rack1-baremetal
          name: rack1-srv1
        networkConfig:
          ref:
            name: rack1-bonded-machine
          addresses:
            - interface: bond0
              ipv4:
                - ip: 192.168.133.20
                  prefix-length: 24
      - name: master-1
        from:
          provider: rack1-baremetal
          name: rack1-srv2
        networkConfig:
          ref:
            name: rack1-bonded-machine
          addresses:
            - interface: bond0
              ipv4:
                - ip: 192.168.133.21
                  prefix-length: 24
      - name: master-2
        from:
          provider: rack1-baremetal
          name: rack1-srv3
        networkConfig:
          ref:
            name: rack1-bonded-machine
          addresses:
            - interface: bond0
              ipv4:
                - ip: 192.168.133.22
                  prefix-length: 24

```

Rules:

- `platform.type` is required: `baremetal`, `vsphere`, `none`, or `external`.
  This is the installer platform render mode, not the substrate type; substrate
  ownership remains with selected `InfraProvider` machines or profiles.
- Bare-metal `provisioningNetwork`, when set, is `disabled`, `managed`, or
  `unmanaged`. `disabled` renders OpenShift bare metal in "no dedicated
  provisioning network" mode and is appropriate for agent installs using
  Redfish virtual media on the existing machine network.
- `components.machines[]` selects provider machines or profiles and applies
  per-machine network overlays.
- `endpoints` is a map keyed by exactly `api`, `apiInt`, and `ingress`.
  Each endpoint must set exactly one ownership field:
  `vip` for OpenShift-managed VIPs, `externalVip` for operator-owned external
  load balancers, or `providedBy` for Bootwright-provisioned load balancers.
- `providedBy.componentRef.name` references an `InfraComponent` with
  `spec.loadBalancer`. `providedBy.address` references
  `spec.loadBalancer.bindAddresses[].name` and is required when the load
  balancer has more than one bind address.
- Bare-metal Redfish virtual-media boot and disconnected agent installs derive
  generated artifact publication from the environment-selected artifact server
  component and route endpoints.
- For vSphere multi-NIC installs, the first adapter network must correspond to
  the machine network unless `platform.vsphere.nodeNetworking` or profile
  `nodeNetworking` says otherwise.

Bootwright-provisioned load balancer endpoints reference named component bind
addresses:

```yaml
endpoints:
  api:
    providedBy:
      componentRef:
        name: control-plane
      address: control-plane-ip
  apiInt:
    providedBy:
      componentRef:
        name: control-plane
      address: control-plane-ip
  ingress:
    providedBy:
      componentRef:
        name: apps
      address: apps-ip
```

## Render Behavior

Bootwright renders:

- `install-config.yaml` metadata, base domain, pull secret, SSH key, pools,
  cluster/service networking, proxy, trust, mirrors, and release inputs from
  `ContainerCluster` plus `Environment`.
- `install-config.yaml networking.machineNetwork[]` from
  `NetworkConfig.spec.machineNetwork[]` referenced by selected machines.
- `install-config.yaml` API and ingress VIPs from endpoint `vip`,
  endpoint `externalVip`, or the referenced load balancer bind address IP.
- `install-config.yaml platform.<type>` from `ClusterInfra.spec.platform`,
  endpoints, and provider capabilities. Single-node clusters render
  `platform.none` unless `ClusterInfra.spec.platform.type` is `external`,
  matching the upstream agent installer constraint for one control-plane and
  zero compute nodes.
- `agent-config.yaml hosts[]` by matching `ContainerCluster.spec.nodes[]` to
  `ClusterInfra.spec.components.machines[]`.
- `agent-config.yaml hosts[].networkConfig` from the referenced
  `NetworkConfig` template plus machine overlays or full overrides.
- `agent-config.yaml hosts[].networkConfig.dns-resolver.config.server` from
  static NMState servers plus `NetworkConfig.spec.dnsRefs[]`, de-duplicated in
  that order.
- `agent-config.yaml hosts[].interfaces[]` from provider MAC inventory or
  deterministic generated MACs for virtual substrates that Bootwright creates.
- Provider or generated machine MACs into matching NMState interfaces when
  present.
- `agent-config.yaml minimalISO` and `bootArtifactsBaseURL` for disconnected
  installs that select an artifact server `clusterInstall` route.
- `agent-config.yaml additionalNTPSources` from
  `Environment.spec.infraComponents.ntpSources[]`.
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
- Extension apply plans from selected `ClusterExtensionBinding` resources.
  OLM extensions generate Namespace, OperatorGroup, Subscription, and custom
  resources. Manifest-set extensions reference declared files and include file
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
- Missing, unknown, or incomplete endpoint keys.
- Endpoint entries with zero or multiple ownership fields.
- `providedBy.componentRef.name` or `providedBy.address` references that do
  not resolve to declared load balancer components and bind addresses.
- Unreferenced load balancers or named bind addresses.
- Endpoint VIPs or machine overlay IPs outside selected machine networks.
- Bare-metal machines selected by a non-bare-metal platform.
- vSphere platform selections backed by a non-vSphere machine profile.
- Invalid environment proxy or registry catalog entries, unresolved managed
  component refs, or conflicting service defaults.
- Clusters that need generated artifact publication unless
  `Environment.spec.infraComponents.artifactServers[]` selects an artifact
  server entry and the required route endpoint resolves.
- Shared infra component service consumers with the same rendered service identity
  but incompatible host, role, realisation, bind address, port, or selected
  capability.
- Missing `ClusterExtension`, `ClusterExtensionSet`, or
  `ClusterExtensionBinding` references.
- `ClusterExtensionSet` reference cycles.
- Unsupported cluster extension types, readiness check types, apply phases, and
  install plan approval values.
- Unsupported `ClusterExtension.spec.provides[]` capabilities. The only current
  value is `kubevirt`; duplicate values and capabilities without readiness
  checks are invalid.
- Unsafe `manifest-set` paths, including absolute paths, directory escapes,
  symlinks, missing files, and non-YAML extensions.
- KubeVirt profiles missing exactly one host reference, referencing a missing
  host cluster, referencing an undeclared kubeconfig secret, missing
  `namespace`, using a non-KubeVirt network config, or creating a cluster
  dependency cycle.
- `ClusterExtensionBinding` cluster selectors that name missing clusters.
- `ClusterExtensionBinding.policy.prune: true`, because pruning is not
  implemented in the MVP.
- `ClusterExtensionBinding.policy.continueOnError: true`, because task-level
  error continuation is not implemented in the MVP.

## CLI Contract

The CLI resolves desired state from the current named context. Contexts are
registered in `~/.bootwright/contexts.yaml` using only this list form:

```yaml
current: lab
contexts:
  - lab
```

All context data is derived from the fixed root-managed path
`/var/lib/bootwright/contexts/<context>/`; `context init` imports one or more
YAML files or directories into that context's `input/` directory. Input
directories are walked for YAML files unless exactly one discovered
`Environment` sets `spec.resources`, in which case only that environment file
plus the listed files are loaded.
Legacy map-shaped registries are rejected with an actionable reset message;
operators may remove `~/.bootwright/contexts.yaml` and recreate contexts with
`bootwright context init <name> -f <path> --yes`.
Unknown fields are rejected at decode time, and all loaded objects are
normalized and validated before any render or apply step.
Context-backed commands fail before doing work when the selected context is
not structurally ready. Controller-local actions run on localhost;
`bootwright context validate` reports each checked aspect as `OK` or
`MISSING`.

Primary commands:

```text
bootwright example init lab --output ./lab-input
bootwright context init lab -f ./lab-input
bootwright context update lab -f ./lab-input
bootwright context validate
bootwright context list
bootwright context use lab
bootwright context current [--short]
bootwright context delete lab [--purge --yes]
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
bootwright render installer --scope <cluster>
bootwright render installer --sensitive
bootwright render installer --output json
bootwright render --output-dir ./rendered --sensitive
bootwright apply infra --dry-run
bootwright apply infra --dry-run --output json
bootwright apply infra --yes
bootwright apply infra --parallelism 4 --yes
bootwright apply infra --scope managed-01 --yes
bootwright check cluster
bootwright check cluster --dry-run --output json
bootwright check extensions
bootwright apply cluster --dry-run
bootwright apply cluster --dry-run --output json
bootwright apply cluster --yes
bootwright apply cluster --override --yes
bootwright apply extensions --dry-run
bootwright apply extensions --dry-run --output json
bootwright apply extensions --yes
bootwright apply all --dry-run
bootwright apply all --dry-run --output json
bootwright apply all --yes
bootwright status
bootwright status --watch
bootwright status --output json
bootwright destroy cluster --yes
bootwright destroy cluster --dry-run --output json
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
`check syntax -f <path>` loads YAML files or directories directly and must not
require or mutate the current context. It is the pre-import validation path for
generated examples, copied examples, and CI jobs that review authored desired
state before `context init`.
Ansible-backed apply commands may execute independent tasks concurrently.
Operators can tune task scheduling with `--parallelism`,
`--parallelism-per-host`, and `--parallelism-redfish`; `0` for any of those
flags means Bootwright uses the maximum safe automatic value. Explicit limits
only reduce automatic concurrency; provider-host and Redfish safety locks still
apply. `apply extensions` uses direct cluster API tasks and must not expose
Ansible executable, become-password, provider-host, or Redfish flags.
`apply <target> --dry-run` is a plan-only action preview. It does
not run host, tool, secret, BMC, or cluster readiness checks and does not
mutate provider hosts, nodes, or clusters; operators must run
`bootwright check <target>` for readiness.
`apply extensions --dry-run` shows extension tasks, selected clusters,
expanded extension order, and generated resource summaries without mutating the
cluster.
`apply cluster` installs each selected cluster, then applies bound extensions
after the cluster install wait task. `apply extensions` uses the installed
cluster kubeconfig and `oc apply` directly for standalone extension convergence.
`apply all` includes infrastructure before the same cluster and extension
phases.
When an apply selects one `ContainerCluster`, raw Ansible stdout/stderr streams
to the terminal between Bootwright prerequisite output and the Bootwright
summary. When an apply selects two or more `ContainerCluster` objects,
Bootwright does not stream live Ansible output to the terminal; it prints a
`Logs` section with one install log path per cluster and keeps task output in
the task artifact log plus the owning cluster log.
`bootwright apply cluster --override` forces OpenShift agent install tasks to
run even when local runtime kubeconfig state reports that the target cluster is
already available. It is for reinstalling after the operator has reset or
replaced the target machines; it does not wipe disks, destroy substrate
machines, power off nodes, or remove provider services.
Without `--override`, `apply cluster` must not regenerate the agent ISO or
reboot nodes when Bootwright can prove that the selected cluster is already
installed for the same rendered desired inputs. Completed installs are proven by
the per-cluster install record, the non-secret desired-input fingerprint, and a
local kubeconfig probe that reports `ClusterVersion Available=True`. If the
stored fingerprint differs from the current rendered inputs, apply must stop and
require either `destroy cluster` or `apply cluster --override` after the
operator has reset or replaced target machines. If an interrupted run already
booted nodes, apply resumes at `openshift-install agent wait-for
install-complete` instead of creating a new ISO or rebooting machines. Extension
tasks still run after skipped or completed install tasks and use their own
per-extension desired hashes and readiness records for idempotency.
Every apply writes `<runs-dir>/current.json` atomically. The
ledger records the run ID, target, scope, selected concurrency limits, task
IDs, task dependencies, task statuses, timestamps, and per-task
`ansible-output.log` paths under
`<runs-dir>/history/<run>/tasks/<task>/`.
Cluster-owned tasks also record
`/var/lib/bootwright/contexts/<context>/runs/history/<run>/clusters/<cluster>/install.log`.
Per-cluster install state is stored under
`<runtime-dir>/install-records/<cluster>.json`; it records the desired
input fingerprint, install status, last safe phase, run ID, timestamps, and
node boot markers, but not secret bytes.
Per-extension state is stored under
`<runtime-dir>/extension-records/<cluster>/<extension>.json`; it records the
desired hash, status, phase, run ID, timestamps, observed resources, and last
observed readiness state, but not kubeconfig or secret bytes.
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
Filtered `apply infra --scope` and `apply all --scope` fail before rendering
when the selected clusters share provider services with unselected clusters;
include every consumer or run without `--scope`.

Fixed storage layout:

- The only user-writable registry is `~/.bootwright/contexts.yaml`.
- The root-managed tree is `/var/lib/bootwright/`, mode `0700`.
- `/var/lib/bootwright/` contains only shared cache/tooling and named
  contexts:
  - `cache/ansible-venv/`
  - `cache/ansible-bundles/<version-or-digest>/`
  - `contexts/<context>/`
- Each context has `input/`, `secrets/`, `rendered/`, `runtime/`, `runs/`,
  and `managed/`.
- Rendered reviewable output lives under `rendered/`, including
  `effective-state.yaml`, `bootwright.lock.yaml`, `ansible/{inventory,vars}.yaml`,
  and `installer/<cluster>/`.
- Secret-inlined installer inputs and install records live under `runtime/`.
- Apply ledgers, leases, task logs, artifacts, and cluster install logs live
  under `runs/`.
- Managed host/service files live under `managed/services/`, `managed/bmc/`,
  and `managed/substrate/`. Artifact server web roots mount only
  `managed/services/artifact-server/<provider>-<name>/public/`; TLS keys and
  generated config stay outside the served root.
- `context init <name> -f <path> --yes` replaces the entire context directory
  after validating staged input.
- `context update <name> -f <path>` requires an existing context and replaces
  only `input/`, preserving secrets, rendered output, runtime data, run
  history, and managed host/service files.

Generated output boundaries:

- User-authored YAML lives under
  `/var/lib/bootwright/contexts/<context>/input/`.
- Placeholder installer output lives under
  `/var/lib/bootwright/contexts/<context>/rendered/installer/<cluster>/`.
- Bootwright-managed secret-inlined runtime installer output lives under
  `/var/lib/bootwright/contexts/<context>/runtime/installer/<cluster>/`.
- External tool input exports written by
  `bootwright render --output-dir <dir> --sensitive` live under the
  requested output directory and include
  `openshift-install/<cluster>/{install,agent}-config.yaml`,
  optional `openshift-install/<cluster>/openshift/` manifests, and Ansible
  inventory and vars files. The command must fail before writing files when
  `--sensitive` is omitted, because the OpenShift install input export
  contains secret material and must be treated as local runtime output.
