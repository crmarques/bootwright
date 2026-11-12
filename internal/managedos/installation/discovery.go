package installation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine/diskselection"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/substrate"
)

type DiscoveryCapability struct {
	runner      Runner
	media       MediaRecords
	identities  Identities
	protections []DiskProtections
	refinements []DiscoveryRefinement
	nonce       func() (string, error)
}

func NewDiscovery(runner Runner) DiscoveryCapability { return DiscoveryCapability{runner: runner} }
func (c DiscoveryCapability) WithMedia(reader MediaRecords) DiscoveryCapability {
	c.media = reader
	return c
}
func (c DiscoveryCapability) WithIdentities(reader Identities) DiscoveryCapability {
	c.identities = reader
	return c
}
func (c DiscoveryCapability) WithProtections(readers ...DiskProtections) DiscoveryCapability {
	c.protections = slices.Clone(readers)
	return c
}
func (c DiscoveryCapability) WithRefinements(owners ...DiscoveryRefinement) DiscoveryCapability {
	c.refinements = slices.Clone(owners)
	return c
}

func (c DiscoveryCapability) WithNonces(source func() (string, error)) DiscoveryCapability {
	c.nonce = source
	return c
}

type DiscoveryEvidence struct {
	Cleaned   bool            `json:"cleaned"`
	Completed bool            `json:"completed"`
	Report    json.RawMessage `json:"report,omitempty"`
	Request   string          `json:"request"`
	Version   string          `json:"version"`
}

func discoveryMachine(m api.Object) bool {
	return substrate.MachineDiscoveryPolicy(m).Present()
}

func (c DiscoveryCapability) requests(ctx context.Context, input lifecycle.PlanInput) ([]DiscoveryRequest, []Requirements, error) {
	var requests []DiscoveryRequest
	var requirements []Requirements
	if input.State == nil {
		return nil, nil, refusal("lifecycle.state", "disk discovery requires compiled state", "")
	}
	for _, m := range InstalledMachines(input.State.Effective()) {
		if !discoveryMachine(m) {
			continue
		}
		if input.Preparation != nil && !slices.Contains(input.Preparation.Targets, m.Name()) {
			continue
		}
		if c.media == nil {
			return nil, nil, refusal("lifecycle.state", "disk discovery has no media reader", "")
		}
		records, err := c.media.MediaRecords(ctx)
		if err != nil {
			return nil, nil, err
		}
		r, needs, err := discoveryRequest(input.State.Effective(), m, input, records)
		if err != nil {
			return nil, nil, err
		}
		requests = append(requests, r)
		requirements = append(requirements, needs)
	}
	return requests, requirements, nil
}

func (c DiscoveryCapability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	plan := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}
	requests, requirements, err := c.requests(ctx, input)
	if err != nil {
		return plan, err
	}
	var media []string
	for index, r := range requests {
		wire, err := r.Canonical()
		if err != nil {
			return plan, err
		}
		prerequisites := requires(requirements[index])
		prerequisites[0].Family = substrate.MachineFamily
		plan.Definitions = append(plan.Definitions, reconciliation.BlockDefinition{ID: r.Identity.Block, Kind: Kind, Object: r.Identity.Object, Family: DiscoveryFamily, Implementation: DiscoveryImplementation, ContentDigest: DiscoveryContentDigest(), Stage: reconciliation.StageMachines, Request: wire,
			Description: "inventory Linux-visible disks of " + r.Identity.Object + " in RAM, then power off",
			Impacts:     []string{"boot-readonly-disk-inventory " + r.Identity.Object, "power-off " + r.Identity.Object},
			Requires:    prerequisites, Exclusive: []string{substrate.ControllerReservationKey(r.Controller.Endpoint)}})
		if input.Verb == reconciliation.Destroy {
			last := &plan.Definitions[len(plan.Definitions)-1]
			removal, err := c.Removal(ctx, reconciliation.Block{BlockDefinition: *last})
			if err != nil {
				return plan, err
			}
			last.Description, last.Impacts = removal.Description, removal.Impacts
		}
		plan.Secrets = append(plan.Secrets, r.SecretReferences()...)
		media = append(media, r.BootMedia.Name)
	}
	if claim, ok := mediaReservation(input.Context.Name, media); ok {
		plan.Reservations = append(plan.Reservations, claim)
	}
	slices.Sort(plan.Secrets)
	plan.Secrets = slices.Compact(plan.Secrets)
	return plan, nil
}

func (c DiscoveryCapability) Claimed(catalog api.Catalog) []string {
	var names []string
	for _, m := range InstalledMachines(catalog) {
		if discoveryMachine(m) {
			names = append(names, m.Name())
		}
	}
	return names
}
func (DiscoveryCapability) SecretReferences(block reconciliation.Block) ([]string, error) {
	r, err := DecodeDiscoveryRequest(block.Request)
	return r.SecretReferences(), err
}

// Roots validates historical proof without live probing or substituting reports.
func (c DiscoveryCapability) Roots(ctx context.Context, input lifecycle.PlanInput) (map[string]diskselection.Proof, error) {
	requests, _, err := c.requests(ctx, input)
	if err != nil {
		return nil, err
	}
	proofs := map[string]diskselection.Proof{}
	for _, r := range requests {
		missing := func() error {
			return refusal("lifecycle.state", "Machine/"+r.Identity.Object+" has no completed disk discovery for the current policy and media", "run bootwright machine discover --context "+input.Context.Name+" --name "+r.Identity.Object)
		}
		if input.AppliedEvidence == nil {
			return nil, missing()
		}
		owned, err := input.AppliedEvidence(ctx, Kind, r.Identity.Object)
		if err != nil {
			return nil, err
		}
		wire, _ := r.Canonical()
		digest, err := reconciliation.RequestDigest(reconciliation.BlockDefinition{Kind: Kind, Object: r.Identity.Object, Implementation: DiscoveryImplementation, ContentDigest: DiscoveryContentDigest(), Request: wire})
		if err != nil {
			return nil, err
		}
		found := false
		for _, record := range owned {
			if record.Implementation != DiscoveryImplementation || record.Block.RequestDigest != digest || record.Verb != reconciliation.Apply || record.State != reconciliation.BlockDone {
				continue
			}
			e, report, err := discoveryEvidence(record.Evidence, digest)
			if err != nil {
				return nil, err
			}
			if !e.Completed || !e.Cleaned {
				return nil, missing()
			}
			expected := report.Binding
			expected.Context, expected.Incarnation, expected.Machine = input.Context.Name, input.Context.Incarnation, r.Identity.Object
			expected.RequestDigest, expected.MediaSHA256, expected.ToolDigest = digest, r.BootMedia.SHA256, DiscoveryContentDigest()
			expected.Operation = record.Operation
			if c.identities == nil {
				return nil, missing()
			}
			pin, pinned, err := c.identities.PinnedIdentity(r.Identity.Object, owned)
			if err != nil {
				return nil, err
			}
			if !pinned {
				return nil, missing()
			}
			expected.UUID = strings.ToLower(strings.TrimSpace(pin.UUID))
			expected.MACs = discoveryMACs(r)
			m, _ := input.State.Effective().Find(api.Machine, r.Identity.Object)
			policy, err := diskselection.PolicyFromValue(substrate.MachineDiscoveryPolicy(m))
			if err != nil {
				return nil, err
			}
			var protected []diskselection.DiskIdentity
			for _, protect := range c.protections {
				claims, err := protect(input.State.Effective(), m)
				if err != nil {
					return nil, refusal("api.invariant", err.Error(), "select stable explicit Ceph data, DB and WAL paths from the recorded inventory")
				}
				protected = append(protected, claims...)
			}
			proof, err := diskselection.Prove(report, expected, policy, protected)
			if err != nil {
				return nil, refusal("lifecycle.state", "Machine/"+r.Identity.Object+": "+err.Error(), "inspect the recorded inventory, refine rootDevice.discover.match and update the context before planning again")
			}
			if found {
				return nil, refusal("lifecycle.state", "disk discovery has multiple owned proofs", "")
			}
			proofs[r.Identity.Object] = proof
			found = true
		}
		if !found {
			return nil, missing()
		}
	}
	return proofs, nil
}

func discoveryMACs(r DiscoveryRequest) []string {
	var out []string
	for _, nic := range r.Interfaces {
		out = append(out, strings.ToLower(nic.MACAddress))
	}
	slices.Sort(out)
	return out
}

func discoveryEvidence(data json.RawMessage, digest string) (DiscoveryEvidence, diskselection.Report, error) {
	e, err := reconciliation.DecodeEvidence[DiscoveryEvidence](data, diskselection.MaxReportBytes+4096, "disk discovery")
	if err != nil {
		return e, diskselection.Report{}, err
	}
	if e.Version != discoveryVersion || e.Request != digest {
		return e, diskselection.Report{}, refusal("lifecycle.state", "disk discovery evidence names another contract or request", "")
	}
	if len(e.Report) == 0 {
		return e, diskselection.Report{}, nil
	}
	r, err := diskselection.DecodeReport(e.Report)
	return e, r, err
}

func (c DiscoveryCapability) binding(ctx context.Context, e lifecycle.Execution, r DiscoveryRequest, create bool) (diskselection.Binding, error) {
	if c.identities == nil {
		return diskselection.Binding{}, refusal("lifecycle.state", "disk discovery has no hardware identity reader", "")
	}
	proofs := e.ApplyProved
	if len(proofs) == 0 {
		proofs = e.Proved
	}
	pin, found, err := c.identities.PinnedIdentity(r.Identity.Object, proofs)
	if err != nil {
		return diskselection.Binding{}, err
	}
	if !found || pin.UUID == "" {
		return diskselection.Binding{}, refusal("lifecycle.state", "disk discovery has no original physical identity proof", "")
	}
	b := diskselection.Binding{Context: e.Context, Incarnation: e.Incarnation, Machine: r.Identity.Object, Operation: e.ApplyOperation, RequestDigest: e.Block.RequestDigest, MediaSHA256: r.BootMedia.SHA256, ToolDigest: DiscoveryContentDigest(), UUID: strings.ToLower(strings.TrimSpace(pin.UUID)), MACs: discoveryMACs(r)}
	for _, raw := range e.ApplyPreparations {
		previous, err := reconciliation.DecodeEvidence[diskselection.Binding](raw, 4096, "disk inventory attempt")
		if err != nil {
			return b, err
		}
		b.Attempt, b.Nonce = previous.Attempt, previous.Nonce
		if previous.Validate() != nil || !reflect.DeepEqual(b, previous) {
			return b, refusal("lifecycle.state", "disk inventory attempt belongs to another request or machine", "")
		}
		return previous, nil
	}
	if !create || e.Prepare == nil {
		return b, refusal("lifecycle.state", "disk discovery has no durable attempt preparation", "")
	}
	if c.nonce == nil {
		return b, refusal("lifecycle.state", "disk discovery has no cryptographic nonce source", "")
	}
	b.Nonce, err = c.nonce()
	if err != nil {
		return b, err
	}
	b.Attempt = fmt.Sprintf("attempt-%06d", e.Attempt)
	if err := b.Validate(); err != nil {
		return b, err
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return b, err
	}
	return b, e.Prepare(ctx, raw)
}

func (c DiscoveryCapability) run(ctx context.Context, e lifecycle.Execution, operation string) (lifecycle.RunResult, error) {
	r, err := DecodeDiscoveryRequest(e.Block.Request)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	b, err := c.binding(ctx, e, r, operation == "apply")
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	if c.runner == nil {
		return lifecycle.RunResult{}, refusal("lifecycle.state", "disk discovery has no runner", "")
	}
	raw, _ := json.Marshal(b)
	files := []lifecycle.MaterialFile{
		{Name: "bmc-user", Part: secrets.UsernamePart, Secret: r.Controller.CredentialsRef, Variable: "controllerUser"},
		{Name: "bmc-password", Part: secrets.PasswordPart, Secret: r.Controller.CredentialsRef, Variable: "controllerPassword"},
		{Name: "artifact-ca", Part: secrets.CertificatePart, Secret: r.Private.CertificateRef, Variable: "artifactCertificate"},
	}
	if r.Controller.TrustBundleRef != "" {
		files = append(files, lifecycle.MaterialFile{Name: "bmc-ca", Part: secrets.CertificatePart, Secret: r.Controller.TrustBundleRef, Variable: "controllerCA"})
	}
	wire, err := r.Canonical()
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	result, err := c.runner.Run(ctx, lifecycle.RunFor(e, lifecycle.Invocation{Implementation: DiscoveryImplementation, Operation: operation, Variable: discoveryVariable, Canonical: wire, Placement: r.Placement, Materials: files, Values: map[string]string{"bindingBase64": base64.StdEncoding.EncodeToString(raw)}, Deadline: time.Duration(r.DeadlineSeconds)*time.Second + time.Hour}))
	if err != nil {
		return result, err
	}
	proof, report, err := discoveryEvidence(result.Evidence, e.Block.RequestDigest)
	if err != nil {
		return result, err
	}
	if proof.Completed && (!proof.Cleaned || report.Validate() != nil || !reflect.DeepEqual(report.Binding, b)) {
		return result, refusal("lifecycle.state", "disk discovery did not prove this attempt and its cleanup", "")
	}
	return result, nil
}

func (c DiscoveryCapability) Apply(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	r, err := c.run(ctx, e, "apply")
	if err != nil {
		return lifecycle.Result{}, err
	}
	proof, _, err := discoveryEvidence(r.Evidence, e.Block.RequestDigest)
	if err != nil || !proof.Completed || !proof.Cleaned {
		return lifecycle.Result{}, refusal("lifecycle.state", "disk discovery did not complete inventory and cleanup", "")
	}
	return lifecycle.Result{Outcome: reconciliation.OutcomeChanged, Evidence: r.Evidence}, nil
}
func (c DiscoveryCapability) Observe(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
	r, err := c.run(ctx, e, "observe")
	if err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, err
	}
	proof, _, err := discoveryEvidence(r.Evidence, e.Block.RequestDigest)
	if err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, err
	}
	effect := reconciliation.EffectPartial
	if proof.Completed && proof.Cleaned {
		effect = reconciliation.EffectCompleted
	}
	return lifecycle.Observation{Effect: effect, Evidence: r.Evidence}, nil
}
func (c DiscoveryCapability) Destroy(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	// Completed discovery must never power off an OS installed later.
	proof, _, err := discoveryEvidence(e.PreviousEvidence, e.Block.RequestDigest)
	if err == nil && proof.Completed && proof.Cleaned {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnchanged, Evidence: e.PreviousEvidence}, nil
	}
	r, err := c.run(ctx, e, "destroy")
	if err != nil {
		return lifecycle.Result{}, err
	}
	proof, _, err = discoveryEvidence(r.Evidence, e.Block.RequestDigest)
	if err != nil || !proof.Cleaned {
		return lifecycle.Result{}, refusal("lifecycle.state", "disk discovery cleanup is incomplete", "")
	}
	return lifecycle.Result{Outcome: reconciliation.OutcomeChanged, Evidence: r.Evidence}, nil
}
func (c DiscoveryCapability) ObserveRemoval(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
	proof, _, err := discoveryEvidence(e.PreviousEvidence, e.Block.RequestDigest)
	if err == nil && proof.Completed && proof.Cleaned {
		return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: e.PreviousEvidence}, nil
	}
	r, err := c.run(ctx, e, "observe")
	if err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, err
	}
	proof, _, err = discoveryEvidence(r.Evidence, e.Block.RequestDigest)
	if err == nil && proof.Cleaned {
		return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: r.Evidence}, nil
	}
	return lifecycle.Observation{Effect: reconciliation.EffectUnknown, Evidence: r.Evidence}, err
}

func (c DiscoveryCapability) Removal(ctx context.Context, b reconciliation.Block) (lifecycle.Removal, error) {
	r, err := DecodeDiscoveryRequest(b.Request)
	if err != nil {
		return lifecycle.Removal{}, err
	}
	return lifecycle.Removal{Description: "remove only an unfinished discovery boot of " + r.Identity.Object + " and retain its disk inventory", Impacts: []string{"cleanup-owned-discovery-media " + r.Identity.Object}, ReplaySafe: true}, ctx.Err()
}
func (DiscoveryCapability) Quiescent(ctx context.Context, _ lifecycle.Probe) (lifecycle.Quiescence, error) {
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "discovery cleanup independently proves its own temporary boot and never stops a later installed OS"}, ctx.Err()
}
func (DiscoveryCapability) RequiresPreStopRemoval(reconciliation.Block) bool { return true }
func (DiscoveryCapability) HostMachines(block reconciliation.Block) ([]string, error) {
	r, err := DecodeDiscoveryRequest(block.Request)
	return []string{r.Identity.Object}, err
}
