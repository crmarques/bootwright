package managedos

import (
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func MediaReferences(catalog api.Catalog) []string {
	names := []string{}
	add := func(reference string) {
		if name, local := strings.CutPrefix(reference, "local-media:"); local && ValidMediaName(name) {
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
