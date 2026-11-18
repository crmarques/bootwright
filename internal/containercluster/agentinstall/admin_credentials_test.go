package agentinstall

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

const consolePassword = "abcdefghijklmnopqrstuvwxyz"

type credentialRunner struct {
	calls   int
	request lifecycle.RunRequest
	result  lifecycle.RunResult
	err     error
	reason  string
}

func (r *credentialRunner) Run(_ context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.calls++
	r.request = request
	if r.reason != "" {
		if named := request.Refusals[r.reason]; named != nil {
			return lifecycle.RunResult{}, named
		}
		return lifecycle.RunResult{}, refusal("lifecycle.unknown", "the adapter capability protocol was invalid", "")
	}
	return r.result, r.err
}

func credentialExecution(t *testing.T) (lifecycle.Execution, secrets.Material) {
	t.Helper()
	execution, _ := installExecution(t, singleNodeCatalog(), testDigest)
	kubeconfig := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("kubeconfig-bytes")})
	execution.LendProduced = func(_ context.Context, input lifecycle.ProducedInput) (secrets.Material, error) {
		if input.Kind != Kind || input.Output != KubeconfigOutput {
			return secrets.Material{}, errors.New("unexpected produced input")
		}
		return kubeconfig, nil
	}
	return execution, kubeconfig
}

func credentialProof(proved bool) []byte {
	if proved {
		return []byte(`{"proved":true,"request":"` + testDigest + `"}`)
	}
	return []byte(`{"proved":false,"request":"` + testDigest + `"}`)
}

func credentialOutputs(guard string) []lifecycle.Produced {
	return []lifecycle.Produced{
		{Name: "guard", Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(guard)})},
		{Name: "stray", Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("stray-secret")})},
	}
}

func preparedGuard() string {
	return `{"guard":{"uid":"kubeadmin-uid"},"password":"` + consolePassword + `"}`
}

func cleared(t *testing.T, materials ...secrets.Material) {
	t.Helper()
	for index, material := range materials {
		value, _ := material.Part(secrets.ValuePart)
		if len(value) == 0 || !bytes.Equal(value, make([]byte, len(value))) {
			t.Fatalf("material %d was not cleared", index)
		}
	}
}

func producedMaterials(produced []lifecycle.Produced) []secrets.Material {
	materials := make([]secrets.Material, 0, len(produced))
	for _, output := range produced {
		materials = append(materials, output.Material)
	}
	return materials
}

func hashed(password []byte) ([]byte, error) { return []byte("hashed-" + string(password)), nil }

func TestInstallPrepareAdminCredentialHashesTheGuardedPassword(t *testing.T) {
	execution, kubeconfig := credentialExecution(t)
	produced := credentialOutputs(preparedGuard())
	runner := &credentialRunner{result: lifecycle.RunResult{Evidence: credentialProof(true), Produced: produced}}
	guard, err := NewInstall(runner).WithPasswordHash(hashed).PrepareAdminCredential(context.Background(), execution)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	defer guard.Clear()
	want := `{"guard":{"uid":"kubeadmin-uid"},"password":"` + consolePassword + `","hash":"hashed-` + consolePassword + `"}`
	if value, _ := guard.Part(secrets.ValuePart); string(value) != want {
		t.Fatalf("guard = %q", value)
	}
	cleared(t, append(producedMaterials(produced), kubeconfig)...)
	request := runner.request
	if runner.calls != 1 || request.Implementation != InstallImplementation || request.Operation != "prepare_credentials" || request.Variable != "bootwright_cluster_admin" ||
		!request.ReadOnly || request.Unrecorded || request.Deadline != 2*time.Minute || !strings.HasSuffix(request.MaterialValues["client"], "/oc") || request.Digest != testDigest {
		t.Fatalf("prepare request = %+v", request)
	}
	if !slices.Equal(request.Outputs, []lifecycle.OutputFile{{Name: "guard", Variable: "guard"}}) {
		t.Fatalf("outputs = %+v", request.Outputs)
	}
	access := lifecycle.MaterialFile{Name: "admin-access", Secret: "admin-access", Part: secrets.ValuePart, Variable: "kubeconfig"}
	if !slices.Contains(request.Materials, access) || slices.ContainsFunc(request.Materials, func(file lifecycle.MaterialFile) bool { return file.Secret == "admin-update" }) {
		t.Fatalf("materials = %+v", request.Materials)
	}
}

func TestInstallUpdateAdminCredentialLendsTheUpdateAndClearsWhatItProduced(t *testing.T) {
	execution, kubeconfig := credentialExecution(t)
	produced := credentialOutputs(preparedGuard())
	runner := &credentialRunner{result: lifecycle.RunResult{Evidence: credentialProof(true), Produced: produced}}
	update := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("candidate")})
	if err := NewInstall(runner).UpdateAdminCredential(context.Background(), execution, update); err != nil {
		t.Fatalf("update: %v", err)
	}
	cleared(t, append(producedMaterials(produced), kubeconfig)...)
	request := runner.request
	if request.Operation != "update_credentials" || request.ReadOnly || request.Unrecorded || len(request.Outputs) != 0 || request.Deadline != 2*time.Minute {
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

func TestInstallAnUnprovedAdminCredentialRunRefusesAndClears(t *testing.T) {
	execution, _ := credentialExecution(t)
	produced := credentialOutputs(preparedGuard())
	capability := NewInstall(&credentialRunner{result: lifecycle.RunResult{Evidence: credentialProof(false), Produced: produced}}).WithPasswordHash(hashed)
	guard, err := capability.PrepareAdminCredential(context.Background(), execution)
	if d := diagnostics.Of(err); len(d) != 1 || d[0].Code != "engine.credential-preparation-invalid" || d[0].Message != "the administrator credential preparation was not proved" || guard.Size() != 0 {
		t.Fatalf("an unproved prepare returned %+v", d)
	}
	cleared(t, producedMaterials(produced)...)
	produced = credentialOutputs(preparedGuard())
	capability = NewInstall(&credentialRunner{result: lifecycle.RunResult{Evidence: []byte(`{"proved":true,"request":"other"}`), Produced: produced}})
	err = capability.UpdateAdminCredential(context.Background(), execution, secrets.Material{})
	if d := diagnostics.Of(err); len(d) != 1 || d[0].Code != "engine.credential-update-unproved" || d[0].Message != "the administrator credential update was not proved" {
		t.Fatalf("an unproved update returned %+v", d)
	}
	cleared(t, producedMaterials(produced)...)
}

func TestInstallAnInvalidOrMissingGuardRefusesAndClears(t *testing.T) {
	execution, _ := credentialExecution(t)
	for name, test := range map[string]struct {
		produced []lifecycle.Produced
		hash     func([]byte) ([]byte, error)
		message  string
	}{
		"short password": {credentialOutputs(`{"guard":{},"password":"short"}`), hashed, "console credential preparation was invalid"},
		"open member":    {credentialOutputs(`{"guard":{},"password":"` + consolePassword + `","extra":1}`), hashed, "console credential preparation was invalid"},
		"no hashing":     {credentialOutputs(preparedGuard()), nil, "password hashing is not configured"},
		"hash failure":   {credentialOutputs(preparedGuard()), func([]byte) ([]byte, error) { return nil, errors.New("no entropy") }, "password generation failed"},
		"no guard":       {credentialOutputs(preparedGuard())[1:], hashed, "the cluster returned no console credential identity"},
	} {
		t.Run(name, func(t *testing.T) {
			capability := NewInstall(&credentialRunner{result: lifecycle.RunResult{Evidence: credentialProof(true), Produced: test.produced}}).WithPasswordHash(test.hash)
			_, err := capability.PrepareAdminCredential(context.Background(), execution)
			if d := diagnostics.Of(err); len(d) != 1 || d[0].Message != test.message {
				t.Fatalf("prepare returned %+v", d)
			}
			cleared(t, producedMaterials(test.produced)...)
		})
	}
}

func TestInstallAFailedAdminCredentialRunClearsWhatItProduced(t *testing.T) {
	execution, _ := credentialExecution(t)
	failed := errors.New("adapter failed")
	for _, operation := range []string{"prepare", "update"} {
		produced := credentialOutputs(preparedGuard())
		capability := NewInstall(&credentialRunner{result: lifecycle.RunResult{Produced: produced}, err: failed}).WithPasswordHash(hashed)
		var err error
		if operation == "prepare" {
			_, err = capability.PrepareAdminCredential(context.Background(), execution)
		} else {
			err = capability.UpdateAdminCredential(context.Background(), execution, secrets.Material{})
		}
		if !errors.Is(err, failed) {
			t.Fatalf("%s returned %v", operation, err)
		}
		cleared(t, producedMaterials(produced)...)
	}
}

func TestInstallANamedAdminCredentialRefusalReachesTheOperatorByName(t *testing.T) {
	execution, _ := credentialExecution(t)
	runner := &credentialRunner{reason: nodeRefusal("controller-read", 0)}
	_, err := NewInstall(runner).WithPasswordHash(hashed).PrepareAdminCredential(context.Background(), execution)
	d := diagnostics.Of(err)
	if len(d) != 1 || d[0].Code != "openshift.node-refused" || !strings.Contains(d[0].Message, "the management controller did not answer a complete node state read") {
		t.Fatalf("a named refusal reached the operator as %+v", d)
	}
}

func TestInstallAnUnconfiguredRunnerRefusesTheAdminCredentialRun(t *testing.T) {
	execution, kubeconfig := credentialExecution(t)
	capability := NewInstall(nil).WithPasswordHash(hashed)
	_, err := capability.PrepareAdminCredential(context.Background(), execution)
	if d := diagnostics.Of(err); len(d) != 1 || d[0].Code != "engine.request-defect" || d[0].Message != "the installation adapter is not configured" {
		t.Fatalf("prepare without a runner returned %+v", d)
	}
	err = capability.UpdateAdminCredential(context.Background(), execution, secrets.Material{})
	if d := diagnostics.Of(err); len(d) != 1 || d[0].Code != "engine.request-defect" || d[0].Message != "the installation adapter is not configured" {
		t.Fatalf("update without a runner returned %+v", d)
	}
	if value, _ := kubeconfig.Part(secrets.ValuePart); string(value) != "kubeconfig-bytes" {
		t.Fatal("an unconfigured runner borrowed the kubeconfig")
	}
}
