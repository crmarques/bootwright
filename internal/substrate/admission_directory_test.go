package substrate

import (
	"fmt"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestALibvirtMachineNeverSharesItsProvidersName(t *testing.T) {
	const field = "$.metadata.name"
	host := topologyHost("host")
	lab := topologyProvider("lab", "host", "8000", topologyProfile())
	other := topologyProvider("other", "host", "8100", topologyProfile())
	hostedOn := func(provider string) api.Object {
		return obj(api.Machine, "lab", m("substrate", m("providerRef", provider, "profileRef", "small")))
	}
	for name, catalog := range map[string]api.Catalog{
		"hosted on the provider it is named after": api.NewCatalog([]api.Object{lab, host, hostedOn("lab")}),
		"hosted on another libvirt provider":       api.NewCatalog([]api.Object{lab, other, host, hostedOn("other")}),
	} {
		t.Run(name, func(t *testing.T) {
			refused := issuesAt(Validate(lab, catalog), field)
			want := api.Issue{Code: "api.invariant", Field: field,
				Message:     "Machine/lab is realized on a libvirt provider and shares this provider's name, and its disk directory, which its destroy removes with everything in it, would hold this provider's virtual-media pool directory",
				Remediation: "rename InfraProvider/lab or Machine/lab, with every reference to the one renamed"}
			if len(refused) != 1 || refused[0] != want {
				t.Fatalf("a Machine sharing its libvirt provider's name: %v", refused)
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

func TestALibvirtProviderDeclaresAtMostTwentyManagedAttachments(t *testing.T) {
	const field = "$.spec.networkAttachments"
	host := topologyHost("host")
	attachments := func(managed int) []api.Value {
		var found []api.Value
		for index := range managed {
			found = append(found, managedAttachment(fmt.Sprintf("net-%02d", index), fmt.Sprintf("br-%02d", index), fmt.Sprintf("10.%d.0.1/24", index)))
		}
		for index := range 3 {
			found = append(found, externalAttachment(fmt.Sprintf("ext-%02d", index), fmt.Sprintf("ext-%02d", index)))
		}
		return found
	}
	validate := func(managed int) []api.Issue {
		provider := topologyProvider("lab", "host", "8000", topologyProfile(), attachments(managed)...)
		return issuesAt(Validate(provider, api.NewCatalog([]api.Object{provider, host})), field)
	}
	if refused := validate(MaxManagedAttachments); len(refused) != 0 {
		t.Fatalf("%d managed attachments were refused: %v", MaxManagedAttachments, refused)
	}
	refused := validate(MaxManagedAttachments + 1)
	want := api.Issue{Code: "api.value", Field: field,
		Message:     "a libvirt provider declares at most 20 managed network attachments, because its host reservation claims three keys for each beside the two of its media pool, within the 64 keys one reservation holds; this one declares 21",
		Remediation: "declare at most 20 attachments with management: managed in spec.networkAttachments of InfraProvider/lab, or make the others external"}
	if len(refused) != 1 || refused[0] != want {
		t.Fatalf("%d managed attachments: %v", MaxManagedAttachments+1, refused)
	}
}
