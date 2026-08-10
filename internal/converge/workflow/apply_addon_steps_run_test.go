package workflow

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionrecords "github.com/crmarques/bootwright/internal/addons/records"
	"github.com/crmarques/bootwright/internal/addons/steps"
	"github.com/crmarques/bootwright/internal/converge/ansible"
)

func TestWriteStepInventoryPinsHostKeysAndKeepsConnectionsAlive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inventory.yaml")
	targets := []stepSSHTarget{{
		label:          "Machine/bastion",
		inventoryName:  "step_0",
		address:        "bastion.example.test",
		user:           "admin",
		keyPath:        "/runs/connection-secrets/bastion-ssh",
		knownHostsPath: "/context/trust/ssh/known_hosts",
	}}
	if err := writeStepInventory(path, targets, "", ""); err != nil {
		t.Fatalf("writeStepInventory: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read step inventory: %v", err)
	}
	rendered := string(data)
	for _, want := range []string{
		"BatchMode=yes",
		"StrictHostKeyChecking=yes",
		"UserKnownHostsFile=/context/trust/ssh/known_hosts",
		"ServerAliveInterval=15",
		"ServerAliveCountMax=3",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("step inventory missing %q; the inventory variable fully replaces ansible.cfg ssh_common_args:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "accept-new") {
		t.Fatalf("step inventory must not downgrade host-key verification to accept-new:\n%s", rendered)
	}
}

func TestStepInventoryScopesTheSSHUserOverrideToOperatorIdentity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inventory.yaml")
	targets := []stepSSHTarget{
		{label: "bastion", inventoryName: "step_0", address: "bastion.example.test", user: "admin"},
		{label: "lab", inventoryName: "step_1", address: "lab.example.test", user: "admin", operatorIdentity: true},
		{label: "arbiter", inventoryName: "step_2", address: "arbiter.example.test", user: "cephadm", userPinned: true, operatorIdentity: true},
	}
	if err := writeStepInventory(path, targets, "", "operator"); err != nil {
		t.Fatalf("writeStepInventory: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read step inventory: %v", err)
	}
	body := string(data)
	if strings.Count(body, "ansible_user: admin") != 1 {
		t.Fatalf("a login named by desired state must survive --ssh-user: %s", body)
	}
	if strings.Count(body, "ansible_user: operator") != 1 {
		t.Fatalf("only the operatorIdentity target takes the override: %s", body)
	}
	if strings.Count(body, "ansible_user: cephadm") != 1 {
		t.Fatalf("a pinned orchestration login must survive --ssh-user even on operatorIdentity machines: %s", body)
	}
}

type addonStepSequenceRunner struct {
	errors []error
	specs  []ansible.RunSpec
}

func (r *addonStepSequenceRunner) Run(_ context.Context, spec ansible.RunSpec) error {
	r.specs = append(r.specs, spec)
	index := len(r.specs) - 1
	if index >= len(r.errors) {
		return nil
	}
	return r.errors[index]
}

func (r *addonStepSequenceRunner) Command(ansible.RunSpec) []string {
	return []string{"ansible-playbook"}
}

func testAddonStepExecutor(t *testing.T, runner ansible.Runner) *addonStepExecutor {
	t.Helper()
	dir := t.TempDir()
	return &addonStepExecutor{
		stdout: io.Discard,
		stderr: io.Discard,
		opts: RunOptions{
			BundleDir: dir,
		},
		plan: extensionPlanView{
			Name: "data-foundation",
			Addon: v1alpha1.ClusterAddon{
				SourcePath: filepath.Join(dir, "addon.yaml"),
			},
		},
		runnerFactory: func(io.Writer, io.Writer) ansible.Runner {
			return runner
		},
	}
}

func testAddonStepTargets(count int) []stepSSHTarget {
	targets := make([]stepSSHTarget, 0, count)
	for i := 0; i < count; i++ {
		targets = append(targets, stepSSHTarget{
			label:         string(rune('a' + i)),
			inventoryName: "step_" + string(rune('0'+i)),
		})
	}
	return targets
}

func runTestAddonStep(t *testing.T, runner ansible.Runner, step v1alpha1.ClusterAddonStep, targetCount int) error {
	t.Helper()
	executor := testAddonStepExecutor(t, runner)
	return executor.runStepAnsible(
		context.Background(),
		step,
		filepath.Join(t.TempDir(), "inventory.yaml"),
		filepath.Join(t.TempDir(), "vars.yaml"),
		t.TempDir(),
		testAddonStepTargets(targetCount),
		nil,
		10*time.Second,
	)
}

func TestFirstReachableRetriesOnlyTypedInitialUnreachable(t *testing.T) {
	runner := &addonStepSequenceRunner{errors: []error{
		&ansible.UnreachableError{Err: errors.New("no route to host")},
		nil,
	}}
	err := runTestAddonStep(t, runner, v1alpha1.ClusterAddonStep{Name: "export", Playbook: "playbooks/export.yaml"}, 3)
	if err != nil {
		t.Fatalf("runStepAnsible: %v", err)
	}
	if len(runner.specs) != 2 {
		t.Fatalf("runs = %d, want 2", len(runner.specs))
	}
	for i, spec := range runner.specs {
		if !spec.ClassifyUnreachable {
			t.Fatalf("run %d did not request unreachable classification", i)
		}
		if want := "step_" + string(rune('0'+i)); spec.Limit != want {
			t.Fatalf("run %d limit = %q, want %q", i, spec.Limit, want)
		}
	}
}

func TestFirstReachableStopsAfterReachableTaskFailure(t *testing.T) {
	runner := &addonStepSequenceRunner{errors: []error{
		errors.New("ceph exporter rejected the key"),
		nil,
	}}
	err := runTestAddonStep(t, runner, v1alpha1.ClusterAddonStep{Name: "export", Playbook: "playbooks/export.yaml"}, 3)
	if err == nil {
		t.Fatal("runStepAnsible succeeded")
	}
	if len(runner.specs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runner.specs))
	}
	for _, want := range []string{"without a definitive pre-mutation unreachable result", "refusing to retry", "may have changed state", "ceph exporter rejected the key"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err, want)
		}
	}
}

func TestFirstReachableStopsAfterLaterReachableTaskFailure(t *testing.T) {
	runner := &addonStepSequenceRunner{errors: []error{
		&ansible.UnreachableError{Err: errors.New("host a unreachable")},
		errors.New("host b task failed after mutation"),
		nil,
	}}
	err := runTestAddonStep(t, runner, v1alpha1.ClusterAddonStep{Name: "export", Playbook: "playbooks/export.yaml"}, 3)
	if err == nil {
		t.Fatal("runStepAnsible succeeded")
	}
	if len(runner.specs) != 2 {
		t.Fatalf("runs = %d, want 2", len(runner.specs))
	}
	if !strings.Contains(err.Error(), "target b") || !strings.Contains(err.Error(), "refusing to retry") {
		t.Fatalf("error = %q", err)
	}
}

func TestFirstReachableFailsAfterEveryTargetIsInitiallyUnreachable(t *testing.T) {
	runner := &addonStepSequenceRunner{errors: []error{
		&ansible.UnreachableError{Err: errors.New("host a unreachable")},
		&ansible.UnreachableError{Err: errors.New("host b unreachable")},
	}}
	err := runTestAddonStep(t, runner, v1alpha1.ClusterAddonStep{Name: "export", Playbook: "playbooks/export.yaml"}, 2)
	if err == nil {
		t.Fatal("runStepAnsible succeeded")
	}
	if len(runner.specs) != 2 {
		t.Fatalf("runs = %d, want 2", len(runner.specs))
	}
	if !strings.Contains(err.Error(), "could not reach any target") {
		t.Fatalf("error = %q", err)
	}
}

func TestAllTargetLimitDoesNotClassifyOrRetry(t *testing.T) {
	runner := &addonStepSequenceRunner{errors: []error{errors.New("task failed")}}
	err := runTestAddonStep(t, runner, v1alpha1.ClusterAddonStep{
		Name:     "export",
		Playbook: "playbooks/export.yaml",
		Target:   v1alpha1.ClusterAddonStepTarget{Limit: v1alpha1.ClusterAddonStepTargetLimitAll},
	}, 3)
	if err == nil {
		t.Fatal("runStepAnsible succeeded")
	}
	if len(runner.specs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runner.specs))
	}
	if runner.specs[0].ClassifyUnreachable {
		t.Fatal("all-target run requested firstReachable classification")
	}
	if runner.specs[0].Limit != "" {
		t.Fatalf("all-target limit = %q, want empty", runner.specs[0].Limit)
	}
}

func TestNormalizeSHA256StepOutput(t *testing.T) {
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	output := v1alpha1.ClusterAddonStepOutput{Format: v1alpha1.ClusterAddonStepOutputFormatSHA256}
	for _, input := range []string{digest, digest + "\n"} {
		got, err := normalizeStepOutput(output, []byte(input))
		if err != nil {
			t.Fatalf("normalizeStepOutput(%q): %v", input, err)
		}
		if string(got) != digest {
			t.Fatalf("normalizeStepOutput(%q) = %q, want %q", input, got, digest)
		}
	}
	for _, input := range []string{
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"sha256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		digest + "\n\n",
		digest + " ",
	} {
		if _, err := normalizeStepOutput(output, []byte(input)); err == nil {
			t.Fatalf("normalizeStepOutput(%q) accepted a non-canonical digest", input)
		}
	}
	output.Secret = true
	if _, err := normalizeStepOutput(output, []byte(digest)); err == nil {
		t.Fatal("normalizeStepOutput accepted secret sha256 evidence")
	}
}

type observedDigestStepRunner struct {
	digest     string
	omitDigest bool
	omitSecret bool
	timeout    bool
}

func (r observedDigestStepRunner) Run(ctx context.Context, spec ansible.RunSpec) error {
	var outputsDir string
	for _, pair := range spec.ExtraVarPairs {
		if strings.HasPrefix(pair, "bootwright_step_outputs_dir=") {
			outputsDir = strings.TrimPrefix(pair, "bootwright_step_outputs_dir=")
		}
	}
	if outputsDir == "" {
		return errors.New("step outputs directory was not provided")
	}
	if err := os.MkdirAll(outputsDir, 0o700); err != nil {
		return err
	}
	if !r.omitSecret {
		if err := os.WriteFile(filepath.Join(outputsDir, "external-details.json"), []byte("[]\n"), 0o600); err != nil {
			return err
		}
	}
	if !r.omitDigest {
		if err := os.WriteFile(filepath.Join(outputsDir, "exporter-script.sha256"), []byte(r.digest), 0o600); err != nil {
			return err
		}
	}
	if r.timeout {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

func (observedDigestStepRunner) Command(ansible.RunSpec) []string {
	return []string{"ansible-playbook"}
}

func observedDigestStepExecutor(t *testing.T, runner ansible.Runner) (*addonStepExecutor, v1alpha1.ClusterAddonStep) {
	t.Helper()
	dir := t.TempDir()
	playbook := filepath.Join(dir, "playbooks", "export.yaml")
	if err := os.MkdirAll(filepath.Dir(playbook), 0o700); err != nil {
		t.Fatalf("mkdir playbooks: %v", err)
	}
	if err := os.WriteFile(playbook, []byte("- hosts: all\n  tasks: []\n"), 0o600); err != nil {
		t.Fatalf("write playbook: %v", err)
	}
	state := addonStepStorageTargetState()
	state.Machines[0].Spec.Addresses = []v1alpha1.MachineAddress{{Name: "ssh", Address: "192.0.2.10"}}
	state.Machines[0].Spec.Access.SSH.AddressRef = v1alpha1.LocalObjectReference{Name: "ssh"}
	state.StorageExports = []v1alpha1.StorageExport{{
		Metadata: v1alpha1.Metadata{Name: "external-storage"},
		Spec: v1alpha1.StorageExportSpec{
			StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
		},
	}}
	step := v1alpha1.ClusterAddonStep{
		Name:     "attach",
		Follows:  v1alpha1.ClusterAddonStepFollowsOperatorReady,
		Playbook: "playbooks/export.yaml",
		Target: v1alpha1.ClusterAddonStepTarget{
			FromInput: &v1alpha1.ClusterAddonStepInputTarget{Input: "external-storage"},
		},
		Outputs: []v1alpha1.ClusterAddonStepOutput{
			{Name: "externalDetails", File: "external-details.json", Secret: true, Format: v1alpha1.ClusterAddonStepOutputFormatJSON},
			{Name: "exporterScript", File: "exporter-script.sha256", Format: v1alpha1.ClusterAddonStepOutputFormatSHA256},
		},
	}
	addon := v1alpha1.ClusterAddon{
		Metadata:   v1alpha1.Metadata{Name: "data-foundation"},
		SourcePath: filepath.Join(dir, "add-on.yaml"),
		Spec: v1alpha1.ClusterAddonSpec{
			Accepts: v1alpha1.ClusterAddonAccepts{Inputs: []v1alpha1.ClusterAddonAcceptedInput{{
				Name:        "external-storage",
				ResourceRef: &v1alpha1.ClusterAddonInputRef{Kind: v1alpha1.KindStorageExport},
			}}},
			Steps: []v1alpha1.ClusterAddonStep{step},
		},
	}
	clustersDir := t.TempDir()
	executor := &addonStepExecutor{
		stdout:     io.Discard,
		stderr:     io.Discard,
		runsDir:    t.TempDir(),
		runID:      "apply-test",
		taskID:     "addon.ocp.data-foundation",
		kubeconfig: filepath.Join(t.TempDir(), "kubeconfig"),
		opts: RunOptions{
			BundleDir:   t.TempDir(),
			ClustersDir: clustersDir,
			SecretsDir:  t.TempDir(),
		},
		state:         state,
		plan:          extensionPlanView{Name: "data-foundation", Cluster: "ocp", Addon: addon},
		runnerFactory: func(io.Writer, io.Writer) ansible.Runner { return runner },
		stepResources: newAddonStepResourcePool(),
		inputs:        []v1alpha1.ClusterAddonBindingInput{{Name: "external-storage", Value: "external-storage"}},
	}
	return executor, step
}

func TestRunStepPersistsObservedDigestOnSuccess(t *testing.T) {
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	executor, step := observedDigestStepExecutor(t, observedDigestStepRunner{digest: digest + "\n"})
	if _, _, err := executor.runStep(context.Background(), step); err != nil {
		t.Fatalf("runStep: %v", err)
	}
	assertObservedStepDigest(t, executor, step, extensionrecords.RecordStatusReady, digest)
}

func TestRunStepPersistsAvailableObservedDigestOnTimeout(t *testing.T) {
	digest := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	executor, step := observedDigestStepExecutor(t, observedDigestStepRunner{digest: digest + "\n", timeout: true})
	step.Timeout = "5ms"
	_, failureRecordAttempted, err := executor.runStep(context.Background(), step)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("runStep error = %v, want timeout", err)
	}
	if !failureRecordAttempted {
		t.Fatal("runStep did not attempt its failure record while holding the storage resource")
	}
	assertObservedStepDigest(t, executor, step, extensionrecords.RecordStatusFailed, digest)
}

func TestRunStepPersistsObservedDigestWhenAnotherOutputIsMissing(t *testing.T) {
	digest := "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	executor, step := observedDigestStepExecutor(t, observedDigestStepRunner{digest: digest + "\n", omitSecret: true})
	_, failureRecordAttempted, err := executor.runStep(context.Background(), step)
	if err == nil || !strings.Contains(err.Error(), "externalDetails") {
		t.Fatalf("runStep error = %v, want missing externalDetails output", err)
	}
	if !failureRecordAttempted {
		t.Fatal("runStep did not attempt its failed record after output capture failed")
	}
	assertObservedStepDigest(t, executor, step, extensionrecords.RecordStatusFailed, digest)
}

func TestRunStepRejectsMalformedObservedDigestBeforeReady(t *testing.T) {
	for _, tc := range []struct {
		name       string
		runner     observedDigestStepRunner
		wantDetail string
	}{
		{name: "malformed", runner: observedDigestStepRunner{digest: "sha256:ABCDEF"}, wantDetail: "64 lowercase hexadecimal"},
		{name: "missing", runner: observedDigestStepRunner{omitDigest: true}, wantDetail: "did not produce declared output"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			executor, step := observedDigestStepExecutor(t, tc.runner)
			_, failureRecordAttempted, err := executor.runStep(context.Background(), step)
			if err == nil || !strings.Contains(err.Error(), tc.wantDetail) {
				t.Fatalf("runStep error = %v, want %q", err, tc.wantDetail)
			}
			if !failureRecordAttempted {
				t.Fatal("runStep did not write a failed record for invalid observed evidence")
			}
			record, found, loadErr := extensionrecords.LoadRecord(executor.opts.ClustersDir, executor.plan.Cluster, executor.plan.Name)
			if loadErr != nil || !found {
				t.Fatalf("LoadRecord found=%t err=%v", found, loadErr)
			}
			if record.Steps[step.Name].Status != extensionrecords.RecordStatusFailed {
				t.Fatalf("step status = %s, want failed", record.Steps[step.Name].Status)
			}
			if len(record.Steps[step.Name].ObservedDigests) != 0 {
				t.Fatalf("invalid evidence persisted as %v", record.Steps[step.Name].ObservedDigests)
			}
			secretPath := steps.OutputPath(executor.opts.ClustersDir, executor.plan.Cluster, executor.plan.Name, step.Name, step.Outputs[0])
			if _, statErr := os.Stat(secretPath); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("secret output survived failed evidence validation at %s: %v", secretPath, statErr)
			}
		})
	}
}

func assertObservedStepDigest(t *testing.T, executor *addonStepExecutor, step v1alpha1.ClusterAddonStep, status extensionrecords.RecordStatus, digest string) {
	t.Helper()
	record, found, err := extensionrecords.LoadRecord(executor.opts.ClustersDir, executor.plan.Cluster, executor.plan.Name)
	if err != nil || !found {
		t.Fatalf("LoadRecord found=%t err=%v", found, err)
	}
	got := record.Steps[step.Name]
	if got.Status != status {
		t.Fatalf("step status = %s, want %s", got.Status, status)
	}
	if got.ObservedDigests["exporterScript"] != digest {
		t.Fatalf("observed digest = %q, want %q", got.ObservedDigests["exporterScript"], digest)
	}
	wantStepDigest, digestErr := executor.stepDigest(step)
	if digestErr != nil {
		t.Fatalf("stepDigest: %v", digestErr)
	}
	if got.Digest != wantStepDigest {
		t.Fatalf("recorded static digest = %q, want %q", got.Digest, wantStepDigest)
	}
	output := step.Outputs[1]
	path := steps.OutputPath(executor.opts.ClustersDir, executor.plan.Cluster, executor.plan.Name, step.Name, output)
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read persisted digest output: %v", readErr)
	}
	if string(data) != digest {
		t.Fatalf("persisted digest output = %q, want normalized %q", data, digest)
	}
	if slices.Contains([]byte(data), byte('\n')) {
		t.Fatalf("persisted digest output retained a trailing newline: %q", data)
	}
}
