package storage

import (
	"math/big"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func replicatedDefaults(cluster api.Object) (*big.Int, *big.Int) {
	if cluster.Spec().Has("ceph", "topology", "stretch") {
		return big.NewInt(4), big.NewInt(2)
	}
	if cluster.Spec().Get("ceph", "cephadm", "bootstrap", "singleHostDefaults").Bool() {
		return big.NewInt(2), big.NewInt(1)
	}
	return big.NewInt(3), big.NewInt(2)
}
func effectiveReplicas(value api.Value, size, minimum *big.Int) (*big.Int, *big.Int) {
	if positive(value.Get("size")) {
		size = numeric(value.Get("size"))
	}
	if positive(value.Get("minSize")) {
		minimum = numeric(value.Get("minSize"))
	}
	return size, minimum
}
func osdHostCount(cluster api.Object) int64 {
	var count int64
	for _, node := range cluster.Spec().Get("ceph", "topology", "nodes").Items() {
		if hasRole(node, "osd") {
			count++
		}
	}
	return count
}
func (a *admission) replication(cluster, policy api.Object) {
	spec := a.object.Spec()
	size, minimum := replicatedDefaults(cluster)
	if policy.Kind() != "" {
		size, minimum = effectiveReplicas(policy.Spec().Get("replicated"), size, minimum)
	}
	size, minimum = effectiveReplicas(spec.Get("replicated"), size, minimum)
	if minimum.Cmp(size) > 0 {
		a.invalid("$.spec.replicated", "effective minimum replicas must not exceed effective replica size")
	}
	if cluster.Spec().Has("ceph", "topology", "stretch") && (size.Cmp(big.NewInt(4)) != 0 || minimum.Cmp(big.NewInt(2)) != 0) {
		a.invalid("$.spec.replicated", "stretch fixes replicated protection at size four and minimum two")
	}
	domain := textOf(spec, "failureDomain")
	if domain == "" {
		domain = textOf(policy.Spec(), "failureDomain")
	}
	if domain == "host" && cluster.Spec().Get("ceph", "topology", "nodes").Len() > 0 && size.Cmp(big.NewInt(osdHostCount(cluster))) > 0 {
		a.invalid("$.spec.replicated.size", "host-domain replicas exceed declared OSD-host capacity")
	}
}
func (a *admission) pool(cluster api.Object) {
	spec := a.object.Spec()
	policy := api.Object{}
	policyReady := true
	if name := textOf(spec, "placementPolicyRef"); name != "" {
		policy, policyReady = a.sameOwner(api.StoragePlacementPolicy, name, "$.spec.placementPolicyRef", cluster)
		if positive(spec.Get("replicated", "size")) || positive(spec.Get("replicated", "minSize")) {
			a.invalid("$.spec.replicated", "placement policy owns non-zero replicated protection values")
		}
	}
	protection := fallback(spec.Get("type"), "replicated")
	if protection == "replicated" {
		a.forbid(spec, "$.spec", "erasure")
		if policyReady {
			a.replication(cluster, policy)
		}
	}
	if protection == "erasure" {
		a.required(spec, "erasure", "$.spec")
		if positive(spec.Get("replicated", "size")) || positive(spec.Get("replicated", "minSize")) {
			a.invalid("$.spec.replicated", "erasure protection forbids non-zero replicated values")
		}
		if cluster.Spec().Has("ceph", "topology", "stretch") {
			a.invalid("$.spec.type", "stretch does not support erasure pools")
		}
		if role := textOf(spec, "role"); role != "" && !slices.Contains([]string{"rbd", "cephfs-data", "rgw"}, role) {
			a.invalid("$.spec.role", "pool role does not permit erasure protection")
		}
		data, coding := spec.Get("erasure", "dataChunks"), spec.Get("erasure", "codingChunks")
		if cluster.Spec().Get("ceph", "topology", "nodes").Len() > 0 && positive(data) && positive(coding) && new(big.Int).Add(numeric(data), numeric(coding)).Cmp(big.NewInt(osdHostCount(cluster))) > 0 {
			a.invalid("$.spec.erasure", "erasure data and coding chunks exceed declared OSD-host capacity")
		}
		for _, key := range []string{"dataChunks", "codingChunks", "plugin", "technique", "crushDeviceClass", "crushRoot", "stripeUnit", "failureDomain", "k", "m", "crush-device-class", "crush-root", "crush-failure-domain", "stripe_unit"} {
			if spec.Get("erasure", "parameters").Has(key) {
				a.invalid("$.spec.erasure.parameters", "erasure parameters cannot repeat typed or derived fields")
			}
		}
	}
	if textOf(spec, "role") == "cephfs-metadata" && protection != "replicated" {
		a.invalid("$.spec.type", "CephFS metadata pools require replicated protection")
	}
	if spec.Has("mirroring") && textOf(spec, "role") != "rbd" {
		a.invalid("$.spec.mirroring", "pool mirroring requires the rbd role")
	}
	if floatPositive(spec.Get("autoscale", "targetSizeRatio")) && spec.Has("autoscale", "targetSizeBytes") {
		a.invalid("$.spec.autoscale", "autoscale ratio and byte targets are mutually exclusive")
	}
	if spec.Has("autoscale", "pgNumMin") && spec.Has("autoscale", "pgNumMax") && numeric(spec.Get("autoscale", "pgNumMin")).Cmp(numeric(spec.Get("autoscale", "pgNumMax"))) > 0 {
		a.invalid("$.spec.autoscale", "minimum PG count must not exceed maximum PG count")
	}
	compression := spec.Get("compression")
	if compression.Len() > 0 && !compression.Has("mode") {
		a.required(compression, "mode", "$.spec.compression")
	}
	if positive(compression.Get("minBlobSize")) && positive(compression.Get("maxBlobSize")) && numeric(compression.Get("minBlobSize")).Cmp(numeric(compression.Get("maxBlobSize"))) > 0 {
		a.invalid("$.spec.compression", "positive minimum blob size must not exceed maximum blob size")
	}
}

func poolRefName(value api.Value) string {
	if value.Type() == api.String {
		return value.Text()
	}
	return textOf(value, "name")
}
func (a *admission) poolRole(name, field, role string, cluster api.Object) {
	if pool, ok := a.sameOwner(api.StoragePool, name, field, cluster); ok && textOf(pool.Spec(), "role") != role {
		a.invalid(field, "referenced pool does not have the consumer's required role")
	}
}
func (a *admission) filesystem(cluster api.Object) {
	spec := a.object.Spec()
	a.poolRole(textOf(spec, "metadataPoolRef"), "$.spec.metadataPoolRef", "cephfs-metadata", cluster)
	names, defaults := []string{}, 0
	for i, value := range spec.Get("dataPoolRefs").Items() {
		name := poolRefName(value)
		names = append(names, name)
		a.poolRole(name, indexed("$.spec.dataPoolRefs", i), "cephfs-data", cluster)
		if value.Get("default").Bool() {
			defaults++
		}
	}
	a.uniqueValues(names, "$.spec.dataPoolRefs", "data pool references must be unique")
	if len(names) > 1 && defaults != 1 {
		a.invalid("$.spec.dataPoolRefs", "multiple data pools require exactly one explicit default")
	}
	if spec.Has("mds") {
		a.placement(cluster, spec.Get("mds", "placement"), "$.spec.mds.placement", "mds", false)
	}
	for i, group := range spec.Get("subvolumeGroups").Items() {
		if name := textOf(group, "poolLayoutRef"); name != "" {
			a.sameOwner(api.StoragePool, name, indexed("$.spec.subvolumeGroups", i)+".poolLayoutRef", cluster)
		}
	}
}
func (a *admission) export(cluster api.Object, managed bool) {
	spec := a.object.Spec()
	if managed {
		a.required(spec, "dataFoundation", "$.spec")
		a.forbid(spec, "$.spec", "externalDetails")
		refs := spec.Get("dataFoundation")
		a.poolRole(textOf(refs, "rbdPoolRef"), "$.spec.dataFoundation.rbdPoolRef", "rbd", cluster)
		a.sameOwner(api.StorageFilesystem, textOf(refs, "filesystemRef"), "$.spec.dataFoundation.filesystemRef", cluster)
		if refs.Has("objectGatewayRef") {
			a.sameOwner(api.StorageObjectGateway, textOf(refs, "objectGatewayRef"), "$.spec.dataFoundation.objectGatewayRef", cluster)
		}
	} else {
		a.required(spec, "type", "$.spec")
		a.required(spec, "externalDetails", "$.spec")
		a.forbid(spec, "$.spec", "dataFoundation")
	}
}
