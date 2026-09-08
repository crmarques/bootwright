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
	spec := object.Spec()
	if object.Kind() == api.StorageCluster && !spec.Has("ceph", "distribution") {
		// A later object can select either release grammar.
		release := spec.Get("ceph", "release")
		a.lexical(release, "$.spec.ceph.release", exactOSS.MatchString(release.Text()) || ossName.MatchString(release.Text()) || vendorRelease.MatchString(release.Text()))
		a.object = object.WithSpec(spec.With("ceph", spec.Get("ceph").Without("release")))
	}
	a.lexicalValues()
	a.object = object
	if cephProduct(object) {
		if textOf(spec, "type") == "ibm-storage-ceph" {
			a.forbid(spec, "$.spec", "rhsm")
			if spec.Get("license", "accept").Type() == api.Boolean && !spec.Get("license", "accept").Bool() {
				a.invalid("$.spec.license.accept", "IBM storage entitlement requires explicit license acceptance")
			}
		} else if textOf(spec, "rhsm", "management") == "external" {
			a.forbid(spec.Get("rhsm"), "$.spec.rhsm", "organizationRef", "activationKeyRef", "connectToInsights", "satellite")
		}
		return a.issues
	}
	switch object.Kind() {
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
		if textOf(ceph, "distribution") == "oss" {
			a.forbid(ceph, "$.spec.ceph", "ibm", "entitlementRef", "packageVersion")
			a.forbid(ceph.Get("cephadm", "ansible"), "$.spec.ceph.cephadm.ansible", "packageVersion")
		}
		gateway := ceph.Get("mgmtGateway")
		if textOf(gateway, "exposure") == "http" {
			a.forbid(gateway, "$.spec.ceph.mgmtGateway", "tls", "oauth2Proxy")
			if gateway.Get("enableAuth").Bool() {
				a.invalid("$.spec.ceph.mgmtGateway.enableAuth", "HTTP management gateway forbids authentication")
			}
		}
		for i, node := range ceph.Get("topology", "nodes").Items() {
			field := indexed("$.spec.ceph.topology.nodes", i)
			if node.Has("devices") && node.Has("osd") {
				a.invalid(field+".osd", "device shorthand and per-node OSD configuration are mutually exclusive")
			}
			a.partialOSD(node.Get("osd"), field+".osd")
		}
		for i, group := range ceph.Get("topology", "osdDrivegroups").Items() {
			a.partialOSD(group.Get("osd"), indexed("$.spec.ceph.topology.osdDrivegroups", i)+".osd")
		}
	case api.StorageObjectGateway:
		if textOf(spec, "endpoint", "scheme") == "http" {
			a.forbid(spec.Get("endpoint"), "$.spec.endpoint", "tls")
		}
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
	return a.issues
}

func (a *admission) orderedPositive(value api.Value, high, low, field string) {
	if positive(value.Get(high)) && positive(value.Get(low)) && numeric(value.Get(low)).Cmp(numeric(value.Get(high))) > 0 {
		a.invalid(field, "positive minimum must not exceed maximum")
	}
}

func (a *admission) partialOSD(osd api.Value, field string) {
	if osd.Get("tpm2").Bool() && osd.Get("encrypted").Type() == api.Boolean && !osd.Get("encrypted").Bool() {
		a.invalid(field+".tpm2", "TPM2 OSD unlock requires encryption")
	}
	for _, key := range []string{"dataDevices", "dbDevices", "walDevices"} {
		selector := osd.Get(key)
		modes := 0
		for _, mode := range []string{"paths", "pathSpecs", "all"} {
			if selector.Has(mode) {
				modes++
			}
		}
		filters := selector.Has("model") || selector.Has("vendor") || selector.Has("rotational") || selector.Has("size")
		if modes > 1 || (modes > 0 && filters) {
			a.invalid(field+"."+key, "device selection modes cannot be combined")
		}
		if key != "dataDevices" && selector.Get("all").Bool() {
			a.invalid(field+"."+key+".all", "all-device selection is valid only for data devices")
		}
	}
}
