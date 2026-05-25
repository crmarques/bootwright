# Ansible Vars Contract

Go renders the variable set consumed by embedded Ansible playbooks. The Go
renderer is the source of truth; this file documents the current shape for role
authors.

## Top-Level Facts

| Fact | Shape |
| --- | --- |
| `bootwright_environment` | environment defaults, bastion hostRef, proxy, mirror, component image declarations |
| `bootwright_hosts` | host SSH endpoints and capability tags |
| `bootwright_providers` | provider capability inventory |
| `bootwright_clusters` | per-cluster endpoints, networks, components, and nodes |
| `bootwright_provider_services` | provider-host service instances with rendered role names |
| `bootwright_provider_host_setups` | provider-host setup roles selected by machine drivers |
| `bootwright_proxy` | effective proxy settings |

## Environment Shape

```yaml
bootwright_environment:
  name: lab
  baseDomain: example.test
  bastion:
    hostRef: lab-host
```

## Cluster Shape

```yaml
bootwright_clusters:
  - name: prod-3node
    installMode: connected
    installMethod: agent
    baseDomain: example.test
    distribution:
      type: openshift
      release: { version, channel, image }
    endpoints:
      - name: api
        address: 192.168.133.10
        externalVip: 192.168.133.10
      - name: ingress
        address: 192.168.133.11
        providedBy: { loadBalancer: apps, address: apps-ip }
    agentIsoPublishTargets:
      - stageHost: services-host
        stagePath: "{{ bootwright_host_state_dir }}/artifacts-server/__BOOTWRIGHT_AGENT_ISO_PUBLISH_TOKEN__/agent-prod-3node.iso"
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
        substrate:
          kind: libvirt | vsphere | kubevirt | baremetal
          libvirt: { bridge }
          vsphere: { portgroup }
          kubevirt: { nad }
          physical: { vlan }
    components:
      - kind: machines
        name: master-0
        providerName: lab-libvirt-provider
        substrateRole: libvirt
        bmcRole: emulated
        bootRole: redfish
        substrateApplyRole: substrate_libvirt
        substrateDestroyRole: substrate_libvirt
        bmcApplyRole: bmc_emulated
        bmcDestroyRole: bmc_emulated
        bootApplyRole: boot_redfish
        mediaPrepareRole: media_libvirt
        hostSetupRoles:
          - host_libvirt
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
        boot:
          redfish: {}
          agentIso: {}
          media:
            libvirt: {}         # consumed only by media_libvirt
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
        applyRole: load_balancer_haproxy
        destroyRole: load_balancer_haproxy
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
        name: default
        providerName: host-services
        hostRef: services-host
        hostAddress: 192.168.133.1
        realisation: http
        applyRole: artifacts_http
        destroyRole: artifacts_http
        bindAddress: 0.0.0.0
        port: 8443
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
        applyRole: dns_dnsmasq
        destroyRole: dns_dnsmasq
        image: docker.io/dockurr/dnsmasq:2.92_p2
        bindAddress: 192.168.133.1
        port: 53
        hostRecords:
          - { name: api.prod-3node.example.test, address: 192.168.133.10 }
          - { name: api-int.prod-3node.example.test, address: 192.168.133.10 }
        domainRecords:
          - { name: apps.prod-3node.example.test, address: 192.168.133.11 }
    nodes:
      master-0:
        role: master
        machineRef:
          clusterInfra: prod-3node-infra
          name: master-0
```

## Provider Service Shape

Provider playbooks consume `bootwright_provider_services[]` instead of scanning
cluster components and hardcoding role names. Go resolves shared service
identity as `(kind, providerName, name)` before rendering; host placement and
ports are conflict fields, and mergeable overlays are unioned in the resolved
graph. The renderer then emits one aggregated Ansible service instance with
`hostRef`, `applyRole`, and `destroyRole`; the service role consumes the rest of
the flat component fields. Mergeable fields such as HAProxy `frontends`,
dnsmasq records, dnsmasq `additionalIngressHosts`, and BMC `machines` carry
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
    applyRole: bmc_emulated
    destroyRole: bmc_emulated
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
bootwright_provider_host_setups:
  - hostRef: lab-host
    hostAddress: 192.168.133.1
    applyRole: host_libvirt
```

## Projection Rule

Roles consume already-projected blocks. They should not rediscover provider
facts from the raw capability map and should not branch on user schema details.
If a role needs a substrate-specific value, image reference, or tool version,
add it to the Go renderer first and consume the flat field in Ansible.
Container-backed managed service components (`loadBalancer`, `proxy`,
`nameResolution`, and `registry`) consume `component.image`; the
`bmc_emulated` role consumes provider-service `bmcEmulated.*`. Layer playbooks
dispatch exact rendered role names (`applyRole`, `destroyRole`,
`substrateApplyRole`, `bootApplyRole`, and `mediaPrepareRole`) rather than
constructing role names from diagnostic labels.

## Task-Scoped Apply Vars

Parallel apply playbooks receive scheduler-selected scope through extra vars:

| Fact | Shape |
| --- | --- |
| `bootwright_task_cluster_name` | ContainerCluster name selected for one OpenShift agent task |
| `bootwright_agent_node_cluster_name` | ContainerCluster name attached to one Ansible pseudo-host in `bootwright_agent_node_hosts` |
| `bootwright_agent_node_machine_name` | ClusterInfra machine component name attached to one Ansible pseudo-host in `bootwright_agent_node_hosts` |
| `bootwright_install_override` | Optional boolean from `bootwright apply cluster --override`; when true the install role ignores prior local kubeconfig availability |

The OpenShift agent role uses those vars to create and publish one cluster ISO,
boot all selected node pseudo-hosts through Ansible host fanout, and run the
final installer wait after the boot-stage task has completed.
