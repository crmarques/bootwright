package agentinstall

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func TestTrustBundlePolicyProjectionGolden(t *testing.T) {
	for _, policy := range []string{"absent", "Always", "Proxyonly"} {
		t.Run(policy, func(t *testing.T) {
			c := externalCatalog()
			objects := c.Objects()
			for index, o := range objects {
				if o.Kind() == api.OpenShiftCluster && policy != "absent" {
					objects[index] = o.WithSpec(o.Spec().WithPath(api.StringValue(policy), "install", "additionalTrustBundlePolicy"))
				}
			}
			media, _, _ := onlyRequests(t, api.NewCatalog(objects))
			if policy == "absent" {
				if _, exists := media.InstallConfig["additionalTrustBundlePolicy"]; exists {
					t.Fatal("policy defaulted")
				}
			} else if media.InstallConfig["additionalTrustBundlePolicy"] != policy {
				t.Fatal(media.InstallConfig)
			}
			data, err := reconciliation.Freeze(media.InstallConfig, "install config")
			if err != nil {
				t.Fatal(err)
			}
			matchesGolden(t, "trust-policy-"+policy, data)
			if media.Version != "cluster-media-agent-v9" {
				t.Fatal(media.Version)
			}
		})
	}
}

func TestTrustBundlePolicyWithoutBundlesRefusesProjection(t *testing.T) {
	c := singleNodeCatalog()
	objects := c.Objects()
	for index, o := range objects {
		if o.Kind() == api.OpenShiftCluster {
			objects[index] = o.WithSpec(o.Spec().WithPath(api.StringValue("Always"), "install", "additionalTrustBundlePolicy"))
		}
	}
	if _, _, _, err := Requests(api.NewCatalog(objects), "controller", testContext); err == nil {
		t.Fatal("policy without bundle accepted")
	}
}
