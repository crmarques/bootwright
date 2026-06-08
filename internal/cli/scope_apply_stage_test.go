package cli

import (
	"reflect"
	"strings"
	"testing"
)

func TestApplyStageScopeResolvesFamiliesAndSubPhases(t *testing.T) {
	cases := []struct {
		stage      string
		wantName   string
		wantPhases []string
	}{
		{"", "all", []string{"fabric", "machines", "deps", "base", "addons"}},
		{"infra", "infra", []string{"fabric", "machines"}},
		{"clusters", "clusters", []string{"deps", "base", "addons"}},
		{"fabric", "fabric", []string{"fabric"}},
		{"machines", "machines", []string{"machines"}},
		{"deps", "deps", []string{"deps"}},
		{"base", "base", []string{"base"}},
		{"addons", "addons", []string{"addons"}},
	}
	for _, tc := range cases {
		scope, err := applyStageScope(tc.stage)
		if err != nil {
			t.Fatalf("applyStageScope(%q) error: %v", tc.stage, err)
		}
		if scope.name != tc.wantName {
			t.Fatalf("applyStageScope(%q) name = %q, want %q", tc.stage, scope.name, tc.wantName)
		}
		if !reflect.DeepEqual(scope.phaseNames, tc.wantPhases) {
			t.Fatalf("applyStageScope(%q) phaseNames = %#v, want %#v", tc.stage, scope.phaseNames, tc.wantPhases)
		}
	}
}

func TestApplyStageScopeRejectsUnknownStage(t *testing.T) {
	for _, stage := range []string{"provider", "machine-infra", "storage-cluster", "container-cluster", "bogus"} {
		if _, err := applyStageScope(stage); err == nil {
			t.Fatalf("applyStageScope(%q) accepted an unknown stage", stage)
		} else if !strings.Contains(err.Error(), "--stage must be one of") {
			t.Fatalf("applyStageScope(%q) error = %q", stage, err)
		}
	}
}
