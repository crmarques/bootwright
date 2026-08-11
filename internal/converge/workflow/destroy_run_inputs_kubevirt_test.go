package workflow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/roles"
	"go.yaml.in/yaml/v3"
)

func TestDestroyTaskMaterializesOnlySelectedKubeVirtHostAndCleansUp(t *testing.T) {
	for _, sibling := range []string{"missing", "corrupt"} {
		for _, outcome := range []string{"success", "failure"} {
			t.Run(sibling+" sibling "+outcome, func(t *testing.T) {
				baseDir := t.TempDir()
				state := twoKubeVirtHostPlanningState()
				opts := destroyInputCacheTestOptions(t, baseDir, state)
				runID := "destroy-kubevirt-" + sibling + "-" + outcome
				inputs, err := newDestroyRunInputs(opts.RunsDir, runID, opts)
				if err != nil {
					t.Fatalf("newDestroyRunInputs: %v", err)
				}
				t.Cleanup(func() {
					if err := inputs.close(); err != nil {
						t.Errorf("close destroy inputs: %v", err)
					}
				})
				opts.destroyRunInputs = inputs

				const selectedHost = "metal-ocp"
				const siblingHost = "metal-ocp-2"
				const selectedContent = "apiVersion: v1\ncurrent-context: selected\n"
				selectedTaskHost := render.MachineInfraHostName("child-ocp", "child-master-0")
				writeEncryptedClusterMaterial(t, opts.ClustersDir, selectedHost, "kubeconfig", selectedContent)
				siblingPath := filepath.Join(ClusterSecretsDir(opts.ClustersDir, siblingHost), "kubeconfig")
				if sibling == "corrupt" {
					if err := os.MkdirAll(filepath.Dir(siblingPath), 0o700); err != nil {
						t.Fatalf("create sibling secret dir: %v", err)
					}
					if err := os.WriteFile(siblingPath, []byte("not-an-envelope\n"), 0o600); err != nil {
						t.Fatalf("write corrupt sibling kubeconfig: %v", err)
					}
				}

				task := ApplyTask{
					Entry: TaskLedgerEntry{
						ID:           "destroy.machine-infra.child-master-0",
						Kind:         DestroyTaskKindMachineInfra,
						Label:        "selected KubeVirt machine",
						ResourceKeys: []string{DestroyMachineResourceKeyPrefix + "child-master-0"},
					},
					Playbook:            roles.PlaybookTaskMachineInfraDestroy,
					Limit:               selectedTaskHost,
					CompletionHostLimit: selectedTaskHost,
					State:               state,
				}
				var materializedPath string
				var runnerErr error
				if outcome == "failure" {
					runnerErr = errors.New("selected runner failed")
				}
				runner := &fakeRunner{
					runReturns: runnerErr,
					onRun: func(spec ansible.RunSpec) error {
						data, err := os.ReadFile(spec.ExtraVars)
						if err != nil {
							return err
						}
						var rendered struct {
							HostKubeconfigs map[string]string `yaml:"bootwright_kubevirt_host_kubeconfigs"`
						}
						if err := yaml.Unmarshal(data, &rendered); err != nil {
							return err
						}
						if len(rendered.HostKubeconfigs) != 1 || rendered.HostKubeconfigs[selectedHost] == "" {
							return fmt.Errorf("selected host kubeconfig map = %v", rendered.HostKubeconfigs)
						}
						if rendered.HostKubeconfigs[siblingHost] != "" || strings.Contains(string(data), siblingPath) {
							return fmt.Errorf("unrelated sibling reached selected task inputs: %v", rendered.HostKubeconfigs)
						}
						materializedPath = rendered.HostKubeconfigs[selectedHost]
						content, err := os.ReadFile(materializedPath)
						if err != nil {
							return err
						}
						if string(content) != selectedContent {
							return fmt.Errorf("selected kubeconfig = %q", content)
						}
						if outcome == "success" {
							return writeDestroyInputCompletionProof(spec.ArtifactsDir, selectedTaskHost)
						}
						return nil
					},
				}
				result := runOneDestroyTask(
					context.Background(),
					io.Discard,
					io.Discard,
					opts.RunsDir,
					runID,
					opts,
					task,
					func(io.Writer, io.Writer) ansible.Runner { return runner },
				)
				if !runner.runCalled {
					t.Fatalf("selected destroy task did not reach the runner: %v", result.err)
				}
				if outcome == "success" && result.err != nil {
					t.Fatalf("runOneDestroyTask: %v", result.err)
				}
				if outcome == "failure" && !errors.Is(result.err, runnerErr) {
					t.Fatalf("runOneDestroyTask error = %v, want %v", result.err, runnerErr)
				}
				if materializedPath == "" {
					t.Fatal("runner observed no selected materialized kubeconfig")
				}
				if _, err := os.Stat(materializedPath); !os.IsNotExist(err) {
					t.Fatalf("selected materialized kubeconfig survived runner return: %v", err)
				}
				if _, err := os.Stat(filepath.Dir(materializedPath)); !os.IsNotExist(err) {
					t.Fatalf("selected materialized kubeconfig directory survived runner return: %v", err)
				}
				if sibling == "missing" {
					if _, err := os.Stat(siblingPath); !os.IsNotExist(err) {
						t.Fatalf("missing sibling kubeconfig was touched: %v", err)
					}
				} else {
					content, err := os.ReadFile(siblingPath)
					if err != nil {
						t.Fatalf("read corrupt sibling kubeconfig: %v", err)
					}
					if string(content) != "not-an-envelope\n" {
						t.Fatalf("corrupt sibling kubeconfig was changed: %q", content)
					}
				}
				if got := opts.DestroyRunInputCounters.Counts().KubeconfigScopes; got != 1 {
					t.Fatalf("KubeconfigScopes = %d, want 1", got)
				}
			})
		}
	}
}
