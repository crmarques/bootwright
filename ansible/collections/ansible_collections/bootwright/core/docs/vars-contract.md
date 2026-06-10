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
| `bootwright_resolved_ntp_sources` | resolved external and managed NTP addresses rendered to installer input |

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
        endpoint: cluster
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
        substratePrepareRole: bootwright.core.machine_substrate_libvirt
        substratePrepareFrom: network
        substrateApplyRole: bootwright.core.machine_substrate_libvirt
        substrateApplyFrom: machine
        substrateDestroyRole: bootwright.core.machine_substrate_libvirt
        bmcApplyRole: bootwright.core.provider_service_bmc_emulated
        bmcDestroyRole: bootwright.core.provider_service_bmc_emulated
        bootApplyRole: bootwright.core.container_cluster_boot_redfish
        mediaPrepareRole: bootwright.core.container_cluster_media_libvirt
        machineSetupRoles:
          - bootwright.core.machine_setup_libvirt
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
          kubeconfig: "{{ bootwright_clusters_dir }}/metal-ocp/secrets/kubeconfig"
          namespace: bootwright-child-ocp
          storageClassRef: lvms-vg1
        boot:
          redfish: {}
          agentIso: {}
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
    applyRole: bootwright.core.machine_setup_libvirt
```

## Managed OS Install Shape

Managed OS install playbooks consume one projected group per storage or service
domain that needs Bootwright-installed machines. Each component carries the
resolved provider, boot media, base install image, Kickstart inputs, and
optional installer kernel arguments.

```yaml
bootwright_managed_os_install_groups:
  - name: ceph-libvirt
    storageClusterName: ceph-libvirt
    components:
      - name: ceph-0
        osInstall:
          profileName: rhel-9-ceph-node-minimal-fips
          os:
            family: rhel
            version: "9.7"
            architecture: x86_64
          installer:
            type: anaconda
            kernelArgs:
              - fips=1
            repositories: []
          image:
            kind: media
            mediaType: dvd
            key: rhel-9.7-x86_64-dvd.iso
            path: /var/lib/bootwright/media/rhel-9.7-x86_64-dvd.iso
            sourceOnTarget: true
          kickstart:
            hostname: ceph-0
            sshUser: root
            sshPublicKeyPath: /var/lib/bootwright/contexts/lab/secrets/ceph-node-ssh.pub
            authorizeMachineSSHKey: true
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
      entitlement:
        name: rhcs
        provider: redhat
        product: ceph
      rhsm:
        organizationPath: /var/lib/bootwright/contexts/lab/secrets/redhat-org
        activationKeyPath: /var/lib/bootwright/contexts/lab/secrets/redhat-activation-key
      registry:
        url: registry.redhat.io
        credentialsPath: /var/lib/bootwright/contexts/lab/secrets/ceph-registry-credentials
        trustBundlePath: /var/lib/bootwright/contexts/lab/secrets/ceph-registry-ca
    remoteWorkDir: /tmp/bootwright-storage-ceph-stretch
    resultPath: "{{ bootwright_ansible_artifacts_dir }}/storage-result.json"
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
`ansible_ssh_common_args` from the node's referenced `Machine.spec.access.ssh`. The
`clusterSSH` vars are also derived from the storage-node Machine SSH identity and
are copied to the seed host for cephadm.

The storage role dispatches its repository preparation on rendered `provider`
capability flags, not on the distribution name. For `distribution: oss` the
`provider` block carries a `community` map with a `release` (defaulting to the
latest stable upstream Ceph release) and an optional `mirror`; the role uses it
to configure the upstream community Ceph package repository with cephadm before
installing `cephadm`. The `redhat` and `ibm` distributions omit `community` and
set `requiresRHSM: true`; one shared, data-driven task file then registers RHSM,
enables `repository.redhatRepos`, installs the optional `repository.ibmRepoURL`
vendor `.repo`, and — when `requiresLicense: true` — installs and accepts the
vendor license. Distributions that set `requiresRegistry: true` additionally run
a registry stage (after host dependencies, before cephadm install) that installs
the entitlement's `registry.trustBundlePath` and logs in to `registry.url` so
every node can pull the Ceph container images cephadm orchestrates. Adding a
distribution is a renderer/table change, not a new branch in the role.

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

`workflow_bastion_apply_tools` installs the controller-side OpenShift CLIs. The
renderer projects these extra vars (see `internal/converge/bastion`):

| Fact | Shape |
| --- | --- |
| `bootwright_openshift_release_version` | OpenShift release the controller CLIs (`oc`, `kubectl`, `openshift-install`) are pinned to |
| `bootwright_clis_install_dir` | Directory the controller CLIs are installed into |
| `bootwright_clis_release_url` | Release-scoped base URL the CLIs and their checksums are fetched from; honors `Environment.spec.defaults.clientsMirror` and otherwise the pinned upstream mirror. The role falls back to `bootwright_clis_mirror_base` only when this var is not projected |

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
`substrateApplyFrom`, `bootApplyRole`, and `mediaPrepareRole`) rather than
constructing role names from diagnostic labels.

## Task-Scoped Apply Vars

Parallel apply playbooks receive scheduler-selected scope through extra vars:

| Fact | Shape |
| --- | --- |
| `bootwright_task_cluster_name` | ContainerCluster name selected for one OpenShift agent task |
| `bootwright_task_machine_name` | Machine name selected for one machine infrastructure task |
| `bootwright_task_managed_os_group_name` | StorageCluster-backed managed OS group selected for one managed OS task |
| `bootwright_task_provider_host_name` | Provider host selected for one shared machine infrastructure prepare/finalize task |
| `bootwright_task_storage_cluster_name` | StorageCluster name selected for one storage task |
| `bootwright_task_storage_prereqs_only` | Optional boolean that limits a storage task to node prerequisites before seed-only cephadm work |
| `bootwright_agent_node_cluster_name` | ContainerCluster name attached to one Ansible pseudo-host in `bootwright_agent_node_hosts` |
| `bootwright_agent_node_machine_name` | Machine name attached to one Ansible pseudo-host in `bootwright_agent_node_hosts` |
| `bootwright_machine_task_cluster_name` | ContainerCluster or managed OS group name attached to one Ansible pseudo-host in `bootwright_machine_task_hosts` |
| `bootwright_machine_task_machine_name` | Machine name attached to one Ansible pseudo-host in `bootwright_machine_task_hosts` |
| `bootwright_machine_task_provider_host_name` | Provider host name attached to one Ansible pseudo-host in `bootwright_machine_task_hosts` |
| `bootwright_install_override` | Optional boolean from `bootwright apply cluster --override`; when true the install role ignores prior local kubeconfig availability |
| `bootwright_ansible_artifacts_dir` | Per-task local artifact directory for controlled runner outputs |

The OpenShift agent role uses those vars to create and publish one cluster ISO,
boot all selected node pseudo-hosts through Ansible host fanout, and run the
final installer wait after the boot-stage task has completed. Machine
infrastructure tasks select one `bootwright_machine_task_hosts` pseudo-host at a
time. Managed OS tasks select the storage group's full pseudo-host group so VM
creation, OS install, SSH wait, and trust recording run in Ansible host fanout.
Managed storage prereq tasks run against the storage-node inventory group and
reserve seed-only cephadm work for the final storage task.
