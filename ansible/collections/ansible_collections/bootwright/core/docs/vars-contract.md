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
        networkAttachment:
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
          redfish: {}         # bare-metal artifactCertificate additionally carries
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

```yaml
bootwright_storage_clusters:
  - name: ceph-stretch
    seedHost: storage__ceph-stretch__ceph-dc1-0
    storageGroup: bootwright_storage_hosts_ceph_stretch
    provider:
      name: redhat
      distribution: redhat
      requiresRHSM: true
      requiresRegistry: true
      requiresLicense: false
      prerequisitePackages:
        - firewalld
        - lvm2
        - podman
        - chrony
      cephadmPackage: cephadm
      cephadmPackageSpec: cephadm-19.2.1-245.el9cp
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
        devices:
          - /dev/sdb
      - hostname: ceph-dc1-1
        inventoryHost: storage__ceph-stretch__ceph-dc1-1
        address: 192.168.133.31
        devices:
          - /dev/sdb
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

The storage role dispatches its repository preparation on the rendered
`provider.name` (the distribution): the repository phase validates the node OS
against the rendered `runtimeOS` family, then includes
`tasks/providers/<name>.yml` — `oss.yml`, `redhat.yml`, or `ibm.yml`. Behavior
inside those files stays keyed to rendered capability flags and data
(`requiresRHSM`, `requiresLicense`, `rhsmManagement`, `repository.*`,
`community.*`), never re-derived from the name: `redhat.yml` and `ibm.yml` are
thin compositions of the shared `subscription.yml` task file, and `ibm.yml`
adds the vendor-specific repository and license steps. For `distribution: oss`
the `provider` block carries a `community` map with a `version` (defaulting to
exact `20.2.2`) or an authored codename `release`, plus an optional `mirror`;
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
previously installed vendor `.repo` file and to run a `dnf repoquery` preflight
before any install (the preflight carries the same `rhsmManagement` guard as the
enable, so it never demands repositories the role did not enable). The preflight
queries the bare package names and asks dnf to print, per available build, the
canonical NEVRA, a paste-ready `packageVersion` value, and every
`name-[epoch:]version[-release][.arch]` spec form dnf itself accepts; it then
asserts twice against that output. The first assert separates an unreadable
repository (non-zero rc) from one that answers but publishes no build of the
package at all. The second runs only when `cephadmPackageSpec` pins a build and
tests membership of that spec in the emitted spec set, so an omitted epoch can
never cause a false miss, and the failure names the builds the repositories do
publish. The pin assert deliberately fails even where the pinned install would
have been a no-op on a node that already carries the build: what the next fresh
or rebuilt node installs from is the repository, not the node's disk. Distributions that set
`requiresRegistry: true` additionally run a registry stage (after host
dependencies, before cephadm install) that installs the entitlement's
`registry.trustBundlePath` and logs in to `registry.url` so every node can pull
the Ceph container images cephadm orchestrates. Adding a distribution is a
renderer/table change plus one provider task file keyed by its name (or a
composition of the shared subscription file), not new branches in the shared
flow.

`cephadmPackage` is always the bare package name and is the key the ownership
record is written under, so destroy — which looks the package up by that bare
name from a static list — still matches. `cephadmPackageSpec` is present only
when `spec.ceph.packageVersion` pins a build, carries the composed
`<package>-<version>`, and is consumed by exactly one task: the `dnf` install
that pins the build. The role never composes the two itself.

The provider also carries the `runtimeOS` family and, for IBM,
`ibm.callHome`. IBM bootstrap adds `--automatically-accept-license`; the
bootstrap phase then enables and acknowledges Call Home or denies it according
to the required authored value. A custom entitlement `registry.url` is paired
with an explicit daemon image base under the same registry namespace by
desired-state validation. The rendered `image` and `imageBase` are already
composed by the renderer from `spec.ceph.image.base` (or the derived vendor
repository) and `spec.ceph.image.version`; the role consumes them verbatim.

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
enumerating boot roles.

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
| `bootwright_task_storage_skip_prereqs` | Optional boolean that limits a storage task to the seed-only cephadm bootstrap, skipping node prerequisites |
| `bootwright_agent_node_cluster_name` | ContainerCluster name attached to one Ansible pseudo-host in `bootwright_agent_node_hosts` |
| `bootwright_agent_node_machine_name` | Machine name attached to one Ansible pseudo-host in `bootwright_agent_node_hosts` |
| `bootwright_machine_task_cluster_name` | ContainerCluster or managed OS group name attached to one Ansible pseudo-host in `bootwright_machine_task_hosts` |
| `bootwright_machine_task_machine_name` | Machine name attached to one Ansible pseudo-host in `bootwright_machine_task_hosts` |
| `bootwright_machine_task_provider_host_name` | Provider host name attached to one Ansible pseudo-host in `bootwright_machine_task_hosts` |
| `bootwright_apply_mode` | Apply intent from the CLI `--mode` flag: `create`, `reconcile` (default), or `rebuild`. `rebuild` authorizes Bootwright-owned reconfigure/rebuild; roles gate destructive resets on it |
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
| `bootwright_apply_full_invocation` | Exact apply invocation with stage/range narrowing removed while retaining context and object selection |
| `bootwright_apply_through_base_invocation` | Exact apply invocation changed to `--through base` while retaining context and object selection |

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
  succeeds (the exporter admin-node pattern); it fails only when every host fails.
- `all` runs one play against every resolved host with no `--limit`.

Each run is bounded by the step's `timeout` (a Go duration, default `10m`); a run
that exceeds it is cancelled and the step fails. Declared `outputs[]` are captured
from `bootwright_step_outputs_dir` after the run (secret outputs persisted `0600`
under the cluster secrets area, non-secret under runtime); see
`docs/concepts/add-ons.md` for the manifest-token surface that consumes them.
