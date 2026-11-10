package libvirt

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// The emulated BMC has no image field: a declared image mirror is its one
// route, and the controller's execution binds the mirror's Secrets.
func TestTheEmulatedBMCRunsTheMirrorsCopyOfItsPinnedImage(t *testing.T) {
	environment := api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue(
		field("mirror", api.MapValue(field("registry", api.MapValue(text("registryRef", "mirror")))))))
	registry := api.NewObject(api.Registry, "mirror", api.Value{}, api.MapValue(
		text("management", "external"), text("url", "mirror.example.test/images"), text("trustBundleRef", "mirror-ca")))
	requests, err := MachineRequests(catalogOf(controller(), provider(), networkConfig(), guest("rhel-01"), environment, registry), "controller", testContext)
	if err != nil || len(requests) != 1 {
		t.Fatalf("requests = %+v (%v)", requests, err)
	}
	controller := requests[0].Controller
	if controller.Image != "mirror.example.test/images/"+emulatorImage || controller.ImageSource == nil || controller.ImageSource.Host != "mirror.example.test" {
		t.Fatalf("controller image = %s (%+v)", controller.Image, controller.ImageSource)
	}
	if !slices.Contains(requests[0].SecretReferences(), "mirror-ca") {
		t.Fatalf("secret references = %v", requests[0].SecretReferences())
	}
	plain, _ := MachineRequests(labCatalog(), "controller", testContext)
	if plain[0].Controller.Image != emulatorImage || plain[0].Controller.ImageSource != nil {
		t.Fatalf("an undeclared mirror moved the controller image: %+v", plain[0].Controller)
	}
}
