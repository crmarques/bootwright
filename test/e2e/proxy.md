# Proxy And Mirror E2E Notes

External proxy defaults live in `Environment.spec.infraComponents.proxies[]`:

```yaml
infraComponents:
  proxies:
    - name: default
      management: external
      connection:
        httpProxy: http://proxy.example.test:3128
        httpsProxy: http://proxy.example.test:3128
        noProxy:
          - .example.test
          - 192.168.133.0/24
proxyFor:
  bootwright: default
  containerClusterInstall: default
```

An empty or omitted `proxyFor` slot inherits the default proxy — the `proxies[]`
entry with `default: true`, or the sole entry when only one is declared; the
reserved value `none` opts a consumer out. See
[The three proxy targets and `proxyFor`](../../docs/advanced/disconnected-proxy.md#the-three-proxy-targets-and-proxyfor)
for the full model, including managed proxy components.

Disconnected mode is set on each `ContainerCluster`:

```yaml
install:
  mode: disconnected
```

See [Disconnected install](../../docs/advanced/disconnected-proxy.md#disconnected-install)
for the mirror trust and registry material this mode requires.
