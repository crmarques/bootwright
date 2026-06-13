---
title: Environment API
description: Environment fields, defaults, secrets, services, mirrors, and entitlements.
---

# Environment

`Environment` owns fleet-wide defaults, selected input resources, selected
clusters, secret declarations, proxy and mirror defaults, install trust,
entitlements, and the managed service catalog.

## Fields

| Field | Required | Description |
| --- | --- | --- |
| `spec.baseDomain` | Yes | Fleet DNS base domain rendered into selected container clusters. |
| `spec.resources[]` | No | YAML files or directories, relative to the Environment file, to load. Omitted means load discovered YAML from the context workspace. |
| `spec.safety.destroyProtection` | No | `allow` or `requiredOverride`; empty means `allow`. |
| `spec.containerClusters[]` | No | Active `ContainerCluster` selection list. Loaded container clusters outside the list are excluded. |
| `spec.storageClusters[]` | No | Active `StorageCluster` selection list. Loaded storage clusters outside the list are excluded. |
| `spec.defaults.install.pullSecretRef` | No | Default pull secret for clusters that omit `install.pullSecretRef`. |
| `spec.defaults.install.nodeSSH` | No | Default node SSH material for clusters that omit `install.nodeSSH`. |
| `spec.defaults.artifactAccess` | No | Default artifact endpoint binding for active artifact consumers. |
| `spec.defaults.clientsMirror` | No | HTTP(S) base URL for mirrored OpenShift client downloads. |
| `spec.secretStorage.mode` | No | `source` or `context`; empty means `source`. |
| `spec.proxyFor.bootwright` | No | Proxy catalog entry used by Bootwright runtime actions; empty or `none` disables. |
| `spec.proxyFor.containerClusterInstall` | No | Proxy catalog entry rendered into cluster install input; empty or `none` disables. |
| `spec.infraComponents` | No | Catalog of external or managed service access entries. |
| `spec.registries` | No | Disconnected mirror and image digest source settings. |
| `spec.installTrust.caBundleRefs[]` | No | Fleet-wide additional CA bundle secret names. |
| `spec.secrets[]` | No | Secret declarations by name, never secret bytes. |
| `spec.entitlements[]` | No | Vendor entitlement declarations for RHEL, Ceph, OpenShift, or IBM Storage Ceph. |
| `spec.componentImages` | No | Managed service image pins by component type and implementation. |

## Artifact Access

`defaults.artifactAccess` and cluster/provider artifact access share this shape:

| Field | Description |
| --- | --- |
| `serverRef` | Names an `Environment.spec.infraComponents.artifactServers[].name`. |
| `providerRef` | Provider-scoped artifact server selector where supported. |
| `redfishVirtualMedia.endpointRef` | Endpoint used by BMCs fetching virtual media. |
| `machineBoot.endpointRef` | Endpoint used by machine boot flows. |
| `containerClusterInstall.endpointRef` | Endpoint used for disconnected or minimal ISO cluster install artifacts. |
| `osInstall.endpointRef` | Endpoint used by managed machine OS install artifacts. |

## Infra Component Catalog

Each catalog entry uses `management: external` or `management: managed`.
Managed entries point at an `InfraComponent` through `componentRef`; external
entries carry connection facts directly.

| Catalog | Entry fields |
| --- | --- |
| `infraComponents.proxies[]` | `name`, `management`, `componentRef`, `endpointRef`, `connection.httpProxy`, `connection.httpsProxy`, `connection.noProxy[]`, `connection.auth.proxyAuthRef` |
| `infraComponents.nameResolution[]` | `name`, `management`, `componentRef`, `endpointRef`, `address`, `additionalIngressHosts[]` |
| `infraComponents.artifactServers[]` | `name`, `management`, `componentRef`, `endpoints[].name`, `endpoints[].url` |
| `infraComponents.registries[]` | `name`, `default`, `management`, `componentRef`, `endpointRef`, `url` |
| `infraComponents.ntp[]` | `name`, `management`, `componentRef`, `endpointRef`, `address` |

## Registries

| Field | Description |
| --- | --- |
| `registries.mirror.url` | External mirror URL. |
| `registries.mirror.credentialsRef` | Secret containing mirror credentials. |
| `registries.mirror.trustBundleRef` | Secret containing mirror CA trust. |
| `registries.imageDigestSources[].source` | Source image registry. |
| `registries.imageDigestSources[].mirrors[]` | Mirror registries for that source. |
| `registries.imageDigestSources[].sourcePolicy` | `NeverContactSource` or `AllowContactingSource`. |

Disconnected `ContainerCluster` installs require mirror trust plus either an
external mirror URL or a managed registry catalog entry.

## Secrets

`spec.secrets[]` is authored as a list. Each item is one of:

| Shape | Meaning |
| --- | --- |
| `- name` | Context-local material written with `bootwright secret set`. |
| `- name:` | Same as scalar context-local material. |
| `- name: {file: <path>}` | Operator-owned local source file. |
| `- name: {file: <path>, keyFile: <path>}` | TLS or paired material with a key file. |
| `- name: {generated: {credentials: ...}}` | Generated username/password-style credentials. |
| `- name: {generated: {selfSignedCertificate: ...}}` | Generated cert/key pair. |
| `- name: {generated: {sshKeyPair: ...}}` | Generated SSH key pair. |

Generated options:

| Field | Description |
| --- | --- |
| `generated.credentials.username` | Optional generated credential username. |
| `generated.selfSignedCertificate.commonName` | Required certificate common name. |
| `generated.selfSignedCertificate.dnsNames[]` | Optional DNS SANs. |
| `generated.selfSignedCertificate.ipAddresses[]` | Optional IP SANs. |
| `generated.selfSignedCertificate.validityDays` | Optional validity period. |
| `generated.sshKeyPair.type` | Optional key type; currently `ed25519`. |
| `generated.sshKeyPair.comment` | Optional public key comment. |

## Entitlements

| Field | Description |
| --- | --- |
| `entitlements[].name` | Local entitlement name referenced by storage or OS install inputs. |
| `entitlements[].provider` | `community`, `redhat`, or `ibm`. |
| `entitlements[].product` | `ceph`, `openshift`, `rhel`, or `ibm-storage-ceph` depending on provider. |
| `entitlements[].rhsm.organizationRef` | Secret for Red Hat organization ID. |
| `entitlements[].rhsm.activationKeyRef` | Secret for Red Hat activation key. |
| `entitlements[].rhsm.connectToInsights` | Whether managed RHEL installs connect to Insights. |
| `entitlements[].registry.url` | Vendor registry URL. |
| `entitlements[].registry.credentialsRef` | Registry entitlement credentials. |
| `entitlements[].registry.trustBundleRef` | Optional registry trust bundle. |
| `entitlements[].license.accept` | Required `true` for IBM Storage Ceph. |

Accepted provider/product pairs:

| Provider | Products |
| --- | --- |
| `community` | `ceph`, `openshift` |
| `redhat` | `ceph`, `rhel`, `openshift` |
| `ibm` | `ibm-storage-ceph` |

## Component Images

`componentImages` pins managed-service images. Accepted keys are:

| Path | Description |
| --- | --- |
| `componentImages.loadBalancer.haproxy` | HAProxy image pin. |
| `componentImages.registry.mirror-registry` | Mirror registry image pin. |
| `componentImages.proxy.squid` | Squid proxy image pin. |
| `componentImages.nameResolution.dnsmasq` | dnsmasq image pin. |
| `componentImages.artifactServer.http` | Artifact server image pin. |

Each image entry sets at least one of `local` or `public`, and every image
reference must use an explicit version tag or digest.
