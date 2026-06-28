package inventory

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestVirtctlMirrorOverride(t *testing.T) {
	if got := VirtctlMirrorOverride(v1alpha1.State{}); got != "" {
		t.Fatalf("no environment override = %q, want empty (default is the host ConsoleCLIDownload)", got)
	}
	state := v1alpha1.State{Environments: []v1alpha1.Environment{{
		Metadata: v1alpha1.Metadata{Name: "env"},
		Spec:     v1alpha1.EnvironmentSpec{Defaults: v1alpha1.EnvironmentDefaultsSpec{VirtctlMirror: "https://mirror.example/virtctl/"}},
	}}}
	if got := VirtctlMirrorOverride(state); got != "https://mirror.example/virtctl" {
		t.Fatalf("override = %q, want the trailing slash trimmed", got)
	}
}
