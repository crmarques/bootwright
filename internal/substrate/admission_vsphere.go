package substrate

import (
	"fmt"
	"path"
	"strings"
	"unicode"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func normalizeVSphere(arm api.Value) api.Value {
	profiles := arm.Get("machineProfiles").Items()
	domains := arm.Get("placements").Items()
	if len(domains) == 1 {
		for index, profile := range profiles {
			profiles[index] = profile.Default("placementRef", domains[0].Get("name"))
		}
		if arm.Has("machineProfiles") {
			arm = arm.With("machineProfiles", api.ListValue(profiles...))
		}
		if arm.Has("isoStaging") {
			arm = arm.With("isoStaging", arm.Get("isoStaging").Default("datastore", domains[0].Get("datastore")))
		}
	}
	if arm.Has("isoStaging") {
		arm = arm.With("isoStaging", arm.Get("isoStaging").Default("folder", api.StringValue("bootwright-vmedia")))
	}
	return arm
}

func validateVSphereAuthored(o api.Object) []api.Issue {
	staging := o.Spec().Get("vsphere", "isoStaging")
	if staging.Present() && !staging.Has("datastore") && !staging.Has("folder") {
		return []api.Issue{issue("$.spec.vsphere.isoStaging", "an authored staging block must set datastore or folder")}
	}
	return nil
}

// Inventory selectors resolve one exact vCenter object. Dot segments, escapes
// and datastore-file syntax have no meaning in an inventory path.
func inventoryPath(value string) bool {
	return strings.HasPrefix(value, "/") && value != "/" && path.Clean(value) == value &&
		!strings.ContainsAny(value, "%\\[]?#") && strings.IndexFunc(value, unicode.IsControl) < 0
}

func validateVSphere(o api.Object, _ api.Catalog) []api.Issue {
	arm := o.Spec().Get("vsphere")
	issues := requirePositiveCapacity(arm, ArmVSphere, "vSphere")
	placements := arm.Get("placements")
	for i, entry := range placements.Items() {
		field := fmt.Sprintf("$.spec.vsphere.placements[%d]", i)
		if _, ok := arm.Get("vcenters").Named("server", entry.Get("server").Text()); !ok {
			issues = append(issues, reference(field+".server", "placement server must name a declared vCenter"))
		}
		for _, key := range []string{"datacenter", "computeCluster", "datastore", "folder", "resourcePool"} {
			if entry.Has(key) && !inventoryPath(entry.Get(key).Text()) {
				issues = append(issues, issue(field+"."+key, "select an absolute vCenter inventory path"))
			}
		}
	}
	for i, profile := range arm.Get("machineProfiles").Items() {
		field := fmt.Sprintf("$.spec.vsphere.machineProfiles[%d]", i)
		if !profile.Has("placementRef") && placements.Len() != 1 {
			issues = append(issues, issue(field+".placementRef", "multiple placements require an explicit profile selection"))
		}
		if profile.Has("template") && !inventoryPath(profile.Get("template").Text()) {
			issues = append(issues, issue(field+".template", "select the template by absolute vCenter inventory path"))
		}
		disks := profile.Get("dataDisks")
		_, root := disks.Named("name", "root")
		if disks.Len() > 59 || root {
			issues = append(issues, issue(field+".dataDisks", "declare at most 59 data disks with names distinct from root"))
		}
	}
	staging := arm.Get("isoStaging")
	if staging.Has("datastore") && !inventoryPath(staging.Get("datastore").Text()) {
		issues = append(issues, issue("$.spec.vsphere.isoStaging.datastore", "select the staging datastore by absolute vCenter inventory path"))
	}
	if staging.Has("folder") && !SafeSegment(staging.Get("folder").Text()) {
		issues = append(issues, issue("$.spec.vsphere.isoStaging.folder", "the staging folder must be a single DNS label"))
	}
	return issues
}

func validateVSphereSegment(field string, segment, _ api.Value) []api.Issue {
	if portgroup := segment.Get("vsphere", "portgroup"); portgroup.Present() && !inventoryPath(portgroup.Text()) {
		return []api.Issue{issue(field+".vsphere.portgroup", "select the portgroup by absolute vCenter inventory path")}
	}
	return nil
}
