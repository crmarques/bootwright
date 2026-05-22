# Proxy And Mirror E2E Notes

External proxy defaults live in `Environment.spec.proxy`:

```yaml
proxy:
  httpProxy: http://proxy.example.test:3128
  httpsProxy: http://proxy.example.test:3128
  noProxy:
    - .example.test
    - 192.168.133.0/24
  useFor:
    bootwright: true
    clusterInstall: true
```

Managed proxy services live in `ClusterInfra.spec.components.proxy` and are
selected from `InfraProvider.spec.proxies[]`. Do not configure an
external proxy URL and a managed proxy component in the same loaded state.

Disconnected mode is set on each `ContainerCluster`:

```yaml
install:
  mode: disconnected
```

It requires mirror trust material under `Environment.spec.registries.mirror` and
either an external mirror URL or a managed registry component.
