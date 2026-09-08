package substrate

import api "github.com/crmarques/bootwright/api/v1alpha1"

func ValidatePartial(o api.Object, c api.Catalog) []api.Issue {
	issues := ValidateAuthored(o, c)
	if o.Kind() != api.InfraProvider {
		return issues
	}
	bmc := o.Spec().Get("libvirt", "bmcEmulationDefaults")
	if bmc.Has("enabled") && !bmc.Get("enabled").Bool() {
		issues = add(issues, issue("$.spec.libvirt.bmcEmulationDefaults.enabled", "libvirt BMC emulation must be enabled"))
	}
	if bmc.Has("port") && bmc.Has("vMediaPort") && bmc.Get("port").Equal(bmc.Get("vMediaPort")) {
		issues = add(issues, issue("$.spec.libvirt.bmcEmulationDefaults.vMediaPort", "BMC and virtual-media ports must differ"))
	}
	if variant := Variant(o); variant != "" {
		for _, attachment := range o.Spec().Get("networkAttachments").Items() {
			for _, arm := range []string{"baremetal", "libvirt", "vsphere", "kubevirt"} {
				if arm != variant && attachment.Has(arm) {
					issues = add(issues, issue("$.spec.networkAttachments", "attachment arm must match its provider substrate"))
				}
			}
		}
	}
	return issues
}
