package openshift

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestTrustBundlePolicyAdmission(t *testing.T) {
	cluster, catalog := fixture("libvirt", 1, false)
	for _, policy := range []string{"Always", "Proxyonly"} {
		selected := cluster.WithSpec(cluster.Spec().WithPath(api.StringValue(policy), "install", "additionalTrustBundlePolicy"))
		found := false
		for _, issue := range Validate(selected, catalog) {
			if issue.Field == "$.spec.install.additionalTrustBundlePolicy" {
				found = true
				if issue.Code != "api.invariant" || issue.Remediation == "" {
					t.Fatal(issue)
				}
			}
		}
		if !found {
			t.Fatal("policy without refs accepted")
		}
		selected = selected.WithSpec(selected.Spec().WithPath(api.StringList("issuer"), "install", "additionalTrustBundleRefs"))
		for _, issue := range Validate(selected, catalog) {
			if issue.Field == "$.spec.install.additionalTrustBundlePolicy" {
				t.Fatal(issue)
			}
		}
	}
	schema := api.Schema(api.OpenShiftCluster)
	install, _ := schema.Field("install")
	policy, ok := install.Shape.Field("additionalTrustBundlePolicy")
	if !ok || policy.Default.Present() || len(policy.Shape.Enums) != 2 {
		t.Fatal(policy)
	}
}
