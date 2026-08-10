---
title: Environment & fleet defaults
description: Environment fields, defaults, secrets, services, mirrors, and entitlements.
---

# Environment & fleet defaults

`Environment` owns fleet-wide concerns: default install material, the active
resource and cluster selection lists, secret declarations, proxy and mirror
defaults, install trust, the infra-component service access
catalog (external or managed entries), and managed-service image pins. It is the
top of the ownership tree — every other kind inherits its fleet defaults and
references its secret and service catalogs.

Exactly one `Environment` is loaded per context. It uses the shared object
envelope and the **Required** / **Default** column convention documented on
[The desired-state model](index.md#object-envelope). A minimal skeleton:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: lab
spec:
  domains:
    base: lab.example.com
```

## Fields

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.domains` | Yes | — | Per-class DNS zones (`base` required, the others default from it). Seeds each machine's implicit `fqdn` address, the composed cluster node FQDNs, and each container cluster's `install-config.yaml` `baseDomain`; see [Machines](machines.md#the-fqdn-address) and [Domain model](#domain-model). |
| `spec.sites[]` | No | — | The estate's site registry — every site it spans. Each entry has a required DNS-label `name` (rendered as a CRUSH bucket name) and an optional `description`. Required as soon as anything names a site, and every reference must match one; see [Sites](#sites). |
| `spec.machineAccess.keyRef` | No | — | Names the `sshKeyPair` `Secret` whose public half every machine Bootwright installs authorizes for its `bootwright` service account, and whose private half Bootwright connects with. Required as soon as any `Machine` sets `os.installProfileRef`. See [Machine access](#machine-access). |
| `spec.resources[]` | No | Discover workspace YAML | YAML files or directories, relative to the Environment file, to load. Omitted loads discovered YAML from the context workspace; when set it must list at least one relative, in-tree path. |
| `spec.safety.destroyProtection` | No | `allow` | `allow` or `protected`; empty means `allow`. `protected` state needs `destroy --authorize protected`. |
| `spec.safety.protectedKinds[]` | No | — | Per-kind destructive-change protection. Each entry is one of `ContainerCluster`, `StorageCluster`, or `Machine`; any other value is rejected. `apply --mode rebuild` on an object of a listed kind fails closed with no token that overrides it — the remedy is an explicit `destroy` first. `--reclaim-devices` alone fails closed and names `--authorize data-loss` as the authorization. `destroy` requires `--authorize protected`. |
| `spec.containerClusters[]` | No | All loaded | Active `ContainerCluster` selection list. When set, loaded container clusters outside the list are excluded. Selection list, not a reference (no `Ref` suffix). |
| `spec.storageClusters[]` | No | All loaded | Active `StorageCluster` selection list. When set, loaded storage clusters outside the list are excluded. Selection list, not a reference (no `Ref` suffix). |
| `spec.defaults.install.pullSecretRef` | No | — | Default pull secret for clusters that omit `install.pullSecretRef`. |
| `spec.defaults.install.nodeSSH` | No | — | Default node SSH material for clusters that omit `install.nodeSSH` (same shape as `ContainerCluster.spec.install.nodeSSH`; see [Container clusters](container-clusters.md)). |
| `spec.defaults.clientsMirror` | No | — | HTTP(S) base URL for mirrored OpenShift client downloads. Validated as an `http(s)` URL when set. |
| `spec.defaults.virtctlMirror` | No | — | HTTP(S) base URL for a mirrored, version-matched `virtctl`. Empty means fetch from each KubeVirt host cluster's OpenShift Virtualization ConsoleCLIDownload; set it for disconnected labs. Validated as an `http(s)` URL when set. |
| `spec.defaults.helmMirror` | No | — | HTTP(S) base URL for mirrored `helm` downloads. Bootwright appends the `latest` channel, so the mirror must expose `<base>/latest/helm-linux-amd64` and `<base>/latest/sha256sum.txt`. Validated as an `http(s)` URL when set. |
| `spec.secretStorage.mode` | No | `source` | `source` or `context`; empty means `source`. `context` requires `bootwright secret generate` to copy `file:`-sourced material into the context store before workflows read it. |
| `spec.proxyFor.bootwright` | No | inherit default | Proxy used by Bootwright runtime actions. A `proxies[]` name overrides; `none` opts out; empty inherits the default proxy. |
| `spec.proxyFor.containerClusterInstall` | No | inherit default | Proxy rendered into cluster install input. A `proxies[]` name overrides; `none` opts out; empty inherits the default proxy. |
| `spec.proxyFor.machineOSInstall` | No | inherit default | Proxy the managed-OS (Anaconda) install fetch routes through — a boot-ISO node reaches its install tree or the Red Hat CDN over the network during install. A `proxies[]` name overrides; `none` opts out; empty inherits the default proxy. Only an external proxy resolves here (see [Validation](#validation)). |
| `spec.infraComponents` | No | — | Catalog of external or managed service access entries. See [Infra-component catalog](#infra-component-catalog). |
| `spec.registries` | No | — | Disconnected mirror and image digest source settings. See [Registries](#registries). |
| `spec.installTrust.caBundleRefs[]` | No | — | Fleet-wide additional CA bundle secret names. |
| `spec.componentImages` | No | — | Managed service image pins by component type and implementation. See [Component images](#component-images). |

## Domain model

`spec.domains` ([ADR 0018](https://github.com/crmarques/bootwright/blob/main/specs/adr/0018-environment-domain-model.md))
names a DNS zone per identity class, so machines can live in a corporate zone
while the clusters Bootwright builds live in a separate cloud zone (and
container and storage clusters in distinct subzones):

```yaml
spec:
  domains:
    base: example.net              # required; the default for the others
    machines: corp.example.net     # machine fqdn zone
    clusters: cloud.example.net    # cluster zone umbrella
    containerClusters: ocp.cloud.example.net
    storageClusters: ceph.cloud.example.net
```

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.domains.base` | Yes | — | Fleet base zone; the fallback every other key defaults to. |
| `spec.domains.machines` | No | `domains.base` | Zone for machine `fqdn` names. |
| `spec.domains.clusters` | No | `domains.base` | Umbrella zone the container/storage cluster keys default from. |
| `spec.domains.containerClusters` | No | `domains.clusters` | Zone for container clusters: node FQDNs and each cluster's `install-config.yaml` `baseDomain`. |
| `spec.domains.storageClusters` | No | `domains.clusters` | Zone for storage clusters: Ceph node FQDNs and the `mgr.<cluster>.<zone>` dashboard alias `cluster info` prints. |

Defaulting chain: `machines` → `base`; `clusters` → `base`;
`containerClusters` → `clusters`; `storageClusters` → `clusters`. An
`Environment` that sets only `base` resolves every identity under that one zone.

Every container cluster in one Environment renders the same install-config
`baseDomain`; a fleet spanning two base domains needs one context per domain
today, and shared InfraComponents must then be duplicated.

Composition under the model:

- **Machine** — `fqdn` defaults to `<machine name>.<domains.machines>`.
- **Container cluster** — the cluster zone is
  `<cluster name>.<domains.containerClusters>`; a node resolves to
  `<nodes[].name>.<cluster name>.<domains.containerClusters>`.
- **Storage cluster** — the cluster zone is
  `<cluster name>.<domains.storageClusters>`; a node resolves to
  `<nodes[].name>.<cluster name>.<domains.storageClusters>`.

`nodes[].name` is a bare DNS label — a dotted value is rejected. To pin a host
outside the zone, author `nodes[].fqdn`, which is used verbatim.

`spec.domains` (DNS zones) is distinct from the `spec.containerClusters[]` /
`spec.storageClusters[]` selection lists (cluster membership) above.

## Sites

`spec.sites[]` declares every site — datacenter, room, availability zone —
the estate spans. It is the vocabulary every other site reference is checked
against:

```yaml
spec:
  sites:
    - name: dc1
    - name: dc2
    - name: dc3
      description: arbiter-only site holding the stretch tiebreaker
```

A machine then says where it stands with
[`spec.placement.site`](machines.md#placement), and a Ceph topology node takes
its site from the machine it binds — you do not repeat it.

The registry is optional: an estate that never names a site declares nothing
and nothing changes. It becomes **required** the moment anything names one — a
machine's `placement.site`, a storage node's `site`, `stretch.dataSites`, or a
`placement.sites` filter. Every reference must then match a declared name.
That is the point: a mistyped `dc9` fails when you load the input, instead of
becoming a third CRUSH bucket that quietly breaks stretch mode.

A site name is a DNS label because it is rendered verbatim as a CRUSH bucket
name. Declaring a site no machine stands in is allowed and reported as an INFO
advisory by `bootwright validate` — a fallback arbiter site is often declared
before its candidate machine exists.

## Machine access

Every machine Bootwright installs carries the same login: a `bootwright` service
account with passwordless `sudo` and no root SSH. `spec.machineAccess.keyRef` is
the one place the fleet names the key that opens it.

```yaml
spec:
  machineAccess:
    keyRef: bootwright-machine-key
```

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: bootwright-machine-key
spec:
  type: sshKeyPair
  source:
    generated:
      keyType: ed25519
```

It must name an `sshKeyPair` `Secret`, and it is required as soon as any
`Machine` sets `os.installProfileRef`. `bootwright secret generate` mints the
material before the first apply, and `bootwright preflight` proves it exists
before an apply touches a machine.

Because this key opens every machine in the fleet, it may **not** also be named
as a `StorageCluster`'s `spec.ceph.cephadm.clusterSSH.keyRef`: `cephadm
bootstrap --ssh-private-key` copies the cluster identity into the Ceph mon
config-key store, where that cluster's manager can read it. Declare a second
generated `sshKeyPair` for the cluster.

Machines you already own are unaffected — they carry their own
[`spec.access`](machines.md#access).

## Infra-component catalog

`spec.infraComponents` is the fleet's catalog of shared-service access entries,
grouped by service kind (`proxies`, `nameResolution`, `artifactServers`,
`registries`, `ntp`). It is the substitution seam: a consumer binds to a service
by catalog name without knowing whether Bootwright runs it or the site provides
it. Each entry sets `management: external` or `management: managed`. Managed
entries point at an [`InfraComponent`](infrastructure.md) through `componentRef`;
external entries carry connection facts directly. `name` and `management` are
required on every entry. The remaining fields are conditional on `management`:
`componentRef` is required on managed entries and rejected on external ones,
while an external entry instead requires its connection facts — `connection.*`
for proxies, `address` for name resolution and NTP, `endpoints[]` for artifact
servers, and `url` for registries — which are rejected on managed entries.

The name-resolution catalog may contain several managed entries, but the
loaded consumers may resolve to only one distinct managed service in a context.
Unused entries do not count, and compatible aliases of the same
`InfraComponent` remain one service. This keeps the controller's one global
resolver route under one lifecycle owner.

Load balancers are deliberately absent from the catalog: one is bound per
cluster endpoint, not fleet-wide, so a `ContainerCluster` endpoint points at a
managed component directly through `source.componentRef` plus a
`bindAddressRef`, and an operator-run load balancer is declared at the consuming
endpoint with `source.type: external` and an `address`.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `proxies[].name` | Yes | — | DNS-label entry name (not `none`). |
| `proxies[].default` | No | `false` | Marks the proxy every consumer inherits when its `proxyFor` slot is empty; see [Validation](#validation). |
| `proxies[].management` | Yes | — | `external` or `managed`. |
| `proxies[].componentRef` | No | — | Selects a managed `InfraComponent` with `spec.proxy`. Managed entries only — rejected on external entries. |
| `proxies[].endpointRef` | No | — | Names an `endpoints[]` entry on the managed component. |
| `proxies[].connection.httpProxy` | No | — | Bare proxy URL; at least one of `httpProxy`/`httpsProxy`/`noProxy` is required on external entries. |
| `proxies[].connection.httpsProxy` | No | — | Bare proxy URL. |
| `proxies[].connection.noProxy[]` | No | — | No-proxy hosts. |
| `proxies[].connection.auth.proxyAuthRef` | No | — | Secret with proxy credentials; URLs must not embed credentials. |
| `proxies[].connection.trustBundleRef` | No | — | Secret (PEM) with the CA a TLS-inspecting proxy re-signs HTTPS with; installed into the trust store of managed hosts that egress through the proxy. See [Disconnected & proxied installs](../advanced/disconnected-proxy.md#tls-inspecting-proxies). |
| `nameResolution[].name` | Yes | — | DNS-label entry name (not `none`). |
| `nameResolution[].management` | Yes | — | `external` or `managed`. |
| `nameResolution[].componentRef` | No | — | Selects a managed `InfraComponent` with `spec.nameResolution`. Managed entries only; all consumed managed entries in one context must resolve to the same component. |
| `nameResolution[].endpointRef` | No | — | Names an `endpoints[]` entry on the managed component. |
| `nameResolution[].address` | No | — | Resolver IP address. External entries only, and required there. |
| `nameResolution[].additionalIngressHosts[]` | No | — | Extra ingress hostnames. |
| `artifactServers[].name` | Yes | — | DNS-label entry name (not `none`). |
| `artifactServers[].default` | No | `false` | Marks the artifact server consumers inherit when `artifactServerEndpoint.serverRef` is empty; see [Artifact Server Default](#artifact-server-default). |
| `artifactServers[].management` | Yes | — | `external` or `managed`. |
| `artifactServers[].componentRef` | No | — | Selects a managed `InfraComponent` with `spec.artifactServer`. Managed entries only. |
| `artifactServers[].endpoints[].name` | Yes (per entry) | — | Endpoint name. `endpoints[]` is required on external entries, rejected on managed. |
| `artifactServers[].endpoints[].url` | Yes (per entry) | — | Endpoint `http(s)` URL. |
| `registries[].name` | Yes | — | DNS-label entry name (not `none`). |
| `registries[].default` | No | `false` | Marks the default registry; see [Validation](#validation). |
| `registries[].management` | Yes | — | `external` or `managed`. |
| `registries[].componentRef` | No | — | Selects a managed `InfraComponent` with `spec.registry`. Managed entries only. |
| `registries[].endpointRef` | No | — | Names an `endpoints[]` entry on the managed component. |
| `registries[].url` | No | — | Registry URL. External entries only, and required there. |
| `ntp[].name` | Yes | — | DNS-label entry name (not `none`). |
| `ntp[].management` | Yes | — | `external` or `managed`. |
| `ntp[].componentRef` | No | — | Selects a managed `InfraComponent` with `spec.ntp`. Managed entries only. |
| `ntp[].endpointRef` | No | — | Names an `endpoints[]` entry on the managed component. |
| `ntp[].address` | No | — | NTP server IP or DNS hostname. External entries only, and required there. |

## Artifact Server Default

The `artifactServers[].default` flag defaults only the *server* selector:
consumers still declare their own `artifactServerEndpoint.endpointRef`, because
endpoint purpose belongs to the consumer.

```yaml
spec:
  infraComponents:
    artifactServers:
      - name: default
        default: true
        management: managed
        componentRef: artifact-server
```

## Registries

`spec.registries` configures disconnected mirroring. It is optional, but a
disconnected `ContainerCluster` install requires mirror trust plus either an
external mirror URL or a managed registry catalog entry. See
[Disconnected & proxied installs](../advanced/disconnected-proxy.md) for the
end-to-end mirror workflow.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `registries.mirror.url` | No | — | External mirror URL. |
| `registries.mirror.credentialsRef` | No | — | Secret containing mirror credentials. |
| `registries.mirror.trustBundleRef` | No | — | Secret containing mirror CA trust. |
| `registries.imageDigestSources[].source` | Yes | — | Source image registry. Required on each entry. |
| `registries.imageDigestSources[].mirrors[]` | Yes | — | Mirror registries for that source. Required on each entry. |
| `registries.imageDigestSources[].sourcePolicy` | No | — | `NeverContactSource` or `AllowContactingSource`. |

## Secrets

Secrets are **not** an `Environment` field. Each secret is its own first-class
[`kind: Secret`](secrets.md#the-secret-object) object with a `spec.type` (what
the material is) and an optional `spec.source` (how it is obtained), and every
`...Ref` in the fleet resolves to one by `metadata.name`. The only secret-related
`Environment` field is `spec.secretStorage.mode` (see the [Fields](#fields)
table), which governs whether `file`-sourced material is read in place (`source`,
the default) or copied into the encrypted context store (`context`).

!!! note "Names only — never bytes"
    Desired state references secrets by name only. Generated material is created
    during apply; operator-owned `source.file` material stays outside versioned
    state. The full `type`/`source` model and the storage modes live on
    [Secrets & entitlements](secrets.md).

## Entitlements

Entitlements are not an `Environment` field. An entitlement is its own
first-class `Entitlement` kind — one object per file, shared fleet-wide, and in a
tree layout it lives under `infra/entitlements/<name>.yaml`. The canonical field
reference lives on [Secrets & entitlements](secrets.md#entitlements): the
`spec.type` vocabulary (`redhat-rhel`, `redhat-ceph`, `ibm-storage-ceph`), the
required `rhsm`/`registry`/`license` arms per type, the
`rhsm.management: managed`/`external` axis, and the
[Corporate Satellite](secrets.md#corporate-satellite) redirect.

An `Entitlement` is referenced by name from
`StorageCluster.spec.ceph.entitlementRef`,
`StorageCluster.spec.ceph.osSubscriptionRef`,
`MachineInstallProfile.spec.subscription.entitlementRef`, and
`MachineInstallProfile.spec.installer.anaconda.packageSource.fromSubscription.entitlementRef`;
the secrets it names are declared as first-class `Secret` objects.

## Component images

`componentImages` pins managed-service images. It is a closed map keyed by
component type, then implementation. Accepted keys are:

| Path | Required | Description |
| --- | --- | --- |
| `componentImages.loadBalancer.haproxy` | No | HAProxy image pin. |
| `componentImages.registry.mirror-registry` | No | Mirror registry image pin. |
| `componentImages.proxy.squid` | No | Squid proxy image pin. |
| `componentImages.nameResolution.dnsmasq` | No | dnsmasq image pin. |
| `componentImages.artifactServer.http` | No | Artifact server image pin. |

Each image entry must set at least one of `local` or `public`, and every image
reference must use an explicit version tag or digest. The mutable `:latest` tag
is rejected.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `<component>.<impl>.local` | No | — | Local (mirrored) image reference; version tag or digest. |
| `<component>.<impl>.public` | No | — | Public image reference; version tag or digest. |

## Validation

Beyond the per-field rules above, the validator enforces:

- **Exactly one `Environment`** is required in the loaded state.
- `spec.domains.base` is required.
- `spec.defaults.clientsMirror` must be an `http(s)` URL when set.
- `spec.defaults.virtctlMirror` must be an `http(s)` URL when set.
- `spec.defaults.helmMirror` must be an `http(s)` URL when set.
- `spec.proxyFor.bootwright`, `spec.proxyFor.containerClusterInstall`, and
  `spec.proxyFor.machineOSInstall` must each name a declared
  `spec.infraComponents.proxies[]` entry, or be empty or the literal `none`.
  Empty inherits the default proxy; `none` opts the consumer out.
- At most one `spec.infraComponents.proxies[]` entry may set `default: true`; a
  fleet with exactly one proxy uses it as the default without the flag.
- At most one `spec.infraComponents.artifactServers[]` entry may set
  `default: true`; a fleet with exactly one artifact server uses it as the
  default without the flag. A consumer whose `artifactServerEndpoint.serverRef`
  is empty resolves to this default, and fails validation when none exists.
- `spec.proxyFor.machineOSInstall` must not resolve — named directly or by
  inheriting a managed default — to a `managed` proxy (the node installs before
  any managed proxy exists); use an external proxy or `none`.
- Loaded `NetworkConfig.spec.nameResolutionRefs[]` consumers may resolve to at
  most one distinct managed name-resolution service. Unused catalog entries and
  compatible aliases of that one service do not violate the limit.
- `spec.containerClusters[]` / `spec.storageClusters[]` entries must be unique
  and match a loaded `ContainerCluster` / `StorageCluster`.
- `spec.resources[]`, when set, must list at least one non-empty path that is
  relative to the Environment file and stays within its directory.
- A native add-on that `context init` or `context update` generated under
  `add-ons/_store/<name>` remains loadable without adding that generated path
  to `spec.resources[]`; its provenance marker limits this exception to the
  single matching `ClusterAddon` descriptor. Authored or malformed lookalikes
  stay excluded.
- At most one `spec.infraComponents.registries[]` entry may set `default: true`;
  a fleet with exactly one registry uses it as the default without the flag.
- Each `spec.registries.imageDigestSources[]` entry requires `source` and at
  least one `mirrors[]` value.
- Proxy URLs and `Entitlement.spec.registry.url` must not embed inline
  credentials.

## Native mapping

The fleet-level keys of the driven tools that land on `Environment` rather than
on the consuming cluster kind. See [conventions](index.md) for how to read the
table.

| Native key or flag | Bootwright path | Class | What the divergence buys |
| --- | --- | --- | --- |
| `baseDomain` (install-config) | `spec.domains.containerClusters` | relocated | fleet-level default — every cluster in the context shares one base domain |
| `proxy` (install-config: `httpProxy`/`httpsProxy`/`noProxy`) | `spec.infraComponents.proxies[].connection`, selected by `spec.proxyFor.containerClusterInstall` | relocated | fleet-level default with per-consumer override |
| `imageDigestSources` (install-config) | `spec.registries.imageDigestSources[]` (mirror keys), plus entries derived from the registry-mirror component | relocated | fleet-level default; release-image entries are derived from the mirror |
| `--registry-json` (cephadm bootstrap) | `Entitlement` registry credentials, named by `StorageCluster` `spec.ceph.entitlementRef` | relocated | secret `…Ref` indirection; one entitlement shared fleet-wide |

## Where to go next

- [The desired-state model](index.md) for the conventions every field table
  shares.
- [Secrets & entitlements](secrets.md) for the `kind: Secret` object.
- [Disconnected & proxied installs](../advanced/disconnected-proxy.md) for the
  proxy and mirror how-to.
