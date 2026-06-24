package converge

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
		scope, err := ApplyStageScope(tc.stage)
		if err != nil {
			t.Fatalf("ApplyStageScope(%q) error: %v", tc.stage, err)
		}
		if scope.Name != tc.wantName {
			t.Fatalf("ApplyStageScope(%q) name = %q, want %q", tc.stage, scope.Name, tc.wantName)
		}
		if !reflect.DeepEqual(scope.PhaseNames, tc.wantPhases) {
			t.Fatalf("ApplyStageScope(%q) phaseNames = %#v, want %#v", tc.stage, scope.PhaseNames, tc.wantPhases)
		}
	}
}

func TestStageScopeOmissions(t *testing.T) {
	cases := []struct {
		stage            string
		wantOmitted      []string
		wantAssumedPrior []string
	}{
		{"", nil, nil},
		{"infra", []string{"deps", "base", "addons"}, nil},
		{"clusters", []string{"fabric", "machines"}, []string{"fabric", "machines"}},
		{"fabric", []string{"machines", "deps", "base", "addons"}, nil},
		{"base", []string{"fabric", "machines", "deps", "addons"}, []string{"fabric", "machines", "deps"}},
		{"addons", []string{"fabric", "machines", "deps", "base"}, []string{"fabric", "machines", "deps", "base"}},
	}
	for _, tc := range cases {
		scope, err := ApplyStageScope(tc.stage)
		if err != nil {
			t.Fatalf("ApplyStageScope(%q): %v", tc.stage, err)
		}
		omitted, assumedPrior := StageScopeOmissions(scope)
		if !reflect.DeepEqual(omitted, tc.wantOmitted) {
			t.Fatalf("StageScopeOmissions(%q) omitted = %#v, want %#v", tc.stage, omitted, tc.wantOmitted)
		}
		if !reflect.DeepEqual(assumedPrior, tc.wantAssumedPrior) {
			t.Fatalf("StageScopeOmissions(%q) assumedPrior = %#v, want %#v", tc.stage, assumedPrior, tc.wantAssumedPrior)
		}
	}
}

func TestApplyStageScopeRejectsUnknownStage(t *testing.T) {
	for _, stage := range []string{"provider", "machine-infra", "storage-cluster", "container-cluster", "bogus"} {
		if _, err := ApplyStageScope(stage); err == nil {
			t.Fatalf("ApplyStageScope(%q) accepted an unknown stage", stage)
		} else if !strings.Contains(err.Error(), "--stage must be one of") {
			t.Fatalf("ApplyStageScope(%q) error = %q", stage, err)
		}
	}
}
