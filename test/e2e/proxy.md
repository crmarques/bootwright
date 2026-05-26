# Proxy And Mirror E2E Notes

External proxy defaults live in `Environment.spec.infraComponents.proxies[]`:

```yaml
infraComponents:
  proxies:
    - name: default
      type: external
      spec:
        httpProxy: http://proxy.example.test:3128
        httpsProxy: http://proxy.example.test:3128
        noProxy:
          - .example.test
          - 192.168.133.0/24
proxyFor:
  bootwright: default
  clusterInstall: default
```

Omitted `proxyFor` values and the reserved value `none` disable proxy use.

Managed proxy services live in `InfraComponent.spec.proxy` and are selected by
environment proxy entries with `type: managed`.

Disconnected mode is set on each `ContainerCluster`:

```yaml
install:
  mode: disconnected
```

It requires mirror trust material under `Environment.spec.registries.mirror` and
either an external mirror URL or a managed registry component.
