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

## Egress allowlist

Before you configure a proxy, the perimeter it fronts must let the estate reach
the upstreams below. These are the **default** public hosts Bootwright's bastion,
OpenShift nodes, and Ceph nodes contact during provisioning when the source is
*not* redirected. Which hosts are actually reached depends on what you provision
and which package/registry/mirror sources you select — a fully mirrored or
Satellite-backed estate reaches **none** of the public hosts here, and every
default is overridable (columns note the field). Allowlist only the rows for the
paths you use.

All hosts are reached over HTTPS (TCP 443) unless noted. On-prem targets — BMC /
Redfish addresses, managed mirror/artifact/DNS components, an internal
Satellite — are deliberately proxy-bypassed via `noProxy` and are not listed.

### Bastion / controller

The host running Bootwright, Ansible, and the agent installer, and hosting the
managed infra components. Routed through `proxyFor.bootwright`.

| Host | Purpose | Override |
| --- | --- | --- |
| `pypi.org`, `files.pythonhosted.org` | `bastion setup` builds the managed Ansible venv: pinned `pip`, `ansible-core`, and — when the estate uses them — `sushy-tools` (libvirt BMC emulation) and `pyvmomi` (vSphere). Package **metadata** is read from `pypi.org`; the wheels/sdists **download** from `files.pythonhosted.org`. | — |
| `mirror.openshift.com` | `oc` / `openshift-install` client binaries and checksums for the release version (OpenShift/OKD). | `spec.defaults.clientsMirror` |
| `mirror.openshift.com` | `helm` client binary and checksum from the mirror's `latest` channel. | `spec.defaults.helmMirror` |
| `docker.io` | Container images for the managed infra components you declare: `library/haproxy` (load balancer), `library/nginx` (artifact server), `openeuler/squid` (forward proxy), `dockurr/dnsmasq` (DNS), `library/registry` (mirror registry). Only declared components pull. | mirror the images |

!!! note "`bastion setup` is the first thing to egress"
    `bastion setup` builds the Ansible venv with `pip`, so it is usually the
    first command to leave the bastion — and the first to hit a misconfigured
    proxy. pip fails unless the proxy allowlists **both** `pypi.org` (the index
    metadata) *and* `files.pythonhosted.org` (the actual package downloads);
    allowlisting only `pypi.org` still breaks the download. The venv build routes
    through `proxyFor.bootwright`, so that target must resolve to a working proxy.
    If `python3.12` is absent, `bastion setup` first runs `dnf install
    python3.12`, which uses the bastion's own OS package repos (RHEL CDN,
    Satellite, or your mirror) rather than any host above.

### OpenShift clusters

Reached by the agent installer on the bastion and by the cluster nodes during
bootstrap; governed by the cluster pull secret. Redirect with a disconnected
install (`registries.mirror` + `imageDigestSources`, see below).

| Host | Purpose |
| --- | --- |
| `quay.io` | OpenShift release image (`openshift-release-dev/ocp-release` and `openshift-release-dev/ocp-v4.0-art-dev`); the release payload then pulls its component images. |
| `registry.redhat.io` | Operator/operand images for OperatorHub content and the `redhat-operators` catalog used by add-ons. |

### Managed-OS (RHEL) nodes

Only when the install profile uses `packageSource.fromSubscription` (registering against
the public Red Hat CDN). Routed through `proxyFor.machineOSInstall`. A
`packageSource.mirror`, a `rhsm.satellite` redirect, or a `hostedTree` local tree
replaces all of these.

| Host | Purpose |
| --- | --- |
| `subscription.rhsm.redhat.com` | RHSM registration and entitlement refresh. |
| `cdn.redhat.com` | RHEL BaseOS/AppStream package content. |
| `cert-api.access.redhat.com`, `console.redhat.com` | Red Hat Insights — only when the entitlement sets `rhsm.connectToInsights: true`. |

### Ceph nodes

Depends on the storage distribution. Managed nodes route package work through the
egress proxy (see [Package managers and `noProxy`](#package-managers-and-noproxy)).
For the subscription distributions, the RHSM registration egress happens in the
machines-phase registration task — after the OS is in place, before the Ceph
deps-phase repo and registry work reaches the remaining rows.

**Red Hat / IBM Storage Ceph (subscription):**

| Host | Purpose | Distribution |
| --- | --- | --- |
| `subscription.rhsm.redhat.com`, `cdn.redhat.com` | RHSM registration (machines phase) and the RHEL BaseOS/AppStream + `rhceph-*-tools` repos (Ceph deps phase). | Red Hat |
| `registry.redhat.io` | `podman login` + RHCS daemon image (`rhceph/rhceph-*-rhel9`) and cephadm monitoring/ingress sidecars. | Red Hat |
| `cp.icr.io` | IBM registry login + IBM Storage Ceph daemon image (`cp/ibm-ceph/ceph-*-rhel9`). | IBM |
| `public.dhe.ibm.com` | IBM Storage Ceph `.repo` file download (unauthenticated). | IBM |
| `cert-api.access.redhat.com`, `console.redhat.com` | Red Hat Insights — only when `rhsm.connectToInsights: true`. | Red Hat |

For a mirrored vendor registry, set `Entitlement.spec.registry.url` to the
mirror root and pin `StorageCluster.spec.ceph.image.base` at that root plus the
canonical vendor repository suffix. `image.version` alone does not satisfy this:
the base Bootwright derives names the vendor registry, which the mirror cannot
serve, so the mirrored base must be stated. Stream `9` uses
`rhceph/rhceph-9-rhel9` for Red Hat or `ibm-ceph/ceph-9-rhel9` for IBM;
arbitrary repositories below the registry root are rejected. The check covers
the vendor namespace and stream (`rhceph/rhceph-<stream>-rhel`) so the two
distributions cannot be crossed; the trailing build base is yours to supply and
is never validated against a release.

**Community Ceph (OSS):** overridable via `spec.ceph.community.mirror` and
`spec.ceph.image.base`.

| Host | Purpose |
| --- | --- |
| `download.ceph.com` | `cephadm` bootstrap binary and the community Ceph repo. |
| `quay.io` | Community Ceph daemon image (`ceph/ceph`). |
| `dl.fedoraproject.org` | EPEL bootstrap package. |

### Add-ons

Pulled by the OpenShift cluster when the corresponding add-on is enabled; merged
into the cluster global pull secret.

| Host | Purpose |
| --- | --- |
| `registry.redhat.io` | `redhat-operators` catalog operands — OpenShift Data Foundation, OpenShift Virtualization, GitOps. |
| `icr.io`, `cp.icr.io` | IBM Fusion Data Foundation catalog index (`cpopen/isf-data-foundation-catalog`) and its entitled operand images. |

!!! note "Building the Bootwright container image"
    Building the image itself (not provisioning) additionally reaches
    `docker.io` (`library/golang`, `redhat/ubi9` base images),
    `galaxy.ansible.com` (bundled Ansible collections), `pypi.org` (Python /
    `ansible-core`), and `proxy.golang.org` (Go modules). Allowlist these only on
    the build host, not on the provisioned estate.

## Environment proxy

A proxy entry lives under `Environment.spec.infraComponents.proxies[]`.
`management: external` describes a proxy Bootwright only consumes; you supply its
URLs inline. Mark one entry `default: true` and **every** proxy consumer routes
through it — no `proxyFor` block is needed:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: lab
spec:
  domains:
    base: bootwright.test
  infraComponents:
    proxies:
      - name: default
        default: true
        management: external
        connection:
          httpProxy: http://proxy.example.test:3128
          httpsProxy: http://proxy.example.test:3128
          noProxy:
            - .example.test
            - 192.168.133.0/24
          auth:
            proxyAuthRef: proxy-credentials
```

## The three proxy targets and `proxyFor`

There are three proxy consumers. Each routes through the `default: true` proxy
unless `spec.proxyFor` says otherwise. `spec.proxyFor` is an **override** map with
one slot per consumer; each slot is either a `proxies[]` name (send this consumer
through that proxy), the reserved value `none` (opt this consumer out), or empty
(inherit the default). So the common case — one proxy for everything — needs no
`proxyFor` at all; you reach for it only to exempt or redirect a consumer:

```yaml
spec:
  proxyFor:
    machineOSInstall: none   # everything except the OS install uses the default
    containerClusterInstall: lab   # this one consumer uses a different proxy
```

| Target | Routes the proxy through to… |
| --- | --- |
| `proxyFor.bootwright` | Bootwright's own runtime actions (package managers, downloads, host-side tooling). |
| `proxyFor.containerClusterInstall` | The OpenShift/OKD installer input — rendered into `install-config.yaml`. |
| `proxyFor.machineOSInstall` | The managed-OS (Anaconda) install fetch — the `rhsm`/`url`/`repo` Kickstart directives. |

With no proxy marked `default: true`, an empty slot means *no proxy for that
consumer* — you then name a proxy in each slot that should be proxied, exactly
as an override.

A managed-OS boot ISO carries no packages, so the node reaches its install tree
or the Red Hat CDN over the network during install; on a proxied estate that
traffic goes through `proxyFor.machineOSInstall`. See
[Managed OS installs](managed-os.md) for the install-media side.

For a fully air-gapped estate that has a RHEL DVD but no package mirror or
reachable CDN, the install profile's
`installer.anaconda.packageSource.hostedTree` is the self-contained option:
Bootwright extracts the DVD once into the artifact server and each node installs
GPG-signed packages from that local tree over the
`hostedTree.artifactServerEndpoint` HTTP endpoint — no external mirror, CDN, or
proxy at install time. The node's package fetch is local, so it is **not**
routed through `proxyFor.machineOSInstall`. See
[hostedTree](managed-os.md#package-source-mirror-fromsubscription-or-hostedtree) for
the media and endpoint wiring.

!!! warning "machineOSInstall must be an external proxy"
    The managed OS is installed *before* any Bootwright-managed proxy could
    exist, so `machineOSInstall` only accepts an `external` proxy entry (one
    carrying `connection`). A managed selection — named directly, or inherited
    from a managed `default: true` proxy — is **rejected at validation**; set
    `proxyFor.machineOSInstall` to an external proxy or `none`. When the proxy
    entry carries `auth.proxyAuthRef`, the credentials are baked into the
    `--proxy=` Kickstart directives at install time and the per-machine install
    ISO is tightened to `0600`.

    Each install fetch honours `noProxy` independently: Bootwright applies
    `--proxy=` to the `rhsm`, `url`, and `repo` Kickstart directives only when
    that directive's host is not bypassed. An internal Satellite, install tree, or
    mirror listed in `noProxy` is fetched directly even while the CDN goes through
    the proxy — Anaconda has no `no_proxy` directive of its own, so Bootwright
    makes the per-target decision at render time.

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

!!! warning "A managed `proxyFor.bootwright` yields no proxy at all"
    A managed selection for `proxyFor.bootwright` yields **no** proxy
    environment for Bootwright's own controller-side actions — not at bootstrap
    and not after convergence, because the resolver keys off the *declared*
    management mode, never the component's state. Today the only affected
    surface is `bastion setup`. Point `proxyFor.bootwright` at an external proxy
    (or `none`) if the controller must egress through one. `machineOSInstall`
    never runs after convergence, so a managed selection there is rejected at
    validation rather than silently skipped (see above).

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
not the `http_proxy` environment — so on a managed Ceph node the machines-phase
registration task (run after the OS is in place, before the Ceph deps-phase
package work) converges the effective proxy (host, port, credentials, and
`noProxy`) into rhsm.conf's `[server]` section before registering, and
`subscription-manager` stamps it into each RHEL repo. Without this the RHSM
plugin marks those repos `proxy = _none_` and dnf reaches the Red Hat CDN
directly, which fails on a proxied estate. When the entitlement sets
`rhsm.management: external`, no registration task is planned and Bootwright
writes no proxy into rhsm.conf at all — the delegated registration playbook
owns that file.

`python-rhsm`'s `no_proxy` matcher understands hostnames and domain suffixes but
**not** CIDR entries, so a plain `10.0.0.0/8` in `noProxy` would leave an internal
host in that range proxied. The registration task handles this two ways. It feeds rhsm.conf a
CIDR-stripped `no_proxy` (domains plus concrete IP literals — the internal hosts a
CIDR covers are already pinned as literals from the estate's known addresses). And
because that pinning cannot resolve a Satellite named only by FQDN, when the
entitlement names a Satellite the node also resolves it (`getent`) and, if its
address falls inside a `noProxy` CIDR, drops the proxy from rhsm.conf entirely so
the internal Satellite is reached directly. To bypass an internal Satellite, list
its domain or hostname in `noProxy` — a CIDR alone is handled too, but a
domain/host entry is the most direct.

### Container image pulls (cephadm / podman)

cephadm runs every Ceph daemon and OSD as a **podman** container and pulls the
Ceph image on each storage host. The play-level proxy environment only reaches
the **seed's** Ansible-invoked `cephadm bootstrap`; the cephadm manager pulls the
image onto the other hosts by SSHing to each and running `podman` as root
non-interactively, which does not inherit that environment. So Bootwright writes
a root-owned `/etc/containers/containers.conf.d/10-bootwright-ceph-proxy.conf`
(`[engine] env` with `HTTP(S)_PROXY`/`NO_PROXY`) on every storage host — mode
`0600` when the proxy URL carries credentials, else `0644`, and removed when no
proxy is configured. Without it, a proxied estate's storage nodes dial the image
registry (e.g. `quay.io`) directly and the OSD image pull times out, leaving the
cluster with zero OSDs. Keep the image registry **out** of `noProxy` so the pull
uses the proxy, while the internal cluster/public CIDRs stay in it.

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

This requires an artifact server endpoint binding: the cluster's
`install.agent.bootArtifacts.artifactServerEndpoint.endpointRef` must resolve
to an endpoint on a managed artifact server. The environment may default only
the `serverRef`; the disconnected cluster declares the endpoint it consumes:

```yaml
spec:
  install:
    mode: disconnected
    agent:
      bootArtifacts:
        artifactServerEndpoint:
          endpointRef: cluster
```

!!! note "minimal ISO and bootArtifactsBaseURL are disconnected-only"
    Bootwright renders `minimalISO: true` and an endpoint-derived
    `bootArtifactsBaseURL` into `agent-config.yaml` only for a cluster whose
    `install.mode` is `disconnected` **and** whose effective
    `install.agent.bootArtifacts.artifactServerEndpoint` resolves an
    artifact-server endpoint. A connected cluster never gets these keys, even if
    it has an artifact endpoint bound for other reasons (for example Redfish
    virtual media).

## RHSM and Satellite redirect

A managed-OS `fromSubscription` install registers against the public Red Hat CDN unless
the referenced entitlement's `rhsm` arm carries a `satellite` block, in which
case the install registers and pulls content from that Red Hat Satellite instead
— no `MachineImage` change is needed; on a managed Ceph cluster the same block
redirects the machines-phase registration task, which trusts the Satellite CA
and binds katello-ca-consumer before registering. This is how a disconnected or
proxied estate keeps RHEL package fetches inside the perimeter. The
entitlement's `rhsm.management` field (`managed`, the default, or `external`)
picks who registers: `external` delegates registration to an operator
`CustomPlaybook` and is rejected for `fromSubscription` installs, whose
install-time registration *is* the package source. See
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

These pins reach the cluster by two routes, both before anything pulls them.
Every `mgr` section key is seeded into the ceph.conf handed to `cephadm bootstrap
--config`, so the monitoring stack that bootstrap deploys **in-process** already
resolves them; and the whole `mgr/cephadm/container_image_*` family is re-applied
with a guarded `ceph config get`/`set` immediately after bootstrap returns, ahead
of the ingress specs that consume HAProxy and keepalived. Bootwright does not
pass `--skip-monitoring-stack` to work around this: a cluster that declares no
monitoring roles and no `monitoring` block renders no monitoring specs at all, so
skipping the stack would silently leave it with no monitoring.

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

See [Troubleshooting → SSH or artifact fetch failures](../troubleshooting.md#ssh-or-artifact-fetch-failures),
[Ownership, idempotency & safety](ownership-and-safety.md), and the
`sno-libvirt-redfish-disconnected-services` example, which layers a `machine trust`
step into its disconnected walkthrough.
