package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// TestUnsupportedMachinePoolFieldsRejected covers F8: MachinePoolSpec fields the
// agent install-config renderer drops (architecture, hyperthreading, platform,
// custom pool name) must be rejected at validation rather than silently ignored.
func TestUnsupportedMachinePoolFieldsRejected(t *testing.T) {
	errs := unsupportedMachinePoolFieldErrors("ContainerCluster/c spec.compute[0]", v1alpha1.MachinePoolSpec{
		Replicas:       3,
		Architecture:   "arm64",
		Hyperthreading: "Disabled",
		Platform:       map[string]any{"aws": map[string]any{}},
		Name:           "custom",
	}, "worker")
	joined := strings.Join(errs, "\n")
	for _, want := range []string{
		"architecture is not supported",
		"hyperthreading is not supported",
		"platform is not supported",
		`name "custom" is not supported`,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %v", want, errs)
		}
	}
}

// TestSupportedMachinePoolFieldsAccepted confirms the canonical default pools
// (replicas + master/worker name) are still accepted.
func TestSupportedMachinePoolFieldsAccepted(t *testing.T) {
	if errs := unsupportedMachinePoolFieldErrors("ContainerCluster/c spec.compute[0]", v1alpha1.MachinePoolSpec{Replicas: 3, Name: "worker"}, "worker"); len(errs) != 0 {
		t.Fatalf("default worker pool should be accepted, got %v", errs)
	}
	if errs := unsupportedMachinePoolFieldErrors("ContainerCluster/c spec.controlPlane", v1alpha1.MachinePoolSpec{Replicas: 3}, "master"); len(errs) != 0 {
		t.Fatalf("default master pool should be accepted, got %v", errs)
	}
}
