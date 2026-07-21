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

**Per-entitlement rhsm, no indirection:** an Entitlement carries its `rhsm` arm
inline for `redhat-rhel` and `redhat-ceph`; `ibm-storage-ceph` carries no `rhsm`
(only registry and license). `Resolve` populates `Resolved.RHSM` from the
entitlement's own arm via `v1alpha1.EntitlementRHSMByName`, so an
`ibm-storage-ceph` resolve carries no RHSM at all. IBM (and OSS) storage nodes
register RHEL through a separate `redhat-rhel` subscription named by the node's
`MachineInstallProfile.spec.subscription` or the cluster's
`StorageCluster.spec.ceph.osSubscriptionRef`; the render layer
(`storageCephProvider`) resolves that entitlement into `Provider.OSRegistration`
and `cephprovider.Vars` emits its `rhsm`/`rhsmManagement` when the Ceph
entitlement carries none. `Resolved.RHSM.Management` defaults to `managed`;
under `external` no secret paths or Satellite resolve, `Vars` emits
`rhsmManagement` but no `rhsm` map, and the machines-phase registration task
is not planned. Guarded by `TestResolveRHELCarriesOwnRHSMAndIBMCarriesNone`,
`TestResolveCarriesSatellite`, `TestResolveExternalManagementCarriesNoMaterial`,
and `TestSelectExternalRHSMManagementProjectsNoRHSMVars`.

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
