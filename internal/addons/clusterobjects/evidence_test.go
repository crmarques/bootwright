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
	receipt := Receipt{Ref: object.Ref, UID: base.UID, DesiredDigest: object.DesiredDigest, Disposition: "created", Basis: Created}
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
		{"reached chain", Observation{CSVs: []CSVObservation{{ReadKnown: true, Installed: "op.v3", Expected: "op.v1", Chain: chain("op.v1", "op.v2", "op.v3")}}}, Completed},
		{"chain from another pin", Observation{CSVs: []CSVObservation{{ReadKnown: true, Installed: "op.v3", Expected: "op.v1", Chain: chain("op.v0", "op.v3")}}}, UnknownRealization},
		{"chain short of the installed csv", Observation{CSVs: []CSVObservation{{ReadKnown: true, Installed: "op.v3", Expected: "op.v1", Chain: chain("op.v1", "op.v2")}}}, UnknownRealization},
		{"chain repeating a csv", Observation{CSVs: []CSVObservation{{ReadKnown: true, Installed: "op.v3", Expected: "op.v1", Chain: chain("op.v1", "op.v3", "op.v1", "op.v3")}}}, UnknownRealization},
		{"chain on the pinned csv", Observation{CSVs: []CSVObservation{{ReadKnown: true, Installed: "op.v1", Expected: "op.v1", Chain: chain("op.v1", "op.v1")}}}, UnknownRealization},
		{"chain without its plan", Observation{CSVs: []CSVObservation{{ReadKnown: true, Installed: "op.v2", Expected: "op.v1", Chain: []ChainLink{{Plan: "install", CSV: "op.v1"}, {CSV: "op.v2"}}}}}, UnknownRealization},
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

func chain(csvs ...string) []ChainLink {
	links := []ChainLink{}
	for i, csv := range csvs {
		links = append(links, ChainLink{Plan: "plan-" + strings.Repeat("a", i+1), CSV: csv})
	}
	return links
}

func TestEvidenceReadsAReachedChainComplete(t *testing.T) {
	sub := Ref{APIVersion: "operators.coreos.com/v1alpha1", Kind: "Subscription", Namespace: "operators", Name: "operator", Namespaced: true}
	p := Program{Steps: []Step{
		{Subscribe: &Subscription{Object: ObjectEntry{Ref: sub, DesiredDigest: "digest"}, CSV: "op.v1"}},
		{ApproveDependent: &DependentApproval{Namespace: "engine", Subscription: "engine", CSV: "engine.v1"}},
	}}
	e := Evidence{
		Receipts:     []Receipt{{Ref: sub, UID: "uid", DesiredDigest: "digest", Disposition: "unchanged", Basis: UIDMatched}},
		Observations: []ObjectObservation{{Ref: sub, Ownership: Own, Equal: true}},
		OLM:          []OLMFacts{{Namespace: "operators", Subscription: "operator", InstalledCSV: "op.v2", Chain: chain("op.v1", "op.v2")}},
		Dependents:   []DependentFacts{{Namespace: "engine", Subscription: "engine", UID: "engine-uid", InstalledCSV: "engine.v3", Chain: chain("engine.v1", "engine.v2", "engine.v3")}},
	}
	if got := EvaluateEvidence(p, e); got != Completed {
		t.Fatal(got)
	}
	e.OLM[0].InstallPlan = InstallPlan{Name: "plan-a"}
	if got := EvaluateEvidence(p, e); got != Completed {
		t.Fatal("a chain from the recorded pinned plan:", got)
	}
	e.OLM[0].InstallPlan = InstallPlan{Name: "other-plan"}
	if got := EvaluateEvidence(p, e); got != UnknownRealization {
		t.Fatal("a chain from a plan other than the pinned one:", got)
	}
	e.OLM[0].InstallPlan = InstallPlan{}
	for _, link := range []ChainLink{{Plan: "", CSV: "op.v2"}, {Plan: "plan-b", CSV: ""}, {Plan: "invalid", CSV: "op.v2"}, {Plan: "plan-b", CSV: "invalid"}} {
		e.OLM[0].Chain = []ChainLink{{Plan: "plan-a", CSV: "op.v1"}, link}
		if ValidateEvidence(e) == nil {
			t.Fatalf("chain link %+v validated", link)
		}
		if got := EvaluateEvidence(p, e); got != UnknownRealization {
			t.Fatalf("chain link %+v: %s", link, got)
		}
	}
	e.OLM[0].Chain = chain("op.v1", "op.v2")
	e.Dependents[0].Chain = chain("engine.v1", "engine.v2")
	if got := EvaluateEvidence(p, e); got != UnknownRealization {
		t.Fatal("a dependent chain short of its installed CSV:", got)
	}
	e.Dependents[0].Chain = nil
	if got := EvaluateEvidence(p, e); got != UnknownRealization {
		t.Fatal("a dependent CSV no chain reached:", got)
	}
}

func TestEvidenceRequiresCompleteUniqueCoverage(t *testing.T) {
	p := programFixture(t)
	o := p.Steps[0].Create
	e := Evidence{Receipts: []Receipt{{Ref: o.Ref, UID: "uid", DesiredDigest: o.DesiredDigest, Disposition: "created", Basis: Created}}, Observations: []ObjectObservation{{Ref: o.Ref, Ownership: Own, Equal: true}}, Checks: []CheckResult{{Index: 0, Satisfied: true, LastObservation: "ready"}}}
	if got := EvaluateEvidence(p, e); got != Completed {
		t.Fatal(got)
	}
	// A receipt names the basis of its ownership; one that names none, as an
	// earlier evidence version wrote, or another word proves nothing.
	for _, basis := range []Basis{MarkRecovered, UIDMatched, "", "adopted"} {
		e.Receipts[0].Basis = basis
		want := Completed
		if basis == "" || basis == "adopted" {
			want = UnknownRealization
			if err := ValidateEvidence(e); err == nil {
				t.Fatalf("a receipt of basis %q validated", basis)
			}
		}
		if got := EvaluateEvidence(p, e); got != want {
			t.Fatalf("basis %q: %s, want %s", basis, got, want)
		}
	}
	e.Receipts[0].Basis = Created
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
	for _, olm := range []OLMFacts{{InstalledCSV: "csv\nsecret"}, {CatalogImage: "https://credentials@example.test/image"}, {MaxOpenShiftVersion: "future"}, {CSVVersion: strings.Repeat("x", 257)}, {InstallPlan: InstallPlan{UID: "uid\x00"}}, {Chain: []ChainLink{{Plan: "Plan", CSV: "op.v1"}}}, {Chain: []ChainLink{{Plan: "plan", CSV: "op\nv1"}}}, {Chain: make([]ChainLink, 17)}} {
		if ValidateEvidence(Evidence{OLM: []OLMFacts{olm}}) == nil {
			t.Fatal("unsafe evidence accepted")
		}
	}
	if ValidateEvidence(Evidence{Dependents: []DependentFacts{{CSVVersion: "1.0\n"}}}) == nil || ValidateEvidence(Evidence{Dependents: []DependentFacts{{Chain: []ChainLink{{Plan: "plan", CSV: "bad name"}}}}}) == nil {
		t.Fatal("unsafe dependent evidence accepted")
	}
	if err := ValidateEvidence(Evidence{OLM: []OLMFacts{{InstalledCSV: "invalid", CatalogImage: "invalid", MaxOpenShiftVersion: "invalid"}}}); err != nil {
		t.Fatal(err)
	}
}

func TestARetentionGapStaysUnknownAndIsTheOnlyCauseItNames(t *testing.T) {
	sub := Ref{APIVersion: "operators.coreos.com/v1alpha1", Kind: "Subscription", Namespace: "operators", Name: "operator", Namespaced: true}
	p := Program{Steps: []Step{
		{Subscribe: &Subscription{Object: ObjectEntry{Ref: sub, DesiredDigest: "digest"}, CSV: "op.v1"}},
		{ApproveDependent: &DependentApproval{Namespace: "engine", Subscription: "engine", CSV: "engine.v1"}},
	}}
	base := func() Evidence {
		return Evidence{
			Receipts:     []Receipt{{Ref: sub, UID: "uid", DesiredDigest: "digest", Disposition: "unchanged", Basis: UIDMatched}},
			Observations: []ObjectObservation{{Ref: sub, Ownership: Own, Equal: true}},
			OLM:          []OLMFacts{{Namespace: "operators", Subscription: "operator", InstalledCSV: "op.v3", Gap: &ChainGap{Cause: PlanCollected, CSV: "op.v2"}}},
			Dependents:   []DependentFacts{{Namespace: "engine", Subscription: "engine", UID: "engine-uid", InstalledCSV: "engine.v1"}},
		}
	}
	e := base()
	if ValidateEvidence(e) != nil || EvaluateEvidence(p, e) != UnknownRealization {
		t.Fatal("a retention gap must validate and read unknown")
	}
	gaps, other := RetentionGaps(p, e)
	if other || len(gaps) != 1 || gaps[0].Subscription != "operator" || gaps[0].Expected != "op.v1" || gaps[0].Installed != "op.v3" || !strings.Contains(gaps[0].Detail(), "no InstallPlan that installed CSV op.v2 remains") {
		t.Fatalf("%+v %v", gaps, other)
	}
	e.Dependents[0].InstalledCSV, e.Dependents[0].Gap = "engine.v4", &ChainGap{Cause: ChainTooLong}
	if gaps, other := RetentionGaps(p, e); other || len(gaps) != 2 || gaps[1].Subscription != "engine" {
		t.Fatalf("a dependent gap: %+v %v", gaps, other)
	}
	for name, change := range map[string]func(*Evidence){
		"an unknown check":       func(e *Evidence) { e.Checks = []CheckResult{{Unknown: true}}; p.Checks = []Check{{}} },
		"a foreign object":       func(e *Evidence) { e.Observations[0].Ownership = Foreign },
		"a receipt mismatch":     func(e *Evidence) { e.Receipts[0].DesiredDigest = "other" },
		"another dependent csv":  func(e *Evidence) { e.Dependents[0].InstalledCSV = "engine.v9" },
		"an unprogrammed source": func(e *Evidence) { e.OLM[0].Subscription = "stranger" },
	} {
		p.Checks = nil
		e := base()
		change(&e)
		if gaps, other := RetentionGaps(p, e); !other && len(gaps) != 0 {
			t.Errorf("%s: a gap hid another unknown cause: %+v", name, gaps)
		}
	}
	p.Checks = nil
	for name, gap := range map[string]ChainGap{
		"an unknown cause":     {Cause: "planDeleted", CSV: "op.v2"},
		"an invalid csv":       {Cause: PlanCollected, CSV: "Op V2"},
		"an empty plan name":   {Cause: DuplicatePlans, CSV: "op.v2", Plans: []string{"plan-a", ""}},
		"a sentinel plan name": {Cause: DuplicatePlans, CSV: "op.v2", Plans: []string{"plan-a", "invalid"}},
	} {
		e := base()
		e.OLM[0].Gap = &gap
		if ValidateEvidence(e) == nil {
			t.Errorf("%s validated", name)
		}
	}
	for name, change := range map[string]func(*Evidence){
		"beside a chain":          func(e *Evidence) { e.OLM[0].Chain = chain("op.v1", "op.v3") },
		"on the pinned csv":       func(e *Evidence) { e.OLM[0].InstalledCSV = "op.v1" },
		"collected naming no csv": func(e *Evidence) { e.OLM[0].Gap.CSV = "" },
		"duplicate of one plan": func(e *Evidence) {
			e.OLM[0].Gap = &ChainGap{Cause: DuplicatePlans, CSV: "op.v3", Plans: []string{"plan-a"}}
		},
		"too long naming a csv": func(e *Evidence) { e.OLM[0].Gap = &ChainGap{Cause: ChainTooLong, CSV: "op.v2"} },
	} {
		e := base()
		change(&e)
		if EvaluateEvidence(p, e) != UnknownRealization {
			t.Errorf("%s: read known", name)
		}
		if gaps, _ := RetentionGaps(p, e); len(gaps) != 0 {
			t.Errorf("%s: malformed gap read as retention: %+v", name, gaps)
		}
	}
}
