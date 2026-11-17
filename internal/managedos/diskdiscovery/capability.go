package diskdiscovery

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
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/machine/diskselection"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/substrate"
)

type Capability struct {
	runner      Runner
	media       installation.MediaRecords
	identities  installation.Identities
	protections []DiskProtections
	refinements []DiscoveryRefinement
	nonce       func() (string, error)
}

func New(runner Runner) Capability { return Capability{runner: runner} }
func (c Capability) WithMedia(reader installation.MediaRecords) Capability {
	c.media = reader
	return c
}
func (c Capability) WithIdentities(reader installation.Identities) Capability {
	c.identities = reader
	return c
}
func (c Capability) WithProtections(readers ...DiskProtections) Capability {
	c.protections = slices.Clone(readers)
	return c
}
func (c Capability) WithRefinements(owners ...DiscoveryRefinement) Capability {
	c.refinements = slices.Clone(owners)
	return c
}

func (c Capability) WithNonces(source func() (string, error)) Capability {
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

func (c Capability) requests(ctx context.Context, input lifecycle.PlanInput) ([]DiscoveryRequest, []installation.Requirements, error) {
	var requests []DiscoveryRequest
	var requirements []installation.Requirements
	var records map[string]installation.MediaRecord
	read := false
	if input.State == nil {
		return nil, nil, refusal("lifecycle.state", "disk discovery requires compiled state", "")
	}
	for _, m := range installation.InstalledMachines(input.State.Effective()) {
		if !discoveryMachine(m) {
			continue
		}
		if input.Preparation != nil && !slices.Contains(input.Preparation.Targets, m.Name()) {
			continue
		}
		if c.media == nil {
			return nil, nil, refusal("lifecycle.state", "disk discovery has no media reader", "")
		}
		if !read {
			found, err := c.media.MediaRecords(ctx)
			if err != nil {
				return nil, nil, err
			}
			records, read = found, true
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

func (c Capability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
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
		dependencies := installation.RequiredObjects(requirements[index])
		dependencies[0].Family = substrate.MachineFamily
		plan.Definitions = append(plan.Definitions, reconciliation.BlockDefinition{ID: r.Identity.Block, Kind: installation.Kind, Object: r.Identity.Object, Family: installation.DiscoveryFamily, Implementation: DiscoveryImplementation, ContentDigest: DiscoveryContentDigest(), Stage: reconciliation.StageMachines, Request: wire,
			Description: discoveryDescription(r),
			Groups:      discoveryGroups(r.Identity.Object, input.Verb == reconciliation.Destroy),
			Impacts:     discoveryImpacts(r),
			Requires:    dependencies, Exclusive: []string{infrastructureservices.ControllerMediaReservationKey(r.Controller.Endpoint).String(), infrastructureservices.ControllerReservationKey(r.Controller.Endpoint).String()}})
		if r.Controller.VirtualMedia.EnableVerification {
			plan.Definitions[len(plan.Definitions)-1].Impacts = append(plan.Definitions[len(plan.Definitions)-1].Impacts,
				"enable-bmc-media-verification "+r.Identity.Object+" (BMC-wide; retained)")
		}
		if r.Controller.VirtualMedia.Trust == substrate.TrustDisableVerification {
			plan.Definitions[len(plan.Definitions)-1].Impacts = append(plan.Definitions[len(plan.Definitions)-1].Impacts,
				"fetch-private-media-without-certificate-verification "+r.Identity.Object)
			if r.Controller.VirtualMedia.RestoreVerification {
				plan.Definitions[len(plan.Definitions)-1].Impacts = append(plan.Definitions[len(plan.Definitions)-1].Impacts,
					"restore-bmc-media-verification-after-boot "+r.Identity.Object)
			}
		}
		if input.Verb == reconciliation.Destroy {
			last := &plan.Definitions[len(plan.Definitions)-1]
			removal, err := c.Removal(ctx, reconciliation.Block{BlockDefinition: *last})
			if err != nil {
				return plan, err
			}
			last.Description, last.Impacts = removal.Description, removal.Impacts
		}
		plan.Secrets = append(plan.Secrets, r.SecretReferences()...)
		plan.Reservations = append(plan.Reservations, infrastructureservices.HostReservation{
			Context: input.Context.Name, Kind: installation.DiscoveryFamily, Service: r.Identity.Object, ObjectKind: installation.Kind,
			Keys: []string{infrastructureservices.ControllerMediaReservationKey(r.Controller.Endpoint).String()},
		})
		media = append(media, r.BootMedia.Name)
	}
	if claim, ok := installation.MediaReservation(input.Context.Name, media); ok {
		plan.Reservations = append(plan.Reservations, claim)
	}
	slices.Sort(plan.Secrets)
	plan.Secrets = slices.Compact(plan.Secrets)
	return plan, nil
}

func discoveryDescription(r DiscoveryRequest) string {
	if r.Inventory == InventoryAutomatic {
		return "read the BMC drive inventory of " + r.Identity.Object + ", or inventory its Linux-visible disks in RAM when the BMC cannot answer, then power off"
	}
	return "inventory Linux-visible disks of " + r.Identity.Object + " in RAM, then power off"
}

func discoveryImpacts(r DiscoveryRequest) []string {
	impacts := []string{"boot-readonly-disk-inventory " + r.Identity.Object, "power-off " + r.Identity.Object}
	if r.Inventory == InventoryAutomatic {
		impacts = []string{"read-bmc-drive-inventory " + r.Identity.Object, "boot-readonly-disk-inventory " + r.Identity.Object + " (only when the BMC inventory is incomplete)", "power-off " + r.Identity.Object + " (only after that boot)"}
	}
	return impacts
}

func (c Capability) Claimed(catalog api.Catalog) []string {
	var names []string
	for _, m := range installation.InstalledMachines(catalog) {
		if discoveryMachine(m) {
			names = append(names, m.Name())
		}
	}
	return names
}
func (Capability) SecretReferences(block reconciliation.Block) ([]string, error) {
	r, err := DecodeDiscoveryRequest(block.Request)
	return r.SecretReferences(), err
}

// Roots validates historical proof without live probing or substituting reports.
func (c Capability) Roots(ctx context.Context, input lifecycle.PlanInput) (map[string]diskselection.Proof, error) {
	requests, _, err := c.requests(ctx, input)
	if err != nil {
		return nil, err
	}
	proofs := map[string]diskselection.Proof{}
	for _, r := range requests {
		report, found, err := c.completedInventory(ctx, input, r)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, refusal("lifecycle.state", "Machine/"+r.Identity.Object+" has no completed disk discovery for the current policy and media", "run bootwright apply --context "+input.Context.Name+" to prepare inventory, or bootwright machine discover --context "+input.Context.Name+" --name "+r.Identity.Object+" --prepare-prerequisites")
		}
		m, _ := input.State.Effective().Find(api.Machine, r.Identity.Object)
		policy, err := diskselection.PolicyFromValue(substrate.MachineDiscoveryPolicy(m))
		if err != nil {
			return nil, err
		}
		protected, err := c.protectedClaims(input.State.Effective(), m)
		if err != nil {
			return nil, refusal("api.invariant", err.Error(), "select stable explicit Ceph data, DB and WAL paths from the recorded inventory")
		}
		proof, err := diskselection.Prove(report, report.Binding, policy, protected)
		if err != nil {
			return nil, refusal("lifecycle.state", "Machine/"+r.Identity.Object+": "+err.Error(), "inspect the recorded inventory, refine spec.hardware.rootDisk.discover.match, or set spec.hardware.rootDisk.discover.inventory to boot when the BMC inventory cannot answer it, and update the context before planning again")
		}
		proofs[r.Identity.Object] = proof
	}
	return proofs, nil
}

func (c Capability) completedInventory(ctx context.Context, input lifecycle.PlanInput, r DiscoveryRequest) (diskselection.Report, bool, error) {
	var inventory diskselection.Report
	if input.AppliedEvidence == nil {
		return inventory, false, nil
	}
	owned, err := input.AppliedEvidence(ctx, installation.Kind, r.Identity.Object)
	if err != nil {
		return inventory, false, err
	}
	wire, err := r.Canonical()
	if err != nil {
		return inventory, false, err
	}
	digest, err := reconciliation.RequestDigest(reconciliation.BlockDefinition{Kind: installation.Kind, Object: r.Identity.Object, Implementation: DiscoveryImplementation, ContentDigest: DiscoveryContentDigest(), Request: wire})
	if err != nil {
		return inventory, false, err
	}
	found := false
	for _, record := range owned {
		if record.Implementation != DiscoveryImplementation || record.Block.RequestDigest != digest || record.Verb != reconciliation.Apply || record.State != reconciliation.BlockDone {
			continue
		}
		e, report, err := discoveryEvidence(record.Evidence, digest)
		if err != nil {
			return inventory, false, err
		}
		if !e.Completed || !e.Cleaned || c.identities == nil {
			return inventory, false, nil
		}
		if r.Inventory == InventoryBoot && report.Source != diskselection.SourceLinux {
			return inventory, false, refusal("lifecycle.state", "a BMC disk inventory answers a discovery that declares a boot inventory", "retain the recorded inventory and resolve its ownership before preparing another")
		}
		expected := report.Binding
		expected.Context, expected.Incarnation, expected.Machine = input.Context.Name, input.Context.Incarnation, r.Identity.Object
		expected.RequestDigest, expected.MediaSHA256, expected.ToolDigest = digest, r.BootMedia.SHA256, DiscoveryContentDigest()
		expected.Operation = record.Operation
		pin, pinned, err := c.identities.PinnedIdentity(r.Identity.Object, owned)
		if err != nil {
			return inventory, false, err
		}
		if !pinned {
			return inventory, false, nil
		}
		expected.UUID = strings.ToLower(strings.TrimSpace(pin.UUID))
		expected.MACs = discoveryMACs(r)
		if expected.Validate() != nil || report.Validate() != nil || !reflect.DeepEqual(report.Binding, expected) {
			return inventory, false, refusal("lifecycle.state", "the recorded disk inventory does not match its original machine and execution", "retain the original inventory and resolve its ownership before preparing another")
		}
		if found {
			return inventory, false, refusal("lifecycle.state", "disk discovery has multiple owned proofs", "")
		}
		inventory, found = report, true
	}
	return inventory, found, nil
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

func (c Capability) binding(ctx context.Context, e lifecycle.Execution, r DiscoveryRequest, create bool) (diskselection.Binding, error) {
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

func (c Capability) run(ctx context.Context, e lifecycle.Execution, operation string) (lifecycle.RunResult, error) {
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
	port := r.MachinePort
	if port.UserVariable == "" || port.PasswordVariable == "" {
		return lifecycle.RunResult{}, refusal("lifecycle.state", "the frozen machine port names no controller account material", "")
	}
	files := []lifecycle.MaterialFile{
		{Name: "bmc-user", Part: secrets.UsernamePart, Secret: r.Controller.CredentialsRef, Variable: port.UserVariable},
		{Name: "bmc-password", Part: secrets.PasswordPart, Secret: r.Controller.CredentialsRef, Variable: port.PasswordVariable},
		{Name: "artifact-ca", Part: secrets.CertificatePart, Secret: r.Private.CertificateRef, Variable: "artifactCertificate"},
	}
	if port.CAVariable != "" {
		files = append(files, lifecycle.MaterialFile{Name: "bmc-ca", Part: secrets.CertificatePart, Secret: r.Controller.TrustBundleRef, Variable: port.CAVariable})
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

func (c Capability) Apply(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
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
func (c Capability) Observe(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
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
func (c Capability) Destroy(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
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
func (c Capability) ObserveRemoval(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
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
	if err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, err
	}
	// A valid unfinished observation proves owned custody or a pristine
	// external boundary. Cleanup must be allowed before inventory completes.
	return lifecycle.Observation{Effect: reconciliation.EffectPartial, Evidence: r.Evidence}, nil
}

func (c Capability) Removal(ctx context.Context, b reconciliation.Block) (lifecycle.Removal, error) {
	r, err := DecodeDiscoveryRequest(b.Request)
	if err != nil {
		return lifecycle.Removal{}, err
	}
	return lifecycle.Removal{Description: "remove only an unfinished discovery boot of " + r.Identity.Object + " and retain its disk inventory", Impacts: []string{"cleanup-owned-discovery-media " + r.Identity.Object}, Groups: discoveryRemovalGroups(b.Groups), ReplaySafe: true}, ctx.Err()
}
func (Capability) Quiescent(ctx context.Context, _ lifecycle.Probe) (lifecycle.Quiescence, error) {
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "discovery cleanup independently proves its own temporary boot and never stops a later installed OS"}, ctx.Err()
}
func (Capability) RequiresPreStopRemoval(reconciliation.Block) bool { return true }
func (Capability) HostMachines(block reconciliation.Block) ([]string, error) {
	r, err := DecodeDiscoveryRequest(block.Request)
	return []string{r.Identity.Object}, err
}

func (Capability) SecretClasses(block reconciliation.Block) (secrets.Classes, error) {
	r, err := DecodeDiscoveryRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return r.Placement.SecretClasses().Add(secrets.AccessCredential, r.Controller.CredentialsRef).
		Add(secrets.TrustAnchor, r.Controller.TrustBundleRef, r.Private.CertificateRef), nil
}
