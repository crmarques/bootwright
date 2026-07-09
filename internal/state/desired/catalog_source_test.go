package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func cataloguedOLMAddon() v1alpha1.ClusterAddon {
	return v1alpha1.ClusterAddon{
		Metadata: v1alpha1.Metadata{Name: "catalogued"},
		Spec: v1alpha1.ClusterAddonSpec{
			Type: v1alpha1.ClusterAddonTypeOLM,
			OLM: &v1alpha1.ClusterAddonOLMSpec{
				Namespace: v1alpha1.ClusterAddonOLMNamespace{Name: "cat-ns", Create: true},
				CatalogSource: &v1alpha1.ClusterAddonOLMCatalogSource{
					Name:  "partner-catalog",
					Image: "icr.io/cpopen/partner-catalog:v1",
				},
				Subscription: v1alpha1.ClusterAddonOLMSubscription{
					Name: "cat-op", Package: "cat-op", Channel: "stable",
					Source: "partner-catalog", SourceNamespace: "openshift-marketplace",
					InstallPlanApproval: v1alpha1.InstallPlanApprovalAutomatic,
				},
			},
		},
	}
}

func TestValidateClusterAddonCatalogSource(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*v1alpha1.ClusterAddon)
		wantErr string
	}{
		{name: "valid", mutate: func(*v1alpha1.ClusterAddon) {}},
		{
			name:    "missing name",
			mutate:  func(a *v1alpha1.ClusterAddon) { a.Spec.OLM.CatalogSource.Name = "" },
			wantErr: "catalogSource.name is required",
		},
		{
			name:    "missing image",
			mutate:  func(a *v1alpha1.ClusterAddon) { a.Spec.OLM.CatalogSource.Image = "" },
			wantErr: "catalogSource.image is required",
		},
		{
			name:    "bad poll interval",
			mutate:  func(a *v1alpha1.ClusterAddon) { a.Spec.OLM.CatalogSource.PollInterval = "often" },
			wantErr: "catalogSource.pollInterval \"often\" is not a valid duration",
		},
		{
			name:    "subscription source mismatch",
			mutate:  func(a *v1alpha1.ClusterAddon) { a.Spec.OLM.Subscription.Source = "redhat-operators" },
			wantErr: "subscription.source \"redhat-operators\" must match catalogSource.name \"partner-catalog\"",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			addon := cataloguedOLMAddon()
			tc.mutate(&addon)
			got := strings.Join(validateClusterAddonOLM(addon), "; ")
			if tc.wantErr == "" {
				if got != "" {
					t.Fatalf("unexpected errors: %s", got)
				}
				return
			}
			if !strings.Contains(got, tc.wantErr) {
				t.Fatalf("errors = %q, want %q", got, tc.wantErr)
			}
		})
	}
}

func TestNormalizeDefaultsSubscriptionSourceToShippedCatalog(t *testing.T) {
	addon := cataloguedOLMAddon()
	addon.Spec.OLM.Subscription.Source = ""
	normalizeClusterAddon(&addon)
	if got := addon.Spec.OLM.Subscription.Source; got != "partner-catalog" {
		t.Fatalf("subscription.source = %q, want partner-catalog", got)
	}
	explicit := cataloguedOLMAddon()
	explicit.Spec.OLM.Subscription.Source = "redhat-operators"
	normalizeClusterAddon(&explicit)
	if got := explicit.Spec.OLM.Subscription.Source; got != "redhat-operators" {
		t.Fatalf("explicit subscription.source overwritten to %q", got)
	}
}
