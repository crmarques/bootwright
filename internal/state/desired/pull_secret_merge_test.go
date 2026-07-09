package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func pullSecretMergeInput() v1alpha1.ClusterAddonAcceptedInput {
	return v1alpha1.ClusterAddonAcceptedInput{
		Name: "ibm-entitlement",
		Schema: v1alpha1.ClusterAddonInputSchema{
			Required:   []string{"entitlementKeyRef"},
			Properties: map[string]v1alpha1.ClusterAddonInputProperty{"entitlementKeyRef": {Secret: true}},
		},
		Effects: []v1alpha1.ClusterAddonInputEffect{{
			Type:     v1alpha1.ClusterAddonInputEffectGlobalPullSecretMerge,
			Registry: "cp.icr.io",
			Username: "cp",
		}},
	}
}

func TestValidateGlobalPullSecretMergeEffect(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*v1alpha1.ClusterAddonAcceptedInput)
		wantErr string
	}{
		{name: "valid", mutate: func(*v1alpha1.ClusterAddonAcceptedInput) {}},
		{
			name:    "missing registry",
			mutate:  func(in *v1alpha1.ClusterAddonAcceptedInput) { in.Effects[0].Registry = "" },
			wantErr: "registry is required",
		},
		{
			name:    "missing username",
			mutate:  func(in *v1alpha1.ClusterAddonAcceptedInput) { in.Effects[0].Username = "" },
			wantErr: "username is required",
		},
		{
			name:    "provider rejected",
			mutate:  func(in *v1alpha1.ClusterAddonAcceptedInput) { in.Effects[0].Provider = "ibm" },
			wantErr: "provider is only valid",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := pullSecretMergeInput()
			tc.mutate(&input)
			got := strings.Join(validateClusterAddonInputEffects("spec.accepts.inputs[0].effects", input.Effects), "; ")
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

func TestValidateStorageExportAttachmentRejectsMergeFields(t *testing.T) {
	effects := []v1alpha1.ClusterAddonInputEffect{{
		Type:     v1alpha1.ClusterAddonInputEffectStorageExportAttachment,
		Provider: v1alpha1.ClusterAddonProvidesDataFoundation,
		Registry: "cp.icr.io",
	}}
	got := strings.Join(validateClusterAddonInputEffects("spec.accepts.inputs[0].effects", effects), "; ")
	if !strings.Contains(got, "registry and username are only valid") {
		t.Fatalf("errors = %q, want registry/username rejection", got)
	}
}

func TestValidateGlobalPullSecretMergeInputSchemaPinned(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*v1alpha1.ClusterAddonAcceptedInput)
		wantErr string
	}{
		{name: "valid", mutate: func(*v1alpha1.ClusterAddonAcceptedInput) {}},
		{
			name: "extra property",
			mutate: func(in *v1alpha1.ClusterAddonAcceptedInput) {
				in.Schema.Properties["other"] = v1alpha1.ClusterAddonInputProperty{Secret: true}
			},
			wantErr: "exactly one secret property",
		},
		{
			name: "non-secret property",
			mutate: func(in *v1alpha1.ClusterAddonAcceptedInput) {
				in.Schema.Properties["entitlementKeyRef"] = v1alpha1.ClusterAddonInputProperty{RefKind: "Secret"}
			},
			wantErr: "must set secret: true",
		},
		{
			name:    "not required",
			mutate:  func(in *v1alpha1.ClusterAddonAcceptedInput) { in.Schema.Required = nil },
			wantErr: "required must include",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := pullSecretMergeInput()
			tc.mutate(&input)
			got := strings.Join(validateGlobalPullSecretMergeInputSchema("entitled", 0, input), "; ")
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
