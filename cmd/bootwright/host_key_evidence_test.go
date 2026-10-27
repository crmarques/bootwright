//go:build linux && amd64

package main

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets/custody"
)

// Persist a synthetic completed removal through the real store, then inspect it
// through the command and production wiring without running any native effect.
func TestClusterInfoAfterDestroyReadsRemovalEvidenceAsUnavailable(t *testing.T) {
	services, repository, input, root := contextFixture(t)
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
	state, _ := compileAcceptance(t, exampleDirectory(t, "libvirt-3-nodes-ceph"))
	services.EnvironmentInspection = wireInspection(labSessionState{effective: state.Effective()}, services.Lifecycle.(lifecycle.Service), services.Secrets.(*custody.Service), nil)
	plan, err := reconciliation.NewPlan(reconciliation.Apply, []reconciliation.BlockDefinition{{
		ID: "install-rhel-01", Description: "install the guest", Stage: reconciliation.StageInfrastructureServices,
		Kind: installation.Kind, Object: "rhel-01", Implementation: installation.Implementation,
		ContentDigest: strings.Repeat("c", 64), Request: json.RawMessage(`{}`),
		Groups: []reconciliation.Group{{ID: "install", Description: "install the guest", Machines: []string{"rhel-01"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	writeRecords(t, repository, func(ctx context.Context, store *operationstore.Store) {
		applied := "op-" + strings.Repeat("01", 16)
		registerDone(ctx, t, store, applied, "", plan, "install-rhel-01")
		removal, err := plan.Inverse()
		if err != nil {
			t.Fatal(err)
		}
		digest, err := removal.Digest()
		if err != nil {
			t.Fatal(err)
		}
		stamp := exitStamp().Format(time.RFC3339)
		operation := operationstore.Operation{Version: operationstore.OperationVersion, ID: "op-" + strings.Repeat("02", 16), Verb: reconciliation.Destroy,
			Context: "alpha", Revision: "rev-1", InputDigest: strings.Repeat("a", 64), PlanDigest: digest, AutomationDigest: strings.Repeat("b", 64),
			Closure: exitClosure(), Source: applied, Bindings: []string{}, State: reconciliation.OperationRunning, Created: stamp, Updated: stamp}
		if err := store.Register(ctx, operation, removal); err != nil {
			t.Fatal(err)
		}
		number, err := store.StartAttempt(ctx, operation.ID, "install-rhel-01", exitBuild())
		if err != nil {
			t.Fatal(err)
		}
		absence, err := json.Marshal(installation.Evidence{Absent: true, Postcondition: true, Request: strings.Repeat("c", 64)})
		if err != nil {
			t.Fatal(err)
		}
		if err := installation.ValidateAbsence(absence, strings.Repeat("c", 64)); err != nil {
			t.Fatal(err)
		}
		if err := store.CompleteAttempt(ctx, operation.ID, "install-rhel-01", number, reconciliation.OutcomeChanged, reconciliation.EffectCompleted, reconciliation.BlockDone, absence); err != nil {
			t.Fatal(err)
		}
		operation.State = reconciliation.OperationDone
		if err := store.UpdateOperation(ctx, operation); err != nil {
			t.Fatal(err)
		}
	})
	before := stateFingerprint(t, root)
	out, stderr := contextRun(t, services, 0, "cluster", "info", "--context", "alpha", "--name", "ceph-lab", "--output", "json")
	result := decodeInspected(t, out)
	if stderr != "" || len(result.Result.Storage) != 1 || string(result.Result.Storage[0]["status"]) != `"unavailable"` {
		t.Fatalf("info = %s, stderr = %s", out, stderr)
	}
	out, stderr = contextRun(t, services, 1, "cluster", "info", "--context", "alpha", "--name", "ceph-lab", "--secrets")
	singleDiagnostic(t, stderr, "access.unavailable")
	if out != "" || !strings.Contains(stderr, "no dashboard administrator password") {
		t.Fatalf("reveal = %q, %q", out, stderr)
	}
	after := stateFingerprint(t, root)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("inspection changed stored state")
	}
}

func TestMachineHostKeysRequiresCompletedApplyEvidence(t *testing.T) {
	const key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICG+RadYRVwHqO7OQsOpyh8SDf1tbTQ8PUmgn7UEmou/"
	present, err := json.Marshal(installation.Evidence{Address: "192.0.2.11", HostKey: key, Postcondition: true})
	if err != nil {
		t.Fatal(err)
	}
	for name, tt := range map[string]struct {
		verb           reconciliation.Verb
		state          reconciliation.BlockState
		data           json.RawMessage
		found, failure bool
	}{
		"completed apply":                {reconciliation.Apply, reconciliation.BlockDone, present, true, false},
		"pending apply":                  {reconciliation.Apply, reconciliation.BlockPending, present, false, false},
		"running apply":                  {reconciliation.Apply, reconciliation.BlockRunning, present, false, false},
		"failed apply":                   {reconciliation.Apply, reconciliation.BlockFailed, present, false, false},
		"unknown apply":                  {reconciliation.Apply, reconciliation.BlockUnknown, present, false, false},
		"destroy with retained host key": {reconciliation.Destroy, reconciliation.BlockDone, present, false, false},
		"empty evidence":                 {reconciliation.Apply, reconciliation.BlockDone, nil, false, false},
		"malformed apply":                {reconciliation.Apply, reconciliation.BlockDone, json.RawMessage(`{`), false, true},
		"absent apply":                   {reconciliation.Apply, reconciliation.BlockDone, json.RawMessage(`{"absent":true}`), false, true},
		"invalid key":                    {reconciliation.Apply, reconciliation.BlockDone, json.RawMessage(`{"address":"192.0.2.11","hostKey":"invalid"}`), false, true},
	} {
		t.Run(name, func(t *testing.T) {
			reader := &readerFake{proofs: map[string][]lifecycle.BlockEvidence{"Machine/m1": {{Implementation: installation.Implementation, Verb: tt.verb, State: tt.state, Evidence: tt.data}}}}
			got, found, err := (machineHostKeys{reconciler: reader}).HostKey(t.Context(), "alpha", "m1")
			if found != tt.found || (err != nil) != tt.failure {
				t.Fatalf("host key = %+v, found %t, error %v", got, found, err)
			}
			if found && (got.Address != "192.0.2.11" || got.HostKey.Type+" "+got.HostKey.PublicKey != key) {
				t.Fatalf("host key = %+v", got)
			}
		})
	}
	if _, found, err := (machineHostKeys{reconciler: &readerFake{}}).HostKey(t.Context(), "alpha", "m1"); found || err != nil {
		t.Fatalf("no evidence = %t, %v", found, err)
	}
	failure := errors.New("unreadable lifecycle evidence")
	if _, found, err := (machineHostKeys{reconciler: &readerFake{failed: failure}}).HostKey(t.Context(), "alpha", "m1"); found || !errors.Is(err, failure) {
		t.Fatalf("read failure = %t, %v", found, err)
	}
}
