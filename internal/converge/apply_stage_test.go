package converge

import (
	"reflect"
	"strings"
	"testing"
)

func TestScopeProvisionsClusterWorkload(t *testing.T) {
	gated := []string{"", "infra", "clusters", "machines", "deps", "base"}
	for _, stage := range gated {
		scope, err := ApplyStageScope(stage)
		if err != nil {
			t.Fatalf("ApplyStageScope(%q): %v", stage, err)
		}
		if !ScopeProvisionsClusterWorkload(scope) {
			t.Errorf("stage %q should gate KubeVirt host readiness", stage)
		}
	}
	for _, stage := range []string{"fabric", "addons"} {
		scope, err := ApplyStageScope(stage)
		if err != nil {
			t.Fatalf("ApplyStageScope(%q): %v", stage, err)
		}
		if ScopeProvisionsClusterWorkload(scope) {
			t.Errorf("stage %q should not gate KubeVirt host readiness", stage)
		}
	}
}

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

func TestApplyThroughScopeBuildsPrefix(t *testing.T) {
	cases := []struct {
		through          string
		wantName         string
		wantArtifacts    string
		wantPhases       []string
		wantLimit        string
		wantTargetsCInst bool
	}{
		{"", "all", "all", []string{"fabric", "machines", "deps", "base", "addons"}, "", true},
		{"fabric", "through-fabric", "through-fabric", []string{"fabric"}, infraAnsibleLimit, false},
		{"machines", "infra", "infra", []string{"fabric", "machines"}, infraAnsibleLimit, false},
		{"infra", "infra", "infra", []string{"fabric", "machines"}, infraAnsibleLimit, false},
		{"deps", "through-deps", "through-deps", []string{"fabric", "machines", "deps"}, "", true},
		{"base", "through-base", "through-base", []string{"fabric", "machines", "deps", "base"}, "", true},
		{"addons", "all", "all", []string{"fabric", "machines", "deps", "base", "addons"}, "", true},
		{"clusters", "all", "all", []string{"fabric", "machines", "deps", "base", "addons"}, "", true},
	}
	for _, tc := range cases {
		scope, err := ApplyThroughScope(tc.through)
		if err != nil {
			t.Fatalf("ApplyThroughScope(%q) error: %v", tc.through, err)
		}
		if scope.Name != tc.wantName {
			t.Errorf("ApplyThroughScope(%q) name = %q, want %q", tc.through, scope.Name, tc.wantName)
		}
		if scope.ArtifactsBaseName != tc.wantArtifacts {
			t.Errorf("ApplyThroughScope(%q) artifactsBaseName = %q, want %q", tc.through, scope.ArtifactsBaseName, tc.wantArtifacts)
		}
		if !reflect.DeepEqual(scope.PhaseNames, tc.wantPhases) {
			t.Errorf("ApplyThroughScope(%q) phaseNames = %#v, want %#v", tc.through, scope.PhaseNames, tc.wantPhases)
		}
		if scope.AnsibleLimit != tc.wantLimit {
			t.Errorf("ApplyThroughScope(%q) ansibleLimit = %q, want %q", tc.through, scope.AnsibleLimit, tc.wantLimit)
		}
		if scope.TargetsContainerInstall != tc.wantTargetsCInst {
			t.Errorf("ApplyThroughScope(%q) targetsContainerInstall = %t, want %t", tc.through, scope.TargetsContainerInstall, tc.wantTargetsCInst)
		}
		if scope.NoAnsible {
			t.Errorf("ApplyThroughScope(%q) NoAnsible = true; a from-beginning prefix always runs ansible", tc.through)
		}
	}
}

func TestApplyThroughScopeOmissionsNeverAssumePrior(t *testing.T) {
	for _, through := range append([]string{""}, ApplyStageNames()...) {
		scope, err := ApplyThroughScope(through)
		if err != nil {
			t.Fatalf("ApplyThroughScope(%q): %v", through, err)
		}
		if _, assumedPrior := StageScopeOmissions(scope); assumedPrior != nil {
			t.Errorf("ApplyThroughScope(%q) assumedPrior = %#v, want nil: a prefix starts at fabric and assumes no prior phase", through, assumedPrior)
		}
	}
}

func TestApplyThroughRejectsUnknownStage(t *testing.T) {
	for _, through := range []string{"provider", "machine-infra", "storage-cluster", "container-cluster", "bogus"} {
		if _, err := ApplyThroughScope(through); err == nil {
			t.Fatalf("ApplyThroughScope(%q) accepted an unknown stage", through)
		} else if !strings.Contains(err.Error(), "--through must be one of") {
			t.Fatalf("ApplyThroughScope(%q) error = %q", through, err)
		}
	}
}
