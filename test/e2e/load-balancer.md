# Load Balancer Modes

Cluster VIP ownership is declared per endpoint in
`ClusterInfra.spec.endpoints`.

Bootwright-provisioned load balancer:

```yaml
endpoints:
  api:
    providedBy:
      loadBalancer: control-plane
      address: control-plane-ip
components:
  loadBalancers:
    - name: control-plane
      from:
        provider: host-services
        name: default
      bindAddresses:
        - name: control-plane-ip
          ip: 192.168.133.10
```

External load balancer:

```yaml
endpoints:
  api:
    externalVip: 192.168.133.10
```

OpenShift-managed endpoint ownership is reserved for supported multi-node agent
installs:

```yaml
endpoints:
  api:
    vip: 192.168.133.10
```
