package clusterobjects

import (
	"strings"
	"testing"
	"time"
)

func TestOwnershipProofTable(t *testing.T) {
	object := objectFixture(t, "settings")
	registered := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	base := Probe{Ref: object.Ref, ReadKnown: true, Present: true, UID: "uid-1", OwnerMark: OwnerMark("operation-1", "block-1", object.Ref), CreatedAt: registered}
	receipt := Receipt{Ref: object.Ref, UID: base.UID, DesiredDigest: object.DesiredDigest, Disposition: "created"}
	cases := []struct {
		name    string
		change  func(*Probe)
		receipt *Receipt
		want    Ownership
	}{
		{"receipt", func(p *Probe) { p.OwnerMark = ""; p.CreatedAt = registered.Add(-time.Hour) }, &receipt, Own},
		{"lost receipt", func(p *Probe) {}, nil, Own},
		{"copied mark", func(p *Probe) { p.Name = "another" }, nil, Foreign},
		{"predates registration", func(p *Probe) { p.CreatedAt = registered.Add(-ClockSkew - time.Nanosecond) }, nil, Foreign},
		{"skew boundary", func(p *Probe) { p.CreatedAt = registered.Add(-ClockSkew) }, nil, Own},
		{"no mark", func(p *Probe) { p.OwnerMark = "" }, nil, Foreign},
		{"receipt uid replaced", func(p *Probe) { p.UID = "uid-2" }, &receipt, Foreign},
		{"absent", func(p *Probe) { p.Present = false }, &receipt, Absent},
		{"unknown", func(p *Probe) { p.ReadKnown = false }, &receipt, Unknown},
		{"missing uid", func(p *Probe) { p.UID = "" }, nil, Unknown},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			probe := base
			test.change(&probe)
			if got := EvaluateOwnership(probe, test.receipt, "operation-1", "block-1", registered); got != test.want {
				t.Fatalf("got %s want %s", got, test.want)
			}
		})
	}
}

func TestDependentProof(t *testing.T) {
	creator := Probe{Ref: objectFixture(t, "settings").Ref, ReadKnown: true, Present: true, UID: "creator", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	expected := DependentApproval{Namespace: "dependent", Subscription: "engine", Package: "engine", Channel: "stable", Source: "catalog", SourceNamespace: "marketplace", CSV: "engine.v1.0.0", Creator: creator.Ref}
	probe := DependentProbe{Probe: Probe{Ref: Ref{APIVersion: "operators.coreos.com/v1alpha1", Kind: "Subscription", Namespace: expected.Namespace, Name: expected.Subscription, Namespaced: true}, ReadKnown: true, Present: true, UID: "subscription", CreatedAt: creator.CreatedAt}, Package: expected.Package, Channel: expected.Channel, Source: expected.Source, SourceNamespace: expected.SourceNamespace, StartingCSV: expected.CSV, Approval: "Manual"}
	if got := EvaluateDependent(probe, expected, creator, Own); got != Dependent {
		t.Fatal(got)
	}
	for name, change := range map[string]func(*DependentProbe){"channel": func(p *DependentProbe) { p.Channel = "other" }, "age": func(p *DependentProbe) { p.CreatedAt = p.CreatedAt.Add(-time.Nanosecond) }, "approval": func(p *DependentProbe) { p.Approval = "Automatic" }} {
		t.Run(name, func(t *testing.T) {
			changed := probe
			change(&changed)
			if got := EvaluateDependent(changed, expected, creator, Own); got != Foreign {
				t.Fatal(got)
			}
		})
	}
}

func TestObservationTable(t *testing.T) {
	for _, test := range []struct {
		name        string
		observation Observation
		want        Realization
	}{
		{"complete", Observation{Objects: []ObjectObservation{{Ownership: Own, Equal: true}}, Checks: []CheckResult{{Satisfied: true}}}, Completed},
		{"absent", Observation{Objects: []ObjectObservation{{Ownership: Absent}}}, NoEffect},
		{"partial", Observation{Objects: []ObjectObservation{{Ownership: Own, Equal: true}, {Ownership: Absent}}}, Partial},
		{"different", Observation{Objects: []ObjectObservation{{Ownership: Own, Equal: false}}}, Partial},
		{"not ready", Observation{Objects: []ObjectObservation{{Ownership: Own, Equal: true}}, Checks: []CheckResult{{Satisfied: false}}}, Partial},
		{"stale satisfied", Observation{Objects: []ObjectObservation{{Ownership: Own, Equal: true}}, Checks: []CheckResult{{Satisfied: true, Stale: true}}}, Completed},
		{"foreign", Observation{Objects: []ObjectObservation{{Ownership: Foreign}}}, UnknownRealization},
		{"read failed", Observation{Objects: []ObjectObservation{{Ownership: Unknown}}}, UnknownRealization},
		{"wrong csv", Observation{Objects: []ObjectObservation{{Ownership: Own, Equal: true}}, CSVs: []CSVObservation{{ReadKnown: true, Installed: "other", Expected: "wanted"}}}, UnknownRealization},
		{"all owned and dependent absent", Observation{Objects: []ObjectObservation{{Ownership: Absent}}, Dependents: []Ownership{Absent}}, Partial},
		{"dependent absent", Observation{Objects: []ObjectObservation{{Ownership: Own, Equal: true}}, Dependents: []Ownership{Absent}}, Partial},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := EvaluateObservation(test.observation); got != test.want {
				t.Fatalf("got %s want %s", got, test.want)
			}
		})
	}
}

func TestEvidenceRequiresCompleteUniqueCoverage(t *testing.T) {
	p := programFixture(t)
	o := p.Steps[0].Create
	e := Evidence{Receipts: []Receipt{{Ref: o.Ref, UID: "uid", DesiredDigest: o.DesiredDigest, Disposition: "created"}}, Observations: []ObjectObservation{{Ref: o.Ref, Ownership: Own, Equal: true}}, Checks: []CheckResult{{Index: 0, Satisfied: true, LastObservation: "ready"}}}
	if got := EvaluateEvidence(p, e); got != Completed {
		t.Fatal(got)
	}
	saved := e.Receipts
	e.Receipts = nil
	if got := EvaluateEvidence(p, e); got != UnknownRealization {
		t.Fatal(got)
	}
	e.Receipts = saved
	e.Observations = nil
	if got := EvaluateEvidence(p, e); got != UnknownRealization {
		t.Fatal(got)
	}
}

func TestObservationTextBound(t *testing.T) {
	got := BoundedObservation("\x00\n" + strings.Repeat("界", 100) + "\t")
	if len(got) > 256 || strings.ContainsAny(got, "\x00\n\t") || len([]rune(got)) != 85 {
		t.Fatal(got)
	}
}

func TestNativeEvidenceValuesAreBoundedAndGrammatical(t *testing.T) {
	for _, olm := range []OLMFacts{{InstalledCSV: "csv\nsecret"}, {CatalogImage: "https://credentials@example.test/image"}, {MaxOpenShiftVersion: "future"}, {CSVVersion: strings.Repeat("x", 257)}, {InstallPlan: InstallPlan{UID: "uid\x00"}}} {
		if ValidateEvidence(Evidence{OLM: []OLMFacts{olm}}) == nil {
			t.Fatal("unsafe evidence accepted")
		}
	}
	if err := ValidateEvidence(Evidence{OLM: []OLMFacts{{InstalledCSV: "invalid", CatalogImage: "invalid", MaxOpenShiftVersion: "invalid"}}}); err != nil {
		t.Fatal(err)
	}
}
