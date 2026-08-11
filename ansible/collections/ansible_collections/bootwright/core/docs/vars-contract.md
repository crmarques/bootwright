# Ansible Vars Contract

Go renders the variable set consumed by embedded Ansible playbooks. The Go
renderer is the source of truth; this file documents the current shape for role
authors.

## Top-Level Facts

| Fact | Shape |
| --- | --- |
| `bootwright_environment` | environment defaults, proxy selections, mirror, component image declarations |
| `bootwright_machines` | machine SSH endpoints and capability tags |
| `bootwright_providers` | provider capability inventory |
| `bootwright_infra_components` | machine-bound infra services such as artifact servers |
| `bootwright_clusters` | per-cluster endpoints, networks, components, and nodes |
| `bootwright_storage_clusters` | managed storage apply inputs, seed hosts, cephadm files, operation files, and attachment contexts |
| `bootwright_managed_os_install_groups` | managed machine OS install groups for storage and service machines |
| `bootwright_provider_services` | provider/BMC service instances with rendered role names |
| `bootwright_infra_component_services` | InfraComponent service instances with rendered role names |
| `bootwright_controller_name_resolution_services` | managed name-resolution services projected for controller routing and exact-answer readiness probes |
| `bootwright_provider_machine_setups` | provider-machine setup roles selected by machine drivers |
| `bootwright_proxy` | effective proxy settings |
| `bootwright_resolved_ntp_sources` | resolved external and managed NTP addresses; consumed by the libvirt substrate network template for DHCP NTP options |
| `bootwright_kubevirt_host_kubeconfigs` | managed KubeVirt host cluster name → controller-local materialized kubeconfig path for the current non-dry Ansible playbook or task; omitted when no managed host kubeconfig is required |

For a real Ansible invocation, each KubeVirt `hostClusterRef` machine component
selected by the current playbook or task receives the same runtime path as its
entry in `bootwright_kubevirt_host_kubeconfigs`. The controller `virtctl`
provisioning playbook indexes this map by host cluster name. An execution
render never falls back to the durable encrypted host path when no runtime
entry exists. The map does not cover
`kubeconfigRef`: that arm follows ordinary declared-secret resolution, so
context material resolves from the task runtime secret store and an explicit
file source in source mode remains the operator-owned source path. Dry-run
creates no plaintext material and omits the map; its machine component retains
the logical managed-host path for command display only.

## Environment Shape

```yaml
bootwright_environment:
  name: lab
  baseDomain: example.test
  proxyFor:
    bootwright: default
    clusterInstall: default
  infraComponents:
    ntp:
      - name: external-01
        management: external
        address: ntp.example.test
      - name: lab-ntp
        management: managed
        componentRef: ntp-server
        endpointRef: cluster
    artifactServers:
      - name: default
        management: managed
        componentRef: artifact-server
```

## Cluster Shape

```yaml
bootwright_clusters:
  - name: prod-3node
    installMode: connected
    installMethod: agent
    nodeSSHPrivateKeyPath: /var/lib/bootwright/contexts/lab/secrets/cluster-admin-ssh-key
    baseDomain: example.test
    distribution:
      type: openshift
      release: { version, channel, image }
    endpoints:
      - name: api
        address: 192.168.133.10
        source:
          type: external
      - name: ingress
        address: 192.168.133.11
        source:
          type: infraComponent
          componentRef: apps
          bindAddressRef: apps-ip
    agentIsoPublishTargets:
      - stageHost: services-host
        stagePath: "{{ bootwright_managed_services_dir }}/artifact-server/public/__BOOTWRIGHT_AGENT_ISO_PUBLISH_TOKEN__/agent-prod-3node.iso"
        fetchUrl: https://192.168.133.1:8443/__BOOTWRIGHT_AGENT_ISO_PUBLISH_TOKEN__/agent-prod-3node.iso
        requiresHTTPS: true
        requiresByteRange: true
    networks:
      - name: rack1-bonded-machine
        cidr: 192.168.133.0/24
        gateway: 192.168.133.1
        machineNetwork:
          - { cidr: 192.168.133.0/24 }
        dnsServers: [192.168.133.1]
        template: {}
    components:
      - kind: machines
        name: master-0
        providerName: lab-libvirt-provider
        substrateRole: libvirt
        bmcRole: emulated
        bootRole: redfish
        externalBMC: true       # only on operator-owned external BMCs (bare-metal
                                # Redfish); reachability checks select probe
                                # targets from this fact, never from bmcRole
        substratePrepareRole: bootwright.core.machine_substrate_libvirt
        substratePrepareFrom: network
        substrateApplyRole: bootwright.core.machine_substrate_libvirt
        substrateApplyFrom: machine
        substrateDestroyRole: bootwright.core.machine_substrate_libvirt
        bmcApplyRole: bootwright.core.provider_service_bmc_emulated
        bmcDestroyRole: bootwright.core.provider_service_bmc_emulated
        bootApplyRole: bootwright.core.container_cluster_boot_redfish
        mediaPrepareRole: bootwright.core.container_cluster_media_libvirt
        cleanupMediaRole: bootwright.core.container_cluster_boot_redfish
        machineSetupRoles:
          - bootwright.core.provider_host_libvirt
        interfaces:
          - name: primary
            macAddress: 52:54:00:12:34:56
            networkAttachment:
              kind: kubevirt
              kubevirt: { nad: bootwright-child/child-machine-net }
          - name: ceph-public
            macAddress: 52:54:00:65:43:21
            networkAttachment:
              kind: kubevirt
              kubevirt: { nad: bootwright-child/ceph-public }
        networkAttachment:
          # Present for the machine-wide attachmentRef form. KubeVirt
          # interfaceAttachments[] instead resolve under each interfaces[] item.
          kind: libvirt | vsphere | kubevirt | baremetal
          libvirt: { bridge }
          vsphere: { portgroup }
          kubevirt: { nad }
          baremetal: { vlan }
        requiresKVM: true
        networkConfig:
          ref: rack1-bonded-machine
          addresses:
            - interface: bond0
              ipv4:
                - { ip: 192.168.133.20, prefix-length: 24 }
        primaryIPAddress: 192.168.133.20
        fromProfile: <profile>  # profile-backed machines only
        profile: {}             # inlined provider profile when present
        kubevirt:
          hostClusterRef: metal-ocp
          kubeconfig: <task-runtime-secrets>/bootwright-material-<id>/kubeconfig
          namespace: bootwright-child-ocp
          storageClassRef: lvms-vg1
        vsphere:                # vCenter-managed machines; consumed by
          server: vcenter.example.test  # machine_substrate_vsphere and the
          port: 443                     # vsphere boot/media roles
          credentialsRef: vcenter-credentials
          credentialsPath: /context/secrets/vcenter-credentials  # user:password file
          disableCertificateVerification: true
          failureDomain: dc1-zone-a     # resolved placement
          topology: { datacenter, computeCluster, datastore, folder, resourcePool, networks }
          isoStaging: { datastore, folder }  # defaults applied by the renderer
          template: rhcos-template      # optional clone source; absent = blank create
        boot:
          redfish: {}         # selected encrypted bare-metal nodes additionally carry
                              # requireTPM2: true; the preflight and boot role prove
                              # exact ComputerSystem TrustedModules evidence
                              # before any Redfish mutation. artifactCertificate carries
                              # host/port (the artifact endpoint origin) so the
                              # certificate import never re-parses fetchUrl
          agentIso: {}        # HTTP-fetched media carries transferProtocol
                              # (HTTP | HTTPS, renderer-derived); vsphere machines:
                              # stageHost is localhost and fetchUrl is the
                              # "[datastore] path" attach target with no protocol
          readiness: {}       # ssh (cluster flow) or none (managed OS)
          media:
            libvirt: {}         # consumed only by bootwright.core.container_cluster_media_libvirt
        bmcEmulated:
          protocol: redfish
          libvirtURI: qemu:///system
          bindAddress: 0.0.0.0
          port: 8000
          vMediaPort: 8001
          credentialsRef: bmc-credentials
          sushyToolsVersion: 2.1.0
      - kind: loadBalancer
        name: apps              # rendered component name
        providerName: lab-provider
        capabilityName: default # InfraProvider capability name
        machineRef: services-host
        machineAddress: 192.168.133.1
        realisation: haproxy
        applyRole: bootwright.core.infra_component_load_balancer_haproxy
        destroyRole: bootwright.core.infra_component_load_balancer_haproxy
        image: docker.io/library/haproxy:3.3.8
        frontends:
          - clusterName: prod-3node
            name: ingress
            vip: 192.168.133.11
            ports:
              - { listenPort: 80, targetPort: 80 }
              - { listenPort: 443, targetPort: 443 }
            backends:
              - { name: master-0, address: 192.168.133.20, role: master }
      - kind: artifacts
        name: artifact-server
        componentName: artifact-server
        providerName: InfraComponent
        machineRef: services-host
        machineAddress: 192.168.133.1
        realisation: http
        applyRole: bootwright.core.infra_component_artifact_server_http
        destroyRole: bootwright.core.infra_component_artifact_server_http
        image: docker.io/library/nginx:1.29.8-alpine3.23
        bindAddress: 0.0.0.0
        port: 8443
        listeners:
          - { name: https, protocol: https, port: 8443 }
        endpoints:
          - { name: bmc, listenerRef: https, addressRef: lab-lan }
          - { name: cluster, listenerRef: https, addressRef: cluster-lan }
        url: https://192.168.133.1:8443/
        tls:
          commonName: 192.168.133.1
          dnsNames: []
          ipAddresses: [192.168.133.1]
      - kind: nameResolution
        name: default
        providerName: lab-provider
        machineRef: services-host
        machineAddress: 192.168.133.1
        realisation: dnsmasq
        applyRole: bootwright.core.infra_component_name_resolution_dnsmasq
        destroyRole: bootwright.core.infra_component_name_resolution_dnsmasq
        image: docker.io/dockurr/dnsmasq:2.92_p2
        bindAddress: 192.168.133.1
        port: 53
        hostRecords:
          - { name: api.prod-3node.example.test, address: 192.168.133.10 }
          - { name: api-int.prod-3node.example.test, address: 192.168.133.10 }
        domainRecords:
          - { name: apps.prod-3node.example.test, address: 192.168.133.11 }
        forwarders:
          - 1.1.1.1
      - kind: ntp
        name: ntp-server
        componentName: ntp-server
        providerName: InfraComponent
        machineRef: services-host
        machineAddress: 192.168.133.1
        realisation: chrony
        applyRole: bootwright.core.infra_component_ntp_chrony
        destroyRole: bootwright.core.infra_component_ntp_chrony
        bindAddress: 192.168.133.1
        port: 123
        upstreamSources:
          - ntp.example.test
        allowedNetworks:
          - 192.168.133.0/24
    nodes:
      master-0:
        role: master
        machineRef:
          name: master-0
```

For the runtime component above, the corresponding top-level map is:

```yaml
bootwright_kubevirt_host_kubeconfigs:
  metal-ocp: <task-runtime-secrets>/bootwright-material-<id>/kubeconfig
```

## Machine Service Shapes

Provider playbooks consume `bootwright_provider_services[]` for provider/BMC
service work. InfraComponent playbooks consume
`bootwright_infra_component_services[]` for machine-bound shared services such as
load balancers, artifact servers, proxies, name resolution, NTP, and registries.
Go resolves shared service identity as `(kind, providerName, name)` before
rendering; machine placement and ports are conflict fields, and mergeable overlays
are unioned in the resolved graph. The renderer then emits one aggregated
Ansible service instance with `machineRef`, `applyRole`, and `destroyRole`; the
service role consumes the rest of the flat component fields. Mergeable fields
such as HAProxy `frontends`, dnsmasq records, dnsmasq
`additionalIngressHosts`, chrony `allowedNetworks`, and BMC `machines` carry
per-cluster entries or graph-unioned values.

```yaml
bootwright_provider_services:
  - kind: bmc
    providerName: lab-libvirt-provider
    name: emulated
    machineRef: bastion
    machineAddress: 192.168.133.1
    realisation: emulated
    bmcRole: emulated
    applyRole: bootwright.core.provider_service_bmc_emulated
    destroyRole: bootwright.core.provider_service_bmc_emulated
    configConsistent: true
    bmcEmulated:
      protocol: redfish
      libvirtURI: qemu:///system
      bindAddress: 0.0.0.0
      port: 8000
      vMediaPort: 8001
      credentialsRef: bmc-credentials
      sushyToolsVersion: 2.1.0
    machines:
      - clusterName: prod-3node
        name: master-0
        providerName: lab-libvirt-provider
```

```yaml
bootwright_infra_component_services:
  - kind: loadBalancer
    providerName: InfraComponent
    name: apps
    componentName: apps
    machineRef: services-host
    machineAddress: 192.168.133.1
    realisation: haproxy
    applyRole: bootwright.core.infra_component_load_balancer_haproxy
    destroyRole: bootwright.core.infra_component_load_balancer_haproxy
    image: docker.io/library/haproxy:3.3.10
    frontends:
      - clusterName: prod-3node
        name: ingress
        vip: 192.168.133.11
        ports:
          - listenPort: 80
            targetPort: 80
          - listenPort: 443
            targetPort: 443
        backends:
          - name: master-0
            address: 192.168.133.20
            role: master
```

Each controller name-resolution task consumes one exact resolved service shape.
Its hash renders that service projection from unscoped desired state, never from
the current `--clusters` or `--machines` selection and never from another
service on the same host. `controllerAddresses` contains concrete
controller-reachable service IPs, `controllerDomains` contains routing domains
and exact out-of-zone names, and `controllerProbes` contains every name/address
answer that must succeed before machine work starts.

```yaml
bootwright_controller_name_resolution_services:
  - kind: nameResolution
    providerName: InfraComponent
    name: dns
    componentName: dns
    machineRef: services-host
    realisation: dnsmasq
    applyRole: bootwright.core.infra_component_name_resolution_dnsmasq
    destroyRole: bootwright.core.infra_component_name_resolution_dnsmasq
    controllerAddresses: [192.168.133.1]
    controllerDomains:
      - machines.example.test
      - prod-3node.ocp.example.test
    controllerProbes:
      - { name: master-0.machines.example.test, address: 192.168.133.20 }
      - { name: api.prod-3node.ocp.example.test, address: 192.168.133.10 }
```

```yaml
bootwright_provider_machine_setups:
  - machineRef: bastion
    machineAddress: 192.168.133.1
    applyRole: bootwright.core.provider_host_libvirt
```

## Managed OS Install Shape

Managed OS install playbooks consume one projected group per storage or service
domain that needs Bootwright-installed machines. Each component carries the
resolved provider, boot media, the OS-install role to dispatch, and the
install-mode-specific inputs that role consumes.

Every managed OS component carries `osInstallRole`, the exact Ansible role name
that lays the operating system down. It is derived from the machine's
`MachineInstallProfile.spec.installer` arm, so the playbook never constructs a
role name and never branches on the install mode:

| Key | Shape |
| --- | --- |
| `osInstallRole` | `bootwright.core.machine_os_install_anaconda` when the profile sets `installer.anaconda`; `bootwright.core.machine_os_install_clone` when it sets `installer.templateClone` |

The `osInstall` block below differs per mode. The Anaconda mode carries
`installer`, `image` and `kickstart`, and the component also carries `boot`. The
template-clone mode carries `guest` instead and has no `installer`, `image`,
`kickstart` or `boot` key at all, because a clone consumes no installer media
and never boots from virtual media.

### Anaconda mode

```yaml
bootwright_managed_os_install_groups:
  - name: ceph-libvirt
    storageClusterName: ceph-libvirt
    components:
      - name: ceph-0
        osInstallRole: bootwright.core.machine_os_install_anaconda
        osInstall:
          profileName: rhel-9-ceph-node-minimal-fips
          os:
            family: rhel
            version: "9.8"
            architecture: x86_64
          installer:
            kernelArgs:
              - fips=1
            repositories: []
          image:
            kind: media
            mediaType: dvd
            key: rhel-9.8-x86_64-dvd.iso
            path: /var/lib/bootwright/media/rhel-9.8-x86_64-dvd.iso
            sourceOnTarget: true
          kickstart:
            hostname: ceph-0
            sshUser: cephadm
            sshPublicKeyPath: /var/lib/bootwright/contexts/lab/secrets/ceph-cluster-ssh.pub
            sudo: nopasswd
            passwordAuthentication: false
            packages:
              environment: minimal
              installWeakDeps: false
              excludeDocs: true
              languages:
                - en_US.UTF-8
              install:
                - cephadm
                - podman
                - lvm2
                - chrony
                - firewalld
            services:
              enabled:
                - sshd
                - chronyd
                - firewalld
              disabled:
                - avahi-daemon
                - cockpit.socket
                - cups
                - kdump
                - postfix
            security:
              selinux:
                mode: enforcing
              firewall:
                enabled: true
              fips:
                enabled: true
```

`installer.kernelArgs` is empty unless a profile requests install-time kernel
arguments such as RHEL FIPS enablement. The Anaconda role adds those arguments
with `mkksiso --cmdline` and includes them in the source identity file so a
changed command line forces install ISO rebuild.

### Template-clone mode

```yaml
bootwright_managed_os_install_groups:
  - name: ceph-vsphere
    storageClusterName: ceph-vsphere
    components:
      - name: ceph-arbiter-0
        osInstallRole: bootwright.core.machine_os_install_clone
        osInstall:
          profileName: rhel-9-arbiter-clone
          os:
            family: rhel
            version: "9.8"
            architecture: x86_64
          guest:
            instanceId: bootwright-ceph-vsphere-ceph-arbiter-0
            hostname: ceph-arbiter-0.lab.example.com
            sshUser: cephadm
            sshPublicKeyPath: /var/lib/bootwright/contexts/lab/secrets/ceph-cluster-ssh.pub
            passwordAuthentication: false
            sudoersPath: /etc/sudoers.d/99-bootwright-cephadm
            growRootFilesystem: true
            disableMarkerPath: /etc/cloud/cloud-init.disabled
            services:
              enabled:
                - sshd
              disabled: []
            network:
              bootproto: static
              device: 00:50:56:aa:bb:cc
              ip: 10.20.30.41
              prefix: 24
              netmask: 255.255.255.0
              gateway: 10.20.30.1
              dnsServers:
                - 10.20.30.10
              interfaces:
                - bootproto: static
                  device: 00:50:56:aa:bb:cc
                  ip: 10.20.30.41
                  prefix: 24
                  netmask: 255.255.255.0
                  hostname: true
          ssh:
            address: 10.20.30.41
            connectionAddress: 10.20.30.41
            user: cephadm
            privateKeyPath: /var/lib/bootwright/contexts/lab/secrets/ceph-cluster-ssh
            knownHostsPath: /var/lib/bootwright/contexts/lab/trust/ceph-arbiter-0/known_hosts
          marker:
            path: /etc/bootwright/install-marker.json
            desiredHash: sha256:...
          network:
            desiredState: {}
```

`guest.network` is the same projection that feeds `kickstart.network` in the
Anaconda mode, so both install modes read one rendered addressing contract. Its
flat `device`/`ip`/`prefix`/`netmask` fields describe the interface carrying the
default route, which is the one interface the cloud-init seed configures;
`interfaces` carries the Anaconda stanza list, which a clone ignores.
`guest` carries only public material: a hostname, an install user, that user's
**public** key, a sudoers path and a static IPv4 primary. The clone role turns it
into a cloud-init payload, and on vSphere that payload travels in the VM's
`extraConfig`, which is plaintext to any vCenter principal that can read the VM.
Nothing secret may be added to `guest`.

`network.desiredState` is the full nmstate document, applied day-2 over SSH by
the shared `bootwright.core.machine_os_identity` role exactly as it is for an
Anaconda install. The seed only has to get SSH answering.

## Guest Seed Facts

`bootwright_machine_guest_seed` is a play-scoped fact the selected OS-install
role sets from its `seed.yml` entrypoint, before the substrate role is
dispatched. It is the hand-off from "how this OS is installed" to "how this
substrate delivers a first-boot payload", and it is the reason the substrate role
never has to know which install mode it is serving:

| Fact | Shape |
| --- | --- |
| `bootwright_machine_guest_seed` | `{}` when the machine's install mode needs no first-boot payload (Anaconda), or `{cloudInit: {metadata: <text>, userData: <text>}}` when it does (template clone) |

The vSphere substrate role translates a `cloudInit` seed into
`guestinfo.metadata` / `guestinfo.userdata` plus their `guestinfo.*.encoding`
keys, base64-encoded, and passes them as `advanced_settings`. Because
`advanced_settings` is diffed against the VM's existing `extraConfig`, the seed
text must be byte-stable across applies: a second apply issues no reconfigure.
Substrates that do not translate the seed ignore it.

## Managed Storage Shape

Managed storage playbooks consume one projected entry per
`StorageCluster.spec.management: managed` Ceph cluster. Go owns validation,
rendered file paths, scheduling, ledgers, and final Data Foundation attachment
records. The storage role owns remote host mutation and `cephadm`, `ceph`, and
`radosgw-admin` command execution.

Every `cephadm shell` argv uses one finite role-default timeout class plus a
15-second TERM-to-KILL escalation. The classes are deliberately separate so a
fast health probe does not set the ceiling for an honest filesystem removal or
multi-operation batch:

| Fact | Default seconds | Command class |
| --- | ---: | --- |
| `bootwright_ceph_probe_timeout_seconds` | 120 | Read-only cluster, config, monmap, service, and idempotency probes |
| `bootwright_ceph_inventory_probe_timeout_seconds` | 300 | Device inventory and refresh probes |
| `bootwright_ceph_config_timeout_seconds` | 300 | Configuration database, config-key, manager-module, registry-login, and cephadm SSH mutations |
| `bootwright_ceph_orchestration_timeout_seconds` | 600 | Service/spec apply, redeploy, reconfigure, host enrollment, and one rendered operation |
| `bootwright_ceph_removal_timeout_seconds` | 1800 | Pool, filesystem, profile, daemon, host, and device-zap removal |
| `bootwright_ceph_tool_timeout_seconds` | 300 | Container-local interpreter and `crushtool` round-trips |
| `bootwright_ceph_operation_batch_timeout_seconds` | 1800 | One staged batch, with a fixed ceiling rather than an operation-count multiplier |
| `bootwright_ceph_timeout_kill_after_seconds` | 15 | Grace period after TERM before `timeout` sends KILL |

The role refuses before its first `cephadm shell` command if any class is zero
or negative. The staged-batch value cannot exceed 1800 seconds, and kill
escalation must stay between 1 and 60 seconds; these hard bounds prevent an
Ansible-precedence override from turning the wrapper back into an unbounded
command.

Readiness gates that need a shorter sample keep their 20-, 60-, or 90-second
cap by taking the minimum of that cap and the probe default. An rc 124 or 137
from any class remains a failed Ansible result: an ordinary read-only miss may
still feed a later evidence gate, but a timed-out read is not evidence. A retry
or loop stops before another attempt or item. A task whose `no_log` protects
command output uses the shared relay to expose only its task name, timeout,
exit code, and read/write classification. The runner reports an unknown
state-changing outcome with the exact CLI-rendered
`bootwright_mutating_invocation`; a read-only diagnostic never infers a
mutating command. `failed_when`, `ignore_errors`, rescue, retry, or loop
aggregation must not consume either timeout code.

```yaml
bootwright_storage_clusters:
  - name: ceph-stretch
    seedHost: storage__ceph-stretch__ceph-dc1-0
    storageGroup: bootwright_storage_hosts_ceph_stretch
    fips: true
    provider:
      name: redhat
      distribution: redhat
      requiresRHSM: true
      requiresRegistry: true
      requiresLicense: false
      artifactPolicy:
        packagePin: optional
        rpmReleaseRequired: false
        cephadmAnsiblePackagePin: optional
        cephadmAnsibleRPMReleaseRequired: false
        imageBaseRequired: true
        imagePinRequired: false
        nativeParityMode: ""
        nativePreparationMode: cephadm-ansible-local
      prerequisitePackages:
        - firewalld
        - lvm2
        - podman
        - chrony
      cephadmPackage: cephadm
      cephadmPackageSpec: cephadm-19.2.1-245.el9cp
      cephadmAnsiblePackage: cephadm-ansible
      nativePreparation:
        runtimePackages:
          - ceph-common
        runtimePackageSpecs:
          - ceph-common-19.2.1-245.el9cp
      packageArtifacts:
        - name: cephadm
          spec: cephadm-19.2.1-245.el9cp
          desiredStatePath: spec.ceph.packageVersion
        - name: ceph-common
          spec: ceph-common-19.2.1-245.el9cp
          desiredStatePath: spec.ceph.packageVersion
      entitlement:
        name: rhcs
        provider: redhat
        product: ceph
      rhsmManagement: managed
      rhsm:
        organizationPath: /var/lib/bootwright/contexts/lab/secrets/redhat-org
        activationKeyPath: /var/lib/bootwright/contexts/lab/secrets/redhat-activation-key
      registry:
        url: registry.redhat.io
        credentialsPath: /var/lib/bootwright/contexts/lab/secrets/ceph-registry-credentials
        trustBundlePath: /var/lib/bootwright/contexts/lab/secrets/ceph-registry-ca
    remoteWorkDir: /tmp/bootwright-storage-ceph-stretch
    resultPath: "{{ bootwright_ansible_artifacts_dir }}/storage-result.json"
    publicNetworkCIDRs:
      - 192.168.133.0/24
    clusterNetworkCIDRs:
      - 192.168.133.0/24
    hosts:
      - hostname: ceph-dc1-0
        inventoryHost: storage__ceph-stretch__ceph-dc1-0
        address: 192.168.133.30
        fipsRequired: true
        devices:
          - /dev/sdb
      - hostname: ceph-dc1-1
        inventoryHost: storage__ceph-stretch__ceph-dc1-1
        address: 192.168.133.31
        fipsRequired: true
        devices: []
        osdDeviceFilters:
          - role: data
            filterLogic: AND
            model: ST16000NM
            rotational: true
            limit: 8
          - role: db
            filterLogic: AND
            vendor: NVME
            size: "1T:"
    bootstrap:
      host: ceph-dc1-0
      monIP: 192.168.133.30
    ceph:
      bootstrapConfPath: "{{ bootwright_rendered_dir }}/storage/ceph-stretch/cephadm/bootstrap-ceph.conf"
      bootstrapSpecPath: "{{ bootwright_rendered_dir }}/storage/ceph-stretch/cephadm/bootstrap-spec.yaml"
      coreServicesSpecPath: "{{ bootwright_rendered_dir }}/storage/ceph-stretch/cephadm/core-services.yaml"
      lateServicesSpecPath: "{{ bootwright_rendered_dir }}/storage/ceph-stretch/cephadm/late-services.yaml"
      operationsPath: "{{ bootwright_rendered_dir }}/storage/ceph-stretch/ceph/operations.yaml"
    clusterSSH:
      user: root
      privateKeyPath: /var/lib/bootwright/contexts/lab/secrets/cephadm-cluster-ssh
      publicKeyPath: /var/lib/bootwright/contexts/lab/secrets/cephadm-cluster-ssh.pub
      knownHostsPath: /var/lib/bootwright/contexts/lab/trust/ssh/known_hosts
    dataFoundationBindings:
      - cluster: prod-3node
        addon: openshift-data-foundation
        input: external-storage
        export: ceph
```

The storage inventory contains one synthetic host per declared storage node in
`bootwright_storage_hosts`, plus a per-cluster storage group. Every storage
node, including the cephadm bootstrap seed, renders as
`storage__<cluster>__<node>`; `seedHost` is the seed node's own host name and is
used to limit the bootstrap play. Every storage host renders
`bootwright_storage_cluster_name`, `bootwright_storage_host_name`,
`ansible_host`, `ansible_user`, `ansible_ssh_private_key_file`, and strict
`ansible_ssh_common_args` from the node's referenced `Machine.spec.access.ssh`.
When `ansible_user` is the cluster's orchestration account rather than the
Machine's own login — because the node revokes root, or because the teardown
selector picked that identity — `ansible_ssh_common_args` also offers
`-o IdentityFile=<bootwright_node_access.accountPrivateKeyPath>`, the private
half of the `cephadm.clusterSSH.keyRef` pair that is the only key authorized for
that account. The Machine key stays offered alongside it. The
`clusterSSH` vars are also derived from the storage-node Machine SSH identity and
are copied to the seed host for cephadm.

`fips: true` is present on a cluster entry only when
`StorageCluster.spec.ceph.security.fips.enabled` is true, and every host in that
cluster then carries `fipsRequired: true`. Standalone preflight resolves the
host fact; storage-node access and storage-cluster apply use the selected
cluster fact. All three paths read `/proc/sys/crypto/fips_enabled` and require
`1` before their first mutation, including for an `os.provided: true` host whose
FIPS installation remains external.

The three host-level OSD gate facts have distinct authority:

- `devices` is the deduplicated set of statically named data, DB, and WAL paths.
  It feeds the exact-path marker, emptiness, reclaim, and destroy gates; a purely
  dynamic host renders an empty list.
- `osdDeviceFilters` is present only for managed dynamic selectors and carries
  one entry per dynamic `data`, `db`, or `wal` role, in that order. Each entry
  holds the effective `filterLogic` and only its authored
  `all`/`model`/`vendor`/`rotational`/`size`/`limit` fields. The storage role
  resolves it against cephadm inventory in a read-only gate before optional
  auto-reclaim and before applying the persistent OSD service; it does not
  re-read the service-spec file to infer the selection.
- `osdReclaimAll: true` is present only for an effectively unbounded managed
  data selector: `all: true` with no `limit`. This fact classifies the host but
  authorizes nothing by itself; automatic zap also requires the cluster in the
  Go-rendered rebuild/data-loss authorization set.

The storage role dispatches its repository preparation on the rendered
`provider.name` (the distribution): the repository phase validates the node OS
against the rendered `runtimeOS` family, then includes
`tasks/providers/<name>.yml` — `oss.yml`, `redhat.yml`, or `ibm.yml`. Behavior
inside those files stays keyed to rendered capability flags and data
(`requiresRHSM`, `requiresLicense`, `rhsmManagement`, `repository.*`,
`community.*`), never re-derived from the name: `redhat.yml` and `ibm.yml` are
thin compositions of the shared `subscription.yml` task file, and `ibm.yml`
adds the vendor-specific repository and license steps. For `distribution: oss`
the `provider` block carries a `community` map with the required authored exact
`version` or codename `release`, plus an optional `mirror`; there is no compiled
current release.
`oss.yml` uses it to configure the upstream community Ceph package repository
with cephadm before installing `cephadm`. It imports the Ceph release signing
key from `<mirror>/keys/release.asc` (fingerprint-pinned) before
`cephadm add-repo`, passes that key location through `--gpg-url` (cephadm's
built-in default, `keys/release.gpg`, does not exist upstream), forwards a
custom mirror through `--repo-url`, and rewrites `gpgkey=` lines in a
pre-existing `ceph.repo` so nodes configured by earlier releases converge to
the working key URL. The `redhat` and `ibm` distributions omit `community` and
set `requiresRHSM: true` plus `rhsmManagement` (`managed` or `external` from the
entitlement's `rhsm.management`); the `rhsm` path map is projected only when
`rhsmManagement` is `managed`. RHSM registration itself runs earlier, in the
machines-phase `task_machine_registration_apply` playbook: it selects the
cluster entry from `bootwright_storage_clusters` via
`bootwright_task_storage_cluster_name`, refuses a non-managed context, and
feeds `provider.rhsm.*` into the `machine_registration_rhsm` role
(`bootwright_registration_*` role vars) for Satellite CA trust and katello
binding, proxy CA trust, the node-side Satellite-in-CIDR bypass decision,
`rhsm.conf` `[server]` proxy and `[rhsm]` `repo_ca_cert` convergence,
`subscription-manager` registration and refresh, and optional Insights
enrollment. The shared `subscription.yml` task file then enables the union of
`repository.redhatRepos` and the host's
`bootwright_machine_repositories.subscription.enable` ids with a purge, so
machine-profile-declared repositories survive the storage phase (the task is
skipped entirely when `rhsmManagement` is `external`, so operator-enabled repo
sets are never purged); `ibm.yml` installs the `repository.ibmRepoURL` vendor
`.repo` and — when `requiresLicense: true` — installs and accepts the vendor
license. When `spec.ceph.ibm.packages.source` is `subscription` the renderer
folds `packages.subscriptionRepos` into `repository.redhatRepos` and omits
`repository.ibmRepoURL`; `ibm.yml` keys on that emptiness to remove any
previously installed vendor `.repo` file and to run one `dnf repoquery`
preflight before any install (the preflight carries the same `rhsmManagement`
guard as the enable, so it never demands repositories the role did not enable).
The command disables every undeclared repository, enables the rendered
`repository.redhatRepos`, forces `skip_if_unavailable=False` globally and per
declared id, and queries every required bare package name together. Dnf prints
the package name, canonical NEVRA, a paste-ready `packageVersion` value, and
every `name-[epoch:]version[-release][.arch]` spec form it accepts. Separate
asserts classify a non-zero repository read, a successful read with no build of
one required package, and a missing exact native-artifact spec. The last tests
membership in the emitted spec set, so an omitted epoch can never cause a false
miss, and the failure names the builds the repositories do publish. The pin
assert deliberately fails even where the pinned install would
have been a no-op on a node that already carries the build: what the next fresh
or rebuilt node installs from is the repository, not the node's disk. Distributions that set
`requiresRegistry: true` additionally run a registry stage (after host
dependencies, before cephadm install) that installs the entitlement's
`registry.trustBundlePath` and logs in to `registry.url` so every node can pull
the Ceph container images cephadm orchestrates. Adding a distribution is a
renderer/table change plus one provider task file keyed by its name (or a
composition of the shared subscription file), not new branches in the shared
flow.

`cephadmPackage` and `cephadmAnsiblePackage` are bare package names.
`cephadmPackageSpec` and `cephadmAnsiblePackageSpec` are present only when their
desired-state fields pin a build. `packageArtifacts[]` is the generic exact
installation contract: every entry carries its bare `name`, composed `spec`,
and `desiredStatePath`; subscription repo probes, DNF installation, ownership,
and the post-native exact-coordinate gate all consume that same list. Bare
names are the ownership keys, so destroy's static `cephadm`, `ceph-common`, and
`cephadm-ansible` candidates match. `nativePreparation.runtimePackages` carries
the provider's bare runtime set and `runtimePackageSpecs` carries its exact
projection when `spec.ceph.packageVersion` is authored. The role never composes
package specs itself.

`artifactPolicy.packagePin` and `cephadmAnsiblePackagePin` independently mark
those coordinates optional, required, or forbidden;
`rpmReleaseRequired` and `cephadmAnsibleRPMReleaseRequired` select full RPM
version-release syntax. Exact installs permit an upgrade but never enable DNF
downgrades, and a build that differs after DNF or native preparation fails
closed. When an optional Red Hat pin is omitted, the corresponding bare package
is installed through the ownership helper with `state: present`.

When `artifactPolicy.nativePreparationMode` is non-empty, the storage-infra
all-node pass dispatches to that closed adapter after native package install and
before runtime/bootstrap work. `cephadm-ansible-local` verifies that the
installed `cephadm-ansible` RPM owns its playbook, syntax-checks it, and executes
it locally on each node. The adapter selects custom origin with no added
repositories, disables both package-upgrade switches and the stock uninstall
set, passes exact package requests when authored, and preserves a fresh report.
Nested Ansible logs, temp paths, and fact cache stay in the root-only transient
work directory. The generic exact-artifact loop re-proves all authored EVRs
afterward.

The provider also carries the `runtimeOS` family and, for IBM,
`ibm.callHome` plus candidate native tokens:

```yaml
nativeCapabilityCandidates:
  cephadmBootstrapLicenseOption: --automatically-accept-license
  cephOrchCallHomeConsentToken: call-home-enabled
```

These values name features to inspect; they do not assert that a release
supports them. A fresh licensed bootstrap uses a bounded
`cephadm bootstrap --help` probe and appends the exact option only when
recognizable help advertises it. The Call Home phase independently inspects
`ceph orch --help`, always reconciles the manager module to `ibm.callHome`, and
acknowledges or denies the automatic state only when the matching signature is
advertised. Probe failure or unrecognizable help refuses before the related
mutation, and no release/package-version branch selects either path. A custom
entitlement `registry.url` is paired
with an explicit daemon image base under the same registry namespace by
desired-state validation. The rendered `image` and `imageBase` are composed
from authored `spec.ceph.image.base` and `spec.ceph.image.version`; only the
community repository may default. `artifactPolicy.imageBaseRequired` and
`imagePinRequired` express the provider requirements. When
`artifactPolicy.nativeParityMode` is non-empty, the container-runtime phase
dispatches to that closed generic adapter. The current `ceph-version` adapter
reads `cephadm version` from the installed RPM, runs `ceph --version` in the
exact image, and requires both reported builds to match the epoch-free package
declaration before bootstrap or service convergence. This proof also runs on
the split seed pass where package prerequisites are skipped.

`ceph.sidecarImagePins` carries the cluster's `config[mgr]`
`mgr/cephadm/container_image_*` entries, excluding `container_image_base`. The
bootstrap phase applies them with a guarded `ceph config get`/`set` immediately
after bootstrap, ahead of the ingress specs; the same keys are also seeded into
the bootstrap ceph.conf so the monitoring stack cephadm deploys in-process
resolves them.

`ceph.operationsPath` points to a phased operation document. Each entry has a
stable `phase`, `name`, and `command`. Create-style operations also declare
`idempotency.kind` and `idempotency.name`; the storage role uses those fields
for skip checks instead of inferring resource identity from operation names or
command positions. Entries that generate credentials declare a `capture` block,
and Go consumes the temporary result file after Ansible completes.

```yaml
operations:
  - phase: storage
    name: create-pool-odf-rbd
    command: [ceph, osd, pool, create, odf-rbd, "128"]
    idempotency:
      kind: ceph-pool
      name: odf-rbd
  - phase: data-foundation
    name: create-data-foundation-rgw-admin-user-prod-3node
    command: [radosgw-admin, user, create, --uid, bootwright.prod-3node.rgw-admin]
    idempotency:
      kind: rgw-user
      name: bootwright.prod-3node.rgw-admin
    capture:
      type: rgw-user
      cluster: prod-3node
```

## Bastion Tools

`workflow_bastion_apply_tools` installs the controller-side OpenShift CLIs and
`helm`. The renderer projects these extra vars (see `internal/converge/bastion`):

| Fact | Shape |
| --- | --- |
| `bootwright_openshift_release_version` | OpenShift release the controller CLIs (`oc`, `kubectl`, `openshift-install`) are pinned to |
| `bootwright_clis_install_dir` | Directory the controller CLIs are installed into |
| `bootwright_clis_release_url` | Release-scoped base URL the CLIs and their checksums are fetched from; honors `Environment.spec.defaults.clientsMirror` and otherwise the pinned upstream mirror. The role falls back to `bootwright_clis_mirror_base` only when this var is not projected |
| `bootwright_helm_release_url` | Channel-scoped base URL `helm` and its checksum manifest are fetched from; honors `Environment.spec.defaults.helmMirror` and otherwise the upstream OpenShift clients mirror, always suffixed with the `latest` channel. The role falls back to `bootwright_helm_mirror_base` only when this var is not projected. `helm` is not release-pinned: the fetch is verified against the channel's `sha256sum.txt`, and `get_url` re-downloads only when the installed binary no longer matches |
| `bootwright_clis_fips_required` | `true` when any OpenShift cluster enables FIPS. The role then also fetches the FIPS-capable `openshift-install-fips` from the RHEL9 client archive (`openshift-install-rhel9-amd64.tar.gz`), which FIPS clusters use to build their agent ISO. Defaults to `false` (only the standard `openshift-install` is installed) |

## Projection Rule

Roles consume already-projected blocks. They should not rediscover provider
facts from the raw capability map and should not branch on user schema details.
If a role needs a substrate-specific value, image reference, or tool version,
add it to the Go renderer first and consume the flat field in Ansible.
Container-backed managed service components (`loadBalancer`, `proxy`,
`nameResolution`, and `registry`) consume `component.image`; host-package
services such as `ntp` consume package/config fields, and the `bmc_emulated`
role consumes provider BMC service `bmcEmulated.*`. Layer playbooks dispatch exact
rendered role names and task entrypoints (`applyRole`, `destroyRole`,
`substratePrepareRole`, `substratePrepareFrom`, `substrateApplyRole`,
`substrateApplyFrom`, `bootApplyRole`, `mediaPrepareRole`, `cleanupMediaRole`,
and `osInstallRole`) rather than constructing role names from diagnostic labels.
`cleanupMediaRole` is set only for boot backends that own a `cleanup_media`
action (Redfish, vSphere), so post-install media cleanup dispatches on it without
enumerating boot roles. When Redfish also dispatches a direct-libvirt
`mediaPrepareRole`, that backend normalizes the public `cleanup_media` action to
its closed internal `cleanup` action before validating it.

## Task-Scoped Apply Vars

Parallel apply playbooks receive scheduler-selected scope through extra vars:

| Fact | Shape |
| --- | --- |
| `bootwright_task_cluster_name` | ContainerCluster name selected for one OpenShift agent or machine infrastructure task |
| `bootwright_task_machine_names` | Comma-separated machine names selected for one machine infrastructure task |
| `bootwright_task_managed_os_group_name` | StorageCluster-backed managed OS group selected for one managed OS task |
| `bootwright_task_provider_host_name` | Provider host selected for one shared machine infrastructure prepare/finalize task |
| `bootwright_task_storage_cluster_name` | StorageCluster name selected for one storage or machine registration task |
| `bootwright_task_storage_prereqs_only` | Optional boolean that limits a storage task to node prerequisites before seed-only cephadm work |
| `bootwright_task_storage_skip_prereqs` | Optional boolean that limits a storage task to seed-only cephadm convergence after earlier node prerequisites; it never skips the provider-image or container-runtime safety proof that precedes cluster and disk mutation |
| `bootwright_agent_node_cluster_name` | ContainerCluster name attached to one Ansible pseudo-host in `bootwright_agent_node_hosts` |
| `bootwright_agent_node_machine_name` | Machine name attached to one Ansible pseudo-host in `bootwright_agent_node_hosts` |
| `bootwright_machine_task_cluster_name` | ContainerCluster or managed OS group name attached to one Ansible pseudo-host in `bootwright_machine_task_hosts` |
| `bootwright_machine_task_machine_name` | Machine name attached to one Ansible pseudo-host in `bootwright_machine_task_hosts` |
| `bootwright_machine_task_provider_host_name` | Provider host name attached to one Ansible pseudo-host in `bootwright_machine_task_hosts` |
| `bootwright_apply_mode` | Apply intent from the CLI `--mode` flag: `create`, `reconcile` (default), or `rebuild`. `rebuild` authorizes Bootwright-owned reconfigure/rebuild; roles gate destructive resets on it |
| `bootwright_controller_name_resolution_automatic_mutation` | Positive controller decision that exactly one consumed managed name-resolution service contributes routes in unscoped desired state; unused catalog entries do not count. Absent or false forbids automatic systemd-resolved mutation but still permits read-only readiness probes |
| `bootwright_controller_name_resolution_mutation_selected` | Positive planner decision that the selected apply range includes `fabric` or `machines`; absent or false permits the controller task to prove readiness but forbids it from changing resolver state |
| `bootwright_ansible_artifacts_dir` | Per-task local artifact directory for controlled runner outputs |

Real mutating runs also receive CLI-rendered remediation commands. They are
shell-quoted from the resolved invocation after context, selection, credentials,
effect flags, and authorizations are known. Runtime roles must use these facts in
operator-facing retry guidance instead of assembling a `bootwright apply` or
`bootwright destroy` command from task-local names.

| Fact | Shape |
| --- | --- |
| `bootwright_mutating_invocation` | Exact current apply or destroy invocation; use after the operator fixes a transient external failure |
| `bootwright_apply_reconcile_invocation` | Exact selected apply invocation with `--mode reconcile` |
| `bootwright_apply_rebuild_invocation` | Exact selected apply invocation with `--mode rebuild` and additive `--authorize data-loss` |
| `bootwright_apply_reclaim_invocation` | Exact selected apply invocation with prior accepted authorizations retained, `data-loss,unowned-devices` unioned, and exactly one `__BOOTWRIGHT_RUNTIME_RECLAIM_DEVICES_7EF51C56__` value for `--reclaim-devices`; a role may replace only that value with one shell-quoted, comma-joined nonempty list of runtime-probed device paths |
| `bootwright_apply_reclaim_devices` | Static reclaim paths already selected by the controller; the runtime reclaim remedy preserves these in the sentinel replacement operand |
| `bootwright_apply_controller_dns_invocation` | Mode-aware exact selected apply invocation for resuming a controller-DNS task that may have partially changed selected fabric/machines work; an interrupted `create` becomes `reconcile`, `reconcile`/`rebuild` remain unchanged, and a machines range omitting required fabric starts at fabric while preserving its ending stage |
| `bootwright_apply_controller_dns_repair_invocation` | Exact `--mode reconcile --stage fabric` apply for every cluster root that consumes this task's shared name-resolution service; it changes no later phase and carries no deps-only reclaim effect |
| `bootwright_apply_controller_dns_resume_invocation` | Byte-equivalent retry of the original selected apply work, used only after a proof-only controller-DNS task failed before any selected mutation |
| `bootwright_apply_full_invocation` | Exact apply invocation with stage/range narrowing removed while retaining context and object selection |
| `bootwright_apply_through_base_invocation` | Exact apply invocation changed to `--through base` while retaining context and object selection |

## Mutation-Control Vars

Go is the sole producer of the intent, authorization, scope, and execution vars
that let an embedded playbook mutate a target. Their registry is
`mutationSafetyVars` in `internal/converge`; a value missing from the run must
under-authorize or narrow work, never widen it. The registry guard requires each
entry to have a Go producer, an Ansible consumer, and this contract entry, and
detects a newly rendered mutation-control name that was not registered.

| Class | Facts | Shape |
| --- | --- | --- |
| Intent | `bootwright_apply_mode` | Exact `create`, `reconcile`, or `rebuild` value selected by the operator |
| Authorization | `bootwright_ceph_authorize_foreign_daemons`, `bootwright_ceph_authorize_unowned_devices`, `bootwright_destroy_authorize_unowned_networks`, `bootwright_destroy_authorize_unowned_vms`, `bootwright_destroy_skip_unreachable` | Boolean risk grants derived from a consumed `--authorize` token; omitted when not granted |
| Authorization | `bootwright_destroy_skip_orphan_sweep` | Positive selector emitted only when `destroy` consumed `stale-input` or `unreadable-records` for evidence the run actually skipped; disables every context-wide or record-only ownership sweep and the controller-resolver preflight/cleanup bracket, so an absent declaration or unreadable record cannot be mistaken for an orphan authorized for deletion. Absent or false permits only the normally planned scope and never suppresses declaration-backed teardown |
| Authorization | `bootwright_ceph_rebuild_authorized_clusters`, `bootwright_ceph_incomplete_bootstrap_authorized_clusters`, `bootwright_ceph_subobject_rebuild_authorized`, `bootwright_ocp_rebuild_authorized_clusters` | Exact clusters or storage sub-objects whose destructive rebuild was acknowledged. The incomplete-bootstrap list is emitted only for selected managed storage clusters whose exact owner-role record names this context, cluster, host, and desired `seedHost`, only under `--mode rebuild` plus consumed `data-loss`; the seed re-validates that record, config present, marker absent, and cluster unreachable before the shared cleanup predicate becomes true |
| Authorization | `bootwright_ceph_filter_reclaim_clusters`, `bootwright_ceph_reclaim_clusters`, `bootwright_ceph_reclaim_devices` | Exact cluster and device allowlists for an acknowledged reclaim; an empty or absent list authorizes no wipe |
| Authorization | `bootwright_ceph_destroy_confirmed_fsids` | StorageCluster-to-fsid attestations accepted by the controller ownership-recovery gate |
| Authorization | `bootwright_substrate_reset_clusters`, `bootwright_substrate_reset_machines` | Exact released or structurally drifted machine substrate whose reset was authorized; the machine form carries `<cluster>/<machine>` pairs |
| Authorization | `bootwright_infra_component_reclaim_records` | Exact owned install-only InfraComponent records the controller authorized for end-of-apply reclaim |
| Authorization | `bootwright_arbiter_allow_degraded`, `bootwright_arbiter_allow_same_site`, `bootwright_arbiter_old_host_offline` | Replacement-arbiter risk grants derived from the consumed tokens and the controller's retirement evidence |
| Scope | `bootwright_destroy_cluster_scope`, `bootwright_destroy_machine_scope`, `bootwright_destroy_storage_scope`, `bootwright_destroy_container_cluster` | Exact cluster, machine, storage-cluster, or single-container-cluster work set for a destroy task |
| Scope | `bootwright_storage_destroy_release_manifest_path` | Controller-only path to the exact FSID and node set whose terminal storage proof is durably staged for host-evidence release; absence or an invalid manifest releases nothing |
| Execution | `bootwright_storage_destroy_release_validation_path` | Controller-only task path written after every selected node passes the fresh release-boundary checks and before any host ownership evidence is removed; an absent boundary invalidates the staged proof so the next retry repeats destructive teardown |
| Execution | `bootwright_host_mutation_lease` | Immutable JSON identity derived from the command-wide lease held by the controller: exact API and kind, unique SHA-256 attempt token, run ID, mutating command, controller hostname, PID and process-start identity, and start timestamp. A shared host role embeds this identity in a host-global exclusive operation guard before its first mutation, rechecks the exact token at every mutation boundary, and removes it last. Missing, foreign, malformed, or stale evidence authorizes nothing; another controller's token is never adopted or expired automatically |
| Execution | `bootwright_host_shared_service_manifest` | Controller-built JSON object keyed by exact selected host. Each value carries the exact API, kind, context, apply/destroy command, host, and sorted unique `{kind,name}` consequences selected for that host. The first selected shared-service task binds the whole host entry to the unique operation guard; every later provider or infra-component task must match it, and only the command-wide finalizer may release it. Missing, extra, malformed, or task-local reconstructions authorize no shared-service mutation |
| Scope | `bootwright_infra_destroy_context_sweep`, `bootwright_infra_component_destroy_scope_records`, `bootwright_infra_component_service_scope` | Context-wide versus selected InfraComponent teardown bounds; absent scope never implies a wider selected teardown |
| Scope | `bootwright_infra_component_apply_skip_records`, `bootwright_ceph_reconcilable_only_clusters` | Exact records or clusters the controller classified for a narrowed non-destructive path |
| Scope | `bootwright_apply_reclaim_devices` | Static reclaim paths already selected by the controller; absence or an empty list widens no runtime reclaim scope |
| Scope | `bootwright_task_cluster_name`, `bootwright_task_host_cluster_name`, `bootwright_task_machine_names`, `bootwright_task_managed_os_group_name`, `bootwright_task_provider_host_name`, `bootwright_task_storage_cluster_name` | Scheduler-selected task identity and work set |
| Scope | `bootwright_agent_node_cluster_name`, `bootwright_agent_node_machine_name`, `bootwright_machine_task_cluster_name`, `bootwright_machine_task_machine_name`, `bootwright_machine_task_provider_host_name` | Per-pseudo-host task identity; roles select the named cluster, machine, and provider host rather than re-deriving scope |
| Scope | `bootwright_arbiter_cluster_name`, `bootwright_arbiter_desired_node`, `bootwright_arbiter_live_node` | Replacement-arbiter cluster and old/new machine selection |
| Execution | `bootwright_destroy_cluster_levels`, `bootwright_destroy_cluster_order`, `bootwright_machine_infra_records_only` | Controller-derived teardown barriers, compatibility order, and records-only cleanup mode |
| Execution | `bootwright_install_wait_target`, `bootwright_task_storage_prereqs_only`, `bootwright_task_storage_skip_prereqs` | Exact task entrypoint inside a split apply workflow |
| Execution | `bootwright_controller_name_resolution_automatic_mutation` | Positive exact-one-consumed-service decision for automatic controller resolver mutation; false or absent permits only read-only proof of an already-correct resolver |
| Execution | `bootwright_controller_name_resolution_mutation_selected` | Positive selected-range decision for controller resolver mutation; false or absent makes later-only apply proof-only and fail-closed on drift |
| Execution | `bootwright_controller_name_resolution_destroy_targets` | Deterministic controller-rendered union of desired managed resolver targets and current-context controller-resolver ownership records. Adapter, infra-component record identity, controller record identity, and drop-in path are registry- or identity-derived rather than trusted from records; malformed or unknown evidence remains an invalid target that preflight refuses instead of being omitted. Cluster-scoped destroy filters this union only by `infraComponentRecordName` |
| Execution | `bootwright_mutating_invocation`, `bootwright_apply_reconcile_invocation`, `bootwright_apply_rebuild_invocation`, `bootwright_apply_reclaim_invocation`, `bootwright_apply_controller_dns_invocation`, `bootwright_apply_controller_dns_repair_invocation`, `bootwright_apply_controller_dns_resume_invocation`, `bootwright_apply_full_invocation`, `bootwright_apply_through_base_invocation`, `bootwright_arbiter_degraded_invocation`, `bootwright_arbiter_same_site_invocation`, `bootwright_arbiter_unreachable_invocation` | Shell-quoted commands derived from the resolved CLI invocation; roles consume these exact variants and never rebuild operator remedies from task-local facts |
| Execution | `bootwright_arbiter_desired_addr`, `bootwright_arbiter_desired_mon`, `bootwright_arbiter_desired_site`, `bootwright_arbiter_failure_domain`, `bootwright_arbiter_live_mon`, `bootwright_arbiter_tiebreaker_mon` | Controller-resolved arbiter identities and topology facts |
| Execution | `bootwright_arbiter_mon_hosts_during`, `bootwright_arbiter_mon_hosts_after`, `bootwright_arbiter_mon_locations`, `bootwright_arbiter_mon_locations_after` | JSON projections of the accepted during/after monitor topology |

The OpenShift agent role uses those vars to create and publish one cluster ISO,
boot all selected node pseudo-hosts through Ansible host fanout, and run the
final installer wait after the boot-stage task has completed. Machine
infrastructure apply tasks select the `bootwright_machine_task_hosts`
pseudo-hosts they provision; prepare and finalize tasks select one provider
host. Managed OS tasks select the storage group's full pseudo-host group so VM
creation, OS install, SSH wait, and trust recording run in Ansible host fanout.
Managed storage prereq tasks run against the storage-node inventory group and
reserve seed-only cephadm work for the final storage task.

## Add-On Step Vars

Add-on steps (`ClusterAddon.spec.steps[]`) run through a separate, narrower engine
than the core layer tasks and `CustomPlaybook`s above
(`internal/converge/workflow`, `runStepAnsible`). A step playbook does **not**
receive the full `bootwright_*` vars contract documented in this file — there is
no `bootwright_environment`, `bootwright_machines`, `bootwright_clusters`, or any
other top-level fact. It runs against an ad-hoc inventory of only the step's
resolved target machines, with an empty `vars.yaml`, and a small curated set of
extra vars.

### Step inventory

The engine writes a one-off inventory (`writeStepInventory`) holding only the
machines the step's `target` resolved to — never the rendered fleet inventory.
Each host carries just its SSH connection facts:

| Host var | Shape |
| --- | --- |
| `ansible_host` | The target machine's resolved SSH address. |
| `bootwright_host_name` | The target `Machine` name (the step's per-host label). |
| `ansible_user` | A storage target's post-install `cephadm.clusterSSH.user`; otherwise the machine's `access.ssh.user`, when set. |
| `ansible_ssh_private_key_file` | Path to the storage target's materialized `cephadm.clusterSSH.keyRef` private key, falling back to the Machine access key when omitted; other targets use the Machine access key. |
| `ansible_ssh_common_args` | `-o BatchMode=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=<trusted known_hosts>`. |

The `vars.yaml` handed to the run is empty (`{}`); every step fact arrives as an
`-e` extra var instead.

### Step extra vars

`stepExtraVarPairs` projects exactly these facts (and nothing else):

| Fact | Shape |
| --- | --- |
| `bootwright_step_name` | The step's `name`. |
| `bootwright_step_anchor` | The step's anchor value — its `gates` (`apply`) or its `follows` (`operatorReady`, `ready`). |
| `bootwright_addon_name` | The bound `ClusterAddon` name. |
| `bootwright_bound_cluster` | The bound `ContainerCluster` name. |
| `bootwright_step_outputs_dir` | Controller-local directory the playbook writes its declared `outputs[]` files into. |
| `bootwright_step_secrets_dir` | Controller-local directory holding only the step's declared `secretRefs` (never the whole store). |
| `bootwright_kubeconfig` | Controller-local path to the bound cluster's kubeconfig. |
| `bootwright_step_refs` | JSON: resolved `refKind` input objects keyed by property name (e.g. `exportRef` → the referenced `StorageExport` object), so a play can read `bootwright_step_refs.exportRef.spec...`. |
| `bootwright_step_inputs` | JSON: binding input name → its values map. |

The step's own `extraVars` map, when set, is appended as one additional JSON `-e`
value. The outputs directory, secrets directory, and kubeconfig are
**controller-local** paths, not readable on the target hosts: read and write them
from `delegate_to: localhost` tasks. That is also how a step drives the bound
cluster's API — for example `oc --kubeconfig {{ bootwright_kubeconfig }}` runs on
the controller.

### Target limit and timeout

`target.limit` selects how the play covers the resolved machines:

- `firstReachable` (default) runs the play against each target host in order,
  `--limit`ed to one host at a time, and stops at the first host whose run
  succeeds (the exporter admin-node pattern). It advances only when the
  machine-readable Ansible result reports `unreachable` before any `ok` or
  `failed` task result. A task failure, timeout, malformed or missing result,
  or connection loss after any task result fails closed on that host because
  the run may already have changed state.
- `all` runs one play against every resolved host with no `--limit`.

Each run is bounded by the step's `timeout` (a Go duration, default `10m`); a run
that exceeds it is cancelled and the step fails. Declared `outputs[]` are captured
from `bootwright_step_outputs_dir` after the run (secret outputs persisted `0600`
under the cluster secrets area, non-secret under runtime); see
`docs/concepts/add-ons.md` for the manifest-token surface that consumes them.
