package storage

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
		a.invalid(field, "positive minimum must not exceed maximum")
	}
}

func (a *admission) intrinsic() {
	spec := a.object.Spec()
	if cephProduct(a.object) {
		if textOf(spec, "type") == "ibm-storage-ceph" {
			a.forbid(spec, "$.spec", "rhsm")
			if (!a.partial || spec.Get("license", "accept").Type() == api.Boolean) && !spec.Get("license", "accept").Bool() {
				a.invalid("$.spec.license.accept", "IBM storage entitlement requires explicit license acceptance")
			}
		} else if textOf(spec, "rhsm", "management") == "external" {
			a.forbid(spec.Get("rhsm"), "$.spec.rhsm", "organizationRef", "activationKeyRef", "connectToInsights", "satellite")
		}
		return
	}
	switch a.object.Kind() {
	case api.StoragePlacementPolicy, api.StoragePool:
		a.orderedPositive(spec.Get("replicated"), "size", "minSize", "$.spec.replicated")
		if spec.Has("placementPolicyRef") && (positive(spec.Get("replicated", "size")) || positive(spec.Get("replicated", "minSize"))) {
			a.invalid("$.spec.replicated", "placement policy owns non-zero replicated protection values")
		}
		if textOf(spec, "type") == "erasure" && (positive(spec.Get("replicated", "size")) || positive(spec.Get("replicated", "minSize"))) {
			a.invalid("$.spec.replicated", "erasure protection forbids non-zero replicated values")
		}
		if floatPositive(spec.Get("autoscale", "targetSizeRatio")) && spec.Has("autoscale", "targetSizeBytes") {
			a.invalid("$.spec.autoscale", "autoscale ratio and byte targets are mutually exclusive")
		}
		pg := spec.Get("autoscale")
		if pg.Get("pgNumMin").Type() == api.Integer && pg.Get("pgNumMax").Type() == api.Integer && numeric(pg.Get("pgNumMin")).Cmp(numeric(pg.Get("pgNumMax"))) > 0 {
			a.invalid("$.spec.autoscale", "minimum PG count must not exceed maximum PG count")
		}
		a.orderedPositive(spec.Get("compression"), "maxBlobSize", "minBlobSize", "$.spec.compression")
	case api.StorageCluster:
		ceph := spec.Get("ceph")
		if textOf(spec, "management") == "external" {
			a.forbid(spec, "$.spec", "ceph")
		}
		if textOf(ceph, "distribution") == "oss" || (!a.partial && !ceph.Has("distribution")) {
			a.forbid(ceph, "$.spec.ceph", "entitlementRef", "ibm", "packageVersion")
			a.forbid(ceph.Get("cephadm", "ansible"), "$.spec.ceph.cephadm.ansible", "packageVersion")
		}
		a.retiredCluster(ceph)
		if a.partial {
			a.nativeServices(ceph, true)
		}

	case api.StorageRBDImage:
		a.rbdImageIntrinsic()
	case api.StorageFilesystem:
		a.subvolumeGroups()
	case api.StorageObjectGateway:
		a.gatewayPoolRoles(spec.Get("pools"))
	case api.StorageNFSExport:
		if spec.Get("ingresses").Len() > 0 && numeric(spec.Get("port")).Cmp(numeric(api.IntegerValue("2049"))) == 0 {
			a.invalid("$.spec.port", "ingress-fronted NFS cannot use backend port 2049")
		}
		for i, export := range spec.Get("exports").Items() {
			if export.Has("filesystemRef") && export.Has("bucket") {
				a.invalid(indexed("$.spec.exports", i), "an NFS export requires exactly one filesystem or bucket")
			}
		}
	case api.StorageExport:
		if spec.Has("dataFoundation") && spec.Has("externalDetails") {
			a.invalid("$.spec", "storage export ownership modes are mutually exclusive")
		}
	}
}
