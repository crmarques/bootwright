package cephcluster

import (
	"net/netip"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/managedos"
)

type derivedManagerModule struct{ module, family, item string }

var derivedManagerModules = []derivedManagerModule{
	{module: "nfs", family: "nfs", item: "B478"},
}

func DerivedManagerModules(ceph api.Value) []string {
	modules := []string{}
	for _, service := range ceph.Get("services").Items() {
		for _, row := range derivedManagerModules {
			if service.Get("service_type").Text() == row.family {
				modules = append(modules, row.module)
			}
		}
	}
	slices.Sort(modules)
	return slices.Compact(modules)
}

func DerivableManagerModules() []string {
	modules := []string{}
	for _, row := range derivedManagerModules {
		modules = append(modules, row.module)
	}
	slices.Sort(modules)
	return modules
}

func (a *admission) clusterServices(ceph api.Value) {
	public := a.prefixes(ceph.Get("networks", "publicCIDRs"), "$.spec.networks.publicCIDRs")
	private := a.prefixes(ceph.Get("networks", "clusterCIDRs"), "$.spec.networks.clusterCIDRs")
	for _, left := range public {
		for _, right := range private {
			if left.Overlaps(right) {
				a.invalid("$.spec.networks", "public and replication networks must be disjoint", "change spec.networks so public and replication CIDRs do not overlap")
			}
		}
	}
	_, configIssues := CoreCephConfig(ceph)
	for _, issue := range configIssues {
		a.issue(issue.Code, issue.Field, issue.Message, issue.Remediation)
	}
	for i, module := range ceph.Get("mgrModules").Strings() {
		if module == "prometheus" {
			a.prometheusModule(ceph, i)
		} else if at := slices.IndexFunc(derivedManagerModules, func(row derivedManagerModule) bool { return row.module == module }); at >= 0 {
			row := derivedManagerModules[at]
			a.issue("api.invariant", indexed("$.spec.mgrModules", i), "manager module "+row.module+" is derived from the declared "+row.family+" services", "remove "+row.module+" from spec.mgrModules; Bootwright enables it for a declared "+row.family+" service")
		} else if module == "smb" {
			a.issue("api.invariant", indexed("$.spec.mgrModules", i), "manager module smb is derived from the declared CephSMBResource clusters", "remove smb from spec.mgrModules; Bootwright enables it for a declared CephSMBResource cluster")
		} else if module == "mirroring" {
			a.issue("api.invariant", indexed("$.spec.mgrModules", i), "manager module mirroring serves CephFS snapshot mirroring, which Bootwright does not realize", "remove mirroring from spec.mgrModules")
		} else if !slices.Contains([]string{"cephadm", "rbd_support"}, module) {
			a.invalid("$.spec.mgrModules", "manager module requires its qualified service owner", "remove the module from spec.mgrModules; declare its service through spec.services when that service is implemented")
		}
	}
	a.nativeServices(ceph, false)
}

func (a *admission) prefixes(values api.Value, field string) []netip.Prefix {
	prefixes := []netip.Prefix{}
	for i, value := range values.Items() {
		prefix, err := netip.ParsePrefix(value.Text())
		if err != nil {
			continue
		}
		prefix = prefix.Masked()
		for _, previous := range prefixes {
			if previous.Overlaps(prefix) {
				a.invalid(indexed(field, i), "declared network ranges must not overlap", "remove duplicate ranges or replace overlapping CIDRs with disjoint ranges")
			}
		}
		prefixes = append(prefixes, prefix)
	}
	return prefixes
}
func declaredPublicNetworks(cluster api.Object, catalog api.Catalog) []netip.Prefix {
	out := []netip.Prefix{}
	for _, value := range cluster.Spec().Get("networks", "publicCIDRs").Items() {
		if prefix, err := netip.ParsePrefix(value.Text()); err == nil {
			out = append(out, prefix.Masked())
		}
	}
	if len(out) != 0 {
		return out
	}
	for _, node := range cluster.Spec().Get("topology", "nodes").Items() {
		machine, ok := catalog.Find(api.Machine, textOf(node, "machineRef"))
		if !ok {
			continue
		}
		for _, value := range machine.Spec().Get("network", "addresses").Items() {
			if prefix, err := netip.ParsePrefix(textOf(value, "address")); err == nil {
				out = append(out, prefix.Masked())
			}
		}
	}
	return out
}
func (a *admission) serviceNetworks(cluster api.Object, values api.Value, field string) {
	declared := declaredPublicNetworks(cluster, a.catalog)
	for i, value := range values.Items() {
		prefix, err := netip.ParsePrefix(value.Text())
		if err != nil {
			continue
		}
		covered := false
		for _, network := range declared {
			if network.Contains(prefix.Masked().Addr()) && network.Bits() <= prefix.Bits() {
				covered = true
			}
		}
		if len(declared) != 0 && !covered {
			a.invalid(indexed(field, i), "service network must be reachable through the cluster's declared public networks", "select service networks within spec.networks.publicCIDRs or declare the required public network")
		}
	}
}
func (a *admission) cephEntitlement() {
	a.intrinsic()
	spec := a.object.Spec()
	product := textOf(spec, "type")
	if product == "redhatCeph" {
		for _, issue := range managedos.ValidateRHSM(a.object, true) {
			remedy := "set " + issue.Field + " for managed RHSM registration"
			if textOf(spec, "rhsm", "management") == "external" {
				remedy = "remove " + issue.Field + " when $.spec.rhsm.management is external"
			} else if issue.Field == "$.spec.rhsm" {
				remedy = "set $.spec.rhsm.management to external for pre-registered hosts, or set $.spec.rhsm.organizationRef and $.spec.rhsm.activationKeyRef for managed registration"
			}
			a.issue(issue.Code, issue.Field, issue.Message, remedy)
		}
	}
	a.required(spec, "registry", "$.spec")
	if spec.Has("registry") {
		a.required(spec.Get("registry"), "credentialsRef", "$.spec.registry")
	}
}

func cephProduct(object api.Object) bool {
	return object.Kind() == api.Entitlement && slices.Contains([]string{"redhatCeph", "ibmStorageCeph"}, textOf(object.Spec(), "type"))
}
func normalizeCephEntitlement(object api.Object) api.Object {
	spec := object.Spec()
	registry := "registry.redhat.io"
	if textOf(spec, "type") == "ibmStorageCeph" {
		registry = "cp.icr.io/cp"
	}
	if spec.Has("registry") {
		spec = spec.With("registry", spec.Get("registry").Default("url", api.StringValue(registry)))
	}
	return managedos.NormalizeRHSM(object.WithSpec(spec))
}
