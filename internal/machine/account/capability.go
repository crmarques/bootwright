package account

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

const (
	Kind           = string(api.Machine)
	Implementation = "machine-enrollment-v1"
)

type Request struct {
	Bootstrap     machine.Bootstrap `json:"bootstrap"`
	ClusterKeyRef string            `json:"clusterKeyRef,omitempty"`
	FleetKeyRef   string            `json:"fleetKeyRef,omitempty"`
	Placement     machine.Placement `json:"placement"`
	Version       string            `json:"version"`
}

func (r Request) keys() []string {
	keys := []string{r.Placement.PrivateKeyRef}
	if r.FleetKeyRef != "" {
		keys = append(keys, r.FleetKeyRef)
	}
	return keys
}

func (r Request) classes() secrets.Classes {
	return r.Bootstrap.Classes().Add(secrets.EffectMaterial, r.keys()...).Add(secrets.TrustAnchor, r.Placement.KnownHostsRef, r.ClusterKeyRef)
}

func (r Request) references() []string {
	return lifecycle.SecretNames(r.Placement.KnownHostsRef, r.Placement.PrivateKeyRef, r.FleetKeyRef, r.ClusterKeyRef, r.Bootstrap.AuthRef, r.Bootstrap.SudoRef)
}

func decode(block reconciliation.Block) (Request, error) {
	r, err := reconciliation.ThawVersion[Request](block.Request, "machine enrollment", Implementation)
	if err != nil {
		return r, err
	}
	if !r.Placement.PinnedSSH() || !r.Placement.Sudo() || !r.Bootstrap.Valid(r.Placement.User) ||
		block.Object != r.Placement.Machine || block.ID != reconciliation.BlockIdentity("machine-enrollment-", block.Object) ||
		r.FleetKeyRef == r.Placement.PrivateKeyRef || r.ClusterKeyRef == r.Placement.PrivateKeyRef || (r.ClusterKeyRef == "") != (r.FleetKeyRef == "") {
		return r, refused(r.Placement, "the enrollment request", "review the selected Machine")
	}
	return r, nil
}

type Capability struct {
	Remote     remoteAccount
	ClusterKey func(api.Catalog, string) string
}

func (Capability) Claimed(api.Catalog) []string                                { return nil }
func (Capability) PrecedesController(reconciliation.BlockDefinition) bool      { return true }
func (Capability) IndependentOfController(reconciliation.BlockDefinition) bool { return true }

func WorkerDigest() string { return fmt.Sprintf("%x", sha256.Sum256([]byte(worker))) }

func ContentDigest() string {
	return reconciliation.ContentDigestOf("bootwright.machine.enrollment-v1", Implementation, WorkerDigest())
}

func (c Capability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	var plan lifecycle.CapabilityPlan
	catalog := input.State.Effective()
	for _, object := range catalog.OfKind(api.Machine) {
		if err := ctx.Err(); err != nil {
			return plan, err
		}
		if input.Preparation != nil && input.Preparation.Kind == machine.EnrollmentFamily && !slices.Contains(input.Preparation.Targets, object.Name()) {
			continue
		}
		bootstrap, enabled := machine.BootstrapOf(object)
		if !enabled {
			continue
		}
		place, err := machine.PlacementFor(object, input.Controller)
		if err != nil {
			return plan, err
		}
		request := Request{Version: Implementation, Placement: place, Bootstrap: bootstrap}
		if c.ClusterKey != nil {
			request.ClusterKeyRef = c.ClusterKey(catalog, object.Name())
		}
		if request.ClusterKeyRef != "" {
			request.FleetKeyRef, _ = machine.FleetKey(catalog)
			if request.FleetKeyRef == "" || request.FleetKeyRef == place.PrivateKeyRef || request.ClusterKeyRef == place.PrivateKeyRef {
				return plan, refused(place, "distinct lifecycle, fleet and native cluster keys", "declare three distinct SSH key pairs")
			}
		}
		canonical, err := reconciliation.Freeze(request, "machine enrollment")
		if err != nil {
			return plan, err
		}
		block := enrollmentBlock(request, canonical)
		if input.Verb == reconciliation.Destroy {
			reverse := removal(request)
			block.Description, block.Impacts, block.Groups = reverse.Description, reverse.Impacts, reverse.Groups
		}
		plan.Definitions = append(plan.Definitions, block)
		plan.Secrets = append(plan.Secrets, request.references()...)
		plan.SSHReservations = append(plan.SSHReservations, lifecycle.SSHReservation{
			Machine: place.Machine, Address: place.Address, Port: place.Port, KnownHostsRef: place.KnownHostsRef,
			Reservation: infrastructureservices.HostReservation{Context: input.Context.Name, Kind: machine.EnrollmentFamily, Service: place.Machine,
				Keys: []string{"login:" + place.User}},
		})
	}
	plan.Secrets = lifecycle.SecretNames(plan.Secrets...)
	return plan, nil
}

func enrollmentBlock(request Request, canonical []byte) reconciliation.BlockDefinition {
	place := request.Placement
	caller := request.Bootstrap.User
	if caller == "" {
		caller = "the invoking operator"
	}
	return reconciliation.BlockDefinition{
		ID: reconciliation.BlockIdentity("machine-enrollment-", place.Machine), Kind: Kind, Object: place.Machine,
		Family: machine.EnrollmentFamily, Implementation: Implementation, Request: canonical, ContentDigest: ContentDigest(),
		Stage: reconciliation.StageInfrastructureServices, Consumes: []string{},
		Description: "connect as " + caller + ", create " + place.User + " and verify its passwordless root sudo",
		Impacts:     []string{"create root-equivalent SSH access for " + place.User},
		Groups:      []reconciliation.Group{{ID: "enroll", Description: "establish the managed SSH login", Machines: []string{place.Machine}}},
		Requires:    []reconciliation.ObjectRef{{Kind: Kind, Object: place.Machine, Family: machine.HardwareFamily}},
	}
}

func (Capability) ProveBinding(_ context.Context, contextName string, block reconciliation.Block, material map[string]secrets.Material) error {
	request, err := decode(block)
	if err != nil {
		return err
	}
	if err := request.Placement.ProveHostPin(contextName, material); err != nil {
		return err
	}
	_, err = publicKeys(request, material)
	return err
}

func (Capability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := decode(block)
	return request.references(), err
}

func (Capability) SecretClasses(block reconciliation.Block) (secrets.Classes, error) {
	request, err := decode(block)
	return request.classes(), err
}

func (Capability) Removal(_ context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	request, err := decode(block)
	return removal(request), err
}

func removal(request Request) lifecycle.Removal {
	return lifecycle.Removal{Description: "remove the owned login " + request.Placement.User + " and keep its home data",
		Impacts: []string{"withdraw the managed SSH and sudo grants"}, Consumes: []string{},
		Groups: []reconciliation.Group{{ID: "withdraw", Description: "remove and verify the managed SSH login", Machines: []string{request.Placement.Machine}}}}
}

func (Capability) Quiescent(context.Context, lifecycle.Probe) (lifecycle.Quiescence, error) {
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "the owned access is removed after dependent effects"}, nil
}

func (c Capability) CheckApplyStart(ctx context.Context, p lifecycle.Probe) error {
	_, err := c.run(ctx, p.Execution(), "observe", false)
	return err
}

func (c Capability) Apply(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	return c.effect(ctx, e, "apply", "ready")
}

func (c Capability) Destroy(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	return c.effect(ctx, e, "destroy", "absent")
}

func (c Capability) effect(ctx context.Context, e lifecycle.Execution, mode, want string) (lifecycle.Result, error) {
	observed, err := c.run(ctx, e, mode, mode == "apply")
	if err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, err
	}
	if observed != want {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, fmt.Errorf("enrollment postcondition was not proved")
	}
	return lifecycle.Result{Outcome: reconciliation.OutcomeChanged, Evidence: evidence(e, observed)}, nil
}

func (c Capability) Observe(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
	return c.observe(ctx, e, false)
}

func (c Capability) ObserveRemoval(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
	return c.observe(ctx, e, true)
}

func (c Capability) observe(ctx context.Context, e lifecycle.Execution, removal bool) (lifecycle.Observation, error) {
	mode := "observe"
	if !removal && e.ReadOnly && !lifecycle.Recorded(e) {
		mode = "inspect"
	}
	state, err := c.run(ctx, e, mode, !removal)
	out := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	if err != nil && state != "partial" {
		return out, err
	}
	switch {
	case state == "ready" && !removal, state == "absent" && removal:
		out.Effect = reconciliation.EffectCompleted
	case state == "partial":
		out.Effect = reconciliation.EffectPartial
	default:
		out.Effect = reconciliation.EffectNoEffect
	}
	out.Evidence = evidence(e, state)
	return out, nil
}

func (c Capability) run(ctx context.Context, e lifecycle.Execution, mode string, prove bool) (string, error) {
	r, err := decode(e.Block)
	if err != nil {
		return "", err
	}
	if !reconciliation.ValidOperationID(e.ApplyOperation) {
		return "", refused(r.Placement, "the enrollment owner", "recover the recorded apply operation")
	}
	mark := fmt.Sprintf("bootwright-%x", sha256.Sum256([]byte(e.ApplyOperation+"\x00"+e.Block.RequestDigest)))
	state, err := c.Remote.manage(ctx, e.Context, r, mark, mode, e.Material)
	if err != nil || state != "ready" || !prove {
		return state, err
	}
	if err := c.Remote.prove(ctx, e.Context, r, e.Material); err != nil {
		return "partial", err
	}
	return state, nil
}

type Evidence struct {
	Request string `json:"request"`
	State   string `json:"state"`
}

func evidence(e lifecycle.Execution, condition string) json.RawMessage {
	data, _ := json.Marshal(Evidence{e.Block.RequestDigest, condition})
	return data
}
