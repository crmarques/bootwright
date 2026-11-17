package diskdiscovery

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/machine/diskselection"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type deadlineRunner struct {
	inventoryRunner
	deadline  time.Duration
	materials []lifecycle.MaterialFile
}

func (r *deadlineRunner) Run(ctx context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.deadline, r.materials = request.Deadline, request.Materials
	return r.inventoryRunner.Run(ctx, request)
}

func TestEveryCapabilityDeadlineCoversItsFrozenBudgets(t *testing.T) {
	ctx := context.Background()
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: testContext, Incarnation: "example-incarnation"}, State: compilation.NewState(discoveryCatalog(), discoveryCatalog(), nil), Controller: "controller"}
	var prepared []json.RawMessage
	runner := &deadlineRunner{inventoryRunner: inventoryRunner{t: t, prepared: &prepared, disks: []diskselection.Disk{inventoryDisk("EXAMPLE-OS", "0x5000000000000001", 1<<30)}}}
	owner := New(runner).WithNonces(func() (string, error) { return strings.Repeat("a", 64), nil }).WithMedia(labMedia()).WithIdentities(volumePins{machine.HardwareIdentity{UUID: "11111111-2222-3333-4444-555555555555"}})
	plan, err := owner.Plan(ctx, input)
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("plan: %v %v", plan, err)
	}
	definitions := slices.Clone(plan.Definitions)
	definitions[0].Requires = nil
	frozen, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	if err != nil {
		t.Fatal(err)
	}
	block := frozen.Blocks[0]
	request, err := DecodeDiscoveryRequest(block.Request)
	if err != nil {
		t.Fatal(err)
	}
	op := "op-" + strings.Repeat("a", 32)
	execution := lifecycle.Execution{Context: testContext, Incarnation: input.Context.Incarnation, Operation: op, ApplyOperation: op, Attempt: 1, Block: block, Prepare: func(_ context.Context, data json.RawMessage) error {
		prepared = append(prepared, slices.Clone(data))
		return nil
	}}
	if _, err := owner.Apply(ctx, execution); err != nil {
		t.Fatal(err)
	}
	if want := time.Duration(request.DeadlineSeconds)*time.Second + time.Hour; runner.deadline != want || runner.deadline > lifecycle.MaxDeadline {
		t.Fatalf("the run states %s, want the frozen %s plus an hour within %s", runner.deadline, time.Duration(request.DeadlineSeconds)*time.Second, lifecycle.MaxDeadline)
	}
	named := map[string]string{}
	for _, file := range runner.materials {
		named[file.Name] = file.Variable
	}
	if port := request.MachinePort; port.UserVariable == "" || named["bmc-user"] != port.UserVariable || named["bmc-password"] != port.PasswordVariable {
		t.Fatalf("the controller material %v is not named by the frozen machine port %+v", named, port)
	}
}
