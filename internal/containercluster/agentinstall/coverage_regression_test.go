package agentinstall

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// The two install capabilities must agree on which clusters they can realize.
func TestAgentClaimsAgreeOnInstallableClusters(t *testing.T) {
	catalog := compactCatalog()
	want := []string{"OpenShiftCluster/ocp"}
	if got := (MediaCapability{}).Claimed(catalog); !slices.Equal(got, want) {
		t.Fatalf("media claims = %v, want %v", got, want)
	}
	if got := (InstallCapability{}).Claimed(catalog); !slices.Equal(got, want) {
		t.Fatalf("install claims = %v, want %v", got, want)
	}

	objects := catalog.Objects()
	for i, object := range objects {
		if object.Kind() == api.OpenShiftCluster {
			objects[i] = object.WithSpec(object.Spec().WithPath(api.StringValue("okd"), "distribution", "type"))
		}
	}
	refused := api.NewCatalog(objects)
	if got := (MediaCapability{}).Claimed(refused); len(got) != 0 {
		t.Errorf("media claimed an unsupported distribution: %v", got)
	}
	if got := (InstallCapability{}).Claimed(refused); len(got) != 0 {
		t.Errorf("install claimed an unsupported distribution: %v", got)
	}
}
