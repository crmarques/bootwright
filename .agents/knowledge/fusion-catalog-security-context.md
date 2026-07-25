# IBM Fusion catalog pod security

**Symptom:** `CatalogSource/isf-data-foundation-catalog` moves from `Unknown`
to `TRANSIENT_FAILURE`, Bootwright remains at the catalog readiness gate, and
the IBM Fusion Data Foundation operator does not appear in the OpenShift
Software Catalog.

**Root cause:** A shipped gRPC catalog creates a registry pod before OLM can
resolve the add-on Subscription. IBM's Fusion Data Foundation catalog manifest
requires `spec.grpcPodConfig.securityContextConfig: restricted`. Omitting the
pod security mode can leave the registry pod incompatible with the namespace's
pod-security enforcement, so the CatalogSource never becomes `READY`.

**Contract:** `ClusterAddon.spec.olm.catalogSource.grpcPodConfig` is a typed
CatalogSource pod override. Its `securityContextConfig` is required when the
block is present and accepts only the OLM values `legacy` or `restricted`.
The native `fusion-data-foundation` 4.21 add-on pins `restricted`.

Bootwright correctly stops before applying the Namespace, OperatorGroup, and
Subscription while the CatalogSource is not ready. That ordering explains why
no Fusion tile or installed operator exists during this failure; bypassing the
gate would only turn it into an OLM resolution race.

Native add-ons already copied into the machine-local store or a context input
snapshot do not change with the binary. Reinstall the native add-on, then
refresh the context input snapshot, before retrying apply when adopting this
content fix in an existing context.
