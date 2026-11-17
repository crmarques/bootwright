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
	command string
	methods map[string]string
}

type relaxationChange int

const (
	relaxationSame relaxationChange = iota
	relaxationAdded
	relaxationDiffers
)

func (s Service) relaxingFor(command string, methods map[string]string) Service {
	s.relaxing = eraseRelaxing{command: command, methods: maps.Clone(methods)}
	return s
}

func (r eraseRelaxing) over(frozen map[string]string) relaxationChange {
	if r.command != relaxingDestroy && len(r.methods) == 0 || maps.Equal(r.methods, frozen) {
		return relaxationSame
	}
	for machine, method := range r.methods {
		if frozen[machine] != method {
			return relaxationAdded
		}
	}
	return relaxationDiffers
}

func frozenRelaxations(plan reconciliation.Plan) map[string]string {
	var methods map[string]string
	for _, block := range plan.Blocks {
		if block.Relaxation == "" {
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

func eraseToken(machine, method string) string {
	return EraseAuthorization + machine + ":" + method
}

func relaxedRemoval(ctx context.Context, capability Capability, block reconciliation.Block, removal Removal, methods map[string]string) (Removal, string, error) {
	method, named := methods[block.Object]
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
		token := eraseToken(machine, methods[machine])
		return failure("lifecycle.authorization",
			"--authorize "+token+" names Machine/"+machine+", which declares no spec.hardware.rootDisk.onRemoval or spec.hardware.dataDisks[].onRemoval this destroy performs, so the relaxation changes nothing",
			"repeat "+contextCommand(contextName, s.relaxing.command, relaxationFlags(others)...)+" without --authorize "+token)
	}
	return nil
}

func refuseChangedRelaxations(contextName string, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) error {
	held := "none"
	if methods := frozenRelaxations(frozen); len(methods) != 0 {
		held = strings.Join(relaxationTokens(methods), ", ")
	}
	return failure("lifecycle.authorization",
		"the "+string(operation.Verb)+" "+operation.ID+" continues only under the erase relaxations its plan froze ("+held+"), and this invocation names a different set",
		"continue it with "+continuationCommand(contextName, operation, frozen, states)+", or replace it with a fresh destroy by naming a relaxation its plan does not hold")
}

func relaxationTokens(methods map[string]string) []string {
	tokens := make([]string, 0, len(methods))
	for _, machine := range slices.Sorted(maps.Keys(methods)) {
		tokens = append(tokens, eraseToken(machine, methods[machine]))
	}
	return tokens
}
