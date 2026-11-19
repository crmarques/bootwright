package ceph

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

type credentialRunner struct {
	calls   int
	request lifecycle.RunRequest
	result  lifecycle.RunResult
	err     error
}

func (r *credentialRunner) Run(_ context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.calls++
	r.request = request
	return r.result, r.err
}

func credentialExecution(t *testing.T) lifecycle.Execution {
	t.Helper()
	r := coreRequest()
	e := withMaterial(executionFor(t, r), r)
	e.PreviousEvidence = evidenceBytes(t, readyEvidence(r))
	return e
}

func credentialProof(e lifecycle.Execution, proved bool) []byte {
	if proved {
		return []byte(`{"proved":true,"request":"` + e.Block.RequestDigest + `"}`)
	}
	return []byte(`{"proved":false,"request":"` + e.Block.RequestDigest + `"}`)
}

func credentialOutputs() []lifecycle.Produced {
	return []lifecycle.Produced{
		{Name: "guard", Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("dashboard-guard")})},
		{Name: "stray", Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("stray-secret")})},
	}
}

func cleared(t *testing.T, produced []lifecycle.Produced) {
	t.Helper()
	for _, output := range produced {
		value, _ := output.Material.Part(secrets.ValuePart)
		if len(value) == 0 || !bytes.Equal(value, make([]byte, len(value))) {
			t.Fatalf("produced output %s was not cleared", output.Name)
		}
	}
}

func TestCephPrepareAdminCredentialCrossesTheReadOnlyRunAndKeepsOnlyTheGuard(t *testing.T) {
	e := credentialExecution(t)
	produced := credentialOutputs()
	runner := &credentialRunner{result: lifecycle.RunResult{Evidence: credentialProof(e, true), Produced: produced}}
	guard, err := New(api.CephCluster, runner).PrepareAdminCredential(context.Background(), e, dashboardLogin)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	defer guard.Clear()
	if value, _ := guard.Part(secrets.ValuePart); string(value) != "dashboard-guard" {
		t.Fatalf("guard = %q", value)
	}
	cleared(t, produced)
	request := runner.request
	if runner.calls != 1 || request.Implementation != Implementation || request.Operation != "prepare_credentials" || request.Variable != "bootwright_ceph" ||
		!request.ReadOnly || request.Unrecorded || request.Deadline != 3*time.Minute || request.MaterialValues["incarnation"] != monmapCreatedAt ||
		request.Placement != (machine.Placement{Connection: machine.ConnectionLocal, Machine: "controller"}) || request.Digest != e.Block.RequestDigest {
		t.Fatalf("prepare request = %+v", request)
	}
	if !slices.Equal(request.Outputs, []lifecycle.OutputFile{{Name: "guard", Variable: "guard"}}) {
		t.Fatalf("outputs = %+v", request.Outputs)
	}
	if slices.ContainsFunc(request.Materials, func(file lifecycle.MaterialFile) bool { return file.Secret == "admin-update" }) {
		t.Fatalf("prepare was lent the update: %+v", request.Materials)
	}
}

func TestCephUpdateAdminCredentialLendsTheUpdateAndClearsWhatItProduced(t *testing.T) {
	e := credentialExecution(t)
	produced := credentialOutputs()
	runner := &credentialRunner{result: lifecycle.RunResult{Evidence: credentialProof(e, true), Produced: produced}}
	update := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("candidate")})
	if err := New(api.CephCluster, runner).UpdateAdminCredential(context.Background(), e, dashboardLogin, update); err != nil {
		t.Fatalf("update: %v", err)
	}
	cleared(t, produced)
	request := runner.request
	if request.Operation != "update_credentials" || request.ReadOnly || request.Unrecorded || len(request.Outputs) != 0 || request.Deadline != 3*time.Minute {
		t.Fatalf("update request = %+v", request)
	}
	want := lifecycle.MaterialFile{Name: "admin-update", Secret: "admin-update", Part: secrets.ValuePart, Variable: "update"}
	if !slices.Contains(request.Materials, want) {
		t.Fatalf("materials = %+v", request.Materials)
	}
	if value, _ := request.Material["admin-update"].Part(secrets.ValuePart); string(value) != "candidate" {
		t.Fatalf("lent update = %q", value)
	}
}

func TestCephAnUnprovedAdminCredentialRunRefusesAndClears(t *testing.T) {
	e := credentialExecution(t)
	produced := credentialOutputs()
	capability := New(api.CephCluster, &credentialRunner{result: lifecycle.RunResult{Evidence: credentialProof(e, false), Produced: produced}})
	guard, err := capability.PrepareAdminCredential(context.Background(), e, dashboardLogin)
	if d := diagnostics.Of(err); len(d) != 1 || d[0].Code != "engine.credential-preparation-invalid" || d[0].Message != "the administrator credential preparation was not proved" || guard.Size() != 0 {
		t.Fatalf("an unproved prepare returned %+v", d)
	}
	cleared(t, produced)
	produced = credentialOutputs()
	capability = New(api.CephCluster, &credentialRunner{result: lifecycle.RunResult{Evidence: []byte(`{"proved":true,"request":"other"}`), Produced: produced}})
	err = capability.UpdateAdminCredential(context.Background(), e, dashboardLogin, secrets.Material{})
	if d := diagnostics.Of(err); len(d) != 1 || d[0].Code != "engine.credential-update-unproved" || d[0].Message != "the administrator credential update was not proved" {
		t.Fatalf("an unproved update returned %+v", d)
	}
	cleared(t, produced)
}

func TestCephAPreparationWithoutAGuardRefusesAndAFailedRunClears(t *testing.T) {
	e := credentialExecution(t)
	produced := credentialOutputs()[1:]
	_, err := New(api.CephCluster, &credentialRunner{result: lifecycle.RunResult{Evidence: credentialProof(e, true), Produced: produced}}).PrepareAdminCredential(context.Background(), e, dashboardLogin)
	if d := diagnostics.Of(err); len(d) != 1 || d[0].Message != "Ceph returned no administrator credential identity" {
		t.Fatalf("a guardless prepare returned %+v", d)
	}
	cleared(t, produced)
	failed := errors.New("adapter failed")
	for _, operation := range []string{"prepare", "update"} {
		produced = credentialOutputs()
		capability := New(api.CephCluster, &credentialRunner{result: lifecycle.RunResult{Produced: produced}, err: failed})
		if operation == "prepare" {
			_, err = capability.PrepareAdminCredential(context.Background(), e, dashboardLogin)
		} else {
			err = capability.UpdateAdminCredential(context.Background(), e, dashboardLogin, secrets.Material{})
		}
		if !errors.Is(err, failed) {
			t.Fatalf("%s returned %v", operation, err)
		}
		cleared(t, produced)
	}
}

func TestCephANativeAdminCredentialRefusalReachesTheOperatorByName(t *testing.T) {
	e := credentialExecution(t)
	runner := &refusingRunner{reason: "dashboard-identity"}
	_, err := New(api.CephCluster, runner).PrepareAdminCredential(context.Background(), e, dashboardLogin)
	d := diagnostics.Of(err)
	if len(d) != 1 || d[0].Code != cephDrifted.Code || d[0].Message != "the Ceph dashboard user admin or its retained password is not the one this operation created" || runner.request.NativeErrors {
		t.Fatalf("a dashboard-identity refusal reached the operator as %+v", d)
	}
}

func TestCephAnUnconfiguredRunnerRefusesTheAdminCredentialRun(t *testing.T) {
	e := credentialExecution(t)
	capability := New(api.CephCluster, nil)
	_, err := capability.PrepareAdminCredential(context.Background(), e, dashboardLogin)
	if d := diagnostics.Of(err); len(d) != 1 || d[0].Code != "engine.request-defect" || d[0].Message != "the Ceph adapter is not configured" {
		t.Fatalf("prepare without a runner returned %+v", d)
	}
	err = capability.UpdateAdminCredential(context.Background(), e, dashboardLogin, secrets.Material{})
	if d := diagnostics.Of(err); len(d) != 1 || d[0].Code != "engine.request-defect" || d[0].Message != "the Ceph adapter is not configured" {
		t.Fatalf("update without a runner returned %+v", d)
	}
}

var dashboardLogin = lifecycle.AdminLogin{Name: "dashboard", User: DashboardUser, Output: AdminPasswordOutput, Seed: DashboardOutput, Custody: lifecycle.AdminProducedPassword}
