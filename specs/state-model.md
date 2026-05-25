# Desired-State Model

Bootwright desired state uses `apiVersion: bootwright.io/v1alpha1` and six
user-authored kinds. The schema intentionally tracks the inputs consumed by
`openshift-install` for agent installs:

- `ContainerCluster` owns install intent and the fields that render mostly to
  `install-config.yaml`.
- `ClusterInfra` owns the selected machines, install endpoints, platform render
  mode, and managed infra components.
- `NetworkConfig` owns reusable `machineNetwork[]` plus the NMState template
  rendered into `agent-config.yaml`.
- `InfraProvider` owns substrate inventory and capabilities.
- `Environment` owns fleet-wide defaults, context resource selection, secret
  sources, proxy defaults, registry mirrors, and Bootwright component image
  pins.
- `Host` owns SSH reachability for substrate or service hosts.

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

  bastion:
    hostRef: lab-host

  resources:
    - hosts.yaml
    - networks.yaml
    - provider.yaml
    - cluster-infra.yaml
    - container-cluster.yaml

  secrets:
    openshift-pull-secret:
    cluster-admin-pub-key:
      file: ~/.ssh/bootwright-ssh-key.pub
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

  proxy:
    httpProxy: http://proxy.example.test:3128
    httpsProxy: http://proxy.example.test:3128
    noProxy:
      - .example.test
      - 192.168.133.0/24
    auth:
      proxyAuthRef:
        name: proxy-credentials
    useFor:
      bootwright: true
      clusterInstall: true

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

  ntpSources:
    - 0.pool.ntp.org
    - 192.168.133.1

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
- `bastion.hostRef` is required and references the `Host` used for
  bastion-side Bootwright and OpenShift installer actions. Ansible connects to
  the referenced Host through `Host.spec.ssh.addressName` and the matching
  `Host.spec.addresses[].address`. It does not synthesize `localhost` when the
  CLI happens to run on the same machine.
- `secrets` declares names, not bytes. An empty entry resolves to
  `<context>/secrets/<name>` and must be populated with `bootwright secret set`
  before the consuming workflow runs. `file:` resolves to the declared local
  path. `generated:` resolves under the context secrets directory; generated
  credentials may be populated with either `bootwright secret set` or
  `bootwright secret generate`.
- TLS-pair consumers use the certificate at `file:` plus the private key at
  `keyFile:` for file-sourced secrets. Context-local and generated TLS pairs
  resolve as `<context>/secrets/<name>` and `<context>/secrets/<name>.key`.
- `clusterTrust.caBundleRefs[]` is optional fleet-wide CA trust rendered into
  every selected cluster install. Entries reference PEM CA bundle secrets.
- `proxy.httpProxy`, `proxy.httpsProxy`, and `proxy.noProxy` keep installer
  field names.
- `proxy.useFor.bootwright` applies to Bootwright runtime actions. When
  `proxy` is declared and this flag is omitted, it defaults to `true`.
- `proxy.useFor.clusterInstall` renders the proxy into installer input. When
  `proxy` is declared and this flag is omitted, it defaults to `true`.
- A managed proxy component and an external environment proxy URL are mutually
  exclusive for the same loaded state.
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
- `ntpSources[]` is optional. Each entry must be a parseable IP address or DNS
  hostname, and duplicate entries are rejected.

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
    sshKeyRef:
      name: cluster-admin-pub-key
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
  controller-facing SSH endpoint. When `spec.ssh.user` is omitted, Bootwright
  does not render `ansible_user`; SSH chooses the local account or configured
  host-specific user. Set `spec.ssh.user` only when Bootwright must force a
  provider-host SSH login name.
- `spec.ssh.keyRef.name`, when set, references `Environment.spec.secrets`.
- `spec.capabilities[]` is a typed tag list used by provider capabilities to
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
          disableCertificateVerification: true
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

Artifact publishers declare reusable generated-artifact publication services:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata:
  name: host-services
spec:
  artifactPublishers:
    - name: default
      http:
        hostRef:
          name: services-host
        port: 8443
        routes:
          redfishVirtualMedia:
            addressName: lab-lan
          clusterInstall:
            addressName: lab-lan
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
- `artifactPublishers[].http` declares one reusable HTTPS publication service
  for generated cluster artifacts, using a self-signed certificate generated
  on the provider host at apply time. Operators author the service host and
  may set the port and bind generated-artifact consumer routes to named
  addresses on that host. The renderer derives the bind address from
  Bootwright defaults.
- `artifactPublishers[].http.port` defaults to `8443`.
- `artifactPublishers[].http.routes.redfishVirtualMedia.addressName` is used
  for BMC ISO fetch URLs. This route should usually reference an IP-address
  entry on the publisher `Host`, because BMC firmware often cannot resolve DNS
  aliases even when the bastion can.
- `artifactPublishers[].http.routes.clusterInstall.addressName` is used for
  disconnected `bootArtifactsBaseURL`.
- If an artifact route is omitted, Bootwright falls back to the publisher
  host's non-loopback SSH address or the referenced cluster network gateway.
- Artifact publisher selection is global in `v1alpha1`: a cluster that needs
  generated artifact publication requires exactly one
  `artifactPublishers[].http` capability in the loaded state.
- Capability names are scoped by capability kind.

## ClusterInfra

`ClusterInfra` owns the infrastructure selected for one cluster: platform
render mode, endpoints, selected machines, and managed infra components.

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
- `providedBy.loadBalancer` references
  `components.loadBalancers[].name`. `providedBy.address` references
  `components.loadBalancers[].bindAddresses[].name` and is required when the
  load balancer has more than one bind address.
- `components.loadBalancers[]`, `proxy`, `nameResolution`, and `registry`
  remain valid sibling component slots.
- `components.loadBalancers[].bindAddresses[].port` is not supported; listener
  ports are derived from endpoint names.
- Bare-metal Redfish virtual-media boot and disconnected agent installs derive
  generated artifact publication from a provider `artifactPublishers[].http`
  capability.
- For vSphere multi-NIC installs, the first adapter network must correspond to
  the machine network unless `platform.vsphere.nodeNetworking` or profile
  `nodeNetworking` says otherwise.

Bootwright-provisioned load balancer endpoints reference named component bind
addresses:

```yaml
endpoints:
  api:
    providedBy:
      loadBalancer: control-plane
      address: control-plane-ip
  apiInt:
    providedBy:
      loadBalancer: control-plane
      address: control-plane-ip
  ingress:
    providedBy:
      loadBalancer: apps
      address: apps-ip

components:
  loadBalancers:
    - name: control-plane
      from:
        provider: lab-provider
        name: default
      bindAddresses:
        - name: control-plane-ip
          ip: 192.168.133.10
    - name: apps
      from:
        provider: lab-provider
        name: default
      bindAddresses:
        - name: apps-ip
          ip: 192.168.133.11
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
- `agent-config.yaml hosts[].interfaces[]` from provider MAC inventory or
  deterministic generated MACs for virtual substrates that Bootwright creates.
- Provider or generated machine MACs into matching NMState interfaces when
  present.
- Install-time OpenShift extra manifests under `openshift/` for declared API
  and ingress serving certificates. Placeholder render output redacts Secret
  data; runtime and `--sensitive` output include TLS material.
- Managed non-machine components from `ClusterInfra.spec.components`.
- Shared provider service identities, consumers, host placement, conflict
  fields, and mergeable overlays from the resolved service graph. Mergeable
  overlays are rendered into generated Ansible vars without mutating authored
  desired state.
- Generated artifact publisher components when a cluster needs agent ISO or
  boot-artifact publication.

## Validation Rules

Validation rejects:

- Unknown fields and unsupported kinds.
- Any old network reference field under providers, machines, or endpoints.
- Any old cluster-to-infra top-level reference on `ContainerCluster`.
- Removed `ContainerCluster.spec.install` fields: `baseDomain`,
  `imageDigestSources`, `installConfigOverrides`, and `agentConfigOverrides`.
- Multiple `ClusterInfra` references inside one `ContainerCluster`.
- `ContainerCluster` node references that do not resolve to a selected machine.
- OpenShift clusters without a pull secret reference after normalization.
- OKD clusters that set an OpenShift release channel.
- Missing, unknown, or incomplete endpoint keys.
- Endpoint entries with zero or multiple ownership fields.
- `providedBy.loadBalancer` or `providedBy.address` references that do not
  resolve to declared load balancers and bind addresses.
- Unreferenced load balancers or named bind addresses.
- Endpoint VIPs or machine overlay IPs outside selected machine networks.
- Bare-metal machines selected by a non-bare-metal platform.
- vSphere platform selections backed by a non-vSphere machine profile.
- External environment proxy or registry URLs that conflict with managed
  `ClusterInfra` proxy or registry components.
- Clusters that need generated artifact publication unless exactly one provider
  `artifactPublishers[].http` capability is declared and reachable from the
  required audience.
- Shared provider service consumers with the same rendered service identity
  but incompatible host, role, realisation, bind address, port, or selected
  capability.

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
YAML files or directories into that context's `input-files/` directory. Input
directories are walked for YAML files unless exactly one discovered
`Environment` sets `spec.resources`, in which case only that environment file
plus the listed files are loaded.
Unknown fields are rejected at decode time, and all loaded objects are
normalized and validated before any render or apply step.
Context-backed commands fail before doing work when the selected context is
not structurally ready or the local host does not match the declared
`Environment.spec.bastion.hostRef`; `bootwright context validate` reports each
checked aspect as `OK` or `MISSING`.

Primary commands:

```text
bootwright context init lab -f ./examples/sno-libvirt-redfish
bootwright context update lab -f ./examples/sno-libvirt-redfish
bootwright context validate
bootwright context use lab
bootwright print-env [--sensitive]
bootwright secret list
bootwright secret show --name <secret-name>
bootwright secret set openshift-pull-secret --pull-secret <path>
bootwright secret generate
bootwright check syntax
bootwright render installer --scope <cluster>
bootwright render --output-dir ./rendered --sensitive
bootwright apply infra --yes
bootwright apply infra --parallelism 4 --yes
bootwright apply infra --scope managed-01 --yes
bootwright apply cluster --yes
bootwright apply cluster --override --yes
bootwright status
bootwright status --watch
bootwright destroy cluster --yes
bootwright destroy infra --yes
bootwright destroy infra --scope http-server --yes
```

Human text output is for operators and is not a parse-stable interface. It is
grouped into sections, status labels, artifact groups, summaries, and
actionable check remediation.
Commands that support `--output json` expose the stable automation surface and
must emit only JSON on stdout. Shell-export commands such as
`bootwright print-env` intentionally emit only `export ...` lines. `secret
show` intentionally emits raw secret bytes on stdout and is a sensitive
raw-output exception.
Apply commands may execute independent tasks concurrently. Operators can tune
task scheduling with `--parallelism`, `--parallelism-per-host`, and
`--parallelism-redfish`; `0` for any of those flags means Bootwright uses the
maximum safe automatic value. Explicit limits only reduce automatic
concurrency; provider-host and Redfish safety locks still apply.
`apply <target> --dry-run` is a plan-only render and command preview. It does
not run host, tool, secret, BMC, or cluster readiness checks and does not run
Ansible; operators must run `bootwright check <target>` for readiness.
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
Every apply writes `<state-dir>/workflow/current-apply.json` atomically. The
ledger records the run ID, target, scope, selected concurrency limits, task
IDs, task dependencies, task statuses, timestamps, and per-task
`ansible-output.log` paths under the root-managed runtime directory.
Cluster-owned tasks also record
`/var/lib/bootwright/contexts/<context>/workflow/runs/<run>/clusters/<cluster>/install.log`.
Task statuses are `pending`, `ready`, `running`, `blocked`, `skipped`, `ok`,
`failed`, and `cancelled`.
While an apply process is active, Bootwright also refreshes
`<state-dir>/workflow/current-apply.lease.json`. Mutating workflow commands
must block when the current apply ledger has a fresh lease. If the ledger still
says `running` but its lease is missing or stale, the next `apply` or `destroy`
marks that previous run `cancelled` before continuing.
`bootwright status` reads local context state without contacting provider
hosts, BMCs, or clusters. Text output summarizes desired-state load status,
declared secret material presence, installer freshness, the current apply,
progress counts, running work, cluster task state, blocked work, failures, and
next steps. It reports whether a running apply lease is fresh or stale.
`bootwright status --watch` refreshes the same readable view until the current
apply reaches a terminal state or its lease is stale.
`bootwright status --output json` includes the full current apply ledger and
activity state when present.
`destroy infra --scope http-server` is a reserved destroy-only scope that
removes only the generated artifact HTTP service used for BMC agent ISO fetches.
Filtered `apply infra --scope` and `apply all --scope` fail before rendering
when the selected clusters share provider services with unselected clusters;
include every consumer or run without `--scope`.

Fixed storage layout:

- The only user-writable registry is `~/.bootwright/contexts.yaml`.
- The root-managed tree is `/var/lib/bootwright/`, mode `0700`.
- Each context is `/var/lib/bootwright/contexts/<context>/` with
  `input-files/`, `secrets/`, `state/`, `runtime/`, `workflow/`, and
  `artifacts-server/tls/`.
- Shared runtime files, including `ansible-venv`, live directly under
  `/var/lib/bootwright/`.
- `context init <name> -f <path> --yes` replaces the entire context directory
  after validating staged input and bastion locality.
- `context update <name> -f <path>` requires an existing context and replaces
  only `input-files/`, preserving secrets, state, runtime, and workflow data.

Generated output boundaries:

- User-authored YAML lives under
  `/var/lib/bootwright/contexts/<context>/input-files/`.
- Placeholder installer output lives under
  `/var/lib/bootwright/contexts/<context>/state/installer/<cluster>/`.
- Bootwright-managed secret-inlined runtime installer output lives under
  `/var/lib/bootwright/contexts/<context>/runtime/<cluster>/installer/`.
- External tool input exports written by
  `bootwright render --output-dir <dir> --sensitive` live under the
  requested output directory and include
  `openshift-install/<cluster>/{install,agent}-config.yaml`,
  optional `openshift-install/<cluster>/openshift/` manifests, and Ansible
  inventory and vars files. The command must fail before writing files when
  `--sensitive` is omitted, because the OpenShift install input export
  contains secret material and must be treated as local runtime output.
