package workflow

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/ansible"
	secretstore "github.com/crmarques/bootwright/internal/secrets"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
	"go.yaml.in/yaml/v3"
)

func writeAllContextSecretMaterials(t *testing.T, secretsDir string, state v1alpha1.State) []string {
	t.Helper()
	store := secretstore.NewContextStore("test", secretsDir)
	var written []string
	for _, declared := range state.Secrets {
		roles := []secretstore.MaterialRole{secretstore.MaterialPrimary}
		switch declared.Spec.Type {
		case v1alpha1.SecretTypeSSHKeyPair:
			roles = []secretstore.MaterialRole{secretstore.MaterialSSHPrivate, secretstore.MaterialSSHPublic}
		case v1alpha1.SecretTypeTLSCertificate:
			roles = []secretstore.MaterialRole{secretstore.MaterialPrimary, secretstore.MaterialTLSKey}
		}
		for _, role := range roles {
			key := secretstore.MaterialKey{Name: declared.Metadata.Name, Role: role}
			if err := store.Write(key, []byte(declared.Metadata.Name+" "+string(role)+"\n")); err != nil {
				t.Fatalf("write context material %s/%s: %v", declared.Metadata.Name, role, err)
			}
			written = append(written, declared.Metadata.Name)
		}
	}
	sort.Strings(written)
	return written
}

func runtimeSecretPathsIn(t *testing.T, path, runSecretsDir string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	prefix := runSecretsDir + string(os.PathSeparator)
	seen := map[string]bool{}
	var walk func(any)
	walk = func(node any) {
		switch typed := node.(type) {
		case string:
			if strings.HasPrefix(typed, prefix) {
				seen[typed] = true
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		case map[string]any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(doc)
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func taskRunOptions(t *testing.T, baseDir string, state v1alpha1.State, playbook string) (RunOptions, string) {
	t.Helper()
	taskRoot := filepath.Join(baseDir, "runs", "history", "run-1", runTaskDirName, "task-1")
	opts := RunOptions{
		State:              state,
		RenderedDir:        filepath.Join(baseDir, "rendered"),
		RenderDir:          filepath.Join(taskRoot, taskRenderedDirName),
		ArtifactsRoot:      taskArtifactsRoot(taskRoot),
		ClustersDir:        filepath.Join(baseDir, "clusters"),
		RunsDir:            filepath.Join(baseDir, "runs"),
		ContextName:        "test",
		SecretsDir:         filepath.Join(baseDir, "context", "secrets"),
		ManagedServicesDir: "/var/lib/bootwright",
		ProviderStateDir:   filepath.Join(baseDir, "provider-state"),
		BundleDir:          t.TempDir(),
		Playbook:           playbook,
	}
	return opts, taskRoot
}

func TestRunPerTaskRendersOnlyTheAnsibleInputsAnsibleReads(t *testing.T) {
	baseDir := t.TempDir()
	state := storageSSHState()
	opts, taskRoot := taskRunOptions(t, baseDir, state, "bootwright.core.task_storage_cluster_apply")
	runner := &fakeRunner{}
	if _, err := Run(context.Background(), opts, runner, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	renderDir := filepath.Join(taskRoot, taskRenderedDirName)
	for _, unwanted := range []string{"effective-state.yaml", "bootwright.lock.yaml"} {
		if _, err := os.Stat(filepath.Join(renderDir, unwanted)); !os.IsNotExist(err) {
			t.Fatalf("per-task render wrote %s (stat err=%v)", unwanted, err)
		}
	}
	if _, err := os.Stat(runner.lastSpec.Inventory); err != nil {
		t.Fatalf("per-task render did not write the inventory: %v", err)
	}
	if _, err := os.Stat(runner.lastSpec.ExtraVars); err != nil {
		t.Fatalf("per-task render did not write the vars file: %v", err)
	}
	if _, err := os.Stat(opts.ClustersDir); !os.IsNotExist(err) {
		t.Fatalf("per-task render touched the shared clusters dir (stat err=%v)", err)
	}
	cephSpec := filepath.Join(renderDir, "storage", "ceph", "cephadm", "bootstrap-spec.yaml")
	if _, err := os.Stat(cephSpec); err != nil {
		t.Fatalf("per-task render must still write Ceph specs the controller copies from: %v", err)
	}
}

func TestRunPerTaskFullRenderStillWritesInstallerAssetsForTheOnceARunPath(t *testing.T) {
	baseDir := t.TempDir()
	state := storageSSHState()
	opts, _ := taskRunOptions(t, baseDir, state, "bootwright.core.task_storage_cluster_apply")
	opts.RenderDir = ""
	opts.ArtifactsRoot = ""
	runner := &fakeRunner{}
	if _, err := Run(context.Background(), opts, runner, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(opts.RenderedDir, "effective-state.yaml")); err != nil {
		t.Fatalf("the once-per-run render must still write the effective state: %v", err)
	}
}

func TestRunPerTaskMaterializesEveryRuntimeSecretPathTheRenderReferences(t *testing.T) {
	for _, tc := range []struct {
		example        string
		mustMaterialze []string
	}{
		{example: "baremetal-redfish-multidc-virtualized-odf-ceph", mustMaterialze: []string{"bmc-credentials"}},
		{example: "baremetal-redfish-fleet", mustMaterialze: []string{"bmc-credentials"}},
		{example: "ceph-ibm-baremetal-redfish", mustMaterialze: []string{"bmc-credentials", "ibm-ceph-registry"}},
		{example: "sno-baremetal-redfish", mustMaterialze: []string{"bmc-credentials"}},
		{example: "sno-libvirt-redfish-corporate-tls", mustMaterialze: []string{"bmc-credentials"}},
	} {
		example := tc.example
		t.Run(example, func(t *testing.T) {
			state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "examples", example)})
			if err != nil {
				t.Fatalf("LoadNormalizeValidate: %v", err)
			}
			baseDir := t.TempDir()
			opts, taskRoot := taskRunOptions(t, baseDir, state, applyProviderPlaybook)
			declared := writeAllContextSecretMaterials(t, opts.SecretsDir, state)
			if len(declared) == 0 {
				t.Skip("example declares no context secrets")
			}
			runSecretsDir := filepath.Join(runtimeSecretBaseDir("", taskArtifactsRoot(taskRoot)), runtimeSecretsDirName)
			var referenced []string
			var materialized []string
			runner := &fakeRunner{onRun: func(spec ansible.RunSpec) error {
				for _, input := range []string{spec.Inventory, spec.ExtraVars} {
					referenced = append(referenced, runtimeSecretPathsIn(t, input, runSecretsDir)...)
				}
				entries, readErr := os.ReadDir(runSecretsDir)
				if readErr != nil {
					t.Fatalf("read runtime secrets dir: %v", readErr)
				}
				for _, entry := range entries {
					materialized = append(materialized, entry.Name())
				}
				for _, path := range referenced {
					info, statErr := os.Stat(path)
					if statErr != nil {
						t.Fatalf("rendered Ansible input references %s but it was not materialized: %v", path, statErr)
					}
					if info.Mode().Perm() != 0o600 {
						t.Fatalf("materialized %s mode = %o, want 0600", path, info.Mode().Perm())
					}
				}
				return nil
			}}
			if _, err := Run(context.Background(), opts, runner, nil); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if !runner.runCalled {
				t.Fatal("runner was not invoked")
			}
			if len(referenced) == 0 {
				t.Fatalf("example %s rendered no runtime secret paths; the coverage check is vacuous", example)
			}
			if len(materialized) == 0 {
				t.Fatal("no context material was materialized for the task")
			}
			present := map[string]bool{}
			for _, name := range materialized {
				present[name] = true
			}
			for _, name := range tc.mustMaterialze {
				if !present[name] {
					t.Fatalf("secret %s is consumed by name through bootwright_secrets_dir but was not materialized; materialized=%v", name, materialized)
				}
			}
			if _, err := os.Stat(runSecretsDir); !os.IsNotExist(err) {
				t.Fatalf("runtime secrets dir survived Run (stat err=%v)", err)
			}
		})
	}
}

func TestRunPerTaskSkipsContextSecretsTheRenderDoesNotReference(t *testing.T) {
	baseDir := t.TempDir()
	state := storageSSHState()
	opts, taskRoot := taskRunOptions(t, baseDir, state, "bootwright.core.task_storage_cluster_apply")
	store := secretstore.NewContextStore("test", opts.SecretsDir)
	if err := store.Write(secretstore.MaterialKey{Name: "ceph-node-ssh", Role: secretstore.MaterialSSHPrivate}, []byte("private\n")); err != nil {
		t.Fatalf("write referenced material: %v", err)
	}
	if err := store.Write(secretstore.MaterialKey{Name: "ceph-node-ssh", Role: secretstore.MaterialSSHPublic}, []byte("public\n")); err != nil {
		t.Fatalf("write referenced public material: %v", err)
	}
	if err := store.Write(secretstore.MaterialKey{Name: "unrelated-bmc-credentials", Role: secretstore.MaterialPrimary}, []byte("admin:hunter2\n")); err != nil {
		t.Fatalf("write unreferenced material: %v", err)
	}
	runSecretsDir := filepath.Join(runtimeSecretBaseDir("", taskArtifactsRoot(taskRoot)), runtimeSecretsDirName)
	var materialized []string
	var knownHosts string
	runner := &fakeRunner{onRun: func(spec ansible.RunSpec) error {
		entries, readErr := os.ReadDir(runSecretsDir)
		if readErr != nil {
			return readErr
		}
		for _, entry := range entries {
			materialized = append(materialized, entry.Name())
		}
		inventory, readErr := os.ReadFile(spec.Inventory)
		if readErr != nil {
			return readErr
		}
		knownHosts = string(inventory)
		return nil
	}}
	if _, err := Run(context.Background(), opts, runner, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	sort.Strings(materialized)
	want := []string{"ceph-node-ssh", "ceph-node-ssh.pub"}
	if strings.Join(materialized, ",") != strings.Join(want, ",") {
		t.Fatalf("materialized %v, want %v", materialized, want)
	}
	contextKnownHosts := filepath.Join(filepath.Dir(opts.SecretsDir), "trust", "ssh", "known_hosts")
	if !strings.Contains(knownHosts, "UserKnownHostsFile="+contextKnownHosts) {
		t.Fatalf("per-task inventory lost the context-managed known_hosts %s:\n%s", contextKnownHosts, knownHosts)
	}
}

func TestSweepStaleRuntimeSecretsMatchesTheLiveTaskLayoutAndSkipsSweptRuns(t *testing.T) {
	baseDir := t.TempDir()
	state := storageSSHState()
	opts, taskRoot := taskRunOptions(t, baseDir, state, "bootwright.core.task_storage_cluster_apply")
	store := secretstore.NewContextStore("test", opts.SecretsDir)
	if err := store.Write(secretstore.MaterialKey{Name: "ceph-node-ssh", Role: secretstore.MaterialSSHPrivate}, []byte("private\n")); err != nil {
		t.Fatalf("write material: %v", err)
	}
	var liveSecretsDir string
	runner := &fakeRunner{onRun: func(spec ansible.RunSpec) error {
		liveSecretsDir = spec.ExtraVarPairs[3]
		return nil
	}}
	if _, err := Run(context.Background(), opts, runner, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	liveSecretsDir = strings.TrimPrefix(liveSecretsDir, "bootwright_secrets_dir=")
	swept := taskRuntimeSecretDirs(taskRoot)
	found := false
	for _, candidate := range swept {
		if candidate == liveSecretsDir {
			found = true
		}
	}
	if !found {
		t.Fatalf("sweep targets %v do not cover the live per-task runtime secrets dir %s", swept, liveSecretsDir)
	}

	if err := os.MkdirAll(liveSecretsDir, 0o700); err != nil {
		t.Fatalf("reseed stale plaintext: %v", err)
	}
	if err := os.WriteFile(filepath.Join(liveSecretsDir, "ceph-node-ssh"), []byte("plaintext\n"), 0o600); err != nil {
		t.Fatalf("write stale plaintext: %v", err)
	}
	stepSecrets := filepath.Join(taskRoot, taskStepsDirName, "install", stepSecretsDirName)
	stepConnection := filepath.Join(taskRoot, taskStepsDirName, "install", stepConnectionSecretsDirName)
	stepOutputs := filepath.Join(taskRoot, taskStepsDirName, "install", stepOutputsDirName)
	stepManifests := filepath.Join(taskRoot, taskStepsDirName, "install", stepManifestsDirName)
	graphSecrets := filepath.Join(opts.RunsDir, "history", "run-1", "runtime", runtimeSecretsDirName)
	keep := filepath.Join(taskRoot, taskRenderedDirName)
	for _, dir := range []string{stepSecrets, stepConnection, stepOutputs, stepManifests, graphSecrets, keep} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "material"), []byte("plaintext\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", dir, err)
		}
	}

	sweepStaleRuntimeSecrets(opts.RunsDir, "run-live")

	for _, gone := range []string{liveSecretsDir, stepSecrets, stepConnection, stepOutputs, stepManifests, graphSecrets} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Fatalf("sweep left plaintext at %s (err=%v): a step's captured outputs and rendered manifests carry the same credential material its secrets dir does — the external-ceph attach writes every shared cephx key into both — so a crashed run leaves them readable until the next destroy", gone, err)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("sweep removed non-secret task output: %v", err)
	}
	marker := filepath.Join(opts.RunsDir, "history", "run-1", runtimeSecretsSweptMarkerName)
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("sweep did not record a swept marker: %v", err)
	}

	if err := os.MkdirAll(liveSecretsDir, 0o700); err != nil {
		t.Fatalf("reseed after marker: %v", err)
	}
	if err := os.WriteFile(filepath.Join(liveSecretsDir, "ceph-node-ssh"), []byte("plaintext\n"), 0o600); err != nil {
		t.Fatalf("write after marker: %v", err)
	}
	sweepStaleRuntimeSecrets(opts.RunsDir, "run-live")
	if _, err := os.Stat(liveSecretsDir); err != nil {
		t.Fatalf("a marked run must be skipped entirely, but it was swept again: %v", err)
	}
}

func TestSweepStaleRuntimeSecretsLeavesTheLiveRunUnmarked(t *testing.T) {
	runsDir := t.TempDir()
	liveSecrets := filepath.Join(runsDir, "history", "run-live", runTaskDirName, "t1", taskArtifactsDirName, "runtime", runtimeSecretsDirName)
	if err := os.MkdirAll(liveSecrets, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	sweepStaleRuntimeSecrets(runsDir, "run-live")
	if _, err := os.Stat(liveSecrets); err != nil {
		t.Fatalf("live run secrets were swept: %v", err)
	}
	marker := filepath.Join(runsDir, "history", "run-live", runtimeSecretsSweptMarkerName)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("live run must not be marked swept (stat err=%v)", err)
	}
}
