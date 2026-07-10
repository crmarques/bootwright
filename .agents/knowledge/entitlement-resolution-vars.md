# Entitlement resolution and the day-2 cephadm vars contract

**Resolve invariants:** `entitlements.Resolve`
(`internal/entitlements/entitlements.go`) preserves two invariants. Secret
material stays behind secret names — everything (RHSM org/activation key,
Satellite trust bundle, registry credentials/trust bundle) resolves to on-disk
paths via `secret.Index`/`secret.ResolveMaterialPath`, never inlined. And
provider/product are derived from the Entitlement's `spec.type` via
`v1alpha1.EntitlementTypeProviderProduct`, so the day-2 cephadm render is
byte-identical to the pre-first-class-Entitlement schema (the old
provider+product fields). The `EntitlementProviderRedHat`/`IBM` and
`EntitlementProductCeph`/`RHEL`/`IBMStorageCeph` constants remain only as
emitted values, never authored input. When an entitlement declares no registry
URL, the distribution's default registry fills in (`registry.redhat.io` or
`cp.icr.io/cp`).

**Inline vs deferred rhsm:** an Entitlement carries its `rhsm` arm inline
(types `redhat-rhel` and `redhat-ceph`) or, for `ibm-storage-ceph`, defers it
to the `redhat-rhel` entitlement named by `spec.rhelEntitlementRef`. `Resolve`
populates `Resolved.RHSM` identically in both cases, so downstream rendering
(`cephprovider.Vars`) does not distinguish the two — the IBM entitlement
contributes only registry and license arms while its RHSM (including any
Satellite redirect) is inherited through the reference. Guarded by
`TestResolveFollowsRHELEntitlementRef`, `TestResolveCarriesSatellite`, and
`TestSelectIBMProviderProjectsLicenseAndRegistry`.

**Resolved Satellite form:** `RHSMSatellite` is the resolved form of a
corporate Red Hat Satellite redirect on an entitlement's `rhsm` arm. An empty
`Hostname` means registration uses the public Red Hat CDN and no `satellite`
key is projected into vars. `TrustBundlePath` is the Satellite CA materialized
on the controller, basenamed by the install marker so the path stays stable
across runs. In `cephprovider.Vars` it projects as
`rhsm.satellite.{hostname, contentBaseURL, caPath}`. Guarded by
`TestResolveCarriesSatellite` and `TestSelectRedHatProviderProjectsSatellite`.

**Validation split:** `validate_entitlements` checks that
`rhsm.satellite.hostname` is a bare hostname and `contentBaseURL` (when set)
an http(s) URL; normalize derives the canonical Satellite 6 content path
`https://<hostname>/pulp/content` when unset. The satellite `trustBundleRef`
CA secret is enforced as a PREFLIGHT secret requirement (mirroring
`registry.trustBundleRef`), deliberately not in the desired-state validator.
