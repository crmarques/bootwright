package inspection

import (
	"context"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestContainerDetailsRemainVisibleWithoutInstallationEvidence(t *testing.T) {
	f := newFixture(t)
	service := f.service("lab").WithContainerDetails(func(_ api.Catalog, cluster api.Object) ContainerDetails {
		return ContainerDetails{Distribution: cluster.Spec().Get("distribution", "type").Text(),
			Endpoints: []ContainerEndpoint{{Service: "api", URL: "https://api." + cluster.Name() + ".example.test:6443"}},
			Nodes:     []ContainerNode{{Name: "control-0", Role: "controlPlane", Machine: "host-0"}}}
	})
	result, err := service.ClusterInfo(context.Background(), ClusterInfoRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, cluster := range result.Clusters {
		details := cluster.Details
		if details == nil || len(details.Endpoints) != 1 || len(details.Nodes) != 1 {
			t.Fatalf("%s details = %+v", cluster.Name, details)
		}
		if cluster.Name == "edge" && (details.Distribution != "openshift" || details.Status != StatusReady || details.Reason != "" || details.Remedy != "") {
			t.Fatalf("installed details = %+v", details)
		}
		if cluster.Name == "okd-lab" && (details.Distribution != "okd" || details.Status != StatusUnavailable || details.Reason == "" || details.Remedy != "bootwright apply --context lab --stage clusters") {
			t.Fatalf("uninstalled details = %+v", details)
		}
	}
	if len(f.custody) != 0 || len(f.clients) != 0 || len(f.objectReads) != 0 {
		t.Fatalf("inspection read credential bytes: dashboard %v, client %v, object %v", f.custody, f.clients, f.objectReads)
	}
}
