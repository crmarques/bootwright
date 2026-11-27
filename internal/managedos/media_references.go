package managedos

import (
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func MediaReferences(catalog api.Catalog) []string {
	names := []string{}
	add := func(reference string) {
		if name := localMediaReference(reference); name != "" {
			names = append(names, name)
		}
	}
	for _, image := range catalog.OfKind(api.MachineImage) {
		add(image.Spec().Get("bootMedia").Text())
	}
	for _, profile := range catalog.OfKind(api.MachineInstallProfile) {
		add(profile.Spec().Get("installer", "anaconda", "packageSource", "hostedTree", "fromMedia").Text())
	}
	slices.Sort(names)
	return slices.Compact(names)
}

func DefaultMediaReferences(catalog api.Catalog) []string {
	var names []string
	for _, environment := range catalog.OfKind(api.Environment) {
		defaults := environment.Spec().Get("defaults")
		for _, value := range []api.Value{
			defaults.Get(string(api.MachineImage), "bootMedia"),
			defaults.Get(string(api.MachineInstallProfile), "installer", "anaconda", "packageSource", "hostedTree", "fromMedia"),
		} {
			if name := localMediaReference(value.Text()); name != "" {
				names = append(names, name)
			}
		}
	}
	return names
}

func localMediaReference(reference string) string {
	if name, local := strings.CutPrefix(reference, "local-media:"); local && ValidMediaName(name) {
		return name
	}
	return ""
}
