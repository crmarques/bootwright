package vsphere

import (
	"context"
	"slices"
	"strconv"
	"time"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/substrate"
)

const machineDeadline = 2 * time.Hour

type MachineCapability struct{ runner Runner }

func NewMachine(runner Runner) MachineCapability { return MachineCapability{runner: runner} }

func actions(verb reconciliation.Verb, r Request) lifecycle.Removal {
	steps := [][2]string{{"create-machine", "create the powered-off VM and its VMDKs"}, {"verify-machine", "verify VM identity, hardware and disk ownership"}}
	description := "create the vSphere VM " + r.Name + " in " + r.Folder
	impact := "create-virtual-machine "
	var consumes, retains []string
	if verb == reconciliation.Destroy {
		steps = [][2]string{{"remove-machine", "unregister the stopped VM and remove its recorded VMDKs"}, {"verify-absent", "verify the VM and recorded VMDKs are absent"}}
		description = "unregister the stopped vSphere VM " + r.Name + " and remove its recorded VMDKs while retaining its VM configuration, NVRAM and logs"
		retains = []string{"the VM home of " + r.Name + ", including its VM configuration, NVRAM and logs"}
		impact = "unregister-virtual-machine "
		consumes = []string{reconciliation.AuthorizationDataLoss}
	}
	impacts := []string{impact + r.Folder + "/" + r.Name}
	if verb == reconciliation.Destroy {
		impacts = append(impacts, "remove-recorded-vmdks "+r.Name, "retain-vm-home "+r.Name)
	}
	slices.Sort(impacts)
	return lifecycle.Removal{Description: description, Retains: retains, Impacts: impacts, Groups: lifecycle.StepGroups(steps, []string{r.Identity.Object}), Consumes: consumes}
}

func (c MachineCapability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.CapabilityPlan{}, err
	}
	if input.State == nil {
		return lifecycle.CapabilityPlan{}, invalid("no compiled state for vSphere Machine planning", "compile the selected environment")
	}
	requests, err := Requests(input.State.Effective(), input.Controller, input.Context.Name)
	if err != nil {
		return lifecycle.CapabilityPlan{}, err
	}
	plan := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}
	for _, r := range requests {
		if input.Preparation != nil && !slices.Contains(input.Preparation.Targets, r.Identity.Object) {
			continue
		}
		data, err := r.Canonical()
		if err != nil {
			return lifecycle.CapabilityPlan{}, err
		}
		a := actions(input.Verb, r)
		plan.Definitions = append(plan.Definitions, reconciliation.BlockDefinition{ID: r.Identity.Block, Kind: Kind, Object: r.Identity.Object, Family: substrate.MachineFamily, Implementation: Implementation, ContentDigest: ContentDigest(), Request: data, Stage: reconciliation.StageMachines,
			Description: a.Description, Groups: a.Groups, Impacts: a.Impacts, Consumes: a.Consumes})
		plan.Secrets = append(plan.Secrets, r.SecretReferences()...)
	}
	slices.Sort(plan.Secrets)
	plan.Secrets = slices.Compact(plan.Secrets)
	return plan, nil
}

func (c MachineCapability) Removal(ctx context.Context, b reconciliation.Block) (lifecycle.Removal, error) {
	r, err := DecodeRequest(b.Request)
	if err != nil {
		return lifecycle.Removal{}, err
	}
	return actions(reconciliation.Destroy, r), nil
}

func (MachineCapability) DeletesMachine(reconciliation.Block) bool { return true }

func (MachineCapability) RemovesHost(reconciliation.Block) bool { return true }

func (MachineCapability) ProbesQuiescence() bool { return true }

func (c MachineCapability) prepare(ctx context.Context, e lifecycle.Execution) (Request, error) {
	if err := ctx.Err(); err != nil {
		return Request{}, err
	}
	if c.runner == nil {
		return Request{}, invalid("vSphere Machine adapter is unavailable", "configure the Machine runner")
	}
	r, err := DecodeRequest(e.Block.Request)
	if err != nil {
		return Request{}, err
	}
	if r.Identity.Context != e.Context || r.Identity.Block != e.Block.ID || r.Identity.Object != e.Block.Object || !r.TLSVerify || r.Owner == "" {
		return Request{}, invalid("the frozen VM request does not belong to this context and block", "restore the original verified request")
	}
	return r, nil
}

func (c MachineCapability) run(ctx context.Context, e lifecycle.Execution, operation string, r Request) (lifecycle.RunResult, error) {
	if e.ApplyOperation == "" || operation != "observe" && e.ApplyRegistered == "" {
		return lifecycle.RunResult{}, unproved.Fail("the original VM creation operation is unavailable", "restore the original operation record")
	}
	data, err := r.Canonical()
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	materials := []lifecycle.MaterialFile{
		{Name: "vcenter-user", Secret: r.CredentialsRef, Part: secrets.UsernamePart, Variable: "controllerUser"},
		{Name: "vcenter-password", Secret: r.CredentialsRef, Part: secrets.PasswordPart, Variable: "controllerPassword"},
	}
	if r.TrustBundleRef != "" {
		materials = append(materials, lifecycle.MaterialFile{Name: "vcenter-ca", Secret: r.TrustBundleRef, Part: secrets.CertificatePart, Variable: "controllerCA"})
	}
	values := map[string]string{"pinned": "{}", "operation": e.ApplyOperation, "registered": e.ApplyRegistered, "block": e.Block.ID}
	var held Evidence
	if len(e.PreviousEvidence) > 0 && string(e.PreviousEvidence) != "null" {
		held, err = decode(e.PreviousEvidence, e.Block.RequestDigest)
		if err != nil {
			return lifecycle.RunResult{}, err
		}
		if held.Absent {
			if err := validateAbsence(e.PreviousEvidence, e.Block.RequestDigest); err != nil {
				return lifecycle.RunResult{}, err
			}
		} else if err := validateOwned(e.PreviousEvidence, r, e.Block.RequestDigest, held.phase()); err != nil {
			return lifecycle.RunResult{}, err
		}
		if held.Owned && held.Owner != r.Owner+":"+e.ApplyOperation {
			return lifecycle.RunResult{}, unproved.Fail("the VM receipt belongs to another operation", "restore the original VM receipt")
		}
		values["pinned"] = string(e.PreviousEvidence)
	}
	if operation == "apply" && held.Retiring {
		return lifecycle.RunResult{}, unproved.Fail("the VM has already started retiring its recorded disks", "continue the original destroy operation")
	}
	if operation == "destroy" && (!held.Owned || held.phase() == creating) {
		return lifecycle.RunResult{}, unproved.Fail("removal has no recorded owned VM and VMDK set", "resolve the original Machine apply first")
	}
	result, err := c.runner.Run(ctx, lifecycle.RunFor(e, lifecycle.Invocation{Implementation: Implementation, Operation: operation, Variable: "bootwright_substrate_vsphere", Canonical: data, Placement: r.Placement, Materials: materials, Values: values, Deadline: machineDeadline}))
	if err != nil {
		return result, err
	}
	observed, err := decode(result.Evidence, e.Block.RequestDigest)
	if err != nil {
		return result, err
	}
	if err := sameReceipt(r, held, observed, r.Owner+":"+e.ApplyOperation); err != nil {
		return lifecycle.RunResult{}, err
	}
	return result, nil
}

func sameReceipt(r Request, held, observed Evidence, owner string) error {
	changed := observed.Retiring && (!held.Owned || held.phase() == creating) || held.VCenterUUID != "" && held.VCenterUUID != observed.VCenterUUID ||
		held.VMHome != "" && held.VMHome != observed.VMHome ||
		held.StagingDatastoreRef != "" && held.StagingDatastoreRef != observed.StagingDatastoreRef
	if observed.Owned {
		disksMatch := slices.Equal(held.Disks, observed.Disks)
		if held.partialCloneRoot(r) && len(observed.Disks) == 1+len(r.DataDisks) && !observed.Retiring {
			disksMatch = len(observed.Disks) > 0 && observed.Disks[0] == held.Disks[0]
		}
		changed = changed || observed.Owner != owner || held.Owned && (held.MoRef != observed.MoRef || held.InstanceUUID != observed.InstanceUUID || !disksMatch)
	}
	if changed {
		return unproved.Fail("the live VM or disks differ from the original Machine receipt", "restore the registered vCenter, VM and VMDKs")
	}
	return nil
}

func (c MachineCapability) mutation() lifecycle.Mutation[Request] {
	return lifecycle.Mutation[Request]{
		Prepare: c.prepare, Run: c.run,
		Unusable: unproved.Fail("the VM adapter returned no usable outcome", "restore the vCenter connection"),
		Reading: func(r Request, digest string) lifecycle.EffectReading {
			presence := func(data []byte) error { return ValidatePresence(data, r, digest) }
			return lifecycle.EffectReading{
				Presence: presence, Unremoved: presence,
				Partial:    func(data []byte) error { return validateOwned(data, r, digest, creating) },
				Unfinished: func(data []byte) error { return validateOwned(data, r, digest, retiring) },
				Absence:    func(data []byte) error { return validateAbsence(data, digest) },
			}
		},
	}
}

func (c MachineCapability) Apply(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	return lifecycle.MutateBlock(ctx, c.mutation(), e, "apply")
}

func (c MachineCapability) Destroy(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	result, err := lifecycle.MutateBlock(ctx, c.mutation(), e, "destroy")
	if err != nil {
		return result, err
	}
	return result, reportRetainedHome(ctx, e, result.Evidence)
}

func reportRetainedHome(ctx context.Context, e lifecycle.Execution, data []byte) error {
	observed, err := decode(data, e.Block.RequestDigest)
	if err != nil {
		return err
	}
	if observed.VMHome == "" && e.Resolution > 0 {
		return nil
	}
	if !validVMHome(observed.VMHome) {
		return unproved.Fail("the destroy receipt does not identify the retained VM configuration", "restore the original VM receipt")
	}
	path := strconv.QuoteToASCII(observed.VMHome)
	if e.Progress != nil {
		e.Progress(ctx, "verify-absent", "ok", lifecycle.ByteProgress{Note: "Retained VM configuration " + path})
	}
	for e.Log != nil && len(path) > 0 {
		n := min(512, len(path))
		if err := e.Log(ctx, operationstore.LogRecord{Event: "retained-vm-home", Block: e.Block.ID, Detail: path[:n]}); err != nil {
			return err
		}
		path = path[n:]
	}
	return nil
}

func (c MachineCapability) Observe(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
	o, err := lifecycle.ObserveBlock(ctx, c.mutation(), e, reconciliation.Apply)
	if o.Effect == reconciliation.EffectCompleted {
		o.Outcome = reconciliation.OutcomeUnchanged
	}
	return o, err
}

func (c MachineCapability) ObserveRemoval(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
	if len(e.PreviousEvidence) == 0 {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, unproved.Fail("removal observation requires the original VM and disk receipts", "restore the original Machine apply record")
	}
	observed, err := lifecycle.ObserveBlock(ctx, c.mutation(), e, reconciliation.Destroy)
	if err == nil && observed.Effect == reconciliation.EffectCompleted && e.Resolution > 0 && e.Operation != e.ApplyOperation {
		err = reportRetainedHome(ctx, e, observed.Evidence)
	}
	return observed, err
}

func (c MachineCapability) StartPower(ctx context.Context, probe lifecycle.Probe) (lifecycle.Quiescence, error) {
	return c.Quiescent(ctx, probe)
}

func (c MachineCapability) StartPowerSecrets(b reconciliation.Block) ([]string, error) {
	return c.SecretReferences(b)
}

func (c MachineCapability) Quiescent(ctx context.Context, p lifecycle.Probe) (lifecycle.Quiescence, error) {
	e := p.Execution()
	r, err := c.prepare(ctx, e)
	if err != nil {
		return lifecycle.Quiescence{}, err
	}
	result, err := c.run(ctx, e, "observe", r)
	unknown := lifecycle.Quiescence{State: lifecycle.Unproved, Reason: "vCenter did not prove the VM's identity and power"}
	if err != nil {
		return unknown, ctx.Err()
	}
	if validateAbsence(result.Evidence, e.Block.RequestDigest) == nil {
		return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "vCenter reports the VM absent"}, nil
	}
	if ValidatePresence(result.Evidence, r, e.Block.RequestDigest) != nil {
		if validateOwned(result.Evidence, r, e.Block.RequestDigest, retiring) == nil {
			return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "the stopped VM has retired its owned disks"}, nil
		}
		return unknown, nil
	}
	obs, _ := decode(result.Evidence, e.Block.RequestDigest)
	if obs.Power == "poweredOff" {
		return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "the owned virtual machine is off"}, nil
	}
	return lifecycle.Quiescence{State: lifecycle.Live, Reason: "the virtual machine is " + obs.Power, Stop: "bootwright machine stop --context " + e.Context + " --name " + r.Identity.Object}, nil
}

func (MachineCapability) SecretReferences(b reconciliation.Block) ([]string, error) {
	r, err := DecodeRequest(b.Request)
	if err != nil {
		return nil, err
	}
	return r.SecretReferences(), nil
}

func (MachineCapability) SecretClasses(b reconciliation.Block) (secrets.Classes, error) {
	r, err := DecodeRequest(b.Request)
	if err != nil {
		return nil, err
	}
	return r.Placement.SecretClasses().Add(secrets.AccessCredential, r.CredentialsRef).Add(secrets.TrustAnchor, r.TrustBundleRef), nil
}

func (MachineCapability) ProveBinding(_ context.Context, name string, b reconciliation.Block, material map[string]secrets.Material) error {
	r, err := DecodeRequest(b.Request)
	if err != nil {
		return err
	}
	return r.Placement.ProveHostPin(name, material)
}
