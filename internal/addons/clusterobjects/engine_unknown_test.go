package clusterobjects

import "testing"

// The engine once carried its unknown read by rewriting its first observation
// to an unknown ownership, and judged a classification Go never read. Each row
// is one engine outcome: the evidence the old engine wrote, judged as Go
// judged it, and the evidence the engine writes now with its unknown field.
// They agree wherever the old evidence had an observation to rewrite; a
// program with none lost the unknown, which Go now reads.
func TestTheEngineUnknownFieldCarriesWhatTheRewrittenObservationCarried(t *testing.T) {
	p := programFixture(t)
	o := p.Steps[0].Create
	checksOnly := Program{Version: Version, Steps: []Step{}, Checks: p.Checks}
	receipt := []Receipt{{Ref: o.Ref, UID: "uid", DesiredDigest: o.DesiredDigest, Disposition: "created", Basis: Created}}
	satisfied := []CheckResult{{Index: 0, Satisfied: true, LastObservation: "present"}}
	unsatisfied := []CheckResult{{Index: 0, LastObservation: "absent"}}
	for name, test := range map[string]struct {
		program  Program
		old, now Evidence
		oldWant  Realization
		want     Realization
	}{
		"an unknown read beside an own object": {p,
			Evidence{Receipts: receipt, Observations: []ObjectObservation{{Ref: o.Ref, Ownership: Unknown}}, Checks: satisfied},
			Evidence{Receipts: receipt, Observations: []ObjectObservation{{Ref: o.Ref, Ownership: Own, Equal: true}}, Checks: satisfied, Unknown: true},
			UnknownRealization, UnknownRealization},
		"an unknown read beside an absent object": {p,
			Evidence{Observations: []ObjectObservation{{Ref: o.Ref, Ownership: Unknown}}, Checks: unsatisfied},
			Evidence{Observations: []ObjectObservation{{Ref: o.Ref, Ownership: Absent}}, Checks: unsatisfied, Unknown: true},
			UnknownRealization, UnknownRealization},
		"no unknown read, completed": {p,
			Evidence{Receipts: receipt, Observations: []ObjectObservation{{Ref: o.Ref, Ownership: Own, Equal: true}}, Checks: satisfied},
			Evidence{Receipts: receipt, Observations: []ObjectObservation{{Ref: o.Ref, Ownership: Own, Equal: true}}, Checks: satisfied},
			Completed, Completed},
		"no unknown read, absent": {p,
			Evidence{Observations: []ObjectObservation{{Ref: o.Ref, Ownership: Absent}}, Checks: unsatisfied},
			Evidence{Observations: []ObjectObservation{{Ref: o.Ref, Ownership: Absent}}, Checks: unsatisfied},
			NoEffect, NoEffect},
		"an unknown read with no object to carry it": {checksOnly,
			Evidence{Checks: satisfied},
			Evidence{Checks: satisfied, Unknown: true},
			Completed, UnknownRealization},
	} {
		if got := EvaluateEvidence(test.program, test.old); got != test.oldWant {
			t.Errorf("%s: the old evidence reads %s, want %s", name, got, test.oldWant)
		}
		if got := EvaluateEvidence(test.program, test.now); got != test.want {
			t.Errorf("%s: the evidence reads %s, want %s", name, got, test.want)
		}
	}
}
