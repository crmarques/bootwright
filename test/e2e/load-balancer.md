# Load Balancer Modes

Cluster endpoint ownership is declared per endpoint in
`ContainerCluster.spec.install.endpoints`.

Bootwright-provisioned load balancer:

```yaml
endpoints:
  api:
    source:
      type: infraComponent
      componentRef:
        name: control-plane
      bindAddress: control-plane-ip
---
apiVersion: bootwright.io/v1alpha1
kind: InfraComponent
metadata:
  name: control-plane
spec:
  loadBalancer:
    type: haProxy
    machineRef:
      name: services-host
    bindAddresses:
      - name: control-plane-ip
        ip: 192.168.133.10
```

External load balancer:

```yaml
endpoints:
  api:
    address: 192.168.133.10
    source:
      type: external
```

OpenShift-managed endpoint ownership is reserved for supported multi-node agent
installs:

```yaml
endpoints:
  api:
    address: 192.168.133.10
```

The omitted `source.type` defaults to `openshift` when a
`ContainerCluster.spec.install.endpointRefs` role uses the endpoint.
