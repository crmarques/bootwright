package lifecycle

import (
	"context"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

type EffectReading struct {
	Presence, Absence, Partial, Unremoved, Unfinished func([]byte) error
}

func ReadEffect(r EffectReading, verb reconciliation.Verb, evidence []byte) (reconciliation.EffectState, error) {
	if verb == reconciliation.Destroy {
		switch {
		case accepts(r.Absence, evidence):
			return reconciliation.EffectCompleted, nil
		case accepts(r.Unremoved, evidence):
			return reconciliation.EffectNoEffect, nil
		case r.Unfinished == nil:
			return reconciliation.EffectUnknown, nil
		}
		if err := r.Unfinished(evidence); err != nil {
			return reconciliation.EffectUnknown, err
		}
		return reconciliation.EffectPartial, nil
	}
	var presence error
	if r.Presence != nil {
		if presence = r.Presence(evidence); presence == nil {
			return reconciliation.EffectCompleted, nil
		}
	}
	switch {
	case accepts(r.Absence, evidence):
		return reconciliation.EffectNoEffect, nil
	case accepts(r.Partial, evidence):
		return reconciliation.EffectPartial, nil
	}
	return reconciliation.EffectUnknown, presence
}

func accepts(check func([]byte) error, evidence []byte) bool {
	return check != nil && check(evidence) == nil
}

type Mutation[R any] struct {
	Prepare  func(context.Context, Execution) (R, error)
	Run      func(context.Context, Execution, string, R) (RunResult, error)
	Reading  func(R, string) EffectReading
	Unusable error
	Proved   func(Execution, R, RunResult) error
}

func MutateBlock[R any](ctx context.Context, m Mutation[R], execution Execution, operation string) (Result, error) {
	unknown := Result{Outcome: reconciliation.OutcomeUnknown}
	request, err := m.Prepare(ctx, execution)
	if err != nil {
		return Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	result, err := m.Run(ctx, execution, operation, request)
	defer ClearProduced(result.Produced)
	if err != nil {
		return Result{Outcome: AttemptOutcome(err)}, err
	}
	if !result.Outcome.Settled() {
		return unknown, m.Unusable
	}
	reading := m.Reading(request, execution.Block.RequestDigest)
	proof := reading.Absence
	if operation == string(reconciliation.Apply) {
		proof = reading.Presence
	}
	if proof == nil {
		return unknown, engineRecordsDamaged.Fail("the "+operation+" evidence has no check", "")
	}
	if err := proof(result.Evidence); err != nil {
		return unknown, err
	}
	if m.Proved != nil {
		if err := m.Proved(execution, request, result); err != nil {
			return unknown, err
		}
	}
	return Result{Outcome: result.Outcome, Evidence: result.Evidence}, nil
}

func ObserveBlock[R any](ctx context.Context, m Mutation[R], execution Execution, verb reconciliation.Verb) (Observation, error) {
	unknown := Observation{Effect: reconciliation.EffectUnknown}
	request, err := m.Prepare(ctx, execution)
	if err != nil {
		return unknown, err
	}
	result, err := m.Run(ctx, execution, "observe", request)
	defer ClearProduced(result.Produced)
	if err != nil {
		return unknown, err
	}
	effect, _ := ReadEffect(m.Reading(request, execution.Block.RequestDigest), verb, result.Evidence)
	return Observation{Effect: effect, Evidence: result.Evidence}, nil
}

func StepGroups(steps [][2]string, machines []string) []reconciliation.Group {
	out := make([]reconciliation.Group, 0, len(steps))
	for _, step := range steps {
		out = append(out, reconciliation.Group{ID: step[0], Description: step[1], Machines: machines})
	}
	return out
}
