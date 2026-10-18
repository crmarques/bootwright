package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// A continuation previewed under another build closes with one Build field that
// names the build that registered it, and says "step" for one position and
// "steps" for several. A preview that carries none has no such field.
func TestPlanNamesTheStepsAnotherBuildRuns(t *testing.T) {
	const closing = "    Build        "
	for _, test := range []struct {
		name  string
		build *lifecycle.AnotherBuild
		want  string
	}{
		{"one step", &lifecycle.AnotherBuild{RegisteredBy: "devel (abcdef1)", Steps: []int{15}},
			closing + "devel (abcdef1) registered this operation; step 15 runs under this build\n"},
		{"several steps", &lifecycle.AnotherBuild{RegisteredBy: "1.4.0 (9f2c1ab)", Steps: []int{15, 16, 17}},
			closing + "1.4.0 (9f2c1ab) registered this operation; steps 15, 16, 17 run under this build\n"},
		{"an unnamed build", &lifecycle.AnotherBuild{Steps: []int{2}},
			closing + "another build registered this operation; step 2 runs under this build\n"},
		{"no other build", nil, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := previewResult()
			result.Continuation, result.AnotherBuild = true, test.build
			result.Waves, result.Widest = 1, 1
			var out bytes.Buffer
			if err := writeLifecyclePlan(WithPresentation(&out, PresentationOptions{Width: 180}), result); err != nil {
				t.Fatal(err)
			}
			rendered := out.String()
			if test.want == "" {
				if strings.Contains(rendered, "Build") || strings.Contains(rendered, "registered this operation") {
					t.Fatalf("a preview with no other build named one: %q", rendered)
				}
				return
			}
			if !strings.Contains(rendered, test.want) || strings.Count(rendered, "registered this operation") != 1 {
				t.Fatalf("plan text = %q, want one line %q", rendered, test.want)
			}
		})
	}
}

// The confirmation an apply or destroy shows before it asks carries the same
// field as the preview.
func TestConfirmationNamesTheStepsAnotherBuildRuns(t *testing.T) {
	result := previewResult()
	result.Continuation = true
	result.AnotherBuild = &lifecycle.AnotherBuild{RegisteredBy: "devel (abcdef1)", Steps: []int{1}}
	var out bytes.Buffer
	if err := NewLifecyclePlanPresenter(&out).PresentLifecyclePlan(context.Background(), *result); err != nil {
		t.Fatal(err)
	}
	const want = "devel (abcdef1) registered this operation; step 1 runs under this build\n"
	if !strings.Contains(out.String(), want) {
		t.Fatalf("confirmation = %q, want %q", out.String(), want)
	}
}

// An operation recorded before executable identity existed has no build to
// name, yet a build that did not register it still says so.
func TestStatusSaysAnotherBuildRegisteredAnOperationWithoutExecutable(t *testing.T) {
	result := &lifecycle.StatusResult{Lifecycle: &lifecycle.LifecycleSummary{Operation: "op-1", AnotherBuild: true}}
	var out bytes.Buffer
	if err := writeLifecycleStatus(&out, result, false); err != nil {
		t.Fatal(err)
	}
	const want = "  Registered by  another build than this one\n"
	if !strings.Contains(out.String(), want) {
		t.Fatalf("status = %q, want %q", out.String(), want)
	}
}
