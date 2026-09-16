package libvirt

import (
	"context"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/substrate"
)

// HostCapability realizes libvirt provider hosts: the hypervisor closure, the
// networks their attachments declare and their virtual-media pool.
type HostCapability struct{ runner Runner }

// MachineCapability realizes one virtual machine together with its own
// management controller, so a machine and the controller that manages it appear
// and disappear together.
type MachineCapability struct{ runner Runner }

func NewHost(runner Runner) HostCapability { return HostCapability{runner: runner} }

func NewMachine(runner Runner) MachineCapability { return MachineCapability{runner: runner} }

const (
	hostVariable    = "bootwright_substrate_host"
	machineVariable = "bootwright_substrate_machine"
)

// Plan derives one block per libvirt provider. It reads no host, endpoint or
// Secret material and performs no effect.
func (c HostCapability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.CapabilityPlan{}, err
	}
	if input.State == nil {
		return lifecycle.CapabilityPlan{}, refusal("lifecycle.state", "lifecycle planning requires compiled desired state", "")
	}
	requests, err := HostRequests(input.State.Effective(), input.Controller, input.Context.Name)
	if err != nil {
		return lifecycle.CapabilityPlan{}, err
	}
	plan := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}
	digest := HostContentDigest()
	for _, request := range requests {
		canonical, err := request.Canonical()
		if err != nil {
			return lifecycle.CapabilityPlan{}, err
		}
		plan.Definitions = append(plan.Definitions, reconciliation.BlockDefinition{
			ID:             request.Identity.Block,
			Description:    hostDescription(input.Verb, request),
			Stage:          reconciliation.StageSubstrates,
			Impacts:        hostImpacts(input.Verb, request),
			Groups:         hostGroups(input.Verb, request),
			Kind:           HostKind,
			Object:         request.Identity.Object,
			Implementation: HostImplementation,
			ContentDigest:  digest,
			Request:        canonical,
		})
		plan.Secrets = append(plan.Secrets, request.SecretReferences()...)
		if !request.Placement.Local() {
			continue
		}
		plan.Reservations = append(plan.Reservations, prerequisites.HostReservation{
			Context: input.Context.Name, Kind: "substrate-host", Service: request.Identity.Object, Keys: request.ReservationKeys(),
		})
	}
	slices.Sort(plan.Secrets)
	plan.Secrets = slices.Compact(plan.Secrets)
	return plan, nil
}

// hostDescription names what the planned verb does to this provider. A removal
// names the networks and pool alone, because the hypervisor closure an apply
// installed is shared host software this context never uninstalls.
func hostDescription(verb reconciliation.Verb, request HostRequest) string {
	if verb == reconciliation.Destroy {
		return "remove the networks and virtual-media pool of " + request.Identity.Object + " from " + request.Placement.Machine
	}
	return "realize the libvirt host of " + request.Identity.Object + " on " + request.Placement.Machine
}

func hostImpacts(verb reconciliation.Verb, request HostRequest) []string {
	path, pool, network, bridge := "create-path", "create-libvirt-pool", "create-libvirt-network", "create-bridge"
	if verb == reconciliation.Destroy {
		path, pool, network, bridge = "remove-path", "remove-libvirt-pool", "remove-libvirt-network", "remove-bridge"
	}
	impacts := []string{path + " " + request.PoolPath, pool + " " + request.PoolName}
	if request.Provisioned && verb != reconciliation.Destroy {
		for _, name := range request.Packages {
			impacts = append(impacts, "install-package "+name)
		}
	}
	for _, declared := range request.Networks {
		if declared.Managed {
			impacts = append(impacts, network+" "+declared.Name, bridge+" "+declared.Bridge)
		}
	}
	slices.Sort(impacts)
	return slices.Compact(impacts)
}

func hostGroups(verb reconciliation.Verb, request HostRequest) []reconciliation.Group {
	machines := []string{request.Placement.Machine}
	steps := [][2]string{
		{"install-hypervisor", "prove the hypervisor closure is present and active"},
		{"define-networks", "define every network the provider declares"},
		{"define-pool", "define the virtual-media pool"},
	}
	if verb == reconciliation.Destroy {
		steps = [][2]string{
			{"remove-networks", "destroy and undefine every owned network"},
			{"remove-pool", "destroy and undefine the virtual-media pool"},
			{"verify-absence", "verify every owned resource is gone"},
		}
	}
	return groupsFor(steps, machines)
}

// Plan derives one block per realized Machine. Each requires its provider host
// block by API object, so this capability never learns another's block grammar.
func (c MachineCapability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.CapabilityPlan{}, err
	}
	if input.State == nil {
		return lifecycle.CapabilityPlan{}, refusal("lifecycle.state", "lifecycle planning requires compiled desired state", "")
	}
	catalog := input.State.Effective()
	requests, err := MachineRequests(catalog, input.Controller, input.Context.Name)
	if err != nil {
		return lifecycle.CapabilityPlan{}, err
	}
	plan := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}
	digest := MachineContentDigest()
	for _, request := range requests {
		canonical, err := request.Canonical()
		if err != nil {
			return lifecycle.CapabilityPlan{}, err
		}
		machine, _ := catalog.Find(api.Machine, request.Identity.Object)
		definition := reconciliation.BlockDefinition{
			ID:             request.Identity.Block,
			Description:    machineDescription(input.Verb, request),
			Stage:          reconciliation.StageMachines,
			Requires:       []reconciliation.ObjectRef{{Kind: HostKind, Object: machine.Spec().Get("substrate", "providerRef").Text()}},
			Impacts:        machineImpacts(input.Verb, request),
			Groups:         machineGroups(input.Verb, request),
			Kind:           MachineKind,
			Object:         request.Identity.Object,
			Implementation: MachineImplementation,
			ContentDigest:  digest,
			Request:        canonical,
		}
		// The disks this block deletes may hold an installed operating system,
		// so its removal is acknowledged before the plan registers.
		if input.Verb == reconciliation.Destroy {
			definition.Consumes = []string{reconciliation.AuthorizationDataLoss}
		}
		plan.Definitions = append(plan.Definitions, definition)
		plan.Secrets = append(plan.Secrets, request.SecretReferences()...)
		if !request.Placement.Local() {
			continue
		}
		plan.Reservations = append(plan.Reservations, prerequisites.HostReservation{
			Context: input.Context.Name, Kind: "substrate-machine", Service: request.Identity.Object, Keys: request.ReservationKeys(),
		})
	}
	slices.Sort(plan.Secrets)
	plan.Secrets = slices.Compact(plan.Secrets)
	return plan, nil
}

func machineDescription(verb reconciliation.Verb, request MachineRequest) string {
	if verb == reconciliation.Destroy {
		return "remove the virtual machine " + request.Identity.Object + " and its controller"
	}
	return "realize the virtual machine " + request.Identity.Object + " and its controller"
}

func machineImpacts(verb reconciliation.Verb, request MachineRequest) []string {
	domain, unit, path, listener, disk :=
		"create-libvirt-domain", "create-container-unit", "create-path", "open-listener", "create-disk"
	if verb == reconciliation.Destroy {
		domain, unit, path, listener, disk =
			"remove-libvirt-domain", "remove-container-unit", "remove-path", "close-listener", "remove-disk"
	}
	impacts := []string{
		domain + " " + request.Domain,
		unit + " " + request.Controller.Unit,
		path + " " + request.Directory,
		listener + " " + request.Controller.Address + ":" + substrate.FormatPort(request.Controller.Port),
	}
	for _, declared := range request.Disks {
		impacts = append(impacts, disk+" "+declared.Path)
	}
	slices.Sort(impacts)
	return slices.Compact(impacts)
}

func machineGroups(verb reconciliation.Verb, request MachineRequest) []reconciliation.Group {
	machines := []string{request.Identity.Object}
	steps := [][2]string{
		{"create-disks", "create every disk the profile declares"},
		{"define-domain", "define the virtual machine"},
		{"start-controller", "start the machine's management controller"},
		{"verify-controller", "verify the controller answers for this machine"},
	}
	if verb == reconciliation.Destroy {
		steps = [][2]string{
			{"stop-controller", "stop and remove the management controller"},
			{"remove-domain", "force the machine off and undefine it"},
			{"remove-disks", "delete every owned disk"},
			{"verify-absence", "verify every owned resource is gone"},
		}
	}
	return groupsFor(steps, machines)
}

func groupsFor(steps [][2]string, machines []string) []reconciliation.Group {
	out := make([]reconciliation.Group, 0, len(steps))
	for _, step := range steps {
		out = append(out, reconciliation.Group{ID: step[0], Description: step[1], Machines: machines})
	}
	return out
}

func (c HostCapability) Apply(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return c.mutate(ctx, execution, "apply")
}

func (c HostCapability) Destroy(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return c.mutate(ctx, execution, "destroy")
}

func (c HostCapability) mutate(ctx context.Context, execution lifecycle.Execution, operation string) (lifecycle.Result, error) {
	unknown := lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}
	request, err := c.prepare(ctx, execution)
	if err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	result, err := c.run(ctx, execution, operation, request)
	if err != nil {
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
	}
	outcome, err := usableOutcome(result, "provider host")
	if err != nil {
		return unknown, err
	}
	digest := execution.Block.RequestDigest
	if operation == "apply" {
		err = ValidateHostPresence(result.Evidence, request, digest)
	} else {
		err = ValidateHostAbsence(result.Evidence, digest)
	}
	if err != nil {
		return unknown, err
	}
	return lifecycle.Result{Outcome: outcome, Evidence: result.Evidence}, nil
}

// Observe is read-only against the frozen request. Live state matching it in
// full is positive completion, nothing present is positive no effect, and
// anything partial or contradictory stays unknown.
func (c HostCapability) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	unknown := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	request, err := c.prepare(ctx, execution)
	if err != nil {
		return unknown, err
	}
	result, err := c.run(ctx, execution, "observe", request)
	if err != nil {
		recordObservationFailure(ctx, execution, err)
		return unknown, nil
	}
	digest := execution.Block.RequestDigest
	if ValidateHostPresence(result.Evidence, request, digest) == nil {
		return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: result.Evidence}, nil
	}
	if ValidateHostAbsence(result.Evidence, digest) == nil {
		return lifecycle.Observation{Effect: reconciliation.EffectNoEffect, Evidence: result.Evidence}, nil
	}
	return lifecycle.Observation{Effect: reconciliation.EffectUnknown, Evidence: result.Evidence}, nil
}

func (c HostCapability) prepare(ctx context.Context, execution lifecycle.Execution) (HostRequest, error) {
	if err := ctx.Err(); err != nil {
		return HostRequest{}, err
	}
	if c.runner == nil {
		return HostRequest{}, refusal("lifecycle.state", "the provider host adapter is not configured", "")
	}
	return DecodeHostRequest(execution.Block.Request)
}

func (c HostCapability) run(ctx context.Context, execution lifecycle.Execution, operation string, request HostRequest) (lifecycle.RunResult, error) {
	canonical, err := request.Canonical()
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	return c.runner.Run(ctx, invocation(execution, HostImplementation, hostVariable, operation, canonical, request.Placement, nil, nil))
}

func (c MachineCapability) Apply(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return c.mutate(ctx, execution, "apply")
}

func (c MachineCapability) Destroy(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return c.mutate(ctx, execution, "destroy")
}

func (c MachineCapability) mutate(ctx context.Context, execution lifecycle.Execution, operation string) (lifecycle.Result, error) {
	unknown := lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}
	request, err := c.prepare(ctx, execution)
	if err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	result, err := c.run(ctx, execution, operation, request)
	if err != nil {
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
	}
	outcome, err := usableOutcome(result, "machine")
	if err != nil {
		return unknown, err
	}
	digest := execution.Block.RequestDigest
	if operation == "apply" {
		err = ValidateMachinePresence(result.Evidence, request, digest)
	} else {
		err = ValidateMachineAbsence(result.Evidence, digest)
	}
	if err != nil {
		return unknown, err
	}
	return lifecycle.Result{Outcome: outcome, Evidence: result.Evidence}, nil
}

// Observe is read-only. Nothing present with no recorded before-state is
// positive no effect; anything partial stays unknown.
func (c MachineCapability) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	unknown := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	request, err := c.prepare(ctx, execution)
	if err != nil {
		return unknown, err
	}
	result, err := c.run(ctx, execution, "observe", request)
	if err != nil {
		recordObservationFailure(ctx, execution, err)
		return unknown, nil
	}
	digest := execution.Block.RequestDigest
	if ValidateMachinePresence(result.Evidence, request, digest) == nil {
		return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: result.Evidence}, nil
	}
	if ValidateMachineAbsence(result.Evidence, digest) == nil {
		return lifecycle.Observation{Effect: reconciliation.EffectNoEffect, Evidence: result.Evidence}, nil
	}
	return lifecycle.Observation{Effect: reconciliation.EffectUnknown, Evidence: result.Evidence}, nil
}

func (c MachineCapability) prepare(ctx context.Context, execution lifecycle.Execution) (MachineRequest, error) {
	if err := ctx.Err(); err != nil {
		return MachineRequest{}, err
	}
	if c.runner == nil {
		return MachineRequest{}, refusal("lifecycle.state", "the machine adapter is not configured", "")
	}
	return DecodeMachineRequest(execution.Block.Request)
}

func (c MachineCapability) run(ctx context.Context, execution lifecycle.Execution, operation string, request MachineRequest) (lifecycle.RunResult, error) {
	canonical, err := request.Canonical()
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	materials := []lifecycle.MaterialFile{
		{Name: "bmc-user", Part: secrets.UsernamePart, Secret: request.Controller.CredentialsRef, Variable: "controllerUser"},
		{Name: "bmc-password", Part: secrets.PasswordPart, Secret: request.Controller.CredentialsRef, Variable: "controllerPassword"},
	}
	return c.runner.Run(ctx, invocation(execution, MachineImplementation, machineVariable, operation, canonical, request.Placement, materials, nil))
}

// invocation is the one shape every substrate effect crosses the adapter
// boundary in. Bound material reaches the adapter only as operation-scoped
// files it removes.
func invocation(execution lifecycle.Execution, implementation, variable, operation string, canonical []byte, placement lifecycle.Placement, materials []lifecycle.MaterialFile, values map[string]string) lifecycle.RunRequest {
	return lifecycle.RunRequest{
		Implementation: implementation,
		Operation:      operation,
		Variable:       variable,
		Digest:         execution.Block.RequestDigest,
		Canonical:      canonical,
		Placement:      placement,
		Materials:      append(slices.Clone(materials), lifecycle.Materials(placement)...),
		MaterialValues: values,
		Sudo:           placement.SudoPasswordRef,
		Launch:         execution.Launch,
		Bundle:         execution.Bundle,
		Area:           execution.Area,
		Material:       execution.Material,
		Log:            execution.Log,
		Progress:       execution.Progress,
		Output:         execution.Output,
	}
}

func usableOutcome(result lifecycle.RunResult, subject string) (reconciliation.Outcome, error) {
	switch result.Outcome {
	case "changed":
		return reconciliation.OutcomeChanged, nil
	case "unchanged":
		return reconciliation.OutcomeUnchanged, nil
	}
	return "", refusal("lifecycle.state", "the "+subject+" adapter reported no usable outcome", "")
}

// recordObservationFailure keeps the reason a resolution could not observe,
// because without it a resolution loop reports only that it could not resolve.
func recordObservationFailure(ctx context.Context, execution lifecycle.Execution, err error) {
	if execution.Log == nil {
		return
	}
	for _, reported := range diagnostics.Of(err) {
		_ = execution.Log(ctx, operationstore.LogRecord{
			Event: "observation-failed", Block: execution.Block.ID, Detail: reported.Code + ": " + reported.Message,
		})
	}
}

// Unsupported names every selected object neither capability can realize, so
// the operation refuses before registration instead of part way through.
func (HostCapability) Unsupported(state *compilation.State) []string {
	if state == nil {
		return nil
	}
	return Unsupported(state.Effective())
}
