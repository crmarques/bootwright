package reconciliation

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestAttemptTransitionCoversEveryOutcome(t *testing.T) {
	want := map[Outcome][2]string{
		OutcomeChanged:   {string(EffectCompleted), string(BlockDone)},
		OutcomeUnchanged: {string(EffectCompleted), string(BlockDone)},
		OutcomeFailed:    {string(EffectUnknown), string(BlockFailed)},
		OutcomeCanceled:  {string(EffectUnknown), string(BlockUnknown)},
		OutcomeUnknown:   {string(EffectUnknown), string(BlockUnknown)},
	}
	for outcome, expected := range want {
		effect, block, err := AttemptTransition(outcome)
		if err != nil || string(effect) != expected[0] || string(block) != expected[1] {
			t.Fatalf("%s transition = %s/%s (%v), want %v", outcome, effect, block, err, expected)
		}
	}
	if _, _, err := AttemptTransition("succeeded"); err == nil {
		t.Fatal("an unrecognized outcome produced a transition")
	}
	for _, outcome := range []Outcome{OutcomeChanged, OutcomeUnchanged, OutcomeFailed, OutcomeCanceled, OutcomeUnknown} {
		if !ValidOutcome(outcome) {
			t.Fatalf("%s is not a recognized outcome", outcome)
		}
	}
}

func TestResolutionTransitionFollowsTheEvidenceTable(t *testing.T) {
	for observed, want := range map[EffectState][2]string{
		EffectCompleted: {string(EffectCompleted), string(BlockDone)},
		EffectNoEffect:  {string(EffectNoEffect), string(BlockFailed)},
		// A target the capability proves is part way realized and its own is
		// converged by repeating the operation, so it fails rather than
		// stranding the context behind an unproved effect.
		EffectPartial: {string(EffectPartial), string(BlockFailed)},
		EffectUnknown: {string(EffectUnknown), string(BlockUnknown)},
	} {
		effect, block, err := ResolutionTransition(observed)
		if err != nil || string(effect) != want[0] || string(block) != want[1] {
			t.Fatalf("%s resolution = %s/%s (%v), want %v", observed, effect, block, err, want)
		}
	}
	if _, _, err := ResolutionTransition("half"); err == nil {
		t.Fatal("an unrecognized observation produced a transition")
	}
}

// A resolution records the outcome its capability proved for a completed
// effect, never one it invents: a proof that changed nothing reads back
// unchanged, and only a completion whose outcome nothing proved reads changed.
func TestResolutionOutcomeRecordsWhatTheCapabilityProved(t *testing.T) {
	for name, tc := range map[string]struct {
		resolved EffectState
		proved   Outcome
		want     Outcome
	}{
		"completed unchanged":      {EffectCompleted, OutcomeUnchanged, OutcomeUnchanged},
		"completed changed":        {EffectCompleted, OutcomeChanged, OutcomeChanged},
		"completed unproved":       {EffectCompleted, "", OutcomeChanged},
		"completed as failed":      {EffectCompleted, OutcomeFailed, OutcomeChanged},
		"completed as unknown":     {EffectCompleted, OutcomeUnknown, OutcomeChanged},
		"no effect":                {EffectNoEffect, OutcomeUnchanged, OutcomeFailed},
		"partial":                  {EffectPartial, OutcomeChanged, OutcomeFailed},
		"unknown":                  {EffectUnknown, OutcomeUnchanged, OutcomeUnknown},
		"an unrecognized evidence": {"half", OutcomeChanged, OutcomeUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			if got := ResolutionOutcome(tc.resolved, tc.proved); got != tc.want {
				t.Fatalf("ResolutionOutcome(%s, %q) = %s, want %s", tc.resolved, tc.proved, got, tc.want)
			}
		})
	}
}

func TestNextOperationStateLetsUnknownDominate(t *testing.T) {
	for name, tc := range map[string]struct {
		states   []BlockState
		boundary bool
		want     OperationState
	}{
		"all done":                       {[]BlockState{BlockDone, BlockDone}, false, OperationDone},
		"pending remains":                {[]BlockState{BlockDone, BlockPending}, false, OperationRunning},
		"failed":                         {[]BlockState{BlockDone, BlockFailed}, false, OperationFailed},
		"unknown over failed":            {[]BlockState{BlockFailed, BlockUnknown}, false, OperationUnknown},
		"unknown over done":              {[]BlockState{BlockDone, BlockUnknown}, false, OperationUnknown},
		"running":                        {[]BlockState{BlockRunning}, false, OperationRunning},
		"empty":                          {nil, false, OperationDone},
		"stage boundary":                 {[]BlockState{BlockDone, BlockPending}, true, OperationPaused},
		"boundary never hides a failure": {[]BlockState{BlockFailed, BlockPending}, true, OperationFailed},
		"boundary never hides unknown":   {[]BlockState{BlockUnknown, BlockPending}, true, OperationUnknown},
		"boundary with everything done":  {[]BlockState{BlockDone}, true, OperationDone},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := NextOperationState(tc.states, tc.boundary)
			if err != nil || got != tc.want {
				t.Fatalf("operation state = %s (%v), want %s", got, err, tc.want)
			}
		})
	}
	if _, err := NextOperationState([]BlockState{"partial"}, false); err == nil {
		t.Fatal("an unrecognized block state produced an operation state")
	}
}

func TestOperationIdentityAllocationAndExhaustion(t *testing.T) {
	entropy := bytes.Repeat([]byte{7}, operationEntropy*(allocationAttempts+1))
	id, err := AllocateOperationID(bytes.NewReader(entropy).Read, func(string) bool { return false })
	if err != nil || !ValidOperationID(id) || !strings.HasPrefix(id, "op-") || len(id) != 35 {
		t.Fatalf("allocated identity = %q (%v)", id, err)
	}
	attempts := 0
	_, err = AllocateOperationID(bytes.NewReader(entropy).Read, func(string) bool { attempts++; return true })
	if err == nil || attempts != allocationAttempts {
		t.Fatalf("collision exhaustion made %d attempts (%v)", attempts, err)
	}
	if _, err = AllocateOperationID(bytes.NewReader(nil).Read, func(string) bool { return false }); err == nil {
		t.Fatal("entropy failure produced an identity")
	}
	if _, err = AllocateOperationID(nil, nil); err == nil {
		t.Fatal("an unconfigured allocator produced an identity")
	}
}

func TestOperationIdentityGrammar(t *testing.T) {
	valid := "op-" + strings.Repeat("ab", 16)
	for name, value := range map[string]string{
		"valid":       valid,
		"no prefix":   strings.Repeat("ab", 16),
		"short":       "op-abcd",
		"uppercase":   "op-" + strings.Repeat("AB", 16),
		"non hex":     "op-" + strings.Repeat("gg", 16),
		"long":        valid + "a",
		"empty":       "",
		"other kinds": "rev-" + strings.Repeat("ab", 16),
	} {
		if got := ValidOperationID(value); got != (name == "valid") {
			t.Fatalf("%s: ValidOperationID(%q) = %t", name, value, got)
		}
	}
}

func TestAttemptNumberFormattingAndExhaustion(t *testing.T) {
	for value, want := range map[int]string{1: "000001", 42: "000042", MaxAttempt: "999999"} {
		got, err := FormatNumber(value)
		if err != nil || got != want {
			t.Fatalf("FormatNumber(%d) = %q (%v), want %q", value, got, err, want)
		}
		parsed, err := ParseNumber(want)
		if err != nil || parsed != value {
			t.Fatalf("ParseNumber(%q) = %d (%v)", want, parsed, err)
		}
	}
	for _, value := range []int{0, -1, MaxAttempt + 1} {
		if _, err := FormatNumber(value); err == nil {
			t.Fatalf("FormatNumber(%d) produced a path", value)
		}
	}
	for _, value := range []string{"1", "0000001", "000000", "abcdef", "", "00000a", "1000000"} {
		if _, err := ParseNumber(value); err == nil {
			t.Fatalf("ParseNumber(%q) accepted a non-canonical number", value)
		}
	}
}

// The guard reads these exact bytes, so a drift here silently changes what a
// context considers disposable.
func TestPristineEvidenceMatchesTheStoredRecord(t *testing.T) {
	data, err := PristineEvidence().Bytes()
	if err != nil || string(data) != "{\"version\":1,\"operation\":\"none\",\"ownership\":\"none\"}\n" {
		t.Fatalf("pristine evidence = %q (%v)", data, err)
	}
}

func TestEvidenceForEveryTerminalState(t *testing.T) {
	for name, tc := range map[string]struct {
		verb      Verb
		state     OperationState
		operation MutationOperation
		ownership MutationOwnership
	}{
		"apply running":   {Apply, OperationRunning, MutationPending, OwnershipRetained},
		"apply failed":    {Apply, OperationFailed, MutationFailed, OwnershipRetained},
		"apply unknown":   {Apply, OperationUnknown, MutationUnknown, OwnershipRetained},
		"apply done":      {Apply, OperationDone, MutationApplied, OwnershipRetained},
		"destroy running": {Destroy, OperationRunning, MutationPending, OwnershipRetained},
		"destroy failed":  {Destroy, OperationFailed, MutationFailed, OwnershipRetained},
		"destroy unknown": {Destroy, OperationUnknown, MutationUnknown, OwnershipRetained},
		"destroy done":    {Destroy, OperationDone, MutationNone, OwnershipNone},
	} {
		t.Run(name, func(t *testing.T) {
			evidence, err := EvidenceFor(tc.verb, tc.state)
			if err != nil || evidence.Operation != tc.operation || evidence.Ownership != tc.ownership {
				t.Fatalf("evidence = %+v (%v), want %s/%s", evidence, err, tc.operation, tc.ownership)
			}
			if _, err := evidence.Bytes(); err != nil {
				t.Fatal(err)
			}
		})
	}
	if _, err := EvidenceFor("reconcile", OperationDone); err == nil {
		t.Fatal("an unrecognized verb produced evidence")
	}
	if _, err := EvidenceFor(Apply, "partial"); err == nil {
		t.Fatal("an unrecognized operation state produced evidence")
	}
	if _, err := (Evidence{Operation: "adopted", Ownership: OwnershipNone}).Bytes(); err == nil {
		t.Fatal("an invalid evidence value encoded")
	}
}

func TestDomainFailuresCarryTheLifecycleCode(t *testing.T) {
	_, err := NewPlan("reconcile", nil)
	var failure *diagnostics.Failure
	if !errors.As(err, &failure) || len(failure.Diagnostics) != 1 || failure.Diagnostics[0].Code != "lifecycle.state" {
		t.Fatalf("plan failure = %v", err)
	}
}

func TestEntropyReaderIsNotRetained(t *testing.T) {
	reader := &countingReader{Reader: bytes.NewReader(bytes.Repeat([]byte{1}, operationEntropy))}
	if _, err := AllocateOperationID(reader.Read, func(string) bool { return false }); err != nil {
		t.Fatal(err)
	}
	if reader.reads != 1 {
		t.Fatalf("allocation performed %d entropy reads, want 1", reader.reads)
	}
}

type countingReader struct {
	io.Reader
	reads int
}

func (r *countingReader) Read(data []byte) (int, error) {
	r.reads++
	return r.Reader.Read(data)
}

func TestValidSegmentMatchesTheIdentityGrammar(t *testing.T) {
	valid := []string{"a", "a0", "alpha-bravo", strings.Repeat("a", 63)}
	invalid := []string{"", "-a", "a-", "A", "a_b", "a.b", "a/b", "a b", strings.Repeat("a", 64)}
	for _, value := range valid {
		if !ValidSegment(value) {
			t.Fatalf("ValidSegment(%q) = false", value)
		}
	}
	for _, value := range invalid {
		if ValidSegment(value) {
			t.Fatalf("ValidSegment(%q) = true", value)
		}
	}
	if slices.Contains(valid, "") {
		t.Fatal("the empty segment must never be valid")
	}
}
