package workflow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/roles"
	"go.yaml.in/yaml/v3"
)

type destroyInputGateRunner struct {
	entered     chan ansible.RunSpec
	release     chan struct{}
	releaseOnce sync.Once
	hosts       map[string]string
}

func newDestroyInputGateRunner(hosts map[string]string) *destroyInputGateRunner {
	return &destroyInputGateRunner{
		entered: make(chan ansible.RunSpec, len(hosts)),
		release: make(chan struct{}),
		hosts:   hosts,
	}
}

func (r *destroyInputGateRunner) Run(ctx context.Context, spec ansible.RunSpec) error {
	select {
	case r.entered <- spec:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-r.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	host, ok := r.hosts[spec.Playbook]
	if !ok {
		return fmt.Errorf("no completion host for %s", spec.Playbook)
	}
	return writeDestroyInputCompletionProof(spec.ArtifactsDir, host)
}

func (*destroyInputGateRunner) Command(ansible.RunSpec) []string {
	return []string{"ansible-playbook"}
}

func (r *destroyInputGateRunner) releaseAll() {
	r.releaseOnce.Do(func() { close(r.release) })
}

func writeDestroyInputCompletionProof(artifactsDir, host string) error {
	if err := os.MkdirAll(artifactsDir, 0o700); err != nil {
		return err
	}
	body := fmt.Sprintf("{\"host\":%q,\"status\":\"ok\",\"completion\":true}\n", host) +
		fmt.Sprintf("{\"schemaVersion\":1,\"status\":\"terminal\",\"processedHosts\":[%q],\"hosts\":{%q:{\"ok\":1,\"failed\":0,\"skipped\":0,\"unreachable\":0,\"completed\":1}}}\n", host, host)
	return os.WriteFile(filepath.Join(artifactsDir, ansible.RunResultName), []byte(body), 0o600)
}

func destroyInputCacheTestOptions(t *testing.T, baseDir string, state v1alpha1.State) RunOptions {
	t.Helper()
	opts := schedulerRunOptions(baseDir)
	opts.State = state
	opts.ContextName = "test"
	opts.OwnershipDir = filepath.Join(baseDir, "ownership")
	prepared, err := render.All(opts.RenderedDir, opts.ClustersDir, opts.SecretsDir, state)
	if err != nil {
		t.Fatalf("prepare immutable render: %v", err)
	}
	opts.PreparedRunRender = prepared
	opts.CacheDestroyRunInputs = true
	opts.DestroyRunInputCounters = &DestroyRunInputCounters{}
	return opts
}

func saveDestroyInputOwnership(t *testing.T, dir, kind, name, host string) {
	t.Helper()
	if err := ownership.SaveResource(dir, ownership.ResourceRecord{
		Kind: kind, Name: name, Context: "test", Host: host,
	}); err != nil {
		t.Fatalf("save ownership %s/%s: %v", kind, name, err)
	}
}

func destroyInputProviderTask(id string, state v1alpha1.State) ApplyTask {
	return ApplyTask{
		Entry:               TaskLedgerEntry{ID: id, Kind: DestroyTaskKindProviderServices, Label: "provider"},
		Playbook:            roles.PlaybookTaskProviderServicesDestroy,
		Limit:               render.GroupProviderHosts,
		CompletionHostLimit: render.GroupProviderHosts,
		State:               state,
	}
}

func destroyInputStorageClusterNames(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read vars %s: %v", path, err)
	}
	var vars struct {
		Clusters []struct {
			Name string `yaml:"name"`
		} `yaml:"bootwright_storage_clusters"`
	}
	if err := yaml.Unmarshal(data, &vars); err != nil {
		t.Fatalf("parse vars %s: %v", path, err)
	}
	names := make([]string, 0, len(vars.Clusters))
	for _, cluster := range vars.Clusters {
		names = append(names, cluster.Name)
	}
	return names
}

func seedDestroyInputRuntime(t *testing.T, runsDir, runID string) (string, string) {
	t.Helper()
	root := filepath.Join(runsDir, "history", runID, "runtime")
	marker := filepath.Join(root, "seeded-plaintext")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("create seeded runtime: %v", err)
	}
	if err := os.WriteFile(marker, []byte("sensitive\n"), 0o600); err != nil {
		t.Fatalf("seed runtime marker: %v", err)
	}
	return root, marker
}

func TestDestroyGraphCachedNoHostsDoesNoTaskPreparation(t *testing.T) {
	baseDir := t.TempDir()
	state := minimalState()
	opts := destroyInputCacheTestOptions(t, baseDir, state)
	task := destroyInputProviderTask("destroy.empty-provider", state)
	prepared, err := PrepareDestroyTaskGraph(opts.RunsDir, opts, []ApplyTask{task}, ConcurrencyLimits{Parallelism: 1})
	if err != nil {
		t.Fatalf("PrepareDestroyTaskGraph: %v", err)
	}
	runner := &fakeRunner{}
	ledger, err := RunPreparedDestroyTaskGraph(context.Background(), io.Discard, io.Discard, opts.RunsDir, opts, ApplyTarget{Name: "empty destroy"}, "", prepared, nil, func(io.Writer, io.Writer) ansible.Runner {
		return runner
	})
	if err != nil {
		t.Fatalf("RunPreparedDestroyTaskGraph: %v", err)
	}
	entry, found := ledger.Task(task.Entry.ID)
	if !found || entry.Status != TaskStatusSkipped {
		t.Fatalf("empty task ledger entry = %+v, found=%t", entry, found)
	}
	if runner.runCalled || runner.lastSpec.Inventory != "" {
		t.Fatalf("no-host task reached runner: called=%t spec=%+v", runner.runCalled, runner.lastSpec)
	}
	wantCounts := DestroyRunInputCounts{OwnershipLoads: 1}
	if got := opts.DestroyRunInputCounters.Counts(); got != wantCounts {
		t.Fatalf("destroy input counts = %+v, want %+v", got, wantCounts)
	}
	taskRoot := filepath.Join(opts.RunsDir, "history", prepared.RunID, "tasks", task.Entry.ID)
	for _, path := range []string{
		filepath.Join(opts.RunsDir, "history", prepared.RunID, "inputs"),
		filepath.Join(opts.RunsDir, "history", prepared.RunID, "runtime"),
		filepath.Join(taskRoot, taskRenderedDirName),
		filepath.Join(taskRoot, taskArtifactsDirName),
	} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("no-host task created %s: %v", path, statErr)
		}
	}
}

func TestDestroyRunSpecUsesTaskInputsAndImmutableAssetRoot(t *testing.T) {
	baseDir := t.TempDir()
	fullState := destroyStorageFanOutState(map[string][]string{
		"ceph-a": {"a0"},
		"ceph-b": {"b0"},
	})
	for i := range fullState.Machines {
		fullState.Machines[i].Spec.OS.Provided = v1alpha1.BoolPtr(true)
		fullState.Machines[i].Spec.Addresses = []v1alpha1.MachineAddress{{Name: "ssh", Address: fmt.Sprintf("192.0.2.%d", i+10)}}
		fullState.Machines[i].Spec.Access.SSH = &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}}
	}
	opts := destroyInputCacheTestOptions(t, baseDir, fullState)
	if got := destroyInputStorageClusterNames(t, opts.PreparedRunRender.VarsPath); !slices.Equal(got, []string{"ceph-a", "ceph-b"}) {
		t.Fatalf("immutable vars storage clusters = %v", got)
	}
	var immutableAsset string
	for _, asset := range opts.PreparedRunRender.StorageAssets {
		if asset.StorageClusterName == "ceph-a" {
			immutableAsset = asset.BootstrapSpecPath
			break
		}
	}
	if immutableAsset == "" {
		t.Fatal("immutable render has no ceph-a storage asset")
	}
	sentinel := []byte("immutable full-run storage asset\n")
	if err := os.WriteFile(immutableAsset, sentinel, 0o600); err != nil {
		t.Fatalf("seed immutable storage asset: %v", err)
	}
	preparedInventory, err := os.ReadFile(opts.PreparedRunRender.InventoryPath)
	if err != nil {
		t.Fatalf("read immutable inventory: %v", err)
	}
	if strings.Contains(string(preparedInventory), "provider-overlay") {
		t.Fatal("immutable inventory unexpectedly contains later ownership")
	}
	saveDestroyInputOwnership(t, opts.OwnershipDir, string(ownership.KindBMCEmulator), "provider-overlay", "provider-overlay")
	inputs, err := newDestroyRunInputs(opts.RunsDir, "destroy-task-overlay", opts)
	if err != nil {
		t.Fatalf("newDestroyRunInputs: %v", err)
	}
	t.Cleanup(func() {
		if err := inputs.close(); err != nil {
			t.Errorf("close destroy inputs: %v", err)
		}
	})
	taskState := fullState
	taskState.StorageClusters = append([]v1alpha1.StorageCluster(nil), fullState.StorageClusters[:1]...)
	taskState.Machines = append([]v1alpha1.Machine(nil), fullState.Machines[:1]...)
	taskRenderDir := filepath.Join(opts.RunsDir, "history", "destroy-task-overlay", "tasks", "storage", taskRenderedDirName)
	opts.State = taskState
	opts.destroyRunInputs = inputs
	opts.RenderDir = taskRenderDir
	opts.ArtifactsRoot = filepath.Join(filepath.Dir(taskRenderDir), taskArtifactsDirName)
	opts.Playbook = roles.PlaybookTaskStorageClusterDestroy
	opts.Limit = render.GroupStorageHosts
	opts.ExtraVarPairs = []string{"bootwright_test_task=storage-overlay"}
	runner := &fakeRunner{}
	result, err := Run(context.Background(), opts, runner, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !runner.runCalled {
		t.Fatal("task overlay did not reach runner")
	}
	spec := runner.lastSpec
	if spec.Inventory == opts.PreparedRunRender.InventoryPath || spec.ExtraVars == opts.PreparedRunRender.VarsPath {
		t.Fatalf("RunSpec reused immutable inventory/vars: %+v", spec)
	}
	inputRoot := filepath.Dir(filepath.Dir(spec.Inventory))
	wantInputPrefix := filepath.Join(opts.RunsDir, "history", "destroy-task-overlay", "inputs") + string(os.PathSeparator)
	if !strings.HasPrefix(inputRoot+string(os.PathSeparator), wantInputPrefix) || spec.ExtraVars != filepath.Join(inputRoot, "ansible", "vars.yaml") {
		t.Fatalf("RunSpec task input paths inventory=%q vars=%q", spec.Inventory, spec.ExtraVars)
	}
	if got := destroyInputStorageClusterNames(t, spec.ExtraVars); !slices.Equal(got, []string{"ceph-a"}) {
		t.Fatalf("task vars storage clusters = %v", got)
	}
	taskInventory, err := os.ReadFile(spec.Inventory)
	if err != nil {
		t.Fatalf("read task inventory: %v", err)
	}
	if !strings.Contains(string(taskInventory), "provider-overlay") {
		t.Fatalf("task inventory lacks current ownership:\n%s", taskInventory)
	}
	wantRenderedDir, err := filepath.Abs(opts.RenderedDir)
	if err != nil {
		t.Fatalf("absolute immutable rendered dir: %v", err)
	}
	if got := extraVarValue(spec.ExtraVarPairs, "bootwright_rendered_dir"); got != wantRenderedDir {
		t.Fatalf("bootwright_rendered_dir = %q, want immutable asset root %q", got, wantRenderedDir)
	}
	if !slices.Contains(spec.ExtraVarPairs, "bootwright_test_task=storage-overlay") || spec.Playbook != roles.PlaybookTaskStorageClusterDestroy || spec.Limit != render.GroupStorageHosts {
		t.Fatalf("RunSpec task overlay = %+v", spec)
	}
	if len(result.Render.StorageAssets) != 1 || result.Render.StorageAssets[0].BootstrapSpecPath != immutableAsset {
		t.Fatalf("task storage assets = %+v, want immutable %s", result.Render.StorageAssets, immutableAsset)
	}
	if got, err := os.ReadFile(immutableAsset); err != nil || !slices.Equal(got, sentinel) {
		t.Fatalf("immutable storage asset changed: content=%q err=%v", got, err)
	}
	for _, root := range []string{taskRenderDir, inputRoot} {
		if _, statErr := os.Stat(filepath.Join(root, "storage")); !os.IsNotExist(statErr) {
			t.Fatalf("task run rewrote storage under %s: %v", root, statErr)
		}
	}
}

func TestDestroyGraphRemovesRuntimeAfterRunnerFailure(t *testing.T) {
	baseDir := t.TempDir()
	state := minimalState()
	opts := destroyInputCacheTestOptions(t, baseDir, state)
	saveDestroyInputOwnership(t, opts.OwnershipDir, string(ownership.KindBMCEmulator), "provider-service", "provider-a")
	task := destroyInputProviderTask("destroy.runner-failure", state)
	prepared, err := PrepareDestroyTaskGraph(opts.RunsDir, opts, []ApplyTask{task}, ConcurrencyLimits{Parallelism: 1})
	if err != nil {
		t.Fatalf("PrepareDestroyTaskGraph: %v", err)
	}
	runtimeRoot, marker := seedDestroyInputRuntime(t, opts.RunsDir, prepared.RunID)
	runnerErr := errors.New("runner failed after materialization")
	runtimeObserved := false
	runner := &fakeRunner{
		runReturns: runnerErr,
		onRun: func(spec ansible.RunSpec) error {
			if _, err := os.Stat(marker); err != nil {
				return fmt.Errorf("seeded runtime marker before runner return: %w", err)
			}
			secretsDir := extraVarValue(spec.ExtraVarPairs, "bootwright_secrets_dir")
			if info, err := os.Stat(secretsDir); err != nil || !info.IsDir() {
				return fmt.Errorf("shared runtime secrets unavailable: info=%v err=%v", info, err)
			}
			runtimeObserved = true
			return nil
		},
	}
	_, err = RunPreparedDestroyTaskGraph(context.Background(), io.Discard, io.Discard, opts.RunsDir, opts, ApplyTarget{Name: "runner failure"}, "", prepared, nil, func(io.Writer, io.Writer) ansible.Runner {
		return runner
	})
	if !errors.Is(err, runnerErr) {
		t.Fatalf("RunPreparedDestroyTaskGraph error = %v, want %v", err, runnerErr)
	}
	if !runtimeObserved {
		t.Fatal("runner did not observe materialized graph runtime")
	}
	if _, statErr := os.Stat(runtimeRoot); !os.IsNotExist(statErr) {
		t.Fatalf("runner failure retained graph runtime %s: %v", runtimeRoot, statErr)
	}
	wantCounts := DestroyRunInputCounts{Renders: 1, OwnershipLoads: 1, SecretMaterializations: 1, RunnerLaunches: 1}
	if got := opts.DestroyRunInputCounters.Counts(); got != wantCounts {
		t.Fatalf("destroy input counts = %+v, want %+v", got, wantCounts)
	}
}

func TestDestroyGraphRemovesRuntimeAfterPreRunFailure(t *testing.T) {
	baseDir := t.TempDir()
	state := storageSSHState()
	opts := destroyInputCacheTestOptions(t, baseDir, state)
	if err := os.MkdirAll(opts.SecretsDir, 0o700); err != nil {
		t.Fatalf("create context secrets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(opts.SecretsDir, "ceph-node-ssh"), []byte("plaintext private key\n"), 0o600); err != nil {
		t.Fatalf("seed blocked plaintext secret: %v", err)
	}
	saveDestroyInputOwnership(t, opts.OwnershipDir, string(ownership.KindBMCEmulator), "provider-service", "provider-a")
	task := destroyInputProviderTask("destroy.pre-run-failure", state)
	prepared, err := PrepareDestroyTaskGraph(opts.RunsDir, opts, []ApplyTask{task}, ConcurrencyLimits{Parallelism: 1})
	if err != nil {
		t.Fatalf("PrepareDestroyTaskGraph: %v", err)
	}
	runtimeRoot, _ := seedDestroyInputRuntime(t, opts.RunsDir, prepared.RunID)
	runner := &fakeRunner{}
	_, err = RunPreparedDestroyTaskGraph(context.Background(), io.Discard, io.Discard, opts.RunsDir, opts, ApplyTarget{Name: "pre-run failure"}, "", prepared, nil, func(io.Writer, io.Writer) ansible.Runner {
		return runner
	})
	if err == nil || (!strings.Contains(err.Error(), "plaintext") && !strings.Contains(err.Error(), "not encrypted")) {
		t.Fatalf("RunPreparedDestroyTaskGraph error = %v, want plaintext pre-run refusal", err)
	}
	if runner.runCalled || runner.lastSpec.Inventory != "" {
		t.Fatalf("pre-run failure reached runner: called=%t spec=%+v", runner.runCalled, runner.lastSpec)
	}
	if _, statErr := os.Stat(runtimeRoot); !os.IsNotExist(statErr) {
		t.Fatalf("pre-run failure retained graph runtime %s: %v", runtimeRoot, statErr)
	}
	wantCounts := DestroyRunInputCounts{Renders: 1, OwnershipLoads: 1, SecretMaterializations: 1}
	if got := opts.DestroyRunInputCounters.Counts(); got != wantCounts {
		t.Fatalf("destroy input counts = %+v, want %+v", got, wantCounts)
	}
}

func TestDestroyGraphSharesPreparedInputsWithoutSerializingRunners(t *testing.T) {
	baseDir := t.TempDir()
	state := minimalState()
	opts := destroyInputCacheTestOptions(t, baseDir, state)
	saveDestroyInputOwnership(t, opts.OwnershipDir, string(ownership.KindBMCEmulator), "provider-service", "provider-a")
	saveDestroyInputOwnership(t, opts.OwnershipDir, string(ownership.KindInfraComponent), "infra-service", "infra-a")
	tasks := []ApplyTask{
		{
			Entry:               TaskLedgerEntry{ID: "destroy.provider-a", Kind: DestroyTaskKindProviderServices, Label: "provider"},
			Playbook:            roles.PlaybookTaskProviderServicesDestroy,
			Limit:               render.GroupProviderHosts,
			CompletionHostLimit: render.GroupProviderHosts,
			ExtraVarPairs:       []string{"bootwright_test_task=provider"},
			State:               state,
		},
		{
			Entry:               TaskLedgerEntry{ID: "destroy.infra-a", Kind: DestroyTaskKindInfraComponents, Label: "infra"},
			Playbook:            roles.PlaybookTaskInfraComponentServicesDestroy,
			Limit:               render.GroupInfraComponentHosts,
			CompletionHostLimit: render.GroupInfraComponentHosts,
			ExtraVarPairs:       []string{"bootwright_test_task=infra"},
			State:               state,
		},
	}
	prepared, err := PrepareDestroyTaskGraph(opts.RunsDir, opts, tasks, ConcurrencyLimits{Parallelism: 2})
	if err != nil {
		t.Fatalf("PrepareDestroyTaskGraph: %v", err)
	}
	runner := newDestroyInputGateRunner(map[string]string{
		roles.PlaybookTaskProviderServicesDestroy:       "provider-a",
		roles.PlaybookTaskInfraComponentServicesDestroy: "infra-a",
	})
	t.Cleanup(runner.releaseAll)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	type graphResult struct {
		ledger RunLedger
		err    error
	}
	done := make(chan graphResult, 1)
	go func() {
		ledger, runErr := RunPreparedDestroyTaskGraph(ctx, io.Discard, io.Discard, opts.RunsDir, opts, ApplyTarget{Name: "test destroy"}, "", prepared, nil, func(io.Writer, io.Writer) ansible.Runner {
			return runner
		})
		done <- graphResult{ledger: ledger, err: runErr}
	}()
	specs := map[string]ansible.RunSpec{}
	for len(specs) < len(tasks) {
		select {
		case spec := <-runner.entered:
			specs[spec.Playbook] = spec
		case <-ctx.Done():
			t.Fatalf("parallel runners did not both enter: %v", ctx.Err())
		}
	}
	providerSpec := specs[roles.PlaybookTaskProviderServicesDestroy]
	infraSpec := specs[roles.PlaybookTaskInfraComponentServicesDestroy]
	if providerSpec.Inventory == "" || providerSpec.Inventory != infraSpec.Inventory {
		t.Fatalf("shared inventory paths = %q, %q", providerSpec.Inventory, infraSpec.Inventory)
	}
	if providerSpec.ExtraVars == "" || providerSpec.ExtraVars != infraSpec.ExtraVars {
		t.Fatalf("shared vars paths = %q, %q", providerSpec.ExtraVars, infraSpec.ExtraVars)
	}
	if providerSpec.Limit != render.GroupProviderHosts || infraSpec.Limit != render.GroupInfraComponentHosts {
		t.Fatalf("task limits = %q, %q", providerSpec.Limit, infraSpec.Limit)
	}
	if !slices.Contains(providerSpec.ExtraVarPairs, "bootwright_test_task=provider") || !slices.Contains(infraSpec.ExtraVarPairs, "bootwright_test_task=infra") {
		t.Fatalf("task overlays = %v, %v", providerSpec.ExtraVarPairs, infraSpec.ExtraVarPairs)
	}
	if providerSpec.ArtifactsDir == infraSpec.ArtifactsDir {
		t.Fatalf("parallel tasks shared artifacts dir %s", providerSpec.ArtifactsDir)
	}
	for _, path := range []string{providerSpec.Inventory, providerSpec.ExtraVars} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("prepared task input %s: %v", path, err)
		}
	}
	runner.releaseAll()
	var result graphResult
	select {
	case result = <-done:
	case <-ctx.Done():
		t.Fatalf("destroy graph did not finish: %v", ctx.Err())
	}
	if result.err != nil {
		t.Fatalf("RunPreparedDestroyTaskGraph: %v", result.err)
	}
	if result.ledger.Status != RunStatusOK {
		t.Fatalf("ledger status = %s, want %s", result.ledger.Status, RunStatusOK)
	}
	wantCounts := DestroyRunInputCounts{
		Renders:                1,
		OwnershipLoads:         1,
		SecretMaterializations: 1,
		RunnerLaunches:         2,
	}
	if got := opts.DestroyRunInputCounters.Counts(); got != wantCounts {
		t.Fatalf("destroy input counts = %+v, want %+v", got, wantCounts)
	}
	if _, err := os.Stat(filepath.Join(opts.RunsDir, "history", prepared.RunID, "runtime")); !os.IsNotExist(err) {
		t.Fatalf("destroy graph runtime remains after completion: %v", err)
	}
}

func TestDestroyRunInputsFreezeOwnershipAndInvalidateAfterRunnerReturn(t *testing.T) {
	for _, tc := range []struct {
		name   string
		runErr error
	}{
		{name: "success"},
		{name: "failure", runErr: errors.New("runner failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			baseDir := t.TempDir()
			state := minimalState()
			opts := destroyInputCacheTestOptions(t, baseDir, state)
			saveDestroyInputOwnership(t, opts.OwnershipDir, string(ownership.KindBMCEmulator), "provider-service", "provider-old")
			inputs, err := newDestroyRunInputs(opts.RunsDir, "destroy-frozen-view", opts)
			if err != nil {
				t.Fatalf("newDestroyRunInputs: %v", err)
			}
			t.Cleanup(func() {
				if err := inputs.close(); err != nil {
					t.Errorf("close destroy inputs: %v", err)
				}
			})
			opts.destroyRunInputs = inputs
			opts.Playbook = roles.PlaybookTaskProviderServicesDestroy
			opts.Limit = render.GroupProviderHosts
			opts.ArtifactsRoot = filepath.Join(baseDir, "artifacts")
			task := ApplyTask{
				Entry:               TaskLedgerEntry{ID: "destroy.provider-service", Kind: DestroyTaskKindProviderServices, ResourceKeys: []string{"provider-service"}},
				Playbook:            opts.Playbook,
				Limit:               opts.Limit,
				CompletionHostLimit: render.GroupProviderHosts,
				State:               state,
			}
			if err := refreshDestroyTaskOwnershipSnapshot(&opts); err != nil {
				t.Fatalf("freeze task ownership: %v", err)
			}
			saveDestroyInputOwnership(t, opts.OwnershipDir, string(ownership.KindBMCEmulator), "provider-service", "provider-new")
			expectedHosts, err := expectedNonStorageDestroyHosts(opts, task)
			if err != nil {
				t.Fatalf("expected hosts: %v", err)
			}
			if !slices.Equal(expectedHosts, []string{"provider-old"}) {
				t.Fatalf("expected hosts = %v, want frozen provider-old", expectedHosts)
			}
			runner := &fakeRunner{
				runReturns: tc.runErr,
				onRun: func(spec ansible.RunSpec) error {
					data, err := os.ReadFile(spec.Inventory)
					if err != nil {
						return err
					}
					inventory := string(data)
					if !strings.Contains(inventory, "provider-old") || strings.Contains(inventory, "provider-new") {
						return fmt.Errorf("inventory did not use frozen ownership:\n%s", inventory)
					}
					return nil
				},
			}
			_, runErr := Run(context.Background(), opts, runner, nil)
			if tc.runErr == nil && runErr != nil {
				t.Fatalf("Run: %v", runErr)
			}
			if tc.runErr != nil && !errors.Is(runErr, tc.runErr) {
				t.Fatalf("Run error = %v, want %v", runErr, tc.runErr)
			}
			current, err := inputs.ownershipSnapshot()
			if err != nil {
				t.Fatalf("reload ownership after runner return: %v", err)
			}
			if len(current) != 1 || current[0].Host != "provider-new" {
				t.Fatalf("current ownership after runner return = %+v", current)
			}
			wantCounts := DestroyRunInputCounts{
				Renders:                1,
				OwnershipLoads:         2,
				SecretMaterializations: 1,
				RunnerLaunches:         1,
			}
			if got := opts.DestroyRunInputCounters.Counts(); got != wantCounts {
				t.Fatalf("destroy input counts = %+v, want %+v", got, wantCounts)
			}
		})
	}
}

func TestDestroyTaskKubeVirtHostClustersUsesExactTaskScope(t *testing.T) {
	state := twoKubeVirtHostPlanningState()
	base := ApplyTask{
		Entry:    TaskLedgerEntry{ID: "destroy.machine-infra", Kind: DestroyTaskKindMachineInfra},
		Playbook: roles.PlaybookTaskMachineInfraDestroy,
		State:    state,
	}
	cases := []struct {
		name      string
		configure func(*ApplyTask)
		want      []string
	}{
		{
			name: "first machine dependency",
			configure: func(task *ApplyTask) {
				task.Entry.ResourceKeys = []string{DestroyMachineResourceKeyPrefix + "child-master-0"}
			},
			want: []string{"metal-ocp"},
		},
		{
			name: "second cluster dependency",
			configure: func(task *ApplyTask) {
				task.Entry.ResourceKeys = []string{"child-ocp-2"}
			},
			want: []string{"metal-ocp-2"},
		},
		{
			name: "explicit machine overlay",
			configure: func(task *ApplyTask) {
				task.ExtraVarPairs = []string{DestroyMachineScopeExtraVar + "=child-master-1,child-master-0"}
			},
			want: []string{"metal-ocp", "metal-ocp-2"},
		},
		{
			name: "records only",
			configure: func(task *ApplyTask) {
				task.Entry.ResourceKeys = []string{DestroyMachineResourceKeyPrefix + "child-master-0"}
				task.ExtraVarPairs = []string{MachineInfraRecordsOnlyExtraVar + "=true"}
			},
		},
		{
			name: "unscoped fallback",
			configure: func(*ApplyTask) {
			},
			want: []string{"metal-ocp", "metal-ocp-2"},
		},
		{
			name: "unknown machine fallback",
			configure: func(task *ApplyTask) {
				task.Entry.ResourceKeys = []string{DestroyMachineResourceKeyPrefix + "future-record-only-shape"}
			},
			want: []string{"metal-ocp", "metal-ocp-2"},
		},
		{
			name: "unrelated playbook",
			configure: func(task *ApplyTask) {
				task.Playbook = roles.PlaybookTaskProviderServicesDestroy
				task.Entry.ResourceKeys = []string{DestroyMachineResourceKeyPrefix + "child-master-0"}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := base
			task.Entry.ResourceKeys = nil
			task.ExtraVarPairs = nil
			tc.configure(&task)
			if got := destroyTaskKubeVirtHostClusters(task); !slices.Equal(got, tc.want) {
				t.Fatalf("host clusters = %v, want %v", got, tc.want)
			}
		})
	}
	opts := RunOptions{
		State:                       state,
		Playbook:                    roles.PlaybookTaskMachineInfraDestroy,
		UseKubeVirtHostClusterScope: true,
		KubeVirtHostClusters:        []string{"metal-ocp-2", " metal-ocp ", "metal-ocp-2", ""},
	}
	if got := kubeVirtHostClustersForRun(opts); !slices.Equal(got, []string{"metal-ocp", "metal-ocp-2"}) {
		t.Fatalf("explicit host cluster scope = %v", got)
	}
}
