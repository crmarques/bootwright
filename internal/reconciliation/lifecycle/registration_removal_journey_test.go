package lifecycle_test

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

const registeredConsumer = "0f8fad5b-d9cb-469f-a165-70867728950e"

type registeringRunner struct {
	mutex    sync.Mutex
	machines *fleetMachines
	request  installation.Request
	runs     []string
}

func (r *registeringRunner) Run(_ context.Context, run lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.machines.mutex.Lock()
	running := r.machines.running
	r.machines.mutex.Unlock()
	r.runs = append(r.runs, run.Operation+":"+map[bool]string{true: "running", false: "stopped"}[running])
	evidence := installation.Evidence{Absent: true, NetworkDifferences: []string{}, Postcondition: true, Request: run.Digest}
	switch run.Operation {
	case "apply":
		evidence = installation.Evidence{
			Address: r.request.Address, HostKey: "ssh-ed25519 AAAAGUEST", Image: true, Marker: run.MaterialValues["marker"], Network: true, NetworkDifferences: []string{},
			NetworkReboot: true, Postcondition: true, Power: "On", Reachable: true, Request: run.Digest, Tree: true, TreeIdentity: r.request.TreeMedia.SHA256,
		}
		if r.request.Registration != nil {
			evidence.Registration = installation.RegistrationEvidence{Consumer: registeredConsumer, State: "owned"}
		}
	case "destroy":
		if !running && r.request.Registration != nil {
			return lifecycle.RunResult{}, run.Refusals["registration-unreachable"]
		}
		if r.request.Registration != nil {
			evidence.Registration = installation.RegistrationEvidence{Consumer: registeredConsumer, State: "unregistered"}
		}
	}
	data, err := json.Marshal(evidence)
	return lifecycle.RunResult{Outcome: "changed", Evidence: data}, err
}

func (r *registeringRunner) took() []string {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	runs := r.runs
	r.runs = nil
	return runs
}

func installationJourney(t *testing.T, golden string) (*lifecycle.CapabilityJourney, *registeringRunner, *fleetMachines) {
	t.Helper()
	data, err := os.ReadFile("../../managedos/installation/testdata/" + golden)
	if err != nil {
		t.Fatal(err)
	}
	var request installation.Request
	if err := json.Unmarshal(data, &request); err != nil {
		t.Fatal(err)
	}
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	machines := &fleetMachines{running: true}
	runner := &registeringRunner{machines: machines, request: request}
	definitions := []reconciliation.BlockDefinition{
		{ID: "machine-rhel-01", Description: "create the Machine rhel-01", Stage: reconciliation.StageMachines, Impacts: []string{"create-machine rhel-01"}, Consumes: []string{},
			Kind: string(api.Machine), Object: "rhel-01", Implementation: fleetMachineImplementation, ContentDigest: strings.Repeat("c", 64), Request: json.RawMessage(`{"name":"rhel-01"}`)},
		{ID: installation.BlockID("rhel-01"), Description: "install the operating system of rhel-01", Stage: reconciliation.StageMachines, Dependencies: []string{"machine-rhel-01"},
			Impacts: []string{"install-operating-system rhel-01"}, Consumes: []string{}, Kind: installation.Kind, Object: "rhel-01", Implementation: installation.Implementation,
			ContentDigest: installation.ContentDigest(), Request: canonical},
	}
	journey := lifecycle.NewBoundJourney(t, []lifecycle.BoundCapability{
		{Binding: lifecycle.CapabilityBinding{Kind: string(api.Machine), Implementation: fleetMachineImplementation}, Capability: machines},
		{Binding: lifecycle.CapabilityBinding{Kind: installation.Kind, Implementation: installation.Implementation}, Capability: installation.New(runner)},
	}, lifecycle.CapabilityPlan{Definitions: definitions, Secrets: request.SecretReferences()})
	holdInstallationSecrets(journey, request)
	result, err := journey.Service.Apply(context.Background(), lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true})
	if err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("the apply = %+v, %+v (%v)", result, diagnostics.Of(err), err)
	}
	if runs := runner.took(); !slices.Equal(runs, []string{"apply:running"}) {
		t.Fatalf("the apply ran %v", runs)
	}
	return journey, runner, machines
}

func holdInstallationSecrets(journey *lifecycle.CapabilityJourney, request installation.Request) {
	for _, name := range request.SecretReferences() {
		journey.Holding(name, secrets.NewMaterial(map[secrets.Part][]byte{
			secrets.PublicKeyPart: []byte("ssh-ed25519 AAAAPUBLIC bootwright"), secrets.PrivateKeyPart: []byte("PRIVATE"),
			secrets.UsernamePart: []byte("admin"), secrets.PasswordPart: []byte("password"), secrets.CertificatePart: []byte("CERTIFICATE"),
			secrets.ValuePart: []byte("Registration-0815"),
		}))
	}
}

func setRunning(machines *fleetMachines, running bool) {
	machines.mutex.Lock()
	defer machines.mutex.Unlock()
	machines.running = running
}

func TestRegisteredInstallationUnregistersBeforeItsMachineStops(t *testing.T) {
	destroy := lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}
	stop := "bootwright machine stop --context " + lifecycle.JourneyContext + " --name rhel-01"
	t.Run("registered", func(t *testing.T) {
		journey, runner, machines := installationJourney(t, "request-lab-rhel-registered.golden")
		preview, err := journey.Service.Plan(context.Background(), lifecycle.PlanRequest{ContextName: lifecycle.JourneyContext})
		if err != nil || len(preview.Stops) != 0 || !slices.Equal(preview.StopsAfterOnline, []string{"rhel-01"}) {
			t.Fatalf("the registered preview stops %v first and %v after (%v)", preview.Stops, preview.StopsAfterOnline, err)
		}
		_, err = journey.Service.Destroy(context.Background(), destroy)
		reported := diagnostics.Of(err)
		if len(reported) == 0 || reported[0].Code != "lifecycle.live" || !strings.Contains(reported[0].Remediation, stop) {
			t.Fatalf("the first destroy = %+v (%v), want lifecycle.live naming the stop", reported, err)
		}
		if runs := runner.took(); !slices.Equal(runs, []string{"destroy:running"}) || len(machines.removed) != 0 {
			t.Fatalf("the first destroy ran %v and removed %v", runs, machines.removed)
		}
		setRunning(machines, false)
		holdInstallationSecrets(journey, runner.request)
		result, err := journey.Service.Destroy(context.Background(), destroy)
		if err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
			t.Fatalf("the destroy after the stop = %+v, %+v (%v)", result, diagnostics.Of(err), err)
		}
		if runs := runner.took(); len(runs) != 0 || !slices.Equal(machines.removed, []string{"rhel-01"}) {
			t.Fatalf("the destroy after the stop ran %v and removed %v", runs, machines.removed)
		}
	})
	t.Run("not registered", func(t *testing.T) {
		journey, runner, machines := installationJourney(t, "request-lab-rhel.golden")
		preview, err := journey.Service.Plan(context.Background(), lifecycle.PlanRequest{ContextName: lifecycle.JourneyContext})
		if err != nil || !slices.Equal(preview.Stops, []string{"rhel-01"}) || len(preview.StopsAfterOnline) != 0 {
			t.Fatalf("the preview stops %v first and %v after (%v)", preview.Stops, preview.StopsAfterOnline, err)
		}
		_, err = journey.Service.Destroy(context.Background(), destroy)
		if reported := diagnostics.Of(err); len(reported) == 0 || reported[0].Code != "lifecycle.live" || !strings.Contains(reported[0].Remediation, stop) {
			t.Fatalf("the destroy = %+v (%v), want lifecycle.live naming the stop", reported, err)
		}
		if runs := runner.took(); len(runs) != 0 || len(machines.removed) != 0 {
			t.Fatalf("the refused destroy ran %v and removed %v", runs, machines.removed)
		}
	})
	t.Run("stopped first", func(t *testing.T) {
		journey, runner, machines := installationJourney(t, "request-lab-rhel-registered.golden")
		setRunning(machines, false)
		_, err := journey.Service.Destroy(context.Background(), destroy)
		reported := diagnostics.Of(err)
		if len(reported) == 0 || !strings.Contains(reported[0].Remediation, "bootwright machine start --context "+lifecycle.JourneyContext+" --name rhel-01") ||
			!strings.Contains(reported[0].Message, "did not answer through its identity channel") {
			t.Fatalf("the destroy of a stopped registered Machine = %+v (%v)", reported, err)
		}
		if runs := runner.took(); !slices.Equal(runs, []string{"destroy:stopped"}) || len(machines.removed) != 0 {
			t.Fatalf("the destroy ran %v and removed %v", runs, machines.removed)
		}
	})
}
