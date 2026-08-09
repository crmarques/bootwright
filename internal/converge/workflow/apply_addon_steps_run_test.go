package workflow

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
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
