package storage

import (
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

var (
	crushRuleName     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	crushClassName    = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	automaticClasses  = []string{"hdd", "ssd", "nvme"}
	ibmErasureMinimum = map[[2]int64]int64{{2, 2}: 5, {4, 2}: 6, {5, 2}: 7, {6, 2}: 8, {8, 3}: 11, {8, 4}: 12}
	jerasureProfiles  = [][2]int64{{4, 2}, {8, 3}, {8, 4}}
	roleApplication   = map[string]string{"rbd": "rbd", "cephfs-metadata": "cephfs", "cephfs-data": "cephfs", "rgw": "rgw"}
)

func TargetSizeBytes(text string) (*big.Int, bool) {
	match := storageSize.FindStringSubmatch(text)
	if match == nil {
		return nil, false
	}
	n, ok := new(big.Int).SetString(match[1], 10)
	if !ok {
		return nil, false
	}
	power := 0
	if match[2] != "" {
		power = strings.IndexByte("BKMGTPE", match[2][0])
	}
	return n.Lsh(n, uint(10*power)), true
}

func RoleApplication(role string) string { return roleApplication[role] }

func declaredClassHosts(cluster api.Object) map[string][]string {
	out := map[string][]string{}
	for _, service := range cluster.Spec().Get("ceph", "services").Items() {
		if textOf(service, "service_type") != "osd" {
			continue
		}
		classes := []string{textOf(service, "spec", "crush_device_class")}
		for _, path := range service.Get("spec", "data_devices", "paths").Items() {
			if path.Type() == api.Mapping {
				classes = append(classes, textOf(path, "crush_device_class"))
			}
		}
		names, issues := ResolveServicePlacement(cluster, service)
		for _, class := range classes {
			if class == "" {
				continue
			}
			if out[class] == nil {
				out[class] = []string{}
			}
			if len(issues) == 0 {
				out[class] = append(out[class], names...)
			}
		}
	}
	for class, hosts := range out {
		slices.Sort(hosts)
		out[class] = slices.Compact(hosts)
	}
	return out
}

func classHostCount(cluster api.Object, class string) (int64, bool) {
	hosts, declared := declaredClassHosts(cluster)[class]
	if !declared || slices.Contains(automaticClasses, class) {
		return osdHostCount(cluster), false
	}
	return int64(len(hosts)), true
}

func (a *admission) deviceClass(cluster api.Object, value api.Value, field string) {
	if !value.Present() {
		return
	}
	class := value.Text()
	if !crushClassName.MatchString(class) {
		a.issue("api.invariant", field, "device class "+class+" is not a native CRUSH class name (letters, digits, '-', '_' and '.')", "rename the device class here and on the OSD service that declares it")
		return
	}
	if _, declared := declaredClassHosts(cluster)[class]; declared || slices.Contains(automaticClasses, class) {
		return
	}
	a.issue("api.invariant", field, "device class "+class+" is neither an automatic class (hdd, ssd, nvme) nor a crush_device_class an OSD service of this cluster declares", "use hdd, ssd or nvme, or declare crush_device_class: "+class+" on the OSD service that owns those devices")
}

func (a *admission) policy(cluster api.Object) {
	spec := a.object.Spec()
	if len(a.object.Name()) > 51 {
		a.issue("api.invariant", "$.metadata.name", "a placement policy name longer than 51 characters does not fit its block identity ceph-policy-<name> of at most 63 characters", "shorten metadata.name to at most 51 characters")
	}
	rule := textOf(spec, "ruleName")
	if spec.Has("ruleName") && !crushRuleName.MatchString(rule) {
		a.issue("api.invariant", "$.spec.ruleName", "a CRUSH rule name uses only letters, digits, '-', '_' and '.', starts with a letter or digit and has at most 128 characters", "rename spec.ruleName within that grammar")
	} else if rule == "replicated_rule" {
		a.issue("api.invariant", "$.spec.ruleName", "replicated_rule is the CRUSH rule the monitor creates at bootstrap; no placement policy owns it", "choose another spec.ruleName; a pool without placementPolicyRef already uses replicated_rule")
	}
	for _, other := range a.catalog.OfKind(api.StoragePlacementPolicy) {
		if other.Name() != a.object.Name() && rule != "" && textOf(other.Spec(), "clusterRef") == cluster.Name() && textOf(other.Spec(), "ruleName") == rule {
			a.issue("api.invariant", "$.spec.ruleName", "placement policy "+other.Name()+" of this cluster already owns CRUSH rule "+rule, "give each placement policy of the cluster its own spec.ruleName")
		}
	}
	for _, pool := range a.catalog.OfKind(api.StoragePool) {
		if pool.Name() == rule && textOf(pool.Spec(), "clusterRef") == cluster.Name() && fallback(pool.Spec().Get("type"), "replicated") == "erasure" {
			a.issue("api.invariant", "$.spec.ruleName", "erasure-coded StoragePool "+pool.Name()+" derives a CRUSH rule named "+rule, "rename spec.ruleName or the StoragePool")
		}
	}
	a.deviceClass(cluster, spec.Get("crushDeviceClass"), "$.spec.crushDeviceClass")
}

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
		if nativeMember(cluster, "osd", textOf(node, "name")) {
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
	if minimum.Cmp(size) > 0 && !(positive(spec.Get("replicated", "size")) && positive(spec.Get("replicated", "minSize"))) {
		a.invalid("$.spec.replicated", "effective minimum replicas must not exceed effective replica size")
	}
	if cluster.Spec().Has("ceph", "topology", "stretch") && (size.Cmp(big.NewInt(4)) != 0 || minimum.Cmp(big.NewInt(2)) != 0) {
		a.invalid("$.spec.replicated", "stretch fixes replicated protection at size four and minimum two")
	}
	domain := textOf(spec, "failureDomain")
	if domain == "" {
		domain = textOf(policy.Spec(), "failureDomain")
	}
	if domain == "" {
		domain = "host"
	}
	class := textOf(policy.Spec(), "crushDeviceClass")
	if policy.Kind() == "" && a.object.Kind() == api.StoragePlacementPolicy {
		class = textOf(spec, "crushDeviceClass")
	}
	if domain == "host" && cluster.Spec().Get("ceph", "topology", "nodes").Len() > 0 {
		hosts := big.NewInt(osdHostCount(cluster))
		classHosts, custom := classHostCount(cluster, class)
		if size.Cmp(hosts) > 0 {
			a.issue("api.invariant", "$.spec.replicated.size", "host-domain replicas exceed declared OSD-host capacity", "add OSD-bearing hosts, or lower replicated.size to at most the declared OSD-host count")
		} else if custom && size.Cmp(big.NewInt(classHosts)) > 0 {
			a.issue("api.invariant", "$.spec.replicated.size", fmt.Sprintf("host-domain replicas exceed the %d OSD hosts whose OSD services declare device class %s", classHosts, class), fmt.Sprintf("declare crush_device_class %s on OSD services of at least %s hosts, or lower replicated.size", class, size))
		} else if textOf(cluster.Spec(), "ceph", "distribution") == "ibm" && !cluster.Spec().Has("ceph", "topology", "stretch") && size.Cmp(hosts) == 0 {
			a.issue("api.invariant", "$.spec.replicated.size", "IBM host-domain replication requires an additional OSD-bearing host beyond the effective replica size", "add OSD-bearing hosts, or explicitly select replicated.size: 2 and minSize: 2 for three OSD-bearing hosts; omitted or zero size retains the default of 3")
		}
	}
}
func (a *admission) pool(cluster api.Object) {
	spec := a.object.Spec()
	policy := api.Object{}
	policyReady := true
	if name := textOf(spec, "placementPolicyRef"); name != "" {
		policy, policyReady = a.sameOwner(api.StoragePlacementPolicy, name, "$.spec.placementPolicyRef", cluster)
	}
	protection := fallback(spec.Get("type"), "replicated")
	if protection == "replicated" {
		a.forbid(spec, "$.spec", "erasure")
		if policyReady {
			a.replication(cluster, policy)
		}
	}
	if protection == "erasure" {
		a.erasure(cluster)
	}
	role := textOf(spec, "role")
	if role == "cephfs-metadata" && protection != "replicated" {
		a.issue("api.invariant", "$.spec.type", "CephFS metadata pools require replicated protection", "set type: replicated for this cephfs-metadata pool")
	}
	if spec.Has("mirroring") && role != "rbd" {
		a.invalid("$.spec.mirroring", "pool mirroring requires the rbd role")
	}
	compression := spec.Get("compression")
	if compression.Len() > 0 && !compression.Has("mode") {
		a.required(compression, "mode", "$.spec.compression")
	}
	if derived := roleApplication[role]; derived != "" && spec.Has("application") && textOf(spec, "application") != derived {
		a.issue("api.invariant", "$.spec.application", "application "+textOf(spec, "application")+" disagrees with role "+role+", which derives application "+derived, "set spec.application to "+derived+" or omit it; the role owns the application")
	}
	for _, key := range []string{"pgNumMin", "pgNumMax"} {
		if role == "cephfs-metadata" && spec.Get("autoscale").Has(key) {
			a.issue("api.invariant", "$.spec.autoscale."+key, "ceph fs new sets pg_num_min on a CephFS metadata pool, so its filesystem owns the pool's placement-group bounds", "remove spec.autoscale.pgNumMin and spec.autoscale.pgNumMax from this cephfs-metadata pool")
		}
	}
}

func (a *admission) erasure(cluster api.Object) {
	spec := a.object.Spec()
	a.required(spec, "erasure", "$.spec")
	if cluster.Spec().Has("ceph", "topology", "stretch") {
		a.invalid("$.spec.type", "stretch does not support erasure pools")
	}
	if role := textOf(spec, "role"); role != "" && !slices.Contains([]string{"rbd", "cephfs-data", "rgw"}, role) {
		a.issue("api.invariant", "$.spec.role", "pool role does not permit erasure protection", "use role rbd, cephfs-data or rgw for an erasure-coded pool, or set type: replicated")
	}
	if spec.Has("placementPolicyRef") {
		a.issue("api.invariant", "$.spec.placementPolicyRef", "an erasure-coded pool derives its CRUSH rule from its own erasure-code profile, so a placement policy cannot own its placement", "remove spec.placementPolicyRef; set spec.erasure.crushDeviceClass to place the pool on a device class")
	}
	for _, policy := range a.catalog.OfKind(api.StoragePlacementPolicy) {
		if textOf(policy.Spec(), "clusterRef") == cluster.Name() && textOf(policy.Spec(), "ruleName") == a.object.Name() {
			a.issue("api.invariant", "$.metadata.name", "placement policy "+policy.Name()+" already owns CRUSH rule "+a.object.Name()+", which this erasure-coded pool would derive", "rename the StoragePool or the policy's spec.ruleName")
		}
	}
	erasure := spec.Get("erasure")
	a.deviceClass(cluster, erasure.Get("crushDeviceClass"), "$.spec.erasure.crushDeviceClass")
	stripe := big.NewInt(4096)
	if erasure.Has("stripeUnit") {
		stripe = numeric(erasure.Get("stripeUnit"))
		if new(big.Int).Rem(stripe, big.NewInt(4096)).Sign() != 0 || stripe.Sign() <= 0 {
			a.issue("api.invariant", "$.spec.erasure.stripeUnit", "the monitor refuses a stripe unit that is not a multiple of 4096 bytes unless forced, and Bootwright never forces it", "set spec.erasure.stripeUnit to a positive multiple of 4096 or omit it (4096)")
		}
	}
	data, coding := erasure.Get("dataChunks"), erasure.Get("codingChunks")
	if new(big.Int).Mul(numeric(data), stripe).Cmp(big.NewInt(4294967295)) > 0 {
		a.issue("api.invariant", "$.spec.erasure.stripeUnit", "the stripe width dataChunks × stripeUnit exceeds the native 32-bit stripe width", "lower spec.erasure.stripeUnit")
	}
	if textOf(cluster.Spec(), "ceph", "distribution") == "ibm" {
		a.ibmErasure(cluster, erasure)
	} else if cluster.Spec().Get("ceph", "topology", "nodes").Len() > 0 && positive(data) && positive(coding) && new(big.Int).Add(numeric(data), numeric(coding)).Cmp(big.NewInt(osdHostCount(cluster))) > 0 {
		a.issue("api.invariant", "$.spec.erasure", "erasure data and coding chunks exceed declared OSD-host capacity", "add OSD-bearing hosts, or lower dataChunks and codingChunks")
	}
	for _, key := range []string{"dataChunks", "codingChunks", "plugin", "technique", "crushDeviceClass", "crushRoot", "stripeUnit", "failureDomain", "k", "m", "crush-device-class", "crush-root", "crush-failure-domain", "stripe_unit"} {
		if erasure.Get("parameters").Has(key) {
			a.invalid("$.spec.erasure.parameters", "erasure parameters cannot repeat typed or derived fields")
		}
	}
}

func (a *admission) ibmErasure(cluster api.Object, erasure api.Value) {
	k, m := numeric(erasure.Get("dataChunks")).Int64(), numeric(erasure.Get("codingChunks")).Int64()
	profile := fmt.Sprintf("%d+%d", k, m)
	minimum, row := ibmErasureMinimum[[2]int64{k, m}]
	if !row && k > 0 && m > 0 {
		message := "erasure profile " + profile + " is not one of the erasure-coding profiles Bootwright supports"
		if k == 8 && m == 6 {
			message = "erasure profile 8+6 is IBM's MSR special case, which needs mon_osd_down_out_subtree_limit = host and a special CRUSH rule that Bootwright does not admit"
		}
		a.issue("api.invariant", "$.spec.erasure", message, "use 2+2, 4+2, 5+2, 6+2, 8+3 or 8+4")
	}
	if hosts, _ := classHostCount(cluster, textOf(erasure, "crushDeviceClass")); row && cluster.Spec().Get("ceph", "topology", "nodes").Len() > 0 && hosts < minimum {
		a.issue("api.invariant", "$.spec.erasure", fmt.Sprintf("erasure profile %s needs at least %d OSD hosts without mon_osd_down_out_subtree_limit = host; %d are declared", profile, minimum, hosts), fmt.Sprintf("add OSD-bearing hosts until %d carry OSDs, or choose a profile whose minimum the cluster meets", minimum))
	}
	plugin := textOf(erasure, "plugin")
	if slices.Contains([]string{"clay", "lrc", "shec"}, plugin) {
		a.issue("api.invariant", "$.spec.erasure.plugin", "Bootwright supports only the isa and jerasure erasure-code plugins; "+plugin+" is not supported", "omit spec.erasure.plugin (isa) or set isa or jerasure")
	}
	if plugin == "jerasure" && !slices.Contains(jerasureProfiles, [2]int64{k, m}) {
		a.issue("api.invariant", "$.spec.erasure.plugin", "jerasure is admitted only for the 4+2, 8+3 and 8+4 profiles IBM lists as supported jerasure values; this pool declares "+profile, "omit spec.erasure.plugin to use isa, or choose 4+2, 8+3 or 8+4")
	}
	if technique := textOf(erasure, "technique"); erasure.Has("technique") && technique != "reed_sol_van" {
		a.issue("api.invariant", "$.spec.erasure.technique", "only the reed_sol_van technique is qualified; "+technique+" is not", "omit spec.erasure.technique or set reed_sol_van")
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
