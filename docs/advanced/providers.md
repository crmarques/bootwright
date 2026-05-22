---
title: Providers
description: InfraProvider capability shapes and cluster machine selection.
---

# Providers

`InfraProvider` declares what a substrate or service host can provide. It does
not decide which cluster consumes the capability.

Current apply support covers libvirt machines with emulated Redfish BMCs and
bare-metal machines with Redfish virtual media. vSphere and OpenShift
Virtualization are valid schema shapes, but their apply adapters are not
converged yet.

## Bare Metal

Bare-metal inventory keeps physical server facts:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata:
  name: rack1-baremetal
spec:
  machines:
    - name: rack1-srv1
      baremetal:
        bootMACAddress: 52:54:00:33:11:10
        interfaces:
          - name: eno1
            macAddress: 52:54:00:33:11:10
          - name: eno2
            macAddress: 52:54:00:33:11:20
        rootDeviceHints:
          deviceName: /dev/sda
        bmc:
          address: redfish-virtualmedia+https://bmc.example.test/redfish/v1/Systems/1
          credentialsRef:
            name: bmc-credentials
          disableCertificateVerification: true
```

The cluster selects that server and adds IP overlays in `ClusterInfra`:

```yaml
components:
  machines:
    - name: master-0
      from:
        provider: rack1-baremetal
        name: rack1-srv1
      networkConfig:
        ref:
          name: rack1-bonded-machine
        addresses:
          - interface: bond0
            ipv4:
              - ip: 192.168.133.20
                prefix-length: 24
```

## vSphere

vSphere profiles keep vCenter, datacenter, failure-domain, and topology facts
inside the provider:

```yaml
spec:
  machineProfiles:
    - name: vsphere-control-plane
      vsphere:
        vcenters:
          - server: vcenter.example.test
            port: 443
            datacenters:
              - dc1
            credentialsRef:
              name: vcenter-credentials
        failureDomains:
          - name: dc1-zone-a
            region: dc1
            zone: zone-a
            server: vcenter.example.test
            topology:
              datacenter: dc1
              computeCluster: /dc1/host/cluster1
              datastore: /dc1/datastore/datastore1
              folder: /dc1/vm/bootwright
              resourcePool: /dc1/host/cluster1/Resources/bootwright
              networks:
                - VM_Network_1
```

The vSphere desired-state shape is present so the schema can stabilize ahead
of the apply adapter. The shipped apply workflows do not converge vSphere
clusters yet.

## Services

Most service capabilities are selected by non-machine component slots:

```yaml
components:
  proxy:
    from:
      provider: host-services
      name: default
    port: 3128
```

Artifact publication is different: generated ISO and boot-artifact publication
is derived from install requirements and uses one provider publisher:

```yaml
spec:
  artifactPublishers:
    - name: default
      http:
        hostRef:
          name: services-host
        port: 9443
        routes:
          redfishVirtualMedia:
            addressName: lab-lan
          clusterInstall:
            addressName: lab-lan
```

Publisher route `addressName` values resolve against the named addresses on
the selected `hostRef`. For `redfishVirtualMedia`, use a BMC-routable IP
address entry in most environments; many BMCs do not reliably resolve DNS
aliases, and Bootwright uses the resolved value directly in the ISO URL sent to
Redfish. Bootwright serves these routes over HTTPS with a self-signed
certificate generated on the provider host. Omit `http.port` to use the
default `8443`.

Supported authored service slots are load balancer, proxy, name resolution,
and registry.

For real BMCs, the artifact publisher route used by
`redfishVirtualMedia.addressName` should usually resolve to an IP address that
the BMC network can reach. Controller reachability alone is not enough for
virtual-media ISO fetches.
