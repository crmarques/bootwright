package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
)

type countingCredentialRunner struct{ calls int }

func (r *countingCredentialRunner) Run(context.Context, RunRequest) (RunResult, error) {
	r.calls++
	return RunResult{}, nil
}

func TestAnAdminCredentialRunPlacedOffTheControllerRefusesAfterReleasingItsBinding(t *testing.T) {
	for name, placement := range map[string]machineref.Placement{
		"no machine": {Connection: machineref.ConnectionLocal},
		"remote":     {Connection: "ssh", Machine: "node-1"},
	} {
		t.Run(name, func(t *testing.T) {
			runner, released := &countingCredentialRunner{}, 0
			call := AdminCredentialCall{Runner: runner, Adapter: "test", Bind: func(_ context.Context, e Execution) (Execution, Invocation, func(), error) {
				return e, Invocation{Placement: placement}, func() { released++ }, nil
			}}
			err := UpdateAdminCredential(context.Background(), call, Execution{}, secrets.Material{})
			if d := diagnostics.Of(err); len(d) != 1 || !refusedBeforeEffect(d[0].Code) || d[0].Message != "the frozen administrator credential request is placed off the controller" || runner.calls != 0 || released != 1 {
				t.Fatalf("an off-controller run returned %+v after %d runs and %d releases", d, runner.calls, released)
			}
		})
	}
}

type recordingCredentialRunner struct {
	requests []RunRequest
	result   RunResult
	err      error
}

func (r *recordingCredentialRunner) Run(_ context.Context, request RunRequest) (RunResult, error) {
	r.requests = append(r.requests, request)
	return r.result, r.err
}

func credentialCall(runner AdminCredentialRunner, released *int) AdminCredentialCall {
	return AdminCredentialCall{Runner: runner, Adapter: "test", Missing: errors.New("no guard produced"),
		Bind: func(_ context.Context, e Execution) (Execution, Invocation, func(), error) {
			return e, Invocation{Placement: machineref.Placement{Connection: machineref.ConnectionLocal, Machine: "controller"}}, func() { *released++ }, nil
		},
		Guard: func(material secrets.Material) (secrets.Material, error) {
			value, _ := material.Part(secrets.ValuePart)
			return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: append([]byte(nil), value...)}), nil
		}}
}

func provedCredential(digest string, produced ...Produced) RunResult {
	return RunResult{Evidence: json.RawMessage(`{"proved":true,"request":"` + digest + `"}`), Produced: produced}
}

func guardOutput(value string) Produced {
	return Produced{Name: "guard", Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(value)})}
}

// A preparation reads the cluster without writing, asks for the guard output,
// and hands the guard back only when the run proved this request.
func TestPrepareAdminCredentialReturnsTheGuardOnlyOnProof(t *testing.T) {
	execution := Execution{Block: reconciliation.Block{RequestDigest: "request-1"}}
	released := 0
	runner := &recordingCredentialRunner{result: provedCredential("request-1", guardOutput("guard-value"))}
	guard, err := PrepareAdminCredential(context.Background(), credentialCall(runner, &released), execution)
	value, _ := guard.Part(secrets.ValuePart)
	if err != nil || string(value) != "guard-value" || released != 1 || len(runner.requests) != 1 {
		t.Fatalf("guard %q, %v after %d runs and %d releases", value, err, len(runner.requests), released)
	}
	if request := runner.requests[0]; request.Operation != PrepareCredentialsOperation || !request.ReadOnly || len(request.Outputs) != 1 || request.Outputs[0].Name != "guard" {
		t.Fatalf("the preparation ran as %+v", request)
	}

	for name, test := range map[string]struct {
		result RunResult
		code   string
	}{
		"a proof of another request": {provedCredential("request-2", guardOutput("guard-value")), "engine.credential-preparation-invalid"},
		"no proof":                   {RunResult{Produced: []Produced{guardOutput("guard-value")}}, "engine.credential-preparation-invalid"},
	} {
		runner.result = test.result
		_, err := PrepareAdminCredential(context.Background(), credentialCall(runner, &released), execution)
		if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != test.code {
			t.Fatalf("%s gave %+v", name, found)
		}
	}
	runner.result = provedCredential("request-1")
	call := credentialCall(runner, &released)
	if _, err := PrepareAdminCredential(context.Background(), call, execution); !errors.Is(err, call.Missing) {
		t.Fatalf("a proved run without a guard gave %v", err)
	}
	failure := errors.New("adapter failed")
	runner.err = failure
	if _, err := PrepareAdminCredential(context.Background(), call, execution); !errors.Is(err, failure) {
		t.Fatalf("the runner's failure was lost: %v", err)
	}
	if _, err := PrepareAdminCredential(context.Background(), AdminCredentialCall{Adapter: "test"}, execution); len(diagnostics.Of(err)) != 1 {
		t.Fatalf("an unconfigured adapter gave %v", err)
	}
}

// An update lends the new value as the run's one extra material, writes, and
// succeeds only when the run proved this request.
func TestUpdateAdminCredentialLendsTheValueAndRequiresProof(t *testing.T) {
	execution := Execution{Block: reconciliation.Block{RequestDigest: "request-1"}}
	released := 0
	runner := &recordingCredentialRunner{result: provedCredential("request-1")}
	update := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("new-password")})
	if err := UpdateAdminCredential(context.Background(), credentialCall(runner, &released), execution, update); err != nil || released != 1 {
		t.Fatalf("update = %v after %d releases", err, released)
	}
	request := runner.requests[0]
	if request.Operation != UpdateCredentialsOperation || request.ReadOnly || len(request.Outputs) != 0 || len(request.Materials) != 1 || request.Materials[0].Secret != "admin-update" || request.Materials[0].Variable != "update" {
		t.Fatalf("the update ran as %+v", request)
	}
	runner.result = provedCredential("request-2")
	err := UpdateAdminCredential(context.Background(), credentialCall(runner, &released), execution, update)
	if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != "engine.credential-update-unproved" || found[0].Remediation != RepeatAdminCredentialUpdate {
		t.Fatalf("an unproved update gave %+v", found)
	}
	failure := errors.New("adapter failed")
	runner.err = failure
	if err := UpdateAdminCredential(context.Background(), credentialCall(runner, &released), execution, update); !errors.Is(err, failure) {
		t.Fatalf("the runner's failure was lost: %v", err)
	}
	if err := UpdateAdminCredential(context.Background(), AdminCredentialCall{Adapter: "test"}, execution, update); len(diagnostics.Of(err)) != 1 {
		t.Fatalf("an unconfigured adapter gave %v", err)
	}
}
