# Ansible Vars Contract

Go renders the variable set consumed by embedded Ansible playbooks. The Go
renderer is the source of truth; this file documents the current shape for role
authors.

## Top-Level Facts

| Fact | Shape |
| --- | --- |
| `bootwright_environment` | environment defaults, proxy selections, mirror, component image declarations |
| `bootwright_hosts` | host SSH endpoints and capability tags |
| `bootwright_providers` | provider capability inventory |
| `bootwright_infra_components` | host-bound infra services such as artifact servers |
| `bootwright_clusters` | per-cluster endpoints, networks, components, and nodes |
| `bootwright_storage_clusters` | managed storage apply inputs, seed hosts, cephadm files, operation files, and attachment contexts |
| `bootwright_provider_services` | provider/BMC service instances with rendered role names |
| `bootwright_infra_component_services` | InfraComponent service instances with rendered role names |
| `bootwright_provider_host_setups` | provider-host setup roles selected by machine drivers |
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
    ntpSources:
      - name: external-01
        type: external
        address: ntp.example.test
      - name: lab-ntp
        type: managed
        componentRef: ntp-server
        endpoint: cluster
    artifactServers:
      - name: default
        type: managed
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
      - name: apps
        address: 192.168.133.11
        source:
          type: infraComponent
          componentRef: apps
          bindAddress: apps-ip
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
        substrateApplyRole: bootwright.core.machine_substrate_libvirt
        substrateDestroyRole: bootwright.core.machine_substrate_libvirt
        bmcApplyRole: bootwright.core.provider_service_bmc_emulated
        bmcDestroyRole: bootwright.core.provider_service_bmc_emulated
        bootApplyRole: bootwright.core.container_cluster_boot_redfish
        mediaPrepareRole: bootwright.core.container_cluster_media_libvirt
        hostSetupRoles:
        networkAttachment:
          kind: libvirt | vsphere | kubevirt | baremetal
          libvirt: { bridge }
          vsphere: { portgroup }
          kubevirt: { nad }
          baremetal: { vlan }
          - bootwright.core.host_libvirt
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
          vmediaPort: 8001
          credentialRef: bmc-credentials
          sushyToolsVersion: 2.1.0
      - kind: loadBalancer
        name: apps              # ClusterInfra component name
        providerName: lab-provider
        capabilityName: default # InfraProvider capability name
        hostRef: services-host
        hostAddress: 192.168.133.1
        realisation: haProxy
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
        hostRef: services-host
        hostAddress: 192.168.133.1
        realisation: http
        applyRole: bootwright.core.infra_component_artifact_server_http
        destroyRole: bootwright.core.infra_component_artifact_server_http
        image: docker.io/library/nginx:1.29.8-alpine3.23
        bindAddress: 0.0.0.0
        port: 8443
        listeners:
          - { name: https, protocol: https, port: 8443 }
        endpoints:
          - { name: bmc, listener: https, hostAddress: lab-lan }
          - { name: cluster, listener: https, hostAddress: cluster-lan }
        url: https://192.168.133.1:8443/
        tls:
          commonName: 192.168.133.1
          dnsNames: []
          ipAddresses: [192.168.133.1]
      - kind: nameResolution
        name: default
        providerName: lab-provider
        hostRef: services-host
        hostAddress: 192.168.133.1
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
      - kind: ntp
        name: ntp-server
        componentName: ntp-server
        providerName: InfraComponent
        hostRef: services-host
        hostAddress: 192.168.133.1
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
          clusterInfra: prod-3node-infra
          name: master-0
```

## Host Service Shapes

Provider playbooks consume `bootwright_provider_services[]` for provider/BMC
service work. InfraComponent playbooks consume
`bootwright_infra_component_services[]` for host-bound shared services such as
load balancers, artifact servers, proxies, name resolution, NTP, and registries.
Go resolves shared service identity as `(kind, providerName, name)` before
rendering; host placement and ports are conflict fields, and mergeable overlays
are unioned in the resolved graph. The renderer then emits one aggregated
Ansible service instance with `hostRef`, `applyRole`, and `destroyRole`; the
service role consumes the rest of the flat component fields. Mergeable fields
such as HAProxy `frontends`, dnsmasq records, dnsmasq
`additionalIngressHosts`, chrony `allowedNetworks`, and BMC `machines` carry
per-cluster entries or graph-unioned values.

```yaml
bootwright_provider_services:
  - kind: bmc
    providerName: lab-libvirt-provider
    name: emulated
    hostRef: lab-host
    hostAddress: 192.168.133.1
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
      vmediaPort: 8001
      credentialRef: bmc-credentials
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
    hostRef: services-host
    hostAddress: 192.168.133.1
    realisation: haProxy
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
bootwright_provider_host_setups:
  - hostRef: lab-host
    hostAddress: 192.168.133.1
    applyRole: bootwright.core.host_libvirt
```

## Managed Storage Shape

Managed storage playbooks consume one projected entry per
`StorageCluster.spec.management: managed` Ceph cluster. Go owns validation,
rendered file paths, scheduling, ledgers, and final Data Foundation attachment
records. The storage role owns remote host mutation and `cephadm`, `ceph`, and
`radosgw-admin` command execution.

```yaml
bootwright_storage_clusters:
  - name: ceph-stretch
    seedHost: storage__ceph-stretch
    remoteWorkDir: /tmp/bootwright-storage-ceph-stretch
    resultPath: "{{ bootwright_ansible_artifacts_dir }}/storage-result.json"
    clusterNetworkCIDRs:
      - 192.168.133.0/24
    bootstrap:
      seedNode: ceph-dc1-0
      monIP: 192.168.133.30
    ceph:
      bootstrapSpecPath: "{{ bootwright_rendered_dir }}/storage/ceph-stretch/cephadm/bootstrap-spec.yaml"
      servicesSpecPath: "{{ bootwright_rendered_dir }}/storage/ceph-stretch/cephadm/services.yaml"
      operationsPath: "{{ bootwright_rendered_dir }}/storage/ceph-stretch/ceph/operations.yaml"
    clusterSSH:
      user: root
      privateKeyPath: /var/lib/bootwright/contexts/lab/secrets/cephadm-cluster-ssh
      publicKeyPath: /var/lib/bootwright/contexts/lab/secrets/cephadm-cluster-ssh.pub
    dataFoundationBindings:
      - cluster: prod-3node
        addon: openshift-data-foundation
        input: external-storage
        export: ceph
```

The storage inventory also contains one synthetic seed host per managed storage
cluster in `bootwright_storage_hosts`. The seed host renders
`bootwright_storage_cluster_name`, `ansible_host`, `ansible_user`, and
`ansible_ssh_private_key_file` from `nodeSSH`; omitted `nodeSSH.user` defaults
to `root`.

## Projection Rule

Roles consume already-projected blocks. They should not rediscover provider
facts from the raw capability map and should not branch on user schema details.
If a role needs a substrate-specific value, image reference, or tool version,
add it to the Go renderer first and consume the flat field in Ansible.
Container-backed managed service components (`loadBalancer`, `proxy`,
`nameResolution`, and `registry`) consume `component.image`; host-package
services such as `ntp` consume package/config fields, and the `bmc_emulated`
role consumes provider BMC service `bmcEmulated.*`. Layer playbooks dispatch exact
rendered role names (`applyRole`, `destroyRole`, `substrateApplyRole`,
`bootApplyRole`, and `mediaPrepareRole`) rather than constructing role names
from diagnostic labels.

## Task-Scoped Apply Vars

Parallel apply playbooks receive scheduler-selected scope through extra vars:

| Fact | Shape |
| --- | --- |
| `bootwright_task_cluster_name` | ContainerCluster name selected for one OpenShift agent task |
| `bootwright_task_storage_cluster_name` | StorageCluster name selected for one storage task |
| `bootwright_agent_node_cluster_name` | ContainerCluster name attached to one Ansible pseudo-host in `bootwright_agent_node_hosts` |
| `bootwright_agent_node_machine_name` | ClusterInfra machine component name attached to one Ansible pseudo-host in `bootwright_agent_node_hosts` |
| `bootwright_install_override` | Optional boolean from `bootwright apply cluster --override`; when true the install role ignores prior local kubeconfig availability |
| `bootwright_ansible_artifacts_dir` | Per-task local artifact directory for controlled runner outputs |

The OpenShift agent role uses those vars to create and publish one cluster ISO,
boot all selected node pseudo-hosts through Ansible host fanout, and run the
final installer wait after the boot-stage task has completed.
