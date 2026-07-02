package preflight

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// TestKubeVirtAPIReadyCheckBoundsProbeWithRequestTimeout pins M7: the host
// cluster KubeVirt API probe must pass kubectl --request-timeout=5s so an
// unreachable host-cluster API fails the check instead of hanging preflight and
// apply indefinitely.
func TestKubeVirtAPIReadyCheckBoundsProbeWithRequestTimeout(t *testing.T) {
	kubeconfig := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	var gotArgs []string
	kubeVirtAPIReadyCheck("metal-ocp", kubeconfig, Deps{
		CommandOutputLocalRoot: func(name string, args ...string) ([]byte, error) {
			if name == "kubectl" {
				gotArgs = args
			}
			return []byte("customresourcedefinition.apiextensions.k8s.io/virtualmachines.kubevirt.io\n"), nil
		},
	})
	if !slices.Contains(gotArgs, "--request-timeout=5s") {
		t.Fatalf("kubectl argv must bound the probe with --request-timeout=5s, got %v", gotArgs)
	}
}

// TestKubeVirtNetworkRefCheckBoundsProbeWithRequestTimeout pins M7 for the
// network-attachment probe, which shells out to kubectl on the same host cluster.
func TestKubeVirtNetworkRefCheckBoundsProbeWithRequestTimeout(t *testing.T) {
	ref := v1alpha1.KubeVirtNetworkRef{Kind: v1alpha1.KubeVirtNetworkKindCUDN, Name: "child-net", Namespace: "bootwright-child-ocp"}
	var gotArgs []string
	kubeVirtNetworkRefCheck("child-net", ref, "/kc", Deps{
		CommandOutputLocalRoot: func(name string, args ...string) ([]byte, error) {
			if name == "kubectl" {
				gotArgs = args
			}
			return []byte("networkattachmentdefinition.k8s.cni.cncf.io/child-net\n"), nil
		},
	})
	if !slices.Contains(gotArgs, "--request-timeout=5s") {
		t.Fatalf("kubectl argv must bound the probe with --request-timeout=5s, got %v", gotArgs)
	}
}
