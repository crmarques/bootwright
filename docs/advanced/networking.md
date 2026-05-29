---
title: Networking
description: NetworkConfig templates, machine overlays, endpoints, and load balancers.
---

# Networking

`NetworkConfig` is the reusable network template for agent installs. It owns:

- `machineNetwork[]`, rendered into `install-config.yaml`
- `template.networkConfig`, rendered into `agent-config.yaml`
- optional substrate hints for Bootwright provider setup

## Bonded Bare-Metal Template

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

Each cluster machine references the template and overlays only its address:

```yaml
networkConfig:
  ref:
    name: rack1-bonded-machine
  addresses:
    - interface: bond0
      ipv4:
        - ip: 192.168.133.20
          prefix-length: 24
```

## Endpoints

Cluster endpoints live in `ClusterInfra.spec.endpoints` as a map keyed by
`api`, `apiInt`, and `ingress`. Each endpoint declares who owns the VIP:

```yaml
endpoints:
  api:
    vip: 192.168.133.10          # OpenShift-managed
  apiInt:
    externalVip: 192.168.133.10  # external LB/DNS, outside Bootwright
  ingress:
    providedBy:
      componentRef:
        name: apps
      address: apps-ip           # Bootwright-provisioned load balancer
```

For Bootwright-provisioned load balancers, declare the target component and its
bind addresses:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: InfraComponent
metadata:
  name: apps
spec:
  loadBalancer:
    type: haProxy
    hostRef:
      name: lab-host
    bindAddresses:
      - name: apps-ip
        ip: 192.168.133.11
```

If a load balancer has exactly one bind address, the endpoint may omit
`providedBy.address`. Listener ports are derived from endpoint names. Effective
VIPs are validated against the machine networks selected by cluster machines.

Bootwright renders and converges the HAProxy provider service for
`providedBy` endpoints. Today, automatic VIP attachment is implemented only
for libvirt-backed cluster infrastructure when the load-balancer host is also
the infra host that can attach the address to the libvirt network. Other
placements require the external network fabric to route the VIP to the
load-balancer host.

## Name Resolution

`NetworkConfig.spec.dnsRefs[]` selects entries from
`Environment.spec.infraComponents.nameResolution[]`. Static resolver servers
may still come from the `NetworkConfig` NMState template; resolved DNS refs are
appended to the rendered `dns-resolver.config.server` list. Keep `dnsRefs`
outside `template.networkConfig`; that map is raw NMState.

Managed name-resolution services render records for `api`, `api-int`, and the
cluster apps wildcard for each consuming cluster. Use
`additionalIngressHosts[]` on the environment entry or the managed
`InfraComponent.spec.nameResolution` when specific ingress hostnames, such as
console or OAuth routes, must resolve before the cluster DNS operator is ready.
Those hostnames point at the consuming cluster's ingress VIP, and values from
the environment entry and component merge for shared services.
