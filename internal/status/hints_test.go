package status

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestContextSecretSetHints(t *testing.T) {
	// bmc-credentials sorts before openshift-pull-secret, but every missing
	// context secret must be emitted and the pull secret must surface first.
	hints := ContextSecretSetHints([]string{"bmc-credentials", v1alpha1.DefaultPullSecretName})
	if len(hints) != 2 {
		t.Fatalf("expected 2 hints, got %d: %v", len(hints), hints)
	}
	if want := "bootwright secret set " + v1alpha1.DefaultPullSecretName + " --pull-secret <path>"; hints[0] != want {
		t.Fatalf("pull secret hint should be first; got %q", hints[0])
	}
	if want := "bootwright secret set bmc-credentials --from-file <path>"; hints[1] != want {
		t.Fatalf("bmc hint = %q, want %q", hints[1], want)
	}

	if got := ContextSecretSetHints(nil); got != nil {
		t.Fatalf("no missing secrets should yield no hints, got %v", got)
	}
}
