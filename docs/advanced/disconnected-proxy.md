---
title: Disconnected & proxied installs
description: Environment proxy defaults, managed vs external proxies, the three proxy targets, mirror registries, image digest sources, and trust.
---

# Disconnected & proxied installs

Proxy and mirror settings have two owners. Egress proxy defaults and the mirror
catalog live on the `Environment`; whether a cluster installs from a mirror at
all is a per-cluster switch on `ContainerCluster`. This page ties them together.
For the field tables, see [Environment](../concepts/environment.md); for how
secret bytes are declared and stored, see [Secrets](../concepts/secrets.md).

The committed references are
`examples/sno-libvirt-redfish-disconnected-services` (a single-node lab with a
managed mirror registry and artifact server behind an external proxy) and
`examples/ceph-ibm-baremetal-redfish` (real bare metal on an external proxy).

## Environment proxy

A proxy entry lives under `Environment.spec.infraComponents.proxies[]`.
`management: external` describes a proxy Bootwright only consumes; you supply its
URLs inline. `spec.proxyFor` then routes each proxy target to a named entry:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: lab
spec:
  baseDomain: bootwright.test
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
    machineOSInstall: default
```

## The three proxy targets

`spec.proxyFor` has three independent slots, each naming a `proxies[]` entry.
Omitting a value — or setting the reserved value `none` — disables proxy use for
that target.

| Target | Routes the proxy through to… |
| --- | --- |
| `proxyFor.bootwright` | Bootwright's own runtime actions (package managers, downloads, host-side tooling). |
| `proxyFor.containerClusterInstall` | The OpenShift/OKD installer input — rendered into `install-config.yaml`. |
| `proxyFor.machineOSInstall` | The managed-OS (Anaconda) install fetch — the `rhsm`/`url`/`repo` Kickstart directives. |

A managed-OS boot ISO carries no packages, so the node reaches its install tree
or the Red Hat CDN over the network during install; on a proxied estate that
traffic goes through `proxyFor.machineOSInstall`. See
[Managed OS installs](managed-os.md) for the install-media side.

!!! warning "machineOSInstall must be an external proxy"
    The managed OS is installed *before* any Bootwright-managed proxy could
    exist, so `machineOSInstall` only takes effect for an `external` proxy entry
    (one carrying `connection`). A managed selection renders no install proxy.
    When the proxy entry carries `auth.proxyAuthRef`, the credentials are baked
    into the `--proxy=` Kickstart directives at install time and the per-machine
    install ISO is tightened to `0600`.

## Managed vs external proxies

An **external** proxy entry carries `connection` URLs you supply. A **managed**
proxy entry instead selects a Bootwright-managed `InfraComponent` (one carrying
`spec.proxy`) by name; Bootwright derives the proxy URL from that component's
service host and port, so you write no `connection` URLs:

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
    bootstrap, or expect Bootwright to skip managed-proxy use for its own runtime
    actions until after the proxy component has converged. For the same reason,
    `proxyFor.machineOSInstall` ignores a managed selection entirely.

### Package managers and `noProxy`

Bootwright applies the egress proxy to package managers (`dnf`/`yum`) through the
**environment** — `http_proxy`, `https_proxy`, and `no_proxy` exported on every
task that runs them, and persisted to `/etc/environment`, `/etc/profile.d`, and
the systemd `DefaultEnvironment`. It does **not** write a `proxy=` line into
`/etc/yum.conf` or `/etc/dnf/dnf.conf`: those files have no `noproxy` directive,
so a config `proxy=` would force every repository — including the `noProxy`
hosts — through the proxy. Driving the proxy from the environment instead lets
the package manager honour the `noProxy` exceptions.

## Auth handling

When a proxy entry carries `auth.proxyAuthRef`, the referenced credentials are
secret bytes. Read-only commands that would print credential bytes fail closed by
default. `bootwright print-env` emits shell exports for the current context,
including proxy variables, and refuses to print a credential-bearing proxy export
unless you pass `--sensitive`:

```bash
eval "$(bootwright print-env --sensitive)"
```

Bootwright does not write the credential-bearing exports to the world-readable
`/etc/environment`, so an operator running `dnf` by hand under a credentialed
proxy must load the exports for the current context first with the same command.

## Disconnected install

Disconnected (air-gapped) install is a per-cluster switch on
`ContainerCluster.spec.install.mode`. The field accepts `connected` (the default
when omitted) or `disconnected`:

```yaml
spec:
  install:
    mode: disconnected
```

A connected cluster pulls release and operator images from upstream registries. A
disconnected cluster pulls everything from a mirror, so the `Environment` must
also provide mirror trust material and a mirror to pull from.

### Mirror registry

`Environment.spec.registries.mirror` points the disconnected install at one
mirror — an external URL with its credentials and CA trust bundle:

```yaml
spec:
  registries:
    mirror:
      url: registry.example.test:5000
      credentialsRef: mirror-registry-credentials
      trustBundleRef: mirror-registry-ca
```

Instead of an external URL you can run a Bootwright-managed registry component
(`infraComponents.registries[]` with `management: managed` and a `componentRef`);
the mirror URL is then derived from the managed registry's service host and the
default mirror port (`5000`). Release image sources are distribution-aware:
OpenShift and OKD disconnected renders use the configured release image source
for that distribution.

### Image digest sources

`Environment.spec.registries.imageDigestSources[]` maps individual image sources
to mirror locations (the installer's `imageDigestSources`):

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

The mirror-derived release `imageDigestSources` are added only under
`install.mode: disconnected`; any `imageDigestSources[]` you declare on the
`Environment` are always rendered.

### Trust bundles

Mirror, proxy, and install trust material are declared as `Environment` secrets
and referenced by name: `trustBundleRef` on the mirror registry, the proxy
`auth.proxyAuthRef`, and `MachineImage.spec.trustRefs[]` for managed-OS ISO
downloads. The secret bytes are never checked in; they live in the local context.
See [Secrets](../concepts/secrets.md).

### Boot artifacts and the minimal ISO

A connected agent install boots from a full agent ISO that embeds the kernel,
initramfs, and root filesystem. A disconnected install cannot reach the internet
for those boot artifacts, so Bootwright renders a **minimal ISO** that fetches
them at boot from the managed artifact server over HTTP. The artifact server's
endpoint URL becomes the installer's `bootArtifactsBaseURL`.

This requires an artifact endpoint binding: the cluster's effective
`artifactAccess.containerClusterInstall.endpointRef` must resolve to an endpoint
on a declared artifact server. A fleet can declare the binding once as an
environment default so every disconnected cluster inherits it:

```yaml
spec:
  defaults:
    artifactAccess:
      serverRef: default
      containerClusterInstall:
        endpointRef: cluster
```

!!! note "minimal ISO and bootArtifactsBaseURL are disconnected-only"
    Bootwright renders `minimalISO: true` and an endpoint-derived
    `bootArtifactsBaseURL` into `agent-config.yaml` only for a cluster whose
    `install.mode` is `disconnected` **and** whose effective
    `artifactAccess.containerClusterInstall` resolves an artifact-server
    endpoint. A connected cluster never gets these keys, even if it has an
    artifact endpoint bound for other reasons (for example Redfish virtual
    media).

## RHSM and Satellite redirect

A managed-OS `redhatCDN` install registers against the public Red Hat CDN unless
the referenced entitlement's `rhsm` arm carries a `satellite` block, in which
case the install registers and pulls content from that Red Hat Satellite instead
— no `MachineImage` change is needed. This is how a disconnected or proxied
estate keeps RHEL package fetches inside the perimeter. See
[Managed OS installs](managed-os.md) and the entitlement model on
[Environment](../concepts/environment.md).

## Host trust for disconnected labs

Disconnected hosts are reached over SSH like any other machine. A non-interactive
pipeline never records SSH server-key trust on first use, so pre-record trust
before the first `preflight`/`apply` rather than relying on trust-on-first-use:

```bash
bootwright host trust
```

See [Operations & recovery](operations.md) and the
`sno-libvirt-redfish-disconnected-services` example, which layers a `host trust`
step into its disconnected walkthrough.
