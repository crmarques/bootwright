package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
)

// readingOf accepts, for each form, evidence that lists the form's name, and
// records each check it was asked in order.
func readingOf(calls *[]string, forms ...string) EffectReading {
	check := func(form string) func([]byte) error {
		if !slices.Contains(forms, form) {
			return nil
		}
		return func(evidence []byte) error {
			*calls = append(*calls, form)
			if slices.Contains(strings.Split(string(evidence), ","), form) {
				return nil
			}
			return errors.New(form + " refused")
		}
	}
	return EffectReading{
		Presence: check("presence"), Absence: check("absence"), Partial: check("partial"),
		Unremoved: check("unremoved"), Unfinished: check("unfinished"),
	}
}

// An apply reads presence, then absence, then partial; a removal reads
// absence, then unremoved, then unfinished. The first form that accepts the
// evidence decides; when none does the effect is unknown with the refusal of
// the presence check for an apply and of the unfinished check for a removal.
// A form left out never accepts and refuses nothing.
func TestTheEffectReadingDecidesEachVerbAndObservation(t *testing.T) {
	every := []string{"presence", "absence", "partial", "unremoved", "unfinished"}
	for _, test := range []struct {
		verb     reconciliation.Verb
		forms    []string
		evidence string
		want     reconciliation.EffectState
		refusal  string
		calls    []string
	}{
		{reconciliation.Apply, every, "presence", reconciliation.EffectCompleted, "", []string{"presence"}},
		{reconciliation.Apply, every, "presence,absence,partial", reconciliation.EffectCompleted, "", []string{"presence"}},
		{reconciliation.Apply, every, "absence", reconciliation.EffectNoEffect, "", []string{"presence", "absence"}},
		{reconciliation.Apply, every, "absence,partial", reconciliation.EffectNoEffect, "", []string{"presence", "absence"}},
		{reconciliation.Apply, every, "partial", reconciliation.EffectPartial, "", []string{"presence", "absence", "partial"}},
		{reconciliation.Apply, every, "unremoved,unfinished", reconciliation.EffectUnknown, "presence refused", []string{"presence", "absence", "partial"}},
		{reconciliation.Apply, every, "", reconciliation.EffectUnknown, "presence refused", []string{"presence", "absence", "partial"}},
		{reconciliation.Destroy, every, "absence", reconciliation.EffectCompleted, "", []string{"absence"}},
		{reconciliation.Destroy, every, "absence,unremoved,unfinished", reconciliation.EffectCompleted, "", []string{"absence"}},
		{reconciliation.Destroy, every, "unremoved", reconciliation.EffectNoEffect, "", []string{"absence", "unremoved"}},
		{reconciliation.Destroy, every, "unremoved,unfinished", reconciliation.EffectNoEffect, "", []string{"absence", "unremoved"}},
		{reconciliation.Destroy, every, "unfinished", reconciliation.EffectPartial, "", []string{"absence", "unremoved", "unfinished"}},
		{reconciliation.Destroy, every, "presence,partial", reconciliation.EffectUnknown, "unfinished refused", []string{"absence", "unremoved", "unfinished"}},
		{reconciliation.Destroy, every, "", reconciliation.EffectUnknown, "unfinished refused", []string{"absence", "unremoved", "unfinished"}},
		{reconciliation.Apply, []string{"presence"}, "absence,partial", reconciliation.EffectUnknown, "presence refused", []string{"presence"}},
		{reconciliation.Apply, []string{"absence", "partial"}, "presence", reconciliation.EffectUnknown, "", []string{"absence", "partial"}},
		{reconciliation.Apply, []string{"absence", "partial"}, "partial", reconciliation.EffectPartial, "", []string{"absence", "partial"}},
		{reconciliation.Destroy, []string{"absence", "unremoved"}, "unfinished", reconciliation.EffectUnknown, "", []string{"absence", "unremoved"}},
		{reconciliation.Destroy, []string{"unfinished"}, "unfinished", reconciliation.EffectPartial, "", []string{"unfinished"}},
		{reconciliation.Destroy, nil, "absence", reconciliation.EffectUnknown, "", nil},
		{reconciliation.Apply, nil, "presence", reconciliation.EffectUnknown, "", nil},
	} {
		var calls []string
		got, err := ReadEffect(readingOf(&calls, test.forms...), test.verb, []byte(test.evidence))
		refusal := ""
		if err != nil {
			refusal = err.Error()
		}
		if got != test.want || refusal != test.refusal || !slices.Equal(calls, test.calls) {
			t.Errorf("%s over %v reading %q = %s (%q) after %v, want %s (%q) after %v",
				test.verb, test.forms, test.evidence, got, refusal, calls, test.want, test.refusal, test.calls)
		}
	}
}

type mutationCall struct {
	request    string
	operations []string
	result     RunResult
	err        error
}

func (c *mutationCall) mutation(prepare error, proved error) Mutation[string] {
	return Mutation[string]{
		Prepare: func(context.Context, Execution) (string, error) { return c.request, prepare },
		Run: func(_ context.Context, _ Execution, operation, request string) (RunResult, error) {
			c.operations = append(c.operations, operation+" "+request)
			return c.result, c.err
		},
		Reading: func(request, digest string) EffectReading {
			reading := readingOf(new([]string), "presence", "absence", "partial", "unremoved", "unfinished")
			if request == "unchecked" {
				reading.Presence, reading.Absence = nil, nil
			}
			return reading
		},
		Unusable: errors.New("no usable outcome"),
		Proved:   func(Execution, string, RunResult) error { return proved },
	}
}

// A mutation prepares, runs, reads the adapter's outcome, then proves the
// evidence for its operation, and clears every produced material on every
// exit, because a result never carries it.
func TestTheMutationSkeletonMapsEachExitAndClearsWhatWasProduced(t *testing.T) {
	refused := failure("lifecycle.state", "refused", "")
	unknownFailure := failure("lifecycle.unknown", "lost", "")
	for name, test := range map[string]struct {
		operation string
		request   string
		outcome   reconciliation.Outcome
		evidence  string
		runErr    error
		prepare   error
		proved    error
		want      reconciliation.Outcome
		refusal   string
		ran       bool
	}{
		"apply proved":          {"apply", "r", reconciliation.OutcomeChanged, "presence", nil, nil, nil, reconciliation.OutcomeChanged, "", true},
		"apply unchanged":       {"apply", "r", reconciliation.OutcomeUnchanged, "presence", nil, nil, nil, reconciliation.OutcomeUnchanged, "", true},
		"destroy proved":        {"destroy", "r", reconciliation.OutcomeChanged, "absence", nil, nil, nil, reconciliation.OutcomeChanged, "", true},
		"apply of an absence":   {"apply", "r", reconciliation.OutcomeChanged, "absence", nil, nil, nil, reconciliation.OutcomeUnknown, "presence refused", true},
		"destroy of a presence": {"destroy", "r", reconciliation.OutcomeChanged, "presence", nil, nil, nil, reconciliation.OutcomeUnknown, "absence refused", true},
		"unusable outcome":      {"apply", "r", "skipped", "presence", nil, nil, nil, reconciliation.OutcomeUnknown, "no usable outcome", true},
		"no outcome":            {"destroy", "r", "", "absence", nil, nil, nil, reconciliation.OutcomeUnknown, "no usable outcome", true},
		"diagnosed run failure": {"apply", "r", "", "", refused, nil, nil, reconciliation.OutcomeFailed, "refused", true},
		"unknown run failure":   {"apply", "r", "", "", unknownFailure, nil, nil, reconciliation.OutcomeUnknown, "lost", true},
		"plain run failure":     {"apply", "r", "", "", errors.New("unreachable"), nil, nil, reconciliation.OutcomeUnknown, "unreachable", true},
		"unprepared":            {"apply", "r", reconciliation.OutcomeChanged, "presence", nil, refused, nil, reconciliation.OutcomeFailed, "refused", false},
		"unproved":              {"apply", "r", reconciliation.OutcomeChanged, "presence", nil, nil, errors.New("another cluster"), reconciliation.OutcomeUnknown, "another cluster", true},
		"unproved refused":      {"destroy", "r", reconciliation.OutcomeChanged, "presence", nil, nil, errors.New("another cluster"), reconciliation.OutcomeUnknown, "absence refused", true},
		"unchecked":             {"apply", "unchecked", reconciliation.OutcomeChanged, "presence", nil, nil, nil, reconciliation.OutcomeUnknown, "the apply evidence has no check", true},
	} {
		t.Run(name, func(t *testing.T) {
			produced := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("secret")})
			call := &mutationCall{request: test.request, err: test.runErr,
				result: RunResult{Outcome: test.outcome, Evidence: json.RawMessage(test.evidence), Produced: []Produced{{Name: "out", Material: produced}}}}
			result, err := MutateBlock(context.Background(), call.mutation(test.prepare, test.proved), Execution{}, test.operation)
			if result.Outcome != test.want || describedRefusal(err) != test.refusal || result.Produced != nil {
				t.Fatalf("mutation = %+v (%s), want %s (%s)", result, describedRefusal(err), test.want, test.refusal)
			}
			wantEvidence := json.RawMessage(nil)
			if err == nil {
				wantEvidence = json.RawMessage(test.evidence)
			}
			if !bytes.Equal(result.Evidence, wantEvidence) {
				t.Fatalf("evidence = %s, want %s", result.Evidence, wantEvidence)
			}
			if test.ran != (len(call.operations) == 1) || test.ran && call.operations[0] != test.operation+" "+test.request {
				t.Fatalf("ran %v", call.operations)
			}
			if value, _ := produced.Part(secrets.ValuePart); test.ran && !bytes.Equal(value, make([]byte, len("secret"))) {
				t.Fatalf("produced material left uncleared: %q", value)
			}
		})
	}
}

// An observation reads the evidence for the verb it resolves, refuses only a
// call that could not prepare or run, and clears what was produced.
func TestTheMutationSkeletonObservesForEachVerb(t *testing.T) {
	for name, test := range map[string]struct {
		verb     reconciliation.Verb
		evidence string
		runErr   error
		prepare  error
		want     reconciliation.EffectState
		failed   bool
	}{
		"apply completed":   {reconciliation.Apply, "presence", nil, nil, reconciliation.EffectCompleted, false},
		"apply no effect":   {reconciliation.Apply, "absence", nil, nil, reconciliation.EffectNoEffect, false},
		"removal completed": {reconciliation.Destroy, "absence", nil, nil, reconciliation.EffectCompleted, false},
		"removal partial":   {reconciliation.Destroy, "unfinished", nil, nil, reconciliation.EffectPartial, false},
		"refused evidence":  {reconciliation.Destroy, "", nil, nil, reconciliation.EffectUnknown, false},
		"run failure":       {reconciliation.Apply, "presence", errors.New("unreachable"), nil, reconciliation.EffectUnknown, true},
		"unprepared":        {reconciliation.Apply, "presence", nil, errors.New("unreadable"), reconciliation.EffectUnknown, true},
	} {
		t.Run(name, func(t *testing.T) {
			produced := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("secret")})
			call := &mutationCall{request: "r", err: test.runErr,
				result: RunResult{Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(test.evidence), Produced: []Produced{{Name: "out", Material: produced}}}}
			observation, err := ObserveBlock(context.Background(), call.mutation(test.prepare, nil), Execution{}, test.verb)
			if observation.Effect != test.want || (err != nil) != test.failed || observation.Outcome != "" || observation.Produced != nil {
				t.Fatalf("observation = %+v (%v), want %s", observation, err, test.want)
			}
			if !test.failed && !bytes.Equal(observation.Evidence, json.RawMessage(test.evidence)) {
				t.Fatalf("evidence = %s", observation.Evidence)
			}
			if test.prepare == nil && (len(call.operations) != 1 || call.operations[0] != "observe r") {
				t.Fatalf("ran %v", call.operations)
			}
			if value, _ := produced.Part(secrets.ValuePart); test.prepare == nil && !bytes.Equal(value, make([]byte, len("secret"))) {
				t.Fatalf("produced material left uncleared: %q", value)
			}
		})
	}
}

func TestStepGroupsNameEveryStepOnTheSameMachines(t *testing.T) {
	groups := StepGroups([][2]string{{"a", "first"}, {"b", "second"}}, []string{"m"})
	if len(groups) != 2 || groups[0].ID != "a" || groups[1].Description != "second" || !slices.Equal(groups[1].Machines, []string{"m"}) {
		t.Fatalf("groups = %+v", groups)
	}
	if groups := StepGroups(nil, []string{"m"}); groups == nil || len(groups) != 0 {
		t.Fatalf("no step = %#v", groups)
	}
}

func describedRefusal(err error) string {
	if err == nil {
		return ""
	}
	if found := diagnostics.Of(err); len(found) != 0 {
		return found[0].Message
	}
	return err.Error()
}
