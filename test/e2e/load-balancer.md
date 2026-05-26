# Load Balancer Modes

Cluster VIP ownership is declared per endpoint in
`ClusterInfra.spec.endpoints`.

Bootwright-provisioned load balancer:

```yaml
endpoints:
  api:
    providedBy:
      componentRef:
        name: control-plane
      address: control-plane-ip
---
apiVersion: bootwright.io/v1alpha1
kind: InfraComponent
metadata:
  name: control-plane
spec:
  loadBalancer:
    type: haProxy
    hostRef:
      name: services-host
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
