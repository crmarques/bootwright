package cephcluster

import api "github.com/crmarques/bootwright/api/v1alpha1"

// ValidatePartial checks contradictions provable from a defaults fragment alone.
// Missing fields and unresolved ownership are intentionally left to admission of
// the effective object after the fragment has been composed with authored input.
func ValidatePartial(object api.Object, catalog api.Catalog) []api.Issue {
	if !isStorage(object.Kind()) && !cephProduct(object) {
		return nil
	}
	a := admission{object: object, catalog: catalog}
	a.lexicalValues()
	a.object = object
	a.partial = true
	a.intrinsic()
	return a.issues
}

func (a *admission) orderedPositive(value api.Value, high, low, field string) {
	if positive(value.Get(high)) && positive(value.Get(low)) && numeric(value.Get(low)).Cmp(numeric(value.Get(high))) > 0 {
		a.invalid(field, "positive minimum must not exceed maximum", "lower "+low+" or raise "+high+" in "+field+" so "+low+" does not exceed "+high)
	}
}

func (a *admission) intrinsic() {
	spec := a.object.Spec()
	if cephProduct(a.object) {
		if textOf(spec, "type") == "ibmStorageCeph" {
			if (!a.partial || spec.Get("license", "accept").Type() == api.Boolean) && !spec.Get("license", "accept").Bool() {
				a.invalid("$.spec.license.accept", "IBM storage entitlement requires explicit license acceptance", "set $.spec.license.accept to true on this ibmStorageCeph Entitlement after accepting its license")
			}
		} else if textOf(spec, "rhsm", "management") == "external" {
			a.forbid(spec.Get("rhsm"), "$.spec.rhsm", "organizationRef", "activationKeyRef", "connectToInsights", "satellite")
		}
		return
	}
	switch a.object.Kind() {
	case api.CephPlacementPolicy, api.CephPool:
		a.orderedPositive(spec.Get("replicated"), "size", "minSize", "$.spec.replicated")
		if spec.Has("placementPolicyRef") && (positive(spec.Get("replicated", "size")) || positive(spec.Get("replicated", "minSize"))) {
			a.invalid("$.spec.replicated", "placement policy owns non-zero replicated protection values", "remove non-zero spec.replicated values or remove spec.placementPolicyRef to author protection here")
		}
		if spec.Has("erasure") && (positive(spec.Get("replicated", "size")) || positive(spec.Get("replicated", "minSize"))) {
			a.invalid("$.spec.replicated", "erasure protection forbids non-zero replicated values", "remove spec.replicated when spec.erasure is declared")
		}
		if floatPositive(spec.Get("autoscale", "targetSizeRatio")) && spec.Has("autoscale", "targetSizeBytes") {
			a.invalid("$.spec.autoscale", "autoscale ratio and byte targets are mutually exclusive", "keep either spec.autoscale.targetSizeRatio or spec.autoscale.targetSizeBytes, removing the other")
		}
		pg := spec.Get("autoscale")
		if pg.Get("pgNumMin").Type() == api.Integer && pg.Get("pgNumMax").Type() == api.Integer && numeric(pg.Get("pgNumMin")).Cmp(numeric(pg.Get("pgNumMax"))) > 0 {
			a.invalid("$.spec.autoscale", "minimum PG count must not exceed maximum PG count", "lower spec.autoscale.pgNumMin or raise spec.autoscale.pgNumMax so the minimum does not exceed the maximum")
		}
		a.orderedPositive(spec.Get("compression"), "maxBlobSize", "minBlobSize", "$.spec.compression")
	case api.CephCluster:
		ceph := spec
		if textOf(spec, "management") == "external" {
			a.forbid(spec, "$.spec", api.CephManagedFields()...)
			break
		}
		if api.CephDistribution(ceph) == "oss" || (!a.partial && !ceph.Has("distribution")) {
			a.forbid(ceph, "$.spec", "entitlementRef", "packageVersion")
			a.forbid(ceph.Get("cephadm", "ansible"), "$.spec.cephadm.ansible", "packageVersion")
		}
		a.retiredCluster(ceph)
		a.nvmeofClusterIntrinsic(ceph)
		if a.partial {
			a.nativeServices(ceph, true)
		}

	case api.CephRBDImage:
		a.rbdImageIntrinsic()
	case api.CephFilesystem:
		a.subvolumeGroups()
	case api.CephObjectGateway:
		a.gatewayPoolRoles(spec.Get("pools"))
	case api.CephNFSExport:
		a.nfsExportIntrinsic()
	case api.CephClient:
		a.clientIntrinsic()
	case api.CephSMBResource:
		a.smbIntrinsic()
	case api.CephNVMeoFSubsystem:
		a.nvmeofSubsystemIntrinsic()
	case api.CephConsumer:
		if spec.Has("dataFoundation") && spec.Has("externalDetails") {
			a.invalid("$.spec", "storage export ownership modes are mutually exclusive", "keep either spec.dataFoundation or spec.externalDetails, removing the other")
		}
	}
}
