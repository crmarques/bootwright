package lifecycle_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/managedos/physicalerase"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type waivingRunner struct {
	mutex sync.Mutex
	runs  []lifecycle.RunRequest
}

func (r *waivingRunner) Run(_ context.Context, run lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.runs = append(r.runs, run)
	evidence, err := json.Marshal(physicalerase.EraseEvidence{Version: physicalerase.EraseImplementation, Request: run.Digest, Prepared: true, Removed: true, Retained: true})
	return lifecycle.RunResult{Outcome: reconciliation.OutcomeChanged, Evidence: evidence}, err
}

type pinnedMachine struct{}

func (pinnedMachine) PinnedIdentity(string, []lifecycle.BlockEvidence) (machineref.HardwareIdentity, bool, error) {
	return machineref.HardwareIdentity{UUID: "example-system"}, true, nil
}

type installedSystem struct{}

func (installedSystem) Claimed(api.Catalog) []string { return nil }

func (installedSystem) Plan(context.Context, lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	return lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}, nil
}

func (installedSystem) Removal(_ context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	return lifecycle.Removal{Description: "remove the installation of " + block.Object, Impacts: []string{"unregister " + block.Object}}, nil
}

func (installedSystem) Apply(_ context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return lifecycle.Result{Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"request":"` + execution.Block.RequestDigest + `"}`)}, nil
}

func (installedSystem) Observe(context.Context, lifecycle.Execution) (lifecycle.Observation, error) {
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted}, nil
}

func (installedSystem) Quiescent(context.Context, lifecycle.Probe) (lifecycle.Quiescence, error) {
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "its Machine is stopped"}, nil
}

func (installedSystem) Destroy(context.Context, lifecycle.Execution) (lifecycle.Result, error) {
	return lifecycle.Result{Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"removed":true}`)}, nil
}

func (installedSystem) ObserveRemoval(context.Context, lifecycle.Execution) (lifecycle.Observation, error) {
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted}, nil
}

func waivableErase(t *testing.T) (*lifecycle.CapabilityJourney, *waivingRunner, string) {
	t.Helper()
	data, err := os.ReadFile("../../managedos/physicalerase/testdata/erase-request-physical.golden")
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	var erase physicalerase.EraseRequest
	if err := json.Unmarshal(data, &erase); err != nil {
		t.Fatal(err)
	}
	install := erase.Install
	private := *install.Private
	private.Path, private.URL = strings.TrimSuffix(private.Path, "-erase"), strings.TrimSuffix(private.URL, "-erase")
	install.Private = &private
	installed, err := install.Canonical()
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	machine := install.Identity.Object
	installing := reconciliation.BlockDefinition{ID: installation.BlockID(machine), Description: "install the operating system of " + machine, Stage: reconciliation.StageMachines,
		Dependencies: []string{physicalerase.EraseBlockID(machine)}, Impacts: []string{"install-operating-system " + machine}, Consumes: []string{},
		Kind: installation.Kind, Object: machine, Implementation: installation.Implementation, ContentDigest: installation.ContentDigest(), Request: installed}
	erase.InstallDigest, err = reconciliation.RequestDigest(installing)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	erased, err := erase.Canonical()
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	definitions := []reconciliation.BlockDefinition{
		{ID: "machine-" + machine, Description: "create the Machine " + machine, Stage: reconciliation.StageMachines, Impacts: []string{"create-machine " + machine}, Consumes: []string{},
			Kind: string(api.Machine), Object: machine, Implementation: fleetMachineImplementation, ContentDigest: strings.Repeat("c", 64), Request: json.RawMessage(`{"name":"` + machine + `"}`)},
		{ID: physicalerase.EraseBlockID(machine), Description: "retain the verified physical erase policy of " + machine, Stage: reconciliation.StageMachines, Dependencies: []string{"machine-" + machine},
			Consumes: []string{}, Kind: installation.Kind, Object: machine, Implementation: physicalerase.EraseImplementation, ContentDigest: physicalerase.EraseContentDigest(), Request: erased},
		installing,
	}
	runner := &waivingRunner{}
	journey := lifecycle.NewBoundJourney(t, []lifecycle.BoundCapability{
		{Binding: lifecycle.CapabilityBinding{Kind: string(api.Machine), Implementation: fleetMachineImplementation}, Capability: &fleetMachines{}},
		{Binding: lifecycle.CapabilityBinding{Kind: installation.Kind, Implementation: physicalerase.EraseImplementation}, Capability: physicalerase.New(runner).WithIdentities(pinnedMachine{})},
		{Binding: lifecycle.CapabilityBinding{Kind: installation.Kind, Implementation: installation.Implementation}, Capability: installedSystem{}},
	}, lifecycle.CapabilityPlan{Definitions: definitions, Secrets: erase.SecretReferences()})
	holdInstallationSecrets(journey, install)
	return journey, runner, machine
}

func TestAPlannedWaiverOfAPhysicalEraseDestroysWithoutBootingTheWorker(t *testing.T) {
	t.Parallel()
	journey, runner, machine := waivableErase(t)
	ctx := context.Background()
	applied, err := journey.Service.Apply(ctx, lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true})
	if err != nil || applied.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("the apply = %+v (%v)", applied, diagnostics.Of(err))
	}
	relaxations := map[string]string{machine: lifecycle.EraseRetain}
	declared, err := journey.Service.Plan(ctx, lifecycle.PlanRequest{ContextName: lifecycle.JourneyContext})
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	preview, err := journey.Service.Plan(ctx, lifecycle.PlanRequest{ContextName: lifecycle.JourneyContext, Relaxations: relaxations})
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	erase := physicalerase.EraseBlockID(machine)
	index := slices.IndexFunc(preview.Steps, func(step lifecycle.PlanStep) bool { return step.ID == erase })
	if index < 0 {
		t.Fatalf("the preview holds no step %s: %+v", erase, preview.Steps)
	}
	step := preview.Steps[index]
	if step.Description != "retain the declared physical disks of "+machine+" without erasing them; power and media state unproved" || !slices.Equal(step.Impacts, []string{
		"retain-disks " + machine + " (erase waived; data, installed OS and Ceph host state remain)",
		"unproved-power-and-media " + machine + " (power and media state unproved; an earlier removal's erase worker may still be running)",
	}) || !slices.Equal(step.Consumes, []string{reconciliation.AuthorizationDataLoss}) {
		t.Fatalf("the waived step is %+v", step)
	}
	digest := waiverDigest(t, preview.Authorizations)
	if digest == waiverDigest(t, declared.Authorizations) {
		t.Fatal("the data-loss digest does not bind the waiver")
	}
	result, err := journey.Service.Destroy(ctx, lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true, Relaxations: relaxations,
		Authorizations: []string{reconciliation.AuthorizationDataLoss + "=" + digest}})
	if err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("the waived destroy = %+v (%v)", result, diagnostics.Of(err))
	}
	runner.mutex.Lock()
	runs := slices.Clone(runner.runs)
	runner.mutex.Unlock()
	if len(runs) != 1 || runs[0].Operation != "destroy" || runs[0].MaterialValues["relaxationBase64"] != base64.StdEncoding.EncodeToString([]byte(lifecycle.EraseRetain)) {
		t.Fatalf("the waived destroy ran %+v", runs)
	}
	if _, pinned := runs[0].MaterialValues["pinnedUUIDBase64"]; pinned || slices.ContainsFunc(runs[0].Materials, func(file lifecycle.MaterialFile) bool {
		return strings.HasPrefix(file.Variable, "controller") || file.Variable == "fleetIdentity"
	}) {
		t.Fatalf("the waiver handed the adapter the controller or the Machine: %v %+v", runs[0].MaterialValues, runs[0].Materials)
	}
	records := journey.Records(t, erase)
	if len(records) == 0 || !strings.Contains(string(records[len(records)-1].Evidence), `"retained":true`) {
		t.Fatalf("the waived erase recorded %+v", records)
	}
}

func waiverDigest(t *testing.T, tokens []string) string {
	t.Helper()
	for _, token := range tokens {
		if digest, found := strings.CutPrefix(token, reconciliation.AuthorizationDataLoss+"="); found {
			return digest
		}
	}
	t.Fatalf("the preview names no data-loss digest: %v", tokens)
	return ""
}
