# Load Balancer Modes

Cluster endpoint ownership is declared per endpoint in
`ContainerCluster.spec.install.endpoints`.

Bootwright-provisioned load balancer:

```yaml
endpoints:
  api:
    source:
      type: infraComponent
      componentRef: control-plane
      bindAddressRef: control-plane-ip
---
apiVersion: bootwright.io/v1alpha1
kind: InfraComponent
metadata:
  name: control-plane
spec:
  loadBalancer:
    implementation: haproxy
    machineRef: services-host
    bindAddresses:
      - name: control-plane-ip
        address: 192.168.133.10
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

The omitted `source.type` defaults to `openshift` for the role-keyed
`ContainerCluster.spec.install.endpoints` entries.
