package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func pullSecretMergeInput() v1alpha1.ClusterAddonAcceptedInput {
	return v1alpha1.ClusterAddonAcceptedInput{
		Name:      "ibm-entitlement",
		SecretRef: &v1alpha1.ClusterAddonInputSecret{},
		Effects: []v1alpha1.ClusterAddonInputEffect{{
			GlobalPullSecretMerge: &v1alpha1.ClusterAddonGlobalPullSecretMergeEffect{
				Registry: "cp.icr.io",
				Username: "cp",
			},
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
			mutate:  func(in *v1alpha1.ClusterAddonAcceptedInput) { in.Effects[0].GlobalPullSecretMerge.Registry = "" },
			wantErr: "registry is required",
		},
		{
			name:    "missing username",
			mutate:  func(in *v1alpha1.ClusterAddonAcceptedInput) { in.Effects[0].GlobalPullSecretMerge.Username = "" },
			wantErr: "username is required",
		},
		{
			name: "requires secret input",
			mutate: func(in *v1alpha1.ClusterAddonAcceptedInput) {
				in.SecretRef = nil
				in.ResourceRef = &v1alpha1.ClusterAddonInputRef{Kind: v1alpha1.KindStorageExport}
			},
			wantErr: "requires a secretRef input",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := pullSecretMergeInput()
			tc.mutate(&input)
			extension := v1alpha1.ClusterAddon{Metadata: v1alpha1.Metadata{Name: "entitled"}}
			got := strings.Join(validateClusterAddonInputEffects("spec.accepts.inputs[0].effects", extension, input), "; ")
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

func TestValidateEffectPresenceUnion(t *testing.T) {
	input := pullSecretMergeInput()
	input.Effects[0].StorageExportAttachment = &v1alpha1.ClusterAddonStorageExportAttachmentEffect{}
	extension := v1alpha1.ClusterAddon{Metadata: v1alpha1.Metadata{Name: "entitled"}}
	got := strings.Join(validateClusterAddonInputEffects("spec.accepts.inputs[0].effects", extension, input), "; ")
	if !strings.Contains(got, "must not set more than one effect arm") {
		t.Fatalf("errors = %q, want multiple arm rejection", got)
	}
}

func TestValidateAcceptedInputPresence(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*v1alpha1.ClusterAddonAcceptedInput)
		wantErr string
	}{
		{name: "valid", mutate: func(*v1alpha1.ClusterAddonAcceptedInput) {}},
		{
			name: "missing arm",
			mutate: func(in *v1alpha1.ClusterAddonAcceptedInput) {
				in.SecretRef = nil
			},
			wantErr: "must set exactly one of resourceRef or secretRef",
		},
		{
			name: "two arms",
			mutate: func(in *v1alpha1.ClusterAddonAcceptedInput) {
				in.ResourceRef = &v1alpha1.ClusterAddonInputRef{Kind: v1alpha1.KindStorageExport}
			},
			wantErr: "must not set both resourceRef and secretRef",
		},
		{
			name: "unknown resource kind",
			mutate: func(in *v1alpha1.ClusterAddonAcceptedInput) {
				in.SecretRef = nil
				in.ResourceRef = &v1alpha1.ClusterAddonInputRef{Kind: "Unknown"}
			},
			wantErr: "is not a known Bootwright kind",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := pullSecretMergeInput()
			tc.mutate(&input)
			got := strings.Join(validateClusterAddonInputPresence("spec.accepts.inputs[0]", input), "; ")
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
