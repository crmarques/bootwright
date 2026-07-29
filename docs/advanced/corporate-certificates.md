---
title: Corporate TLS — cluster URL certificates & trusted CAs
description: Replace the default API and ingress serving certificates with corporate-issued ones, and add corporate CAs to the cluster install trust.
---

# Corporate TLS — cluster URL certificates & trusted CAs

A corporate estate usually has its own PKI, and that shows up in two distinct
places on a cluster. Bootwright models them as two independent features — set
either, both, or neither:

- **Serving certificates** — the certificates clients *see* when they hit the
  cluster URLs, `api.<cluster>.<domains.containerClusters>` and the `*.apps.<cluster>.<domains.containerClusters>`
  wildcard. Replace the installer's default self-signed certificates with
  corporate-issued ones so browsers and `oc` trust the endpoints without a
  warning.
- **Trusted CAs** — the certificate authorities the cluster itself must *trust*
  on egress: a TLS-inspecting proxy, an internal registry or service, or the CA
  that signed the serving certificates above.

These are independent: declaring a serving certificate does **not** trust its
issuer, and trusting a CA does **not** change what the cluster serves. The
committed reference that wires both is
[`examples/sno-libvirt-redfish-corporate-tls`](https://github.com/crmarques/bootwright/tree/main/examples/sno-libvirt-redfish-corporate-tls)
— the smallest connected lab, with corporate URL certificates and a trusted CA.

For the field tables see
[Container clusters](../concepts/container-clusters.md#serving-certificates); for
how the secret bytes are declared and stored see [Secrets](../concepts/secrets.md).

| Concern | What it controls | Where you declare it |
| --- | --- | --- |
| Serving certificates | What clients see at `api.…` and `*.apps.…` | `ContainerCluster.spec.install.servingCertificates` |
| Cluster-scoped trust | Extra CAs one cluster trusts | `ContainerCluster.spec.install.additionalTrustBundleRefs[]` |
| Fleet-wide trust | Extra CAs every cluster trusts | `Environment.spec.installTrust.caBundleRefs[]` |
| Mirror trust | The mirror registry's CA | `Environment.spec.registries.mirror.trustBundleRef` (see [Disconnected & proxied installs](disconnected-proxy.md)) |
| Proxy trust | The TLS-inspecting proxy's signing CA, auto-folded into every cluster install | `Environment.spec.infraComponents.proxies[].connection.trustBundleRef` (see [Disconnected & proxied installs](disconnected-proxy.md#tls-inspecting-proxies)) |

## Serving certificates for the cluster URLs

`ContainerCluster.spec.install.servingCertificates` overrides the default
cluster serving certificates. Both arms are optional; each validates its
contents once present.

```yaml
spec:
  install:
    servingCertificates:
      apiServer:
        namedCertificates:
          - names:
              - api.sno-libvirt.bootwright.test
            secretRef: api-serving-tls
      ingress:
        defaultCertificateRef: ingress-serving-tls
```

- **`apiServer.namedCertificates[]`** maps a serving certificate to the external
  API hostnames it covers. Bootwright renders a day-2 `APIServer` cluster config
  (`spec.servingCerts.namedCertificates`) plus the TLS secret in
  `openshift-config`.
- **`ingress.defaultCertificateRef`** sets the default ingress certificate.
  Bootwright renders a day-2 `IngressController` (`spec.defaultCertificate`) plus
  the TLS secret in `openshift-ingress`.

Two rules the validator enforces:

- **Never name the internal endpoint.** `namedCertificates[].names` must not
  include `api-int.<cluster>.<domains.containerClusters>`; the internal API endpoint keeps its
  installer-generated certificate. List only the external `api.<cluster>.<domains.containerClusters>`
  (and any extra external API aliases).
- **The ingress certificate must cover the wildcard.** The default ingress
  certificate has to be valid for `*.apps.<cluster>.<domains.containerClusters>` so it covers
  the console, oauth, and every other route host. Bootwright checks coverage
  against the console hostname when it resolves real secret material.

## Trusted CAs

Corporate CAs are merged into the install-config `additionalTrustBundle` (with
`additionalTrustBundlePolicy: Always`) from four sources, deduplicated by name:

```yaml
# Per cluster — extra CAs this cluster trusts:
spec:
  install:
    additionalTrustBundleRefs:
      - corporate-ca
```

```yaml
# Fleet-wide — extra CAs every cluster in the environment trusts:
spec:
  installTrust:
    caBundleRefs:
      - corporate-ca
```

The two remaining sources are declared elsewhere and folded into the same bundle
automatically: the mirror registry's own CA
(`Environment.spec.registries.mirror.trustBundleRef`; see
[Disconnected & proxied installs](disconnected-proxy.md#trust-bundles)) and the
signing CA of the TLS-inspecting proxy the cluster install egresses through
(`proxies[].connection.trustBundleRef`; see
[TLS-inspecting proxies](disconnected-proxy.md#tls-inspecting-proxies)). You do
**not** need to repeat either in `additionalTrustBundleRefs[]`.
Use the cluster-scoped list for CAs only one cluster needs, and the fleet-wide
list for a corporate root every cluster shares.

### Where CA trust is declared

Seven places name a CA bundle. Only the first four fold into a cluster's
install trust:

| Field | Trusts | Folded into |
| --- | --- | --- |
| [`ContainerCluster.spec.install.additionalTrustBundleRefs[]`](../concepts/container-clusters.md#install) | Whatever this one cluster must trust | `additionalTrustBundle` |
| [`Environment.spec.installTrust.caBundleRefs[]`](../concepts/environment.md#fields) | A corporate root every cluster shares | `additionalTrustBundle` |
| [`Environment.spec.infraComponents.proxies[].connection.trustBundleRef`](../concepts/environment.md#infra-component-catalog) | The TLS-inspecting proxy's re-signing CA | `additionalTrustBundle`, plus the trust store of managed hosts that egress through it |
| [`Environment.spec.registries.mirror.trustBundleRef`](../concepts/environment.md#registries) | The mirror registry | `additionalTrustBundle` |
| [`MachineImage.spec.trustRefs[]`](../concepts/machines.md#machineimage) | The host serving the install ISO | Nothing — used only for that download |
| [`Entitlement.spec.rhsm.satellite.trustBundleRef`](../concepts/secrets.md#corporate-satellite) | A corporate Red Hat Satellite | Nothing — trusted on the node before registration |
| [`Entitlement.spec.registry.trustBundleRef`](../concepts/secrets.md#types) | An entitled content registry | Nothing — trusted where that registry is pulled |

## Declaring a cert is not trusting its issuer

This is the common trap. If you replace the cluster URL certificates with
corporate-issued ones, clients still need to trust the issuing CA, and so do
in-cluster components that talk to those URLs. Serving certificates and trust are
separate fields, so add the issuing CA explicitly:

```yaml
spec:
  install:
    additionalTrustBundleRefs:
      - corporate-ca          # trust the issuer
    servingCertificates:      # serve the issued certificates
      apiServer:
        namedCertificates:
          - names:
              - api.sno-libvirt.bootwright.test
            secretRef: api-serving-tls
      ingress:
        defaultCertificateRef: ingress-serving-tls
```

## Supplying the bytes

Each `secretRef` / `caBundleRef` names a `kind: Secret` object; the operator
supplies the bytes through the local context, never checked in. Serving
certificates are a `tlsCertificate` secret (certificate-and-key pair); a CA
bundle is a `caBundle` secret (raw PEM):

```text
bootwright secret set --name corporate-ca       --raw-file ./corp-ca.pem
bootwright secret set --name api-serving-tls     --tls-cert ./api.crt     --tls-key ./api.key
bootwright secret set --name ingress-serving-tls --tls-cert ./ingress.crt --tls-key ./ingress.key
```

For a self-contained lab the example declares these as `generated` secrets
instead (self-signed `tlsCertificate` serving certs and a `caBundle` trust
anchor), so `bootwright secret generate` materializes them with no external PKI.
See [Secrets](../concepts/secrets.md) for the full secret workflow and the
generated-material shapes.
