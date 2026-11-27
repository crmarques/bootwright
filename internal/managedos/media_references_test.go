package managedos

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestMediaReferencesNameEachStoreImageTheInputBootsOrServes(t *testing.T) {
	profile, catalog := hostedTreeProfile("local-media:dvd.iso")
	objects := append(catalog.Objects(), profile,
		obj(api.MachineImage, "again", m("bootMedia", "local-media:boot.iso")),
		obj(api.MachineImage, "elsewhere", m("bootMedia", "https://images.example.test/other.iso")),
		obj(api.MachineImage, "unset", m()))
	got := MediaReferences(api.NewCatalog(objects))
	if want := []string{"boot.iso", "dvd.iso"}; !slices.Equal(got, want) {
		t.Fatalf("references = %v, want %v", got, want)
	}
	if got := MediaReferences(api.Catalog{}); len(got) != 0 || got == nil {
		t.Fatalf("an empty catalog named %v, want an empty list", got)
	}
}

func TestDefaultMediaReferencesNameOnlyTheCatalogMediaFields(t *testing.T) {
	defaults := m("MachineImage", m("bootMedia", "local-media:unused-boot.iso"),
		"MachineInstallProfile", m("installer", m("anaconda", m("packageSource", m("hostedTree", m("fromMedia", "local-media:unused-dvd.iso"))))))
	catalog := api.NewCatalog([]api.Object{
		obj(api.Environment, "lab", m("defaults", defaults)),
		obj(api.Environment, "other", m()),
	})
	if got := DefaultMediaReferences(catalog); !slices.Equal(got, []string{"unused-boot.iso", "unused-dvd.iso"}) {
		t.Fatalf("references = %v", got)
	}
}
