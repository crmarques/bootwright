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

## Name Resolution

`NetworkConfig.spec.template.dnsRefs[]` selects entries from
`Environment.spec.infraComponents.nameResolution[]`. Static resolver servers
may still come from the `NetworkConfig` NMState template; resolved DNS refs are
appended to the rendered NMState server list and never rendered as
Bootwright-only fields.
