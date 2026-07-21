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
  baseDomain: lab.example.com
```

In the tables below, **Required** marks fields the author must set.
**Required: No** with a stated default means the field is normalize-defaulted or
simply optional: omit it and Bootwright uses the default. A blank Default cell
means there is no default — an omitted optional field stays unset.

## Fields

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.baseDomain` | Yes | — | Fleet DNS base domain rendered into selected container clusters. Also seeds each machine's implicit `dnsEntry` address and the composed node FQDNs; see [Machines](machines.md#the-dnsentry-address). |
| `spec.resources[]` | No | Discover workspace YAML | YAML files or directories, relative to the Environment file, to load. Omitted loads discovered YAML from the context workspace; when set it must list at least one relative, in-tree path. |
| `spec.safety.destroyProtection` | No | `allow` | `allow` or `requiredOverride`; empty means `allow`. |
| `spec.safety.protectedKinds[]` | No | — | Per-kind destructive-change protection. Each entry is one of `ContainerCluster`, `StorageCluster`, or `Machine`; any other value is rejected. A run that would destructively rebuild an object of a listed kind (`apply --converge-drifted`, `--reclaim-devices`) or tear one down (`destroy`) fails closed instead. |
| `spec.containerClusters[]` | No | All loaded | Active `ContainerCluster` selection list. When set, loaded container clusters outside the list are excluded. Selection list, not a reference (no `Ref` suffix). |
| `spec.storageClusters[]` | No | All loaded | Active `StorageCluster` selection list. When set, loaded storage clusters outside the list are excluded. Selection list, not a reference (no `Ref` suffix). |
| `spec.defaults.install.pullSecretRef` | No | — | Default pull secret for clusters that omit `install.pullSecretRef`. |
| `spec.defaults.install.nodeSSH` | No | — | Default node SSH material for clusters that omit `install.nodeSSH` (same shape as `ContainerCluster.spec.install.nodeSSH`; see [Container clusters](container-clusters.md)). |
| `spec.defaults.artifactServerRef` | No | — | Default artifact server name for consumer-owned `artifactServerEndpoint.serverRef` fields. |
| `spec.defaults.clientsMirror` | No | — | HTTP(S) base URL for mirrored OpenShift client downloads. Validated as an `http(s)` URL when set. |
| `spec.defaults.virtctlMirror` | No | — | HTTP(S) base URL for a mirrored, version-matched `virtctl`. Empty means fetch from each KubeVirt host cluster's OpenShift Virtualization ConsoleCLIDownload; set it for disconnected labs. Validated as an `http(s)` URL when set. |
| `spec.secretStorage.mode` | No | `source` | `source` or `context`; empty means `source`. `context` requires `bootwright secret generate` to copy `file:`-sourced material into the context store before workflows read it. |
| `spec.proxyFor.bootwright` | No | inherit default | Proxy used by Bootwright runtime actions. A `proxies[]` name overrides; `none` opts out; empty inherits the `default: true` proxy. |
| `spec.proxyFor.containerClusterInstall` | No | inherit default | Proxy rendered into cluster install input. A `proxies[]` name overrides; `none` opts out; empty inherits the `default: true` proxy. |
| `spec.proxyFor.machineOSInstall` | No | inherit default | Proxy the managed-OS (Anaconda) install fetch routes through — a boot-ISO node reaches its install tree or the Red Hat CDN over the network during install. Only an **external** proxy applies (the node installs before any managed proxy exists), so a managed value or a managed inherited default is rejected. A `proxies[]` name overrides; `none` opts out; empty inherits the `default: true` proxy. |
| `spec.infraComponents` | No | — | Catalog of external or managed service access entries. See [Infra-component catalog](#infra-component-catalog). |
| `spec.registries` | No | — | Disconnected mirror and image digest source settings. See [Registries](#registries). |
| `spec.installTrust.caBundleRefs[]` | No | — | Fleet-wide additional CA bundle secret names. |
| `spec.componentImages` | No | — | Managed service image pins by component type and implementation. See [Component images](#component-images). |

## Artifact Server Default

`defaults.artifactServerRef` names a catalog entry in
`spec.infraComponents.artifactServers[]`. It defaults only the server selector:
consumers still declare their own `artifactServerEndpoint.endpointRef`, because
endpoint purpose belongs to the consumer.

```yaml
spec:
  defaults:
    artifactServerRef: default
```

Consumers may override the server by setting
`artifactServerEndpoint.serverRef`; otherwise Bootwright applies this default
when resolving the endpoint.

## Infra-component catalog

`spec.infraComponents` is the fleet's catalog of shared-service access entries,
grouped by service kind (`proxies`, `nameResolution`, `artifactServers`,
`registries`, `ntp`). Each entry sets `management: external` or
`management: managed`. Managed entries point at an
[`InfraComponent`](infrastructure.md) through `componentRef`; external entries
carry connection facts directly. `name` and `management` are required on every
entry. The remaining fields are conditional on `management` — fields marked
managed-only are rejected on external entries and vice versa.

| Field | Required | Description |
| --- | --- | --- |
| `proxies[].name` | Yes | DNS-label entry name (not `none`). |
| `proxies[].default` | No | Marks the proxy every consumer inherits when its `proxyFor` slot is empty. At most one `proxies[]` entry may set it. |
| `proxies[].management` | Yes | `external` or `managed`. |
| `proxies[].componentRef` | For `managed` | Selects a managed `InfraComponent` with `spec.proxy`. Rejected on external entries. |
| `proxies[].endpointRef` | No | Names an `endpoints[]` entry on the managed component. |
| `proxies[].connection.httpProxy` | No | Bare proxy URL; at least one of `httpProxy`/`httpsProxy`/`noProxy` is required on external entries. |
| `proxies[].connection.httpsProxy` | No | Bare proxy URL. |
| `proxies[].connection.noProxy[]` | No | No-proxy hosts. |
| `proxies[].connection.auth.proxyAuthRef` | No | Secret with proxy credentials; URLs must not embed credentials. |
| `proxies[].connection.trustBundleRef` | No | Secret (PEM) with the CA a TLS-inspecting proxy re-signs HTTPS with; installed into the trust store of managed hosts that egress through the proxy. See [Disconnected & proxied installs](../advanced/disconnected-proxy.md#tls-inspecting-proxies). |
| `nameResolution[].name` | Yes | DNS-label entry name (not `none`). |
| `nameResolution[].management` | Yes | `external` or `managed`. |
| `nameResolution[].componentRef` | For `managed` | Selects a managed `InfraComponent` with `spec.nameResolution`. |
| `nameResolution[].endpointRef` | No | Names an `endpoints[]` entry on the managed component. |
| `nameResolution[].address` | For `external` | Resolver IP address (external entries only). |
| `nameResolution[].additionalIngressHosts[]` | No | Extra ingress hostnames. |
| `artifactServers[].name` | Yes | DNS-label entry name (not `none`). |
| `artifactServers[].management` | Yes | `external` or `managed`. |
| `artifactServers[].componentRef` | For `managed` | Selects a managed `InfraComponent` with `spec.artifactServer`. |
| `artifactServers[].endpoints[].name` | For `external` | Endpoint name; `endpoints` is required on external entries, rejected on managed. |
| `artifactServers[].endpoints[].url` | For `external` | Endpoint `http(s)` URL. |
| `registries[].name` | Yes | DNS-label entry name (not `none`). |
| `registries[].default` | No | Marks the default registry; at most one entry may set it. |
| `registries[].management` | Yes | `external` or `managed`. |
| `registries[].componentRef` | For `managed` | Selects a managed `InfraComponent` with `spec.registry`. |
| `registries[].endpointRef` | No | Names an `endpoints[]` entry on the managed component. |
| `registries[].url` | For `external` | Registry URL (external entries only). |
| `ntp[].name` | Yes | DNS-label entry name (not `none`). |
| `ntp[].management` | Yes | `external` or `managed`. |
| `ntp[].componentRef` | For `managed` | Selects a managed `InfraComponent` with `spec.ntp`. |
| `ntp[].endpointRef` | No | Names an `endpoints[]` entry on the managed component. |
| `ntp[].address` | For `external` | NTP server IP or DNS hostname (external entries only). |

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
| `<component>.<impl>.local` | At least one of `local`/`public` | — | Local (mirrored) image reference; version tag or digest. |
| `<component>.<impl>.public` | At least one of `local`/`public` | — | Public image reference; version tag or digest. |

## Validation

Beyond the per-field rules above, the validator enforces:

- **Exactly one `Environment`** is required in the loaded state.
- `spec.baseDomain` is required.
- `spec.defaults.clientsMirror` must be an `http(s)` URL when set.
- `spec.defaults.virtctlMirror` must be an `http(s)` URL when set.
- `spec.proxyFor.bootwright`, `spec.proxyFor.containerClusterInstall`, and
  `spec.proxyFor.machineOSInstall` must each name a declared
  `spec.infraComponents.proxies[]` entry, or be empty or the literal `none`.
  Empty inherits the `default: true` proxy; `none` opts the consumer out.
- At most one `spec.infraComponents.proxies[]` entry may set `default: true`.
- `spec.proxyFor.machineOSInstall` must not resolve — named directly or by
  inheriting a managed default — to a `managed` proxy (the node installs before
  any managed proxy exists); use an external proxy or `none`.
- `spec.containerClusters[]` / `spec.storageClusters[]` entries must be unique
  and match a loaded `ContainerCluster` / `StorageCluster`.
- `spec.resources[]`, when set, must list at least one non-empty path that is
  relative to the Environment file and stays within its directory.
- At most one `spec.infraComponents.registries[]` entry may set `default: true`.
- Each `spec.registries.imageDigestSources[]` entry requires `source` and at
  least one `mirrors[]` value.
- Proxy URLs and `Entitlement.spec.registry.url` must not embed inline
  credentials.

See [The desired-state model](index.md) for the conventions every field table
shares, [Secrets & entitlements](secrets.md) for the `kind: Secret` object,
and [Disconnected & proxied installs](../advanced/disconnected-proxy.md) for the
proxy and mirror how-to.
