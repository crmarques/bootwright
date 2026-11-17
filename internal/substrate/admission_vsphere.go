package substrate

import (
	"fmt"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func normalizeVSphere(arm api.Value) api.Value {
	profiles := arm.Get("machineProfiles").Items()
	domains := arm.Get("failureDomains").Items()
	if len(domains) == 1 {
		for index, profile := range profiles {
			profiles[index] = profile.Default("failureDomainRef", domains[0].Get("name"))
		}
		if arm.Has("machineProfiles") {
			arm = arm.With("machineProfiles", api.ListValue(profiles...))
		}
		if arm.Has("isoStaging") {
			arm = arm.With("isoStaging", arm.Get("isoStaging").Default("datastore", domains[0].Get("topology", "datastore")))
		}
	}
	if arm.Has("isoStaging") {
		arm = arm.With("isoStaging", arm.Get("isoStaging").Default("folder", api.StringValue("bootwright-vmedia")))
	}
	return arm
}

func validateVSphereAuthored(o api.Object) []api.Issue {
	staging := o.Spec().Get(ArmVSphere, "isoStaging")
	if staging.Present() && !staging.Has("datastore") && !staging.Has("folder") {
		return []api.Issue{issue("$.spec.vsphere.isoStaging", "an authored staging block must set datastore or folder")}
	}
	return nil
}

func validateVSphere(o api.Object, _ api.Catalog) []api.Issue {
	arm := o.Spec().Get(ArmVSphere)
	issues := []api.Issue{}
	domains := arm.Get("failureDomains").Items()
	for index, domain := range domains {
		path := fmt.Sprintf("$.spec.vsphere.failureDomains[%d]", index)
		server, ok := arm.Get("vcenters").Named("server", domain.Get("server").Text())
		if domain.Has("server") && !ok {
			issues = api.AppendIssues(issues, reference(path+".server", "failure-domain server must name a declared vCenter"))
		} else if ok && domain.Get("topology", "datacenter").Present() && !slices.Contains(server.Get("datacenters").Strings(), domain.Get("topology", "datacenter").Text()) {
			issues = api.AppendIssues(issues, reference(path+".topology.datacenter", "failure-domain datacenter must be declared on its vCenter"))
		}
	}
	issues = api.AppendIssues(issues, requirePositiveCapacity(arm, ArmVSphere, "vSphere")...)
	for index, profile := range arm.Get("machineProfiles").Items() {
		path := fmt.Sprintf("$.spec.vsphere.machineProfiles[%d]", index)
		if !profile.Has("failureDomainRef") && len(domains) > 1 {
			issues = api.AppendIssues(issues, issue(path+".failureDomainRef", "multiple failure domains require an explicit profile selection"))
		}
	}
	return issues
}

func validateVSphereSegment(path string, segment, arm api.Value) []api.Issue {
	if arm.Get("failureDomains").Len() > 1 && !segment.Get(ArmVSphere).Has("distributedSwitch") {
		return []api.Issue{issue(path+".vsphere.distributedSwitch", "multiple failure domains require an explicit distributed switch")}
	}
	return nil
}
