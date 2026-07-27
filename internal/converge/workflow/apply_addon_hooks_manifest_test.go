package workflow

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

type recordingHookManifestRunner struct {
	kubeconfig string
	args       []string
}

func (r *recordingHookManifestRunner) Run(_ context.Context, kubeconfig string, args []string, _ []byte) ([]byte, error) {
	r.kubeconfig = kubeconfig
	r.args = append([]string(nil), args...)
	return nil, nil
}

func TestApplyHookManifestsUsesMaterializedKubeconfig(t *testing.T) {
	addonDir := t.TempDir()
	manifestDir := filepath.Join(addonDir, "manifests")
	if err := os.MkdirAll(manifestDir, 0o700); err != nil {
		t.Fatalf("mkdir manifests: %v", err)
	}
	manifestPath := filepath.Join(manifestDir, "configmap.yaml")
	if err := os.WriteFile(manifestPath, []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: hook-data\n"), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	materializedKubeconfig := filepath.Join(t.TempDir(), "kubeconfig")
	runner := &recordingHookManifestRunner{}
	executor := &addonHookExecutor{
		runsDir:    t.TempDir(),
		runID:      "apply-test",
		taskID:     "addon.demo.storage",
		kubeconfig: materializedKubeconfig,
		plan: extensionPlanView{
			Name:    "storage",
			Cluster: "demo",
			Addon: v1alpha1.ClusterAddon{
				SourcePath: filepath.Join(addonDir, "add-on.yaml"),
			},
		},
		ocRunner: runner,
	}
	hook := v1alpha1.ClusterAddonStep{
		Name: "attach",
		Manifests: []v1alpha1.ClusterAddonStepManifest{{
			Path: "manifests/configmap.yaml",
		}},
	}

	observed, err := executor.applyHookManifests(context.Background(), hook, nil)
	if err != nil {
		t.Fatalf("applyHookManifests: %v", err)
	}
	if runner.kubeconfig != materializedKubeconfig {
		t.Fatalf("hook manifest kubeconfig = %q, want materialized path %q", runner.kubeconfig, materializedKubeconfig)
	}
	if len(runner.args) == 0 || runner.args[0] != "apply" {
		t.Fatalf("hook manifest oc args = %v, want apply", runner.args)
	}
	if len(observed) != 1 || observed[0] != "ConfigMap/hook-data" {
		t.Fatalf("observed resources = %v, want ConfigMap/hook-data", observed)
	}
}
