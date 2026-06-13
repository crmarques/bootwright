---
title: Proxy And Disconnected Installs
description: Environment proxy defaults, managed proxies, and disconnected mirror inputs.
---

# Proxy And Disconnected Installs

Proxy and mirror settings have two different owners. Egress proxy defaults and
the mirror catalog live on the `Environment`; whether a cluster installs from a
mirror at all is a per-cluster switch on `ContainerCluster`. This page is the
how-to that ties them together; the exact field tables are owned by the
[Environment](../api/environment.md) and
[ContainerCluster](../api/container-cluster.md) API references.

Complete desired-state examples live under `examples/`. Start with
`sno-libvirt-redfish-disconnected-services` for a single-node lab with managed
proxy and registry services, or `baremetal-redfish-fleet` for the larger fleet
layout that separates shared infrastructure under `infra/`. See
[Reference Examples](examples.md) for the full catalog.

## Environment proxy

A proxy entry lives under `Environment.spec.infraComponents.proxies[]`.
`management: external` describes a proxy Bootwright only consumes; you supply
its URLs inline:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: lab
spec:
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
          auth:
            proxyAuthRef: proxy-credentials

  proxyFor:
    bootwright: default
    containerClusterInstall: default
```

`proxyFor.bootwright` names the proxy applied to Bootwright's own runtime
actions. `proxyFor.containerClusterInstall` names the proxy rendered into
installer input. Both take the `name` of a `proxies[]` entry. Omitting a value
— or setting the reserved value `none` — disables proxy use for that target.

When a proxy entry carries `auth.proxyAuthRef`, the referenced credentials are
secret bytes. `bootwright print-env` emits shell exports for the current
context, including proxy variables, and it **fails closed** rather than printing
a credential-bearing proxy export unless you pass `--sensitive`:

```bash
eval "$(bootwright print-env --sensitive)"
```

Without `--sensitive`, `print-env` refuses to emit the export. This matches the
broader rule that read-only commands which would print credential bytes fail
closed by default; see [Secrets](secrets.md).

### Managed proxies

A managed proxy entry uses `management: managed` and selects a
Bootwright-managed `InfraComponent` (one carrying `spec.proxy`) by name through
`componentRef`. Bootwright derives the proxy URL from that component's selected
service host and port, so you do not write `connection` URLs for a managed
proxy:

```yaml
spec:
  infraComponents:
    proxies:
      - name: default
        management: managed
        componentRef: egress-proxy
        # endpointRef: <endpoints[] entry on egress-proxy>  # optional
```

!!! warning "Managed proxies are not available at bootstrap"
    A managed proxy is created by infrastructure convergence, so `bastion setup`
    cannot depend on a managed `proxyFor.bootwright` selection: the proxy does
    not exist yet when the bastion is prepared. Use an external proxy for
    bootstrap, or expect Bootwright to skip managed-proxy use for its own
    runtime actions until after the proxy component has been converged.

## Disconnected install

Disconnected (air-gapped) install is a per-cluster switch on
`ContainerCluster.spec.install.mode`. The field accepts `connected` (the
default when omitted) or `disconnected`:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: ContainerCluster
metadata:
  name: prod
spec:
  install:
    mode: disconnected
```

A connected cluster pulls release and operator images from the upstream
registries. A disconnected cluster pulls everything from a mirror, so the
`Environment` must also provide mirror trust material and a mirror to pull from.

### Mirror registry

`Environment.spec.registries.mirror` points the disconnected install at one
mirror. Supply an external mirror URL with its credentials and CA trust bundle:

```yaml
spec:
  registries:
    mirror:
      url: registry.example.test:5000
      credentialsRef: mirror-registry-credentials
      trustBundleRef: mirror-registry-ca
```

Instead of an external URL you can run a Bootwright-managed registry component
(`infraComponents.registries[]` with `management: managed` and a
`componentRef`); the mirror URL is then derived from the managed registry's
service host and the default mirror port (`5000`).

Release image sources are distribution-aware. OpenShift and OKD disconnected
renders use the configured release image source for that distribution rather
than assuming the same upstream registry.

### Image digest sources

`Environment.spec.registries.imageDigestSources[]` is the other half of the
mirror story: it maps individual image sources to mirror locations (the
installer's `imageDigestSources`). Each entry carries:

```yaml
spec:
  registries:
    imageDigestSources:
      - source: quay.io/openshift-release-dev/ocp-release
        mirrors:
          - registry.example.test:5000/ocp-release
        sourcePolicy: NeverContactSource
```

- `source` (required) — the image repository being mirrored.
- `mirrors` (required) — one or more mirror repositories that serve it.
- `sourcePolicy` (optional) — `NeverContactSource` or `AllowContactingSource`.
  When omitted, Bootwright defaults it to `NeverContactSource` so a disconnected
  install never reaches back to the upstream source.

For the full field reference, see
[Environment → registries](../api/environment.md).

### Boot artifacts and the minimal ISO

A connected agent install boots from a full agent ISO that already embeds the
kernel, initramfs, and root filesystem. A disconnected install cannot reach the
internet for those boot artifacts, so Bootwright instead renders a **minimal
ISO** — a small ISO that fetches the kernel, initramfs, and root filesystem at
boot from the managed artifact server over HTTP. The artifact server's endpoint
URL becomes the installer's `bootArtifactsBaseURL`.

This requires an artifact endpoint binding: the cluster's effective
`artifactAccess.containerClusterInstall.endpointRef` must resolve to an endpoint
on a declared artifact server. A fleet can declare the common binding once as an
environment default so every disconnected cluster inherits it:

```yaml
spec:
  defaults:
    artifactAccess:
      serverRef: default
      containerClusterInstall:
        endpointRef: cluster
```

The default's `serverRef` and `endpointRef` names are validated where they are
declared — against `spec.infraComponents.artifactServers[].name` and the
selected server's endpoints — even while no cluster consumes them, so a typo
fails immediately rather than when a disconnected or bare-metal consumer appears
later.

!!! note "minimalISO / bootArtifactsBaseURL are disconnected-only"
    Bootwright renders `minimalISO: true` and an endpoint-derived
    `bootArtifactsBaseURL` into `agent-config.yaml` only for a cluster whose
    `install.mode` is `disconnected` **and** whose effective
    `artifactAccess.containerClusterInstall` resolves an artifact-server
    endpoint. A connected cluster never gets these keys, even if it has an
    artifact endpoint bound for other reasons (for example Redfish virtual
    media). Likewise, the mirror-derived release `imageDigestSources` are added
    only under `install.mode: disconnected`; any `imageDigestSources[]` you
    declare on the `Environment` are always rendered.

## Host trust for disconnected labs

Disconnected hosts are reached over SSH the same way as any other machine. A
non-interactive pipeline never records SSH server-key trust on first use, so
pre-record trust before the first `preflight`/`apply` rather than relying on
trust-on-first-use:

```bash
bootwright host trust
```

See [Operations and Recovery](operations.md) and the
`sno-libvirt-redfish-disconnected-services` example, which layers a
`host trust` step into its disconnected walkthrough.
