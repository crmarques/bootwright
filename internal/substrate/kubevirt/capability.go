package kubevirt

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/substrate"
)

// HostCapability proves one KubeVirt provider's host cluster. It changes
// nothing there: the cluster, its namespace, storage and networks are the
// operator's, so its removal takes nothing back.
type HostCapability struct{ runner Runner }

// MachineCapability realizes one VirtualMachine and its root disk on the
// host cluster its provider proved.
type MachineCapability struct{ runner Runner }

func NewHost(runner Runner) HostCapability { return HostCapability{runner: runner} }

func NewMachine(runner Runner) MachineCapability { return MachineCapability{runner: runner} }

const (
	hostVariable    = "bootwright_substrate_kubevirt_host"
	machineVariable = "bootwright_substrate_kubevirt_machine"
	hostDeadline    = 10 * time.Minute
	machineDeadline = 20 * time.Minute
)

// Plan derives one proof block per KubeVirt provider. It reads no host,
// endpoint or Secret material and performs no effect.
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
			Family:         "substrate-host",
			Implementation: HostImplementation,
			ContentDigest:  digest,
			Request:        canonical,
		})
		plan.Secrets = append(plan.Secrets, request.SecretReferences()...)
	}
	slices.Sort(plan.Secrets)
	plan.Secrets = slices.Compact(plan.Secrets)
	return plan, nil
}

func hostDescription(verb reconciliation.Verb, request HostRequest) string {
	if verb == reconciliation.Destroy {
		return "release the proof of the host cluster of " + request.Identity.Object + " and leave the host as it is"
	}
	return "prove the host cluster of " + request.Identity.Object + " and its namespace " + request.Namespace
}

// hostImpacts lists nothing for a removal, which reads nothing and changes
// nothing, and the namespace the proof reads for an apply.
func hostImpacts(verb reconciliation.Verb, request HostRequest) []string {
	if verb == reconciliation.Destroy {
		return nil
	}
	return []string{"read-namespace " + request.Namespace}
}

func hostGroups(verb reconciliation.Verb, request HostRequest) []reconciliation.Group {
	steps := [][2]string{
		{"reach-host", "reach the host cluster's API through the provider's kubeconfig"},
		{"prove-host", "prove OpenShift Virtualization, the namespace, the root class, the upload proxy, access and networks"},
	}
	if verb == reconciliation.Destroy {
		steps = [][2]string{{"release-proof", "take nothing back; the host cluster is the operator's"}}
	}
	return groupsFor(steps, []string{request.Placement.Machine})
}

func groupsFor(steps [][2]string, machines []string) []reconciliation.Group {
	out := make([]reconciliation.Group, 0, len(steps))
	for _, step := range steps {
		out = append(out, reconciliation.Group{ID: step[0], Description: step[1], Machines: machines})
	}
	return out
}

func (c HostCapability) Removal(_ context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	request, err := DecodeHostRequest(block.Request)
	if err != nil {
		return lifecycle.Removal{}, err
	}
	return lifecycle.Removal{
		Description: hostDescription(reconciliation.Destroy, request),
		Impacts:     hostImpacts(reconciliation.Destroy, request),
		Groups:      hostGroups(reconciliation.Destroy, request),
	}, nil
}

// Apply runs the proof. It changes nothing on the host, so a proof that holds
// is unchanged and one that does not fails before any Machine is touched.
func (c HostCapability) Apply(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	request, err := c.prepare(ctx, execution)
	if err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	result, err := c.prove(ctx, execution, "apply", request)
	if err != nil {
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
	}
	outcome, err := usableOutcome(result.Outcome, "KubeVirt provider")
	if err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, err
	}
	if err := ValidateHostPresence(result.Evidence, request, execution.Block.RequestDigest); err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, err
	}
	return lifecycle.Result{Outcome: outcome, Evidence: result.Evidence}, nil
}

// Destroy publishes the proof released: it reads nothing on the host, because
// the proof created nothing there to take back.
func (c HostCapability) Destroy(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	request, err := c.prepare(ctx, execution)
	if err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	result, err := c.prove(ctx, execution, "destroy", request)
	if err != nil {
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
	}
	outcome, err := usableOutcome(result.Outcome, "KubeVirt provider")
	if err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, err
	}
	if err := ValidateHostAbsence(result.Evidence, execution.Block.RequestDigest); err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, err
	}
	return lifecycle.Result{Outcome: outcome, Evidence: result.Evidence}, nil
}

func usableOutcome(reported, subject string) (reconciliation.Outcome, error) {
	switch reported {
	case "changed":
		return reconciliation.OutcomeChanged, nil
	case "unchanged":
		return reconciliation.OutcomeUnchanged, nil
	}
	return "", refusal("lifecycle.state", "the "+subject+" adapter reported no usable outcome", "")
}

// Observe repeats the proof read-only: a holding proof is completed, any
// other answer unknown.
func (c HostCapability) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	unknown := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	request, err := c.prepare(ctx, execution)
	if err != nil {
		return unknown, err
	}
	result, err := c.prove(ctx, execution, "observe", request)
	if err != nil {
		return unknown, err
	}
	if ValidateHostPresence(result.Evidence, request, execution.Block.RequestDigest) != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown, Evidence: result.Evidence}, nil
	}
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Outcome: reconciliation.OutcomeUnchanged, Evidence: result.Evidence}, nil
}

func (c HostCapability) ObserveRemoval(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	evidence, err := c.released(ctx, execution)
	if err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, err
	}
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: evidence}, nil
}

func (c HostCapability) released(ctx context.Context, execution lifecycle.Execution) ([]byte, error) {
	if _, err := c.prepare(ctx, execution); err != nil {
		return nil, err
	}
	evidence, err := json.Marshal(HostEvidence{
		Absent: true, Failures: []string{}, Networks: []NetworkEvidence{}, Postcondition: true, Request: execution.Block.RequestDigest,
	})
	if err != nil {
		return nil, refusal("lifecycle.state", "the KubeVirt provider removal evidence cannot be encoded", "")
	}
	return evidence, nil
}

// Quiescent is derived: the proof holds nothing a removal could interrupt.
func (HostCapability) Quiescent(context.Context, lifecycle.Probe) (lifecycle.Quiescence, error) {
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "its removal takes nothing back"}, nil
}

// Unsupported refuses every selected KubeVirt object this arm cannot realize,
// before registration.
func (HostCapability) Unsupported(state *compilation.State) []lifecycle.Refusal {
	if state == nil {
		return nil
	}
	return Refusals(state.Effective())
}

func (c HostCapability) prepare(ctx context.Context, execution lifecycle.Execution) (HostRequest, error) {
	if err := ctx.Err(); err != nil {
		return HostRequest{}, err
	}
	if c.runner == nil {
		return HostRequest{}, refusal("lifecycle.state", "the KubeVirt provider adapter is not configured", "")
	}
	return DecodeHostRequest(execution.Block.Request)
}

func (c HostCapability) prove(ctx context.Context, execution lifecycle.Execution, operation string, request HostRequest) (lifecycle.RunResult, error) {
	canonical, err := request.Canonical()
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	return c.runner.Run(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{
		Implementation: HostImplementation, Operation: operation, Variable: hostVariable,
		Canonical: canonical, Placement: request.Placement, Materials: kubeconfig(request.KubeconfigRef), Deadline: hostDeadline,
	}))
}

func (HostCapability) AccessCredentials(block reconciliation.Block) ([]string, error) {
	request, err := DecodeHostRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return []string{request.KubeconfigRef}, nil
}

func (HostCapability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeHostRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.SecretReferences(), nil
}

func (HostCapability) Claimed(catalog api.Catalog) []string {
	var claimed []string
	for _, provider := range providers(catalog) {
		claimed = append(claimed, provider.Identity())
	}
	return claimed
}

func kubeconfig(reference string) []lifecycle.MaterialFile {
	return []lifecycle.MaterialFile{{Name: "host-kubeconfig", Part: secrets.ValuePart, Secret: reference, Variable: "kubeconfig"}}
}

// Plan derives one block per Machine a KubeVirt provider realizes. Each
// requires its provider's proof block by API object.
func (c MachineCapability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.CapabilityPlan{}, err
	}
	if input.State == nil {
		return lifecycle.CapabilityPlan{}, refusal("lifecycle.state", "lifecycle planning requires compiled desired state", "")
	}
	requests, err := MachineRequests(input.State.Effective(), input.Controller, input.Context.Name)
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
		definition := reconciliation.BlockDefinition{
			ID:             request.Identity.Block,
			Description:    machineDescription(input.Verb, request),
			Stage:          reconciliation.StageMachines,
			Requires:       []reconciliation.ObjectRef{{Kind: HostKind, Object: request.Provider}},
			Impacts:        machineImpacts(input.Verb, request),
			Groups:         machineGroups(input.Verb, request),
			Kind:           MachineKind,
			Object:         request.Identity.Object,
			Family:         substrate.MachineFamily,
			Implementation: MachineImplementation,
			ContentDigest:  digest,
			Request:        canonical,
		}
		// The DataVolumes this block deletes hold what was installed, so its
		// removal is acknowledged before the plan registers.
		if input.Verb == reconciliation.Destroy {
			definition.Consumes = []string{reconciliation.AuthorizationDataLoss}
		}
		plan.Definitions = append(plan.Definitions, definition)
		plan.Secrets = append(plan.Secrets, request.SecretReferences()...)
	}
	slices.Sort(plan.Secrets)
	plan.Secrets = slices.Compact(plan.Secrets)
	return plan, nil
}

func machineDescription(verb reconciliation.Verb, request MachineRequest) string {
	if verb == reconciliation.Destroy {
		return "remove the VirtualMachine " + request.Namespace + "/" + request.Name + " of " + request.Identity.Object + " and its disks"
	}
	return "realize " + request.Identity.Object + " as the VirtualMachine " + request.Namespace + "/" + request.Name
}

func machineImpacts(verb reconciliation.Verb, request MachineRequest) []string {
	vm, volume := "create-virtual-machine ", "create-data-volume "
	impacts := []string{vm + request.Namespace + "/" + request.Name, volume + request.Namespace + "/" + request.RootVolume}
	if verb == reconciliation.Destroy {
		vm, volume = "remove-virtual-machine ", "remove-data-volume "
		impacts = []string{vm + request.Namespace + "/" + request.Name, volume + request.Namespace + "/" + request.RootVolume,
			volume + request.Namespace + "/" + request.MediaVolume}
	}
	slices.Sort(impacts)
	return impacts
}

func machineGroups(verb reconciliation.Verb, request MachineRequest) []reconciliation.Group {
	steps := [][2]string{
		{"create-machine", "create the VirtualMachine and its root disk"},
		{"verify-machine", "verify the VirtualMachine and its root disk are this context's"},
	}
	if verb == reconciliation.Destroy {
		steps = [][2]string{
			{"remove-machine", "delete the stopped VirtualMachine and its upload media"},
			{"verify-absence", "verify the VirtualMachine, its DataVolumes and its claims are gone"},
		}
	}
	return groupsFor(steps, []string{request.Identity.Object})
}

func (c MachineCapability) Removal(_ context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	request, err := DecodeMachineRequest(block.Request)
	if err != nil {
		return lifecycle.Removal{}, err
	}
	return lifecycle.Removal{
		Description: machineDescription(reconciliation.Destroy, request),
		Impacts:     machineImpacts(reconciliation.Destroy, request),
		Consumes:    []string{reconciliation.AuthorizationDataLoss},
		Groups:      machineGroups(reconciliation.Destroy, request),
	}, nil
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
	outcome, err := usableOutcome(result.Outcome, "KubeVirt machine")
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
	if err := samePin(result.Evidence, digest, execution.ApplyProved, request.Provider); err != nil {
		return unknown, err
	}
	return lifecycle.Result{Outcome: outcome, Evidence: result.Evidence}, nil
}

// samePin holds a mutation's evidence to the cluster identity its provider's
// proof recorded, beside the adapter's own comparison.
func samePin(data []byte, digest string, records []lifecycle.BlockEvidence, provider string) error {
	evidence, err := decodeMachineEvidence(data, digest)
	if err != nil {
		return err
	}
	pinned, err := provedHost(records, provider)
	if err != nil {
		return err
	}
	if evidence.KubeSystemUID != pinned.KubeSystemUID {
		return refusal("lifecycle.state", "the KubeVirt machine evidence names another cluster than its provider's proof", "")
	}
	return nil
}

// Observe is read-only: nothing present is positive no effect, this block's
// own VirtualMachine part way realized positive partial, and a foreign one or
// an unanswered host unknown.
func (c MachineCapability) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	return c.observe(ctx, execution, reconciliation.Apply)
}

func (c MachineCapability) ObserveRemoval(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	return c.observe(ctx, execution, reconciliation.Destroy)
}

// machineEffect is the one reading of an observation for the verb the block
// was frozen for, as the libvirt arm reads its own.
func machineEffect(verb reconciliation.Verb, evidence []byte, request MachineRequest, digest string) (reconciliation.EffectState, error) {
	if verb == reconciliation.Destroy {
		switch {
		case ValidateMachineAbsence(evidence, digest) == nil:
			return reconciliation.EffectCompleted, nil
		case ValidateMachineUnremoved(evidence, request, digest) == nil:
			return reconciliation.EffectNoEffect, nil
		}
		if err := ValidateMachineRemovalUnfinished(evidence, request, digest); err != nil {
			return reconciliation.EffectUnknown, err
		}
		return reconciliation.EffectPartial, nil
	}
	presence := ValidateMachinePresence(evidence, request, digest)
	switch {
	case presence == nil:
		return reconciliation.EffectCompleted, nil
	case ValidateMachineAbsence(evidence, digest) == nil:
		return reconciliation.EffectNoEffect, nil
	case ValidateMachinePartial(evidence, digest) == nil:
		return reconciliation.EffectPartial, nil
	}
	return reconciliation.EffectUnknown, presence
}

func (c MachineCapability) observe(ctx context.Context, execution lifecycle.Execution, verb reconciliation.Verb) (lifecycle.Observation, error) {
	unknown := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	request, err := c.prepare(ctx, execution)
	if err != nil {
		return unknown, err
	}
	result, err := c.run(ctx, execution, "observe", request)
	if err != nil {
		return unknown, err
	}
	effect, _ := machineEffect(verb, result.Evidence, request, execution.Block.RequestDigest)
	observation := lifecycle.Observation{Effect: effect, Evidence: result.Evidence}
	if effect == reconciliation.EffectCompleted && verb == reconciliation.Apply {
		observation.Outcome = reconciliation.OutcomeUnchanged
	}
	return observation, nil
}

func (c MachineCapability) prepare(ctx context.Context, execution lifecycle.Execution) (MachineRequest, error) {
	if err := ctx.Err(); err != nil {
		return MachineRequest{}, err
	}
	if c.runner == nil {
		return MachineRequest{}, refusal("lifecycle.state", "the KubeVirt machine adapter is not configured", "")
	}
	return DecodeMachineRequest(execution.Block.Request)
}

// proof is what the machine adapter takes from its provider's proof: the
// server and cluster identity it pins, the root class and the receipts of
// this block's earlier evidence.
type proof struct {
	KubeSystemUID string    `json:"kubeSystemUID"`
	Receipts      []Receipt `json:"receipts"`
	Server        string    `json:"server"`
	StorageClass  string    `json:"storageClass"`
}

func (c MachineCapability) run(ctx context.Context, execution lifecycle.Execution, operation string, request MachineRequest) (lifecycle.RunResult, error) {
	canonical, err := request.Canonical()
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	if execution.ApplyOperation == "" || operation != "observe" && execution.ApplyRegistered == "" {
		return lifecycle.RunResult{}, refusal("lifecycle.state", "the KubeVirt machine's ownership identity is unavailable", "")
	}
	pinned, err := provedHost(execution.ApplyProved, request.Provider)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	pinned.Receipts = []Receipt{}
	if previous, err := decodeMachineEvidence(execution.PreviousEvidence, execution.Block.RequestDigest); err == nil {
		if previous.KubeSystemUID != "" && previous.KubeSystemUID != pinned.KubeSystemUID {
			return lifecycle.RunResult{}, refusal("lifecycle.state", "the host cluster of "+request.Provider+" is not the one this VirtualMachine was realized on",
				"correct spec.kubevirt.kubeconfigRef of InfraProvider/"+request.Provider+" so it reaches the cluster the operation realized its Machines on")
		}
		pinned.Receipts = previous.Receipts
	}
	encoded, err := json.Marshal(pinned)
	if err != nil {
		return lifecycle.RunResult{}, refusal("lifecycle.state", "the KubeVirt machine's pinned host cannot be encoded", "")
	}
	return c.runner.Run(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{
		Implementation: MachineImplementation, Operation: operation, Variable: machineVariable,
		Canonical: canonical, Placement: request.Placement, Materials: kubeconfig(request.KubeconfigRef), Deadline: machineDeadline,
		Values: map[string]string{"operation": execution.ApplyOperation, "block": execution.Block.ID,
			"registered": execution.ApplyRegistered, "pinned": string(encoded)},
	}))
}

// provedHost reads the provider proof this block depends on from the original
// apply's durable record: a holding proof of the provider it names.
func provedHost(records []lifecycle.BlockEvidence, provider string) (proof, error) {
	for _, record := range records {
		if record.Implementation != HostImplementation || record.Object != provider || record.Verb != reconciliation.Apply {
			continue
		}
		request, err := DecodeHostRequest(record.Block.Request)
		if err != nil {
			return proof{}, err
		}
		if err := ValidateHostPresence(record.Evidence, request, record.Block.RequestDigest); err != nil {
			return proof{}, err
		}
		evidence, _ := decodeHostEvidence(record.Evidence, record.Block.RequestDigest)
		return proof{KubeSystemUID: evidence.KubeSystemUID, Server: evidence.Server, StorageClass: evidence.StorageClass}, nil
	}
	return proof{}, refusal("lifecycle.state", "the proof of the host cluster of InfraProvider/"+provider+" is not recorded for this operation",
		"run the apply again so the provider's proof precedes its Machines")
}

// Quiescent proves the VirtualMachine is not running: only an instance the
// host reports absent is idle, and a host that does not answer leaves it
// unproved, never stopped.
func (c MachineCapability) Quiescent(ctx context.Context, probe lifecycle.Probe) (lifecycle.Quiescence, error) {
	execution := probe.Execution()
	request, err := c.prepare(ctx, execution)
	if err != nil {
		return lifecycle.Quiescence{}, err
	}
	stop := "bootwright machine stop --context " + execution.Context + " --name " + request.Identity.Object
	result, err := c.run(ctx, execution, "observe", request)
	if err != nil {
		return lifecycle.Quiescence{State: lifecycle.Unproved, Reason: "its VirtualMachineInstance could not be read", Stop: stop}, nil
	}
	evidence, err := decodeMachineEvidence(result.Evidence, execution.Block.RequestDigest)
	switch {
	case err != nil || !evidence.Answered:
		return lifecycle.Quiescence{State: lifecycle.Unproved, Reason: "the host cluster did not answer for its VirtualMachineInstance", Stop: stop}, nil
	case evidence.Instance != "":
		return lifecycle.Quiescence{State: lifecycle.Live, Reason: "its VirtualMachineInstance is " + evidence.Instance, Stop: stop}, nil
	}
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "its VirtualMachineInstance is absent"}, nil
}

// ProbesQuiescence says a VirtualMachine's removal is gated on its instance
// being absent, which only the host can show.
func (MachineCapability) ProbesQuiescence() bool { return true }

func (MachineCapability) RemovesHost(reconciliation.Block) bool { return true }

func (MachineCapability) DeletesMachine(reconciliation.Block) bool { return true }

func (MachineCapability) AccessCredentials(block reconciliation.Block) ([]string, error) {
	request, err := DecodeMachineRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return []string{request.KubeconfigRef}, nil
}

func (MachineCapability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeMachineRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.SecretReferences(), nil
}

func (MachineCapability) Claimed(catalog api.Catalog) []string {
	var claimed []string
	for _, provider := range providers(catalog) {
		for _, machine := range substrate.HostedMachines(catalog, provider.Name()) {
			claimed = append(claimed, machine.Identity())
		}
	}
	return claimed
}

// ProvedClaim claims the VirtualMachine by the server its proof reached, once
// its evidence proves it this block's, so two contexts of one controller store
// never realize it.
func (MachineCapability) ProvedClaim(contextName string, block reconciliation.Block, evidence json.RawMessage) (prerequisites.HostReservation, bool, error) {
	request, err := DecodeMachineRequest(block.Request)
	if err != nil {
		return prerequisites.HostReservation{}, false, err
	}
	observed, err := decodeMachineEvidence(evidence, block.RequestDigest)
	if err != nil || !observed.Owned || observed.Absent {
		return prerequisites.HostReservation{}, false, nil
	}
	key, ok := ClaimKey(observed.Server, request.Namespace, request.Name)
	if !ok {
		return prerequisites.HostReservation{}, false, refusal("lifecycle.state", "the KubeVirt machine evidence names no server to claim its VirtualMachine by", "")
	}
	return prerequisites.HostReservation{Context: contextName, Kind: "substrate-machine", Service: request.Identity.Object, Keys: []string{key}}, true, nil
}

// ClaimKey names one VirtualMachine by its host's API server, written as its
// host and port, the port its scheme's default when the server names none.
func ClaimKey(server, namespace, name string) (string, bool) {
	parsed, err := url.Parse(server)
	if err != nil || !origin(server) {
		return "", false
	}
	host := parsed.Host
	if parsed.Port() == "" {
		host = parsed.Host + ":443"
	}
	return "kubevirt-vm:" + host + "/" + namespace + "/" + name, true
}

var _ lifecycle.ProvingClaimer = MachineCapability{}
