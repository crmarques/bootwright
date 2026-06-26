---
title: Environment API
description: Environment fields, defaults, secrets, services, mirrors, and entitlements.
---

# Environment

`Environment` owns fleet-wide defaults, selected input resources, selected
clusters, secret declarations, proxy and mirror defaults, install trust,
entitlements, and the infra-component service access catalog (external or
managed entries).

Exactly one `Environment` is loaded per context. The standard object envelope
(`apiVersion`, `kind`, `metadata.name`) applies — see
[Object Envelope](index.md#object-envelope). A minimal skeleton:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: lab
spec:
  baseDomain: lab.example.com
```

In the tables below, **Required** marks fields the author must set.
**Required: No** with a stated default means the field is normalize-defaulted
or simply optional: omit it and Bootwright uses the default. A blank Default
cell means there is no default — an omitted optional field stays unset.

## Fields

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.baseDomain` | Yes | — | Fleet DNS base domain rendered into selected container clusters. |
| `spec.resources[]` | No | Discover workspace YAML | YAML files or directories, relative to the Environment file, to load. Omitted loads discovered YAML from the context workspace; when set it must list at least one relative, in-tree path. |
| `spec.safety.destroyProtection` | No | `allow` | `allow` or `requiredOverride`; empty means `allow`. |
| `spec.containerClusters[]` | No | All loaded | Active `ContainerCluster` selection list. When set, loaded container clusters outside the list are excluded. Selection list, not a reference (no `Ref` suffix). |
| `spec.storageClusters[]` | No | All loaded | Active `StorageCluster` selection list. When set, loaded storage clusters outside the list are excluded. Selection list, not a reference (no `Ref` suffix). |
| `spec.defaults.install.pullSecretRef` | No | — | Default pull secret for clusters that omit `install.pullSecretRef`. |
| `spec.defaults.install.nodeSSH` | No | — | Default node SSH material for clusters that omit `install.nodeSSH` (same shape as `ContainerCluster.spec.install.nodeSSH`; see [ContainerCluster](container-cluster.md)). |
| `spec.defaults.artifactAccess` | No | — | Default artifact endpoint binding for active artifact consumers. See [Artifact access](#artifact-access). |
| `spec.defaults.clientsMirror` | No | — | HTTP(S) base URL for mirrored OpenShift client downloads. Validated as an `http(s)` URL when set. |
| `spec.secretStorage.mode` | No | `source` | `source` or `context`; empty means `source`. `context` requires `bootwright secret generate` to copy `file:`-sourced material into the context store before workflows read it. |
| `spec.proxyFor.bootwright` | No | — | Proxy catalog entry used by Bootwright runtime actions; empty or `none` disables. Must name a declared `infraComponents.proxies[]` entry or `none`. |
| `spec.proxyFor.containerClusterInstall` | No | — | Proxy catalog entry rendered into cluster install input; empty or `none` disables. Must name a declared `infraComponents.proxies[]` entry or `none`. |
| `spec.proxyFor.machineOSInstall` | No | — | Proxy the managed-OS (Anaconda) install fetch routes through — a boot-ISO node reaches its install tree or the Red Hat CDN over the network during install. Only an **external** proxy applies (the node installs before any managed proxy exists); empty or `none` disables. Must name a declared `infraComponents.proxies[]` entry or `none`. |
| `spec.infraComponents` | No | — | Catalog of external or managed service access entries. See [Infra-component catalog](#infra-component-catalog). |
| `spec.registries` | No | — | Disconnected mirror and image digest source settings. See [Registries](#registries). |
| `spec.installTrust.caBundleRefs[]` | No | — | Fleet-wide additional CA bundle secret names. |
| `spec.secrets[]` | No | — | Secret declarations by name, never secret bytes. See [Secrets](#secrets). |
| `spec.entitlements[]` | No | — | Vendor entitlement declarations for RHEL, Ceph, OpenShift, or IBM Storage Ceph. See [Entitlements](#entitlements). |
| `spec.componentImages` | No | — | Managed service image pins by component type and implementation. See [Component images](#component-images). |

## Artifact access

`defaults.artifactAccess` and cluster/provider artifact access share this shape.
All fields are optional name references; the names are validated at the
declaration site even when no cluster currently consumes them.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `serverRef` | No | — | Names an `Environment.spec.infraComponents.artifactServers[].name`. |
| `providerRef` | No | — | Provider-scoped artifact server selector where supported. |
| `redfishVirtualMedia.endpointRef` | No | — | Endpoint used by BMCs fetching virtual media. |
| `machineBoot.endpointRef` | No | — | Endpoint used by machine boot flows. |
| `containerClusterInstall.endpointRef` | No | — | Endpoint used for disconnected or minimal ISO cluster install artifacts. |
| `osInstall.endpointRef` | No | — | Endpoint used by managed machine OS install artifacts. |

## Infra-component catalog

Each catalog entry sets `management: external` or `management: managed`.
Managed entries point at an `InfraComponent` through `componentRef`; external
entries carry connection facts directly. `name` and `management` are required
on every entry. The remaining fields are conditional on `management` — fields
marked managed-only are rejected on external entries and vice versa.

| Field | Required | Description |
| --- | --- | --- |
| `proxies[].name` | Yes | DNS-label entry name (not `none`). |
| `proxies[].management` | Yes | `external` or `managed`. |
| `proxies[].componentRef` | For `managed` | Selects a managed `InfraComponent` with `spec.proxy`. Rejected on external entries. |
| `proxies[].endpointRef` | No | Names an `endpoints[]` entry on the managed component. |
| `proxies[].connection.httpProxy` | No | Bare proxy URL; at least one of `httpProxy`/`httpsProxy`/`noProxy` is required on external entries. |
| `proxies[].connection.httpsProxy` | No | Bare proxy URL. |
| `proxies[].connection.noProxy[]` | No | No-proxy hosts. |
| `proxies[].connection.auth.proxyAuthRef` | No | Secret with proxy credentials; URLs must not embed credentials. |
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
external mirror URL or a managed registry catalog entry.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `registries.mirror.url` | No | — | External mirror URL. |
| `registries.mirror.credentialsRef` | No | — | Secret containing mirror credentials. |
| `registries.mirror.trustBundleRef` | No | — | Secret containing mirror CA trust. |
| `registries.imageDigestSources[].source` | Yes | — | Source image registry. Required on each entry. |
| `registries.imageDigestSources[].mirrors[]` | Yes | — | Mirror registries for that source. Required on each entry. |
| `registries.imageDigestSources[].sourcePolicy` | No | — | `NeverContactSource` or `AllowContactingSource`. |

## Secrets

`spec.secrets[]` is authored as a list. Each item is one of the shapes below.
Entry names must be DNS labels. Scalar and null-valued items declare
context-local material — generated or operator-supplied material written through
the encrypted context secret store. `file` and `generated` are mutually
exclusive; `keyFile` requires `file`.

| Shape | Required parts | Meaning |
| --- | --- | --- |
| `- name` | name | Context-local material (encrypted context store). |
| `- name:` | name | Same as scalar context-local material. |
| `- name: {file: <path>}` | `file` | Operator-owned local source file. |
| `- name: {file: <path>, keyFile: <path>}` | `file` (and `keyFile`) | TLS or paired material with a key file; `keyFile` requires `file`. |
| `- name: {generated: {credentials: ...}}` | `generated.credentials` | Generated username/password-style credentials. |
| `- name: {generated: {selfSignedCertificate: ...}}` | `generated.selfSignedCertificate` | Generated cert/key pair. |
| `- name: {generated: {sshKeyPair: ...}}` | `generated.sshKeyPair` | Generated SSH key pair. |

The object form requires `file`, `keyFile`, or `generated`; use a scalar item
(or an omitted/null value) for context-local material. A `generated:` block
must set exactly one of `credentials`, `selfSignedCertificate`, or `sshKeyPair`.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `generated.credentials.username` | No | — | Generated credential username (no whitespace, colon, or newlines). |
| `generated.selfSignedCertificate.commonName` | Yes | — | Certificate common name. Required when `selfSignedCertificate` is used. |
| `generated.selfSignedCertificate.dnsNames[]` | No | — | DNS SANs. |
| `generated.selfSignedCertificate.ipAddresses[]` | No | — | IP SANs. |
| `generated.selfSignedCertificate.validityDays` | No | — | Validity period; must not be negative. |
| `generated.sshKeyPair.type` | No | `ed25519` | Key type; currently only `ed25519`. |
| `generated.sshKeyPair.comment` | No | — | Public key comment (no leading/trailing whitespace or newlines). |

!!! note
    Desired state references secrets by name only — never bytes. Generated
    material is created during apply; operator-owned `file:` material stays
    outside versioned state. See [Secrets](../advanced/secrets.md).

## Entitlements

Each entitlement declares named vendor-controlled access for one
provider/product pair. `name`, `provider`, and `product` are always required;
the `rhsm`, `registry`, `license`, and `rhelEntitlementRef` arms become required
per pair (see [Required arms](#required-arms)).

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `entitlements[].name` | Yes | — | Local entitlement name referenced by storage or OS install inputs. |
| `entitlements[].provider` | Yes | — | `community`, `redhat`, or `ibm`. |
| `entitlements[].product` | Yes | — | `ceph`, `openshift`, `rhel`, or `ibm-storage-ceph`, depending on provider. |
| `entitlements[].rhsm.organizationRef` | Conditional | — | Secret for the Red Hat organization ID. Required wherever `rhsm` is required. |
| `entitlements[].rhsm.activationKeyRef` | Conditional | — | Secret for the Red Hat activation key. Required wherever `rhsm` is required. |
| `entitlements[].rhsm.connectToInsights` | No | `false` | Whether managed RHEL installs connect to Insights. |
| `entitlements[].registry.url` | No | — | Vendor registry URL; must not embed credentials (use `credentialsRef`). |
| `entitlements[].registry.credentialsRef` | Conditional | — | Registry entitlement credentials. Required for `redhat/ceph` and `ibm/ibm-storage-ceph`. |
| `entitlements[].registry.trustBundleRef` | No | — | Registry trust bundle. |
| `entitlements[].license.accept` | Conditional | `false` | Must be `true` for `ibm/ibm-storage-ceph`. |
| `entitlements[].rhelEntitlementRef` | Conditional | — | Names a `redhat/rhel` entitlement supplying the RHEL subscription. Required for `ibm/ibm-storage-ceph`; rejected on every other pair (which carry `rhsm` inline). |

### Provider and product pairs

Only the following pairs are accepted; any other combination is rejected.

| Provider | Products |
| --- | --- |
| `community` | `ceph`, `openshift` |
| `redhat` | `ceph`, `rhel`, `openshift` |
| `ibm` | `ibm-storage-ceph` |

### Required arms

The required `rhsm`/`registry`/`license`/`rhelEntitlementRef` arms follow from
the provider/product pair rather than a discriminator field:

| Provider / product | Required arms |
| --- | --- |
| `community/ceph` | none |
| `community/openshift` | none |
| `redhat/openshift` | none |
| `redhat/rhel` | `rhsm` (`organizationRef` + `activationKeyRef`) |
| `redhat/ceph` | `rhsm` + `registry.credentialsRef` |
| `ibm/ibm-storage-ceph` | `registry.credentialsRef` + `license.accept: true` + `rhelEntitlementRef` (no inline `rhsm`) |

IBM Storage Ceph ships its own image registry (`cp.icr.io`) and product license
but runs on RHEL it does not itself entitle, so its RHEL subscription is a
separate `redhat/rhel` entitlement named via `rhelEntitlementRef` — an inline
`rhsm` arm on an `ibm/ibm-storage-ceph` entitlement is rejected. (`redhat/ceph`
stays bundled: a single Red Hat subscription entitles both RHEL and the `rhceph`
tools repo, so its own `rhsm` arm covers both.)

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
- `spec.proxyFor.bootwright`, `spec.proxyFor.containerClusterInstall`, and
  `spec.proxyFor.machineOSInstall` must each name a declared
  `spec.infraComponents.proxies[]` entry, or be empty or the literal `none`.
- `spec.containerClusters[]` / `spec.storageClusters[]` entries must be unique
  and match a loaded `ContainerCluster` / `StorageCluster`.
- `spec.resources[]`, when set, must list at least one non-empty path that is
  relative to the Environment file and stays within its directory.
- At most one `spec.infraComponents.registries[]` entry may set `default: true`.
- Each `spec.registries.imageDigestSources[]` entry requires `source` and at
  least one `mirrors[]` value.
- Proxy URLs and `entitlements[].registry.url` must not embed inline
  credentials.
