---
title: Proxy And Disconnected Installs
description: Environment proxy defaults, managed proxies, and disconnected mirror inputs.
---

# Proxy And Disconnected Installs

Proxy and mirror settings are split by source of truth.

Complete desired-state examples live under `examples/`:
`sno-libvirt-redfish-external-proxy`,
`sno-libvirt-redfish-managed-proxy`,
`sno-libvirt-redfish-disconnected-external-mirror`, and
`sno-libvirt-redfish-managed-registry`.

## Environment Proxy

```yaml
spec:
  proxy:
    httpProxy: http://proxy.example.test:3128
    httpsProxy: http://proxy.example.test:3128
    noProxy:
      - .example.test
      - 192.168.133.0/24
    auth:
      proxyAuthRef:
        name: proxy-credentials
    useFor:
      bootwright: true
      clusterInstall: true
```

`useFor.bootwright` applies to Bootwright runtime actions. `useFor.clusterInstall`
renders the proxy into installer input.

Do not combine an external proxy URL with `ClusterInfra.components.proxy`; a
managed proxy URL is derived from its selected service capability and port.

## Disconnected Install

Disconnected mode is cluster-scoped:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: ContainerCluster
metadata:
  name: prod
spec:
  install:
    mode: disconnected
```

The environment must also provide mirror trust material and either an external
mirror URL or a managed registry component:

```yaml
spec:
  registries:
    mirror:
      url: registry.example.test:5000
      credentialsRef:
        name: mirror-registry-credentials
      trustBundleRef:
        name: mirror-registry-ca
```

Release image sources are distribution-aware. OpenShift and OKD disconnected
renders must use the configured release image source rather than assuming the
same upstream registry.
