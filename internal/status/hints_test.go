package status

import (
	"reflect"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestContextSecretSetHints(t *testing.T) {
	hints := ContextSecretSetHints("matrix", []string{"bmc-credentials", v1alpha1.DefaultPullSecretName})
	if len(hints) != 2 {
		t.Fatalf("expected 2 hints, got %d: %v", len(hints), hints)
	}
	if want := []string{"bootwright", "secret", "set", "--name", v1alpha1.DefaultPullSecretName, "--pull-secret", "<path>", "--context", "matrix"}; !reflect.DeepEqual(hints[0].Args, want) {
		t.Fatalf("pull secret hint should be first; got %#v", hints[0])
	}
	if want := []string{"bootwright", "secret", "set", "--name", "bmc-credentials", "--from-file", "<path>", "--context", "matrix"}; !reflect.DeepEqual(hints[1].Args, want) {
		t.Fatalf("bmc hint = %#v, want %#v", hints[1].Args, want)
	}

	if got := ContextSecretSetHints("matrix", nil); got != nil {
		t.Fatalf("no missing secrets should yield no hints, got %v", got)
	}
}

func TestNextStepHintsCarryResolvedContextAndNeverInventYes(t *testing.T) {
	contextName := "matrix; $(touch /tmp/not-run)"
	hints := NextStepHints(true, v1alpha1.State{}, "", "", contextName, nil, true, true)
	if len(hints) == 0 {
		t.Fatal("loaded state must produce next-step hints")
	}
	for _, hint := range hints {
		if hint.Action != "" {
			if hint.Action != NextStepActionApply || hint.ContextName != contextName || len(hint.Args) != 0 {
				t.Fatalf("typed next-step action lost its exact context or carried backend argv: %+v", hint)
			}
			continue
		}
		if len(hint.Args) == 0 {
			t.Fatalf("resolved context produced command-free hint: %+v", hint)
		}
		if containsWord(hint.Args, "--yes") {
			t.Fatalf("normal status hint invented --yes: %v", hint.Args)
		}
		if len(hint.Args) < 2 || hint.Args[len(hint.Args)-2] != "--context" || hint.Args[len(hint.Args)-1] != contextName {
			t.Fatalf("normal status hint lost the exact context as one argv value: %v", hint.Args)
		}
	}
}

func TestNextStepHintsAreCommandFreeWithoutAResolvedContext(t *testing.T) {
	hints := NextStepHints(true, v1alpha1.State{}, "", "", "", nil, true, true)
	if len(hints) != 1 || len(hints[0].Args) != 0 || !strings.Contains(hints[0].Guidance, "no runnable command") {
		t.Fatalf("missing context hints = %+v, want one command-free fail-closed instruction", hints)
	}
	secretHints := ContextSecretSetHints("", []string{"bmc-credentials"})
	if len(secretHints) != 1 || len(secretHints[0].Args) != 0 {
		t.Fatalf("missing context secret hint must be command-free, got %+v", secretHints)
	}
}

func containsWord(words []string, want string) bool {
	for _, word := range words {
		if word == want {
			return true
		}
	}
	return false
}
