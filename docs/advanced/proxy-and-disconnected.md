---
title: Proxy And Disconnected Installs
description: Environment proxy defaults, managed proxies, and disconnected mirror inputs.
---

# Proxy And Disconnected Installs

Proxy and mirror settings are split by source of truth.

Complete desired-state examples live under `examples/`. Start with
`sno-libvirt-redfish-disconnected-services` for a single-node lab with managed
proxy and registry services, or `baremetal-redfish-fleet` for the larger fleet
layout that separates shared infrastructure under `infra/`.

## Environment Proxy

```yaml
spec:
  infraComponents:
    proxies:
      - name: default
        type: external
        connection:
          httpProxy: http://proxy.example.test:3128
          httpsProxy: http://proxy.example.test:3128
          noProxy:
            - .example.test
            - 192.168.133.0/24
          auth:
            proxyAuthRef: proxy-credentials

  proxyFor:
    bootwright: default
    containerClusterInstall: default
```

`proxyFor.bootwright` applies to Bootwright runtime actions.
`proxyFor.containerClusterInstall` renders the selected proxy into installer input.
Omitted values and the reserved value `none` disable proxy use.

Managed proxy entries use `type: managed` and reference an `InfraComponent`
with `spec.proxy`; the URL is derived from its selected service host and port.
External proxies are available to every Bootwright phase. Managed proxies are
created by infrastructure convergence, so `bastion setup` cannot depend on a
managed `proxyFor.bootwright` selection. Use an external proxy for bootstrap,
or expect Bootwright to skip managed-proxy use until after the proxy component
has been converged.

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
      credentialsRef: mirror-registry-credentials
      trustBundleRef: mirror-registry-ca
```

Release image sources are distribution-aware. OpenShift and OKD disconnected
renders must use the configured release image source rather than assuming the
same upstream registry.

Disconnected agent installs also need an artifact endpoint binding for cluster
install boot artifacts. A fleet can declare the common binding as an
environment default:

```yaml
spec:
  defaults:
    artifactAccess:
      serverRef: default
      containerClusterInstall:
        endpointRef: cluster
```

The default's `serverRef` and `endpointRef` names are validated where they
are declared — against `spec.infraComponents.artifactServers[].name` and the
selected server's endpoints — even while no cluster consumes them, so a typo
fails immediately rather than when a disconnected or bare-metal consumer
appears later.

When the effective `ContainerCluster.spec.install.artifactAccess` sets
`containerClusterInstall.endpointRef`, Bootwright renders
`minimalISO: true` and an endpoint-derived `bootArtifactsBaseURL` into
`agent-config.yaml`.
