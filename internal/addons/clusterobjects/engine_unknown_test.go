package clusterobjects

import "testing"

// Each row is one engine outcome: the evidence the engine writes with its
// unknown field, and the realization Go reads from it. A program with no
// object to carry the unknown still reads it.
func TestTheEngineUnknownFieldDecidesTheRealization(t *testing.T) {
	p := programFixture(t)
	o := p.Steps[0].Create
	checksOnly := Program{Version: Version, Steps: []Step{}, Checks: p.Checks}
	receipt := []Receipt{{Ref: o.Ref, UID: "uid", DesiredDigest: o.DesiredDigest, Disposition: "created", Basis: Created}}
	satisfied := []CheckResult{{Index: 0, Satisfied: true, LastObservation: "present"}}
	unsatisfied := []CheckResult{{Index: 0, LastObservation: "absent"}}
	for name, test := range map[string]struct {
		program Program
		now     Evidence
		want    Realization
	}{
		"an unknown read beside an own object": {p,
			Evidence{Receipts: receipt, Observations: []ObjectObservation{{Ref: o.Ref, Ownership: Own, Equal: true}}, Checks: satisfied, Unknown: true},
			UnknownRealization},
		"an unknown read beside an absent object": {p,
			Evidence{Observations: []ObjectObservation{{Ref: o.Ref, Ownership: Absent}}, Checks: unsatisfied, Unknown: true},
			UnknownRealization},
		"no unknown read, completed": {p,
			Evidence{Receipts: receipt, Observations: []ObjectObservation{{Ref: o.Ref, Ownership: Own, Equal: true}}, Checks: satisfied},
			Completed},
		"no unknown read, absent": {p,
			Evidence{Observations: []ObjectObservation{{Ref: o.Ref, Ownership: Absent}}, Checks: unsatisfied},
			NoEffect},
		"an unknown read with no object to carry it": {checksOnly,
			Evidence{Checks: satisfied, Unknown: true},
			UnknownRealization},
	} {
		if got := EvaluateEvidence(test.program, test.now); got != test.want {
			t.Errorf("%s: the evidence reads %s, want %s", name, got, test.want)
		}
	}
}
