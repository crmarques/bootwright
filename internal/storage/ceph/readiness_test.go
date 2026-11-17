package ceph

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

const readinessCases = "../../../ansible/collections/ansible_collections/bootwright/core/tests/unit/goldens/ceph_readiness_cases.json"

type readinessCase struct {
	ID      string            `json:"id"`
	Action  string            `json:"action"`
	Request json.RawMessage   `json:"request"`
	Status  string            `json:"status"`
	Healthy bool              `json:"healthy"`
	Facts   ReadinessEvidence `json:"facts"`
}

type pgCase struct {
	Action string     `json:"action"`
	Ready  bool       `json:"ready"`
	Counts PGEvidence `json:"counts"`
}

// The collection's worker once judged readiness alone. Each recorded case
// holds the facts it now publishes and the verdict its convergence wait still
// reaches, which the collection's test_ceph_readiness_cases.py proves; Go's
// predicate over the same facts must reach the same verdict.
func TestGoJudgesTheRecordedReadinessCasesAsTheWorkerDid(t *testing.T) {
	data, err := os.ReadFile(filepath.FromSlash(readinessCases))
	if err != nil {
		t.Fatal(err)
	}
	var cases struct {
		Health []readinessCase `json:"health"`
		PGs    []pgCase        `json:"pgs"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases.Health) == 0 || len(cases.PGs) == 0 {
		t.Fatal("no recorded readiness cases")
	}
	for _, c := range cases.Health {
		var r Request
		if err := json.Unmarshal(c.Request, &r); err != nil {
			t.Fatalf("%s: %v", c.ID, err)
		}
		r.Action = c.Action
		e := Evidence{Action: c.Action, Health: c.Status, Readiness: c.Facts}
		if !validReadiness(e) {
			t.Errorf("%s: the published facts do not validate", c.ID)
		}
		if got := healthReady(e, r); got != c.Healthy {
			t.Errorf("%s: Go reads health ready %t, the worker %t", c.ID, got, c.Healthy)
		}
	}
	for _, c := range cases.PGs {
		r := Request{Action: c.Action}
		e := Evidence{Action: c.Action, Health: "HEALTH_OK", Readiness: readyGates(r)}
		e.Readiness.PGs = c.Counts
		if got := readinessGates(e, r); got != c.Ready {
			t.Errorf("%s %+v: Go reads ready %t, the worker %t", c.Action, c.Counts, got, c.Ready)
		}
	}
}

func TestOnlyAnAcceptedOrUnneededLicenseLetsAClusterBeReady(t *testing.T) {
	for _, test := range []struct {
		action, license string
		ready, valid    bool
	}{
		{"cluster", "accepted", true, true}, {"cluster", "unneeded", true, true}, {"cluster", "pending", false, true}, {"cluster", "", false, true},
		{"pool", "", true, true}, {"pool", "accepted", true, false}, {"cluster", "other", false, false},
	} {
		r := Request{Action: test.action}
		e := Evidence{Action: test.action, Health: "HEALTH_OK", Readiness: readyGates(r)}
		e.Readiness.License = test.license
		if validReadiness(e) != test.valid || test.valid && readinessGates(e, r) != test.ready {
			t.Errorf("%s with license %q: valid %t ready %t", test.action, test.license, validReadiness(e), readinessGates(e, r))
		}
	}
}

func TestAReadyClaimTheReadinessFactsDoNotSupportIsUnknown(t *testing.T) {
	r := runtimeRequest()
	for name, mutate := range map[string]func(*Evidence){
		"a blocking check":    func(e *Evidence) { e.Health, e.Readiness.Checks = "HEALTH_WARN", []string{"MON_DOWN"} },
		"unclean PGs":         func(e *Evidence) { e.Readiness.PGs.Clean-- },
		"a pending license":   func(e *Evidence) { e.Readiness.License = "pending" },
		"an unnamed check":    func(e *Evidence) { e.Health, e.Readiness.Unnamed = "HEALTH_WARN", true },
		"unsorted checks":     func(e *Evidence) { e.Readiness.Checks = []string{"B", "A"} },
		"a scoped unlisted":   func(e *Evidence) { e.Readiness.Scoped = []string{"POOL_FULL"} },
		"more clean than PGs": func(e *Evidence) { e.Readiness.PGs.Clean++ },
	} {
		e := readyEvidence(r)
		mutate(&e)
		if effect, err := EvaluateEvidence(evidenceBytes(t, e), r, "digest", false); err == nil || effect != reconciliation.EffectUnknown {
			t.Errorf("%s: %s %v", name, effect, err)
		}
	}
}
