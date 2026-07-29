# Load Balancer Modes

Cluster endpoint ownership is declared per endpoint in
`ContainerCluster.spec.install.endpoints`. For the `source.type` values, their
defaults, and which clusters may use each one, see
[Endpoints](../../docs/advanced/networking.md#endpoints). The case-shaped
fragments to edit before a run:

Bootwright-provisioned load balancer:

```yaml
endpoints:
  api:
    source:
      type: infraComponent
      componentRef: control-plane
      bindAddressRef: control-plane-ip
```

backed by the referenced component:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: InfraComponent
metadata:
  name: control-plane
spec:
  type: loadBalancer
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

OpenShift-managed endpoint ownership — multi-node agent installs only:

```yaml
endpoints:
  api:
    address: 192.168.133.10
```
