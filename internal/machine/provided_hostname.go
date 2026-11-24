package machine

import api "github.com/crmarques/bootwright/api/v1alpha1"

const ProvidedHostnameFamily = "os-provided-hostname"

func ProvidedHostname(object api.Object, controller string) string {
	if !api.MachineProvided(object) || object.Name() == controller {
		return ""
	}
	entry, found := object.Spec().Get("network", "addresses").Named("name", "fqdn")
	if !found || entry.Get("address").Text() == "" {
		return ""
	}
	if _, err := PlacementFor(object, controller); err != nil {
		return ""
	}
	return entry.Get("address").Text()
}
