package lifecycle

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

const (
	relaxingDestroy = "destroy"
	relaxingPlan    = "plan"
)

type eraseRelaxing struct {
	command      string
	methods      map[string]string
	unregistered map[string]bool
}

type relaxationChange int

const (
	relaxationSame relaxationChange = iota
	relaxationAdded
	relaxationDiffers
)

func (s Service) relaxingFor(command string, methods map[string]string, unregistered []string) Service {
	s.relaxing = eraseRelaxing{command: command, methods: maps.Clone(methods)}
	for _, name := range unregistered {
		if s.relaxing.unregistered == nil {
			s.relaxing.unregistered = map[string]bool{}
		}
		s.relaxing.unregistered[name] = true
	}
	return s
}

func (r eraseRelaxing) over(frozen reconciliation.Plan) relaxationChange {
	methods, unregistered := frozenRelaxations(frozen), frozenUnregistered(frozen)
	if r.command != relaxingDestroy && len(r.methods) == 0 && len(r.unregistered) == 0 || maps.Equal(r.methods, methods) && maps.Equal(r.unregistered, unregistered) {
		return relaxationSame
	}
	for machine, method := range r.methods {
		if methods[machine] != method {
			return relaxationAdded
		}
	}
	for machine := range r.unregistered {
		if !unregistered[machine] {
			return relaxationAdded
		}
	}
	return relaxationDiffers
}

func (r eraseRelaxing) unregisteredFlags() []string {
	if len(r.unregistered) == 0 {
		return nil
	}
	return []string{"--unregistered", strings.Join(slices.Sorted(maps.Keys(r.unregistered)), ",")}
}

func frozenRelaxations(plan reconciliation.Plan) map[string]string {
	var methods map[string]string
	for _, block := range plan.Blocks {
		if block.Relaxation == "" || block.Relaxation == Unregistered {
			continue
		}
		if methods == nil {
			methods = map[string]string{}
		}
		methods[block.Object] = block.Relaxation
	}
	return methods
}

func relaxationFlags(methods map[string]string) []string {
	flags := make([]string, 0, 2*len(methods))
	for _, token := range relaxationTokens(methods) {
		flags = append(flags, "--authorize", token)
	}
	return flags
}

func relaxationToken(machine, method string) string {
	if method == Abandon {
		return AbandonAuthorization + machine
	}
	return EraseAuthorization + machine + ":" + method
}

func abandonedBlocks(plan reconciliation.Plan) map[string]bool {
	var abandoned map[string]bool
	for _, block := range plan.Blocks {
		if block.Relaxation == Abandon {
			if abandoned == nil {
				abandoned = map[string]bool{}
			}
			abandoned[block.ID] = true
		}
	}
	return abandoned
}

func relaxedRemoval(ctx context.Context, capability Capability, block reconciliation.Block, removal Removal, methods map[string]string) (Removal, string, error) {
	method, named := methods[block.Object]
	if named && method == Abandon {
		return abandonedRemoval(ctx, capability, block, removal)
	}
	relaxer, relaxes := capability.(RemovalRelaxer)
	if !named || !relaxes || !relaxer.RelaxesMachine(block) {
		return removal, "", nil
	}
	relaxed, err := relaxer.Relax(ctx, block, method)
	if err != nil {
		return Removal{}, "", err
	}
	relaxed.Consumes = slices.Compact(slices.Sorted(slices.Values(slices.Concat(removal.Consumes, relaxed.Consumes, []string{reconciliation.AuthorizationDataLoss}))))
	return relaxed, method, nil
}

func abandonedRemoval(ctx context.Context, capability Capability, block reconciliation.Block, removal Removal) (Removal, string, error) {
	abandoner, abandons := capability.(RemovalAbandoner)
	if !abandons || !abandoner.AbandonsMachine(block) {
		return removal, "", nil
	}
	abandoned, err := abandoner.Abandon(ctx, block)
	if err != nil {
		return Removal{}, "", err
	}
	abandoned.Consumes = []string{}
	return abandoned, Abandon, nil
}

func (s Service) refuseInapplicableRelaxations(contextName string, decided operationPlan) error {
	methods := s.relaxing.methods
	if len(methods) == 0 {
		return nil
	}
	var held map[string]string
	if decided.verb == reconciliation.Destroy && !decided.noop && !decided.unclaimed && !decided.finalize {
		held = frozenRelaxations(decided.plan)
	}
	for _, machine := range slices.Sorted(maps.Keys(methods)) {
		if held[machine] == methods[machine] {
			continue
		}
		others := maps.Clone(methods)
		delete(others, machine)
		token := relaxationToken(machine, methods[machine])
		reason := "declares no spec.hardware.rootDisk.onRemoval or spec.hardware.dataDisks[].onRemoval this destroy performs, so the relaxation changes nothing"
		if slices.ContainsFunc(decided.plan.Blocks, func(block reconciliation.Block) bool { return block.Object == machine && s.recordsNothing(block) }) {
			reason = "holds a removal step whose apply records prove it has nothing to take back, so the relaxation changes nothing"
		}
		if methods[machine] == Abandon {
			reason = "holds no step this destroy can leave in place, so the abandonment changes nothing"
		}
		return authorizeUnneeded.Fail(
			"--authorize "+token+" names Machine/"+machine+", which "+reason,
			"repeat "+contextCommand(contextName, s.relaxing.command, invocationFlags(decided.floor(), reconciliation.Selection{}, append(relaxationFlags(others), s.relaxing.unregisteredFlags()...))...)+" without --authorize "+token)
	}
	return nil
}

func (s Service) refuseUnattendedAbandonment(contextName string, unattended bool) error {
	if !unattended {
		return nil
	}
	for _, machine := range slices.Sorted(maps.Keys(s.relaxing.methods)) {
		if s.relaxing.methods[machine] != Abandon {
			continue
		}
		return authorizeAbandonUnattended.Fail(
			"an unattended destroy cannot leave the steps of Machine/"+machine+" in place, because --authorize "+relaxationToken(machine, Abandon)+" takes a confirmation an operator answers",
			"run "+contextCommand(contextName, s.relaxing.command, append(relaxationFlags(s.relaxing.methods), s.relaxing.unregisteredFlags()...)...)+" in a terminal without --yes, and confirm its plan")
	}
	return nil
}

func refuseChangedRelaxations(contextName string, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) error {
	held := relaxationTokens(frozenRelaxations(frozen))
	if flags := unregisteredFlags(frozen); len(flags) != 0 {
		held = append(held, strings.Join(flags, " "))
	}
	if len(held) == 0 {
		held = []string{"none"}
	}
	return authorizeRelaxationChanged.Fail(
		"the "+string(operation.Verb)+" "+operation.ID+" continues only under the relaxations its plan froze ("+strings.Join(held, ", ")+"), and this invocation names a different set",
		"continue it with "+continuationCommand(contextName, operation, frozen, states)+", or replace it with a fresh destroy by naming a relaxation its plan does not hold")
}

func relaxationTokens(methods map[string]string) []string {
	tokens := make([]string, 0, len(methods))
	for _, machine := range slices.Sorted(maps.Keys(methods)) {
		tokens = append(tokens, relaxationToken(machine, methods[machine]))
	}
	return tokens
}
