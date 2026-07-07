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

For a fully air-gapped estate that has a RHEL DVD but no package mirror or
reachable CDN, the boot ISO's `packageSource: hostedTree` is the self-contained
option: Bootwright extracts the DVD once into the artifact server and each node
installs GPG-signed packages from that local tree over the `machineBoot` http
endpoint — no external mirror, CDN, or proxy at install time. The node's package
fetch is local, so it is **not** routed through `proxyFor.machineOSInstall`. See
[hostedTree](managed-os.md#package-source-mirror-redhatcdn-or-hostedtree) for the
media and endpoint wiring.

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

Red Hat subscription content is the exception. The certificate-based RHEL repos
in `/etc/yum.repos.d/redhat.repo` take their proxy from `/etc/rhsm/rhsm.conf`,
not the `http_proxy` environment — so on a managed Ceph node Bootwright writes
the effective proxy (host, port, credentials, and `noProxy`) into rhsm.conf's
`[server]` section before registering, and `subscription-manager` stamps it into
each RHEL repo. Without this the RHSM plugin marks those repos `proxy = _none_`
and dnf reaches the Red Hat CDN directly, which fails on a proxied estate.

### TLS-inspecting proxies

A proxy that terminates and re-signs HTTPS (SSL-bump / TLS inspection) presents
its own CA on every upstream — including `cdn.redhat.com` — instead of tunnelling
the origin's real certificate. Managed hosts egressing through it must trust that
CA, or every HTTPS fetch fails certificate verification (`unable to get local
issuer certificate`) even though the proxy tunnel itself is reachable. Declare
the proxy's signing CA as a PEM secret and reference it from the proxy
connection:

```yaml
spec:
  infraComponents:
    proxies:
      - name: default
        management: external
        connection:
          httpProxy: http://proxy.example.test:3128
          auth:
            proxyAuthRef: proxy-credentials
          trustBundleRef: corporate-proxy-ca   # the proxy's SSL-bump CA (PEM)
```

Bootwright installs the bundle into the trust store
(`/etc/pki/ca-trust/source/anchors/`, then `update-ca-trust`) of managed hosts
that egress through the proxy, before their package work runs. Leave it unset for
a plain CONNECT-tunnelling proxy that presents the origin's real certificate.

## Auth handling

When a proxy entry carries `auth.proxyAuthRef`, the referenced credentials are
secret bytes. Bootwright loads them into its own Ansible, `dnf`, and subscription
runs automatically, and does not write them to the world-readable
`/etc/environment`. An operator running `dnf` by hand under a credentialed proxy
must set the proxy environment variables (`HTTP_PROXY`/`HTTPS_PROXY`, including
the credentials) in their own shell first.

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

!!! warning "Set `mode: disconnected` on every cluster of a disconnected fleet"
    `install.mode` is per-cluster and defaults to `connected` even when the
    `Environment` declares `registries.mirror` and a managed registry. Bootwright
    does not infer disconnectedness from the fleet, so a cluster that omits `mode`
    validates as connected and then stalls at install trying to reach upstream
    registries the network cannot. Set `mode: disconnected` explicitly on each
    `ContainerCluster` in an air-gapped fleet.

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
See [Secrets](../concepts/secrets.md). For trusting corporate CAs and serving
corporate certificates on the cluster URLs, see
[Corporate TLS](corporate-certificates.md).

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

!!! note "Managed-Ceph registry credential rotation"
    For managed Ceph, each `apply` re-pushes the resolved registry credentials to
    the cephadm manager store (`ceph cephadm registry-login`). Rotating the
    entitlement's registry credentials therefore takes effect cluster-wide on the
    next `apply` — day-2 daemon pulls authenticate from the manager store, not
    from a node-level `podman login`, so no manual re-login is needed.

### Pinning the Ceph monitoring and ingress sidecar images

`spec.ceph.image` pins only the Ceph **daemon** image. cephadm pulls its
monitoring and ingress sidecars (Prometheus, Grafana, Alertmanager,
node-exporter, HAProxy, keepalived) from compiled-in **upstream** defaults that a
disconnected estate cannot reach — and an IBM cluster's defaults point at
`registry.redhat.io`, which an IBM (`cp.icr.io`) entitlement cannot pull. Pin
each sidecar to your mirror or entitled registry under `spec.ceph.config[mgr]`:

```yaml
spec:
  ceph:
    config:
      mgr:
        mgr/cephadm/container_image_prometheus: mirror.example.test:5000/prometheus/prometheus:v2.53.0
        mgr/cephadm/container_image_grafana: mirror.example.test:5000/ceph/grafana:10.4.0
        mgr/cephadm/container_image_alertmanager: mirror.example.test:5000/prometheus/alertmanager:v0.27.0
        mgr/cephadm/container_image_node_exporter: mirror.example.test:5000/prometheus/node-exporter:v1.7.0
        mgr/cephadm/container_image_haproxy: mirror.example.test:5000/library/haproxy:2.8
        mgr/cephadm/container_image_keepalived: mirror.example.test:5000/library/keepalived:2.2.8
```

`bootwright validate` raises a non-blocking advisory when monitoring is enabled
on a disconnected (mirror/`imageDigestSources`) or IBM cluster that has not
pinned these, so the gap is caught at author time rather than as a stalled
deploy.

## Host trust for disconnected labs

Disconnected hosts are reached over SSH like any other machine. A non-interactive
pipeline never records SSH server-key trust on first use, so pre-record trust
before the first `preflight`/`apply` rather than relying on trust-on-first-use:

```bash
bootwright machine trust
```

See [Operations & recovery](operations.md) and the
`sno-libvirt-redfish-disconnected-services` example, which layers a `machine trust`
step into its disconnected walkthrough.
