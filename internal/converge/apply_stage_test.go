package converge

import (
	"reflect"
	"slices"
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
	for _, stage := range []string{"fabric", "add-ons"} {
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
		{"", "all", []string{"fabric", "machines", "deps", "base", "add-ons"}},
		{"infra", "infra", []string{"fabric", "machines"}},
		{"clusters", "clusters", []string{"deps", "base", "add-ons"}},
		{"fabric", "fabric", []string{"fabric"}},
		{"machines", "machines", []string{"machines"}},
		{"deps", "deps", []string{"deps"}},
		{"base", "base", []string{"base"}},
		{"add-ons", "add-ons", []string{"add-ons"}},
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
		{"infra", []string{"deps", "base", "add-ons"}, nil},
		{"clusters", []string{"fabric", "machines"}, []string{"fabric", "machines"}},
		{"fabric", []string{"machines", "deps", "base", "add-ons"}, nil},
		{"base", []string{"fabric", "machines", "deps", "add-ons"}, []string{"fabric", "machines", "deps"}},
		{"add-ons", []string{"fabric", "machines", "deps", "base"}, []string{"fabric", "machines", "deps", "base"}},
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
		{"", "all", "all", []string{"fabric", "machines", "deps", "base", "add-ons"}, "", true},
		{"fabric", "through-fabric", "through-fabric", []string{"fabric"}, infraAnsibleLimit, false},
		{"machines", "infra", "infra", []string{"fabric", "machines"}, infraAnsibleLimit, false},
		{"infra", "infra", "infra", []string{"fabric", "machines"}, infraAnsibleLimit, false},
		{"deps", "through-deps", "through-deps", []string{"fabric", "machines", "deps"}, "", true},
		{"base", "through-base", "through-base", []string{"fabric", "machines", "deps", "base"}, "", true},
		{"add-ons", "all", "all", []string{"fabric", "machines", "deps", "base", "add-ons"}, "", true},
		{"clusters", "all", "all", []string{"fabric", "machines", "deps", "base", "add-ons"}, "", true},
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

func TestApplyThroughEndRunsFullGraph(t *testing.T) {
	scope, err := ApplyThroughScope(ThroughEndSentinel)
	if err != nil {
		t.Fatalf("ApplyThroughScope(%q): %v", ThroughEndSentinel, err)
	}
	if !reflect.DeepEqual(scope.PhaseNames, AllScope.PhaseNames) {
		t.Fatalf("ApplyThroughScope(end) phases = %#v, want the full graph", scope.PhaseNames)
	}
	if !slices.Contains(ApplyThroughNames(), ThroughEndSentinel) {
		t.Fatalf("ApplyThroughNames() = %#v, want it to include %q", ApplyThroughNames(), ThroughEndSentinel)
	}
}

func TestApplyRangeScopeResolvesInclusiveRanges(t *testing.T) {
	cases := []struct {
		stage, through string
		wantName       string
		wantPhases     []string
		wantLimit      string
		wantTargetsCI  bool
	}{
		{"infra", "clusters", "all", []string{"fabric", "machines", "deps", "base", "add-ons"}, "", true},
		{"infra", "end", "all", []string{"fabric", "machines", "deps", "base", "add-ons"}, "", true},
		{"fabric", "machines", "infra", []string{"fabric", "machines"}, infraAnsibleLimit, false},
		{"infra", "infra", "infra", []string{"fabric", "machines"}, infraAnsibleLimit, false},
		{"deps", "add-ons", "clusters", []string{"deps", "base", "add-ons"}, clustersAnsibleLimit, true},
		{"clusters", "end", "clusters", []string{"deps", "base", "add-ons"}, clustersAnsibleLimit, true},
		{"infra", "base", "through-base", []string{"fabric", "machines", "deps", "base"}, "", true},
		{"fabric", "deps", "through-deps", []string{"fabric", "machines", "deps"}, "", true},
		{"base", "base", "base", []string{"base"}, phases["base"].AnsibleLimit, false},
		{"deps", "base", "range-deps-base", []string{"deps", "base"}, clustersAnsibleLimit, true},
		{"base", "add-ons", "range-base-add-ons", []string{"base", "add-ons"}, clustersAnsibleLimit, true},
		{"machines", "deps", "range-machines-deps", []string{"machines", "deps"}, "", true},
		{"machines", "end", "range-machines-add-ons", []string{"machines", "deps", "base", "add-ons"}, "", true},
	}
	for _, tc := range cases {
		scope, err := ApplyRangeScope(tc.stage, tc.through)
		if err != nil {
			t.Fatalf("ApplyRangeScope(%q,%q): %v", tc.stage, tc.through, err)
		}
		if scope.Name != tc.wantName {
			t.Errorf("ApplyRangeScope(%q,%q) name = %q, want %q", tc.stage, tc.through, scope.Name, tc.wantName)
		}
		if !reflect.DeepEqual(scope.PhaseNames, tc.wantPhases) {
			t.Errorf("ApplyRangeScope(%q,%q) phases = %#v, want %#v", tc.stage, tc.through, scope.PhaseNames, tc.wantPhases)
		}
		if scope.AnsibleLimit != tc.wantLimit {
			t.Errorf("ApplyRangeScope(%q,%q) ansibleLimit = %q, want %q", tc.stage, tc.through, scope.AnsibleLimit, tc.wantLimit)
		}
		if scope.TargetsContainerInstall != tc.wantTargetsCI {
			t.Errorf("ApplyRangeScope(%q,%q) targetsContainerInstall = %t, want %t", tc.stage, tc.through, scope.TargetsContainerInstall, tc.wantTargetsCI)
		}
	}
}

func TestApplyRangeScopeMidGraphAssumesPriorPhases(t *testing.T) {
	scope, err := ApplyRangeScope("deps", "base")
	if err != nil {
		t.Fatalf("ApplyRangeScope(deps,base): %v", err)
	}
	omitted, assumedPrior := StageScopeOmissions(scope)
	if !reflect.DeepEqual(omitted, []string{"fabric", "machines", "add-ons"}) {
		t.Fatalf("range deps..base omitted = %#v, want fabric, machines, add-ons", omitted)
	}
	if !reflect.DeepEqual(assumedPrior, []string{"fabric", "machines"}) {
		t.Fatalf("range deps..base assumedPrior = %#v, want fabric, machines", assumedPrior)
	}
}

func TestApplyRangeScopeRejectsReversedRange(t *testing.T) {
	for _, tc := range []struct{ stage, through string }{
		{"clusters", "infra"},
		{"base", "deps"},
		{"add-ons", "base"},
	} {
		if _, err := ApplyRangeScope(tc.stage, tc.through); err == nil {
			t.Fatalf("ApplyRangeScope(%q,%q) accepted a reversed range", tc.stage, tc.through)
		} else if !strings.Contains(err.Error(), "starts after") {
			t.Fatalf("ApplyRangeScope(%q,%q) error = %q, want start-after-end message", tc.stage, tc.through, err)
		}
	}
}

func TestApplyRangeScopeRejectsUnknownTokens(t *testing.T) {
	if _, err := ApplyRangeScope("bogus", "base"); err == nil || !strings.Contains(err.Error(), "--stage must be one of") {
		t.Fatalf("ApplyRangeScope(bogus,base) error = %v, want --stage vocabulary", err)
	}
	if _, err := ApplyRangeScope("deps", "bogus"); err == nil || !strings.Contains(err.Error(), "--through must be one of") {
		t.Fatalf("ApplyRangeScope(deps,bogus) error = %v, want --through vocabulary", err)
	}
	if _, err := ApplyRangeScope("deps", "end"); err != nil {
		t.Fatalf("ApplyRangeScope(deps,end) rejected the end sentinel: %v", err)
	}
}
