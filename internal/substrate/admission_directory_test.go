package substrate

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestALibvirtMachineCanShareItsProvidersName(t *testing.T) {
	const field = "$.metadata.name"
	host := topologyHost("host")
	lab := topologyProvider("lab", "host", "8000", topologyProfile())
	other := topologyProvider("other", "host", "8100", topologyProfile())
	hostedOn := func(provider string) api.Object {
		return obj(api.Machine, "lab", m("substrate", m("providerRef", provider, "libvirt", m("profileRef", "small"))))
	}
	for name, catalog := range map[string]api.Catalog{
		"hosted on the provider it is named after": api.NewCatalog([]api.Object{lab, host, hostedOn("lab")}),
		"hosted on another libvirt provider":       api.NewCatalog([]api.Object{lab, other, host, hostedOn("other")}),
	} {
		t.Run(name, func(t *testing.T) {
			refused := issuesAt(Validate(lab, catalog), field)
			if len(refused) != 0 {
				t.Fatalf("disjoint machine and provider directories were refused: %v", refused)
			}
			disk, pool := DiskDirectory("lab", "lab"), PoolPath("lab", "lab")
			if strings.HasPrefix(pool, disk+"/") || strings.HasPrefix(disk, pool+"/") || disk == pool {
				t.Fatalf("machine and provider directories overlap: %s, %s", disk, pool)
			}
		})
	}
	metal := obj(api.InfraProvider, "metal", m("baremetal", m()))
	for name, catalog := range map[string]api.Catalog{
		"hosted on a baremetal provider": api.NewCatalog([]api.Object{lab, host, metal, hostedOn("metal")}),
		"with no substrate":              api.NewCatalog([]api.Object{lab, host, obj(api.Machine, "lab", m())}),
		"the provider's own host": api.NewCatalog([]api.Object{
			topologyProvider("lab", "lab", "8000", topologyProfile()), topologyHost("lab")}),
	} {
		t.Run(name, func(t *testing.T) {
			provider, _ := catalog.Find(api.InfraProvider, "lab")
			if refused := issuesAt(Validate(provider, catalog), field); len(refused) != 0 {
				t.Fatalf("a Machine named lab %s was refused: %v", name, refused)
			}
		})
	}
}
