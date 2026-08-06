package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

type absentCRDRunner struct{ reads int }

func (r *absentCRDRunner) Run(_ context.Context, _ string, args []string, _ []byte) ([]byte, error) {
	r.reads++
	return nil, fmt.Errorf("error: the server doesn't have a resource type %q", strings.Join(args, " "))
}

func storageClusterCRDRequirement() v1alpha1.ClusterAddonReadinessCheck {
	return v1alpha1.ClusterAddonReadinessCheck{
		Condition: &v1alpha1.ClusterAddonConditionReadiness{
			APIVersion: "apiextensions.k8s.io/v1",
			Kind:       "CustomResourceDefinition",
			Name:       "storageclusters.ocs.openshift.io",
			Condition:  v1alpha1.ClusterAddonConditionRequirement{Type: "Established", Status: "True"},
		},
	}
}

func requiresStepExecutor(t *testing.T, readRunner, ocRunner *absentCRDRunner) (*addonStepExecutor, v1alpha1.ClusterAddonStep) {
	t.Helper()
	addonDir := t.TempDir()
	manifestDir := filepath.Join(addonDir, "manifests")
	if err := os.MkdirAll(manifestDir, 0o700); err != nil {
		t.Fatalf("mkdir manifests: %v", err)
	}
	manifest := filepath.Join(manifestDir, "storagecluster.yaml")
	if err := os.WriteFile(manifest, []byte("apiVersion: ocs.openshift.io/v1\nkind: StorageCluster\nmetadata:\n  name: ocs-external-storagecluster\n"), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	executor := &addonStepExecutor{
		stderr:     os.Stderr,
		runsDir:    t.TempDir(),
		runID:      "apply-test",
		taskID:     "addon.demo.fusion-data-foundation",
		kubeconfig: filepath.Join(t.TempDir(), "kubeconfig"),
		opts:       RunOptions{ClustersDir: t.TempDir()},
		plan: extensionPlanView{
			Name:    "fusion-data-foundation",
			Cluster: "demo",
			Addon: v1alpha1.ClusterAddon{
				SourcePath: filepath.Join(addonDir, "add-on.yaml"),
				Spec: v1alpha1.ClusterAddonSpec{
					Readiness: v1alpha1.ClusterAddonReadiness{Timeout: "40ms"},
				},
			},
		},
		ocRunner:   ocRunner,
		readRunner: readRunner,
	}
	step := v1alpha1.ClusterAddonStep{
		Name:      "attach-external-storage",
		Requires:  []v1alpha1.ClusterAddonReadinessCheck{storageClusterCRDRequirement()},
		Manifests: []v1alpha1.ClusterAddonStepManifest{{Path: "manifests/storagecluster.yaml"}},
	}
	return executor, step
}

func TestRunStepDoesNotApplyManifestsBeforeTheRequiredAPIExists(t *testing.T) {
	readRunner := &absentCRDRunner{}
	ocRunner := &absentCRDRunner{}
	executor, step := requiresStepExecutor(t, readRunner, ocRunner)

	observed, err := executor.runStep(context.Background(), step)
	if err == nil {
		t.Fatal("expected runStep to fail while the required API is absent")
	}
	if ocRunner.reads != 0 {
		t.Fatalf("runStep applied %d manifests before the required API existed", ocRunner.reads)
	}
	if len(observed) != 0 {
		t.Fatalf("runStep reported observed resources %v it never applied", observed)
	}
	if readRunner.reads == 0 {
		t.Fatal("runStep never polled for the declared requirement")
	}
	if !strings.Contains(err.Error(), "storageclusters.ocs.openshift.io") {
		t.Fatalf("failure %q does not name the missing API", err.Error())
	}
}

func TestStepDigestCoversRequires(t *testing.T) {
	executor, step := requiresStepExecutor(t, &absentCRDRunner{}, &absentCRDRunner{})
	withRequires, err := executor.stepDigest(step)
	if err != nil {
		t.Fatalf("stepDigest: %v", err)
	}
	bare := step
	bare.Requires = nil
	withoutRequires, err := executor.stepDigest(bare)
	if err != nil {
		t.Fatalf("stepDigest: %v", err)
	}
	if withRequires == withoutRequires {
		t.Fatal("stepDigest ignores requires, so editing it would never re-run an onChange step")
	}
}
