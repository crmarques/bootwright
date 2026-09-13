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

// capabilityBinding is what the resolved capabilities together need before
// their blocks may run: the host resources they claim and the Secrets their
// execution consumes.
type capabilityBinding struct {
	reservations []prerequisites.HostReservation
	secrets      []string
}

// compile reads the frozen input once, so a decision never compiles the same
// revision twice to answer two questions about it.
func (s Service) compile(ctx context.Context, view View) (*compilation.State, error) {
	state, report, err := s.compiler.Compile(ctx, view.Inputs())
	if err != nil {
		return nil, err
	}
	if state == nil || report == nil {
		return nil, failure("lifecycle.state", "the frozen desired state could not be compiled", "repair the input with validate and import it again")
	}
	return state, nil
}

// planFrom asks every resolvable capability for its blocks. Planning is pure:
// it opens no payload, contacts nothing and materializes no Secret.
func (s Service) planFrom(ctx context.Context, view View, state *compilation.State, verb reconciliation.Verb) (reconciliation.Plan, capabilityBinding, error) {
	controller, err := controllerMachine(state)
	if err != nil {
		return reconciliation.Plan{}, capabilityBinding{}, err
	}
	kinds := s.capabilities.Kinds()
	if len(kinds) == 0 {
		return reconciliation.Plan{}, capabilityBinding{}, failure("lifecycle.state", "no lifecycle capability is available for the selected state", "")
	}
	input := PlanInput{Verb: verb, Context: view.Identity(), State: state, Controller: controller}
	var definitions []reconciliation.BlockDefinition
	var binding capabilityBinding
	for _, kind := range kinds {
		capability, ok := s.capabilities.Resolve(kind, "")
		if !ok {
			return reconciliation.Plan{}, capabilityBinding{}, failure("lifecycle.state", "a declared lifecycle capability does not resolve", "")
		}
		contribution, err := capability.Plan(ctx, input)
		if err != nil {
			return reconciliation.Plan{}, capabilityBinding{}, err
		}
		definitions = append(definitions, contribution.Definitions...)
		binding.reservations = append(binding.reservations, contribution.Reservations...)
		for _, reference := range contribution.Secrets {
			if !slices.Contains(binding.secrets, reference) {
				binding.secrets = append(binding.secrets, reference)
			}
		}
	}
	slices.Sort(binding.secrets)
	plan, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	if err != nil {
		return reconciliation.Plan{}, capabilityBinding{}, err
	}
	if verb == reconciliation.Destroy {
		plan = plan.Inverse()
	}
	return plan, binding, nil
}

func (s Service) freshPlan(ctx context.Context, view View, verb reconciliation.Verb) (reconciliation.Plan, capabilityBinding, error) {
	state, err := s.compile(ctx, view)
	if err != nil {
		return reconciliation.Plan{}, capabilityBinding{}, err
	}
	return s.planFrom(ctx, view, state, verb)
}

// refuseUnsupported names every selected object whose realization this
// executable cannot perform, before any registration or effect. A capability
// reports what it cannot do within its own kinds; the engine reports every
// effect-bearing kind no capability claims at all.
func (s Service) refuseUnsupported(state *compilation.State) error {
	kinds := s.capabilities.Kinds()
	var unsupported []string
	for _, kind := range kinds {
		capability, ok := s.capabilities.Resolve(kind, "")
		if !ok {
			continue
		}
		if reporter, ok := capability.(UnsupportedReporter); ok {
			unsupported = append(unsupported, reporter.Unsupported(state)...)
		}
	}
	unsupported = append(unsupported, Unrealizable(state.Effective(), kinds)...)
	slices.Sort(unsupported)
	unsupported = slices.Compact(unsupported)
	if len(unsupported) == 0 {
		return nil
	}
	return failure("lifecycle.state",
		"this executable cannot realize "+strings.Join(unsupported, ", "),
		"remove those objects from the selected Environment, or use an example within the supported shape such as "+supportedExample)
}

const supportedExample = "examples/managed-infra-components"

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
