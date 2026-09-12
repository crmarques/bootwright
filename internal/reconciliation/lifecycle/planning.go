package lifecycle

import (
	"context"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// capabilityBinding names one resolved capability and the blocks it owns.
type capabilityBinding struct {
	kind         string
	capability   Capability
	reservations []prerequisites.HostReservation
	secrets      []string
}

// freshPlan compiles the frozen input and asks every resolvable capability for
// its blocks. Planning is pure: it opens no payload, contacts nothing and
// materializes no Secret.
func (s Service) freshPlan(ctx context.Context, view View, verb reconciliation.Verb) (reconciliation.Plan, capabilityBinding, error) {
	state, report, err := s.compiler.Compile(ctx, view.Inputs())
	if err != nil {
		return reconciliation.Plan{}, capabilityBinding{}, err
	}
	if state == nil || report == nil {
		return reconciliation.Plan{}, capabilityBinding{}, failure("lifecycle.state", "the frozen desired state could not be compiled", "repair the input with validate and import it again")
	}
	controller, err := controllerMachine(state)
	if err != nil {
		return reconciliation.Plan{}, capabilityBinding{}, err
	}
	capability, ok := s.capabilities.Resolve(artifactServerKind, "")
	if !ok {
		return reconciliation.Plan{}, capabilityBinding{}, failure("lifecycle.state", "no lifecycle capability is available for the selected state", "")
	}
	input := PlanInput{Verb: verb, Context: view.Identity(), State: state, Controller: controller}
	contribution, err := capability.Plan(ctx, input)
	if err != nil {
		return reconciliation.Plan{}, capabilityBinding{}, err
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, contribution.Definitions)
	if err != nil {
		return reconciliation.Plan{}, capabilityBinding{}, err
	}
	if verb == reconciliation.Destroy {
		plan = plan.Inverse()
	}
	binding := capabilityBinding{
		kind: artifactServerKind, capability: capability,
		reservations: contribution.Reservations, secrets: slices.Clone(contribution.Secrets),
	}
	return plan, binding, nil
}

const artifactServerKind = "ArtifactServer"

// refuseUnsupported names every selected object whose realization this
// executable cannot perform, before any registration or effect.
func (s Service) refuseUnsupported(ctx context.Context, view View) error {
	state, _, err := s.compiler.Compile(ctx, view.Inputs())
	if err != nil {
		return err
	}
	if state == nil {
		return failure("lifecycle.state", "the frozen desired state could not be compiled", "repair the input with validate and import it again")
	}
	capability, ok := s.capabilities.Resolve(artifactServerKind, "")
	if !ok {
		return failure("lifecycle.state", "no lifecycle capability is available for the selected state", "")
	}
	reporter, ok := capability.(UnsupportedReporter)
	if !ok {
		return nil
	}
	unsupported := reporter.Unsupported(state)
	if len(unsupported) == 0 {
		return nil
	}
	return failure("lifecycle.state",
		"this executable cannot realize "+strings.Join(unsupported, ", "),
		"remove those objects from the selected Environment, or use an example within the supported shape such as examples/lab-artifacts")
}

func controllerMachine(state *compilation.State) (string, error) {
	environments := state.Effective().OfKind(api.Environment)
	if len(environments) != 1 {
		return "", failure("lifecycle.state", "the selected state does not contain exactly one Environment", "repair the input with validate")
	}
	name := environments[0].Spec().Get("controller", "machineRef").Text()
	if name == "" {
		return "", failure("lifecycle.state", "the Environment declares no controller Machine", "set spec.controller.machineRef")
	}
	return name, nil
}
