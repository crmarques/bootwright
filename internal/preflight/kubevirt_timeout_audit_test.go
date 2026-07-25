package preflight

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

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
	if !slices.Contains(gotArgs, "--raw=/apis/apiextensions.k8s.io/v1/customresourcedefinitions/virtualmachines.kubevirt.io") {
		t.Fatalf("kubectl argv must probe the CRD without discovery, got %v", gotArgs)
	}
}

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
	if !slices.Contains(gotArgs, "--raw=/apis/k8s.cni.cncf.io/v1/namespaces/bootwright-child-ocp/network-attachment-definitions/child-net") {
		t.Fatalf("kubectl argv must probe the network resource without discovery, got %v", gotArgs)
	}
}
