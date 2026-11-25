package lifecycle_test

import (
	"context"
	"encoding/json"
	"errors"
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
	// unprepared fails the apply before it records its marker proof, as an
	// installation that never reached registration does.
	unprepared  bool
	unreachable bool
	failStart   bool
	failStop    bool
	probed      *probedMachines
}

func (r *registeringRunner) Run(ctx context.Context, run lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.machines.mutex.Lock()
	running := r.machines.running
	r.machines.mutex.Unlock()
	seen := run.Operation + ":" + map[bool]string{true: "running", false: "stopped"}[running]
	if decided, ok := run.MaterialValues["unregister"]; ok {
		seen += ":" + decided
	}
	r.runs = append(r.runs, seen)
	evidence := installation.Evidence{Absent: true, NetworkDifferences: []string{}, Postcondition: true, Request: run.Digest}
	switch run.Operation {
	case "apply":
		if r.request.Registration != nil && r.unprepared {
			return lifecycle.RunResult{}, run.Refusals["registration-refused"]
		}
		if r.request.Registration != nil {
			marker, _ := json.Marshal(map[string]string{"marker": run.MaterialValues["marker"]})
			if err := run.Prepare(ctx, marker); err != nil {
				return lifecycle.RunResult{}, err
			}
		}
		evidence = installation.Evidence{
			Address: r.request.Address, HostKey: "ssh-ed25519 AAAAGUEST", Image: true, Marker: run.MaterialValues["marker"], Network: true, NetworkDifferences: []string{},
			NetworkReboot: true, Postcondition: true, Power: "On", Reachable: true, Request: run.Digest, Tree: true, TreeIdentity: r.request.TreeMedia.SHA256,
		}
		if r.request.Registration != nil {
			evidence.Registration = installation.RegistrationEvidence{Consumer: registeredConsumer, State: "owned"}
		}
		evidence.Update = installation.UpdateEvidence{State: "none"}
		if r.request.Update != nil {
			evidence.Update = installation.UpdateEvidence{Kernel: "5.14.0-611.5.1.el9_7.x86_64", Release: r.request.Update.Release, State: "complete"}
		}
	case "observe":
		if run.MaterialValues["unregister"] == "guest" {
			evidence.Power = map[bool]string{true: "On", false: "Off"}[running]
		}
	case "destroy":
		if run.MaterialValues["unregister"] == "skip" {
			break
		}
		if (!running || r.unreachable) && r.request.Registration != nil {
			return lifecycle.RunResult{}, run.Refusals["registration-unreachable"]
		}
		if r.request.Registration != nil {
			evidence.Registration = installation.RegistrationEvidence{Consumer: registeredConsumer, State: "unregistered"}
		}
	}
	data, err := json.Marshal(evidence)
	return lifecycle.RunResult{Outcome: "changed", Evidence: data}, err
}

// probedMachines counts the stop proofs the engine asks of the Machine.
func (m *probedMachines) StartPowerSecrets(reconciliation.Block) ([]string, error) { return nil, nil }

func (m *probedMachines) StartPower(ctx context.Context, probe lifecycle.Probe) (lifecycle.Quiescence, error) {
	return m.fleetMachines.Quiescent(ctx, probe)
}

func (m *probedMachines) Apply(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	setRunning(m.fleetMachines, true)
	return m.fleetMachines.Apply(ctx, e)
}

type probedMachines struct {
	*fleetMachines
	probes int
}

func (m *probedMachines) Quiescent(ctx context.Context, probe lifecycle.Probe) (lifecycle.Quiescence, error) {
	m.mutex.Lock()
	m.probes++
	m.mutex.Unlock()
	return m.fleetMachines.Quiescent(ctx, probe)
}

func (m *probedMachines) probed() int {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.probes
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
	journey, runner, machines := boundInstallation(t, golden, false, "rhel-01")
	result, err := journey.Service.Apply(context.Background(), lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true})
	if err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("the apply = %+v, %+v (%v)", result, diagnostics.Of(err), err)
	}
	if runs := runner.took(); !slices.Equal(runs, []string{"apply:running"}) {
		t.Fatalf("the apply ran %v", runs)
	}
	return journey, runner, machines
}

func boundInstallation(t *testing.T, golden string, unprepared bool, objects ...string) (*lifecycle.CapabilityJourney, *registeringRunner, *fleetMachines) {
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
	probed := &probedMachines{fleetMachines: &fleetMachines{}}
	machines := probed.fleetMachines
	runner := &registeringRunner{machines: machines, request: request, unprepared: unprepared, probed: probed}
	definitions := []reconciliation.BlockDefinition{}
	for _, object := range objects {
		definitions = append(definitions,
			reconciliation.BlockDefinition{ID: "machine-" + object, Description: "create the Machine " + object, Stage: reconciliation.StageMachines, Impacts: []string{"create-machine " + object}, Consumes: []string{},
				Kind: string(api.Machine), Object: object, Implementation: fleetMachineImplementation, ContentDigest: strings.Repeat("c", 64), Request: json.RawMessage(`{"name":"` + object + `"}`)},
			reconciliation.BlockDefinition{ID: installation.BlockID(object), Description: "install the operating system of " + object, Stage: reconciliation.StageMachines, Dependencies: []string{"machine-" + object},
				Impacts: []string{"install-operating-system " + object}, Consumes: []string{}, Kind: installation.Kind, Object: object, Implementation: installation.Implementation,
				ContentDigest: installation.ContentDigest(), Request: canonical})
	}
	journey := lifecycle.NewBoundJourney(t, []lifecycle.BoundCapability{
		{Binding: lifecycle.CapabilityBinding{Kind: string(api.Machine), Implementation: fleetMachineImplementation, Family: reconciliation.ManagedMachineFamily}, Capability: probed},
		{Binding: lifecycle.CapabilityBinding{Kind: installation.Kind, Implementation: installation.Implementation}, Capability: installation.New(runner)},
	}, lifecycle.CapabilityPlan{Definitions: definitions, Secrets: request.SecretReferences()})
	journey.WithMachineDriver(func(_ context.Context, _ lifecycle.Execution, _ lifecycle.BlockEvidence, start bool) error {
		runner.mutex.Lock()
		defer runner.mutex.Unlock()
		if start && runner.failStart {
			return errors.New("power start response lost")
		}
		if !start && runner.failStop {
			return errors.New("shutdown did not settle")
		}
		setRunning(machines, start)
		return nil
	})
	holdInstallationSecrets(journey, request)
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
	t.Parallel()
	destroy := lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}
	stop := "bootwright machine stop --context " + lifecycle.JourneyContext + " --name rhel-01"
	t.Run("registered", func(t *testing.T) {
		journey, runner, machines := installationJourney(t, "request-lab-rhel-registered.golden")
		preview, err := journey.Service.Plan(context.Background(), lifecycle.PlanRequest{ContextName: lifecycle.JourneyContext})
		if err != nil || !slices.Equal(preview.Stops, []string{"rhel-01"}) || !slices.Equal(preview.StopsAfterOnline, []string{"rhel-01"}) {
			t.Fatalf("the registered preview stops %v first and %v after (%v)", preview.Stops, preview.StopsAfterOnline, err)
		}
		_, err = journey.Service.Destroy(context.Background(), destroy)
		reported := diagnostics.Of(err)
		if len(reported) == 0 || reported[0].Code != "lifecycle.start-power" || !strings.Contains(reported[0].Remediation, stop) {
			t.Fatalf("the first destroy = %+v (%v), want lifecycle.live naming the stop", reported, err)
		}
		if runs := runner.took(); len(runs) != 0 || len(machines.removed) != 0 {
			t.Fatalf("the first destroy ran %v and removed %v", runs, machines.removed)
		}
		setRunning(machines, false)
		holdInstallationSecrets(journey, runner.request)
		result, err := journey.Service.Destroy(context.Background(), destroy)
		if err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
			t.Fatalf("the destroy after the stop = %+v, %+v (%v)", result, diagnostics.Of(err), err)
		}
		if runs := runner.took(); !slices.Equal(runs, []string{"destroy:running:guest"}) || !slices.Equal(machines.removed, []string{"rhel-01"}) {
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
		if reported := diagnostics.Of(err); len(reported) == 0 || reported[0].Code != "lifecycle.start-power" || !strings.Contains(reported[0].Remediation, stop) {
			t.Fatalf("the destroy = %+v (%v), want lifecycle.live naming the stop", reported, err)
		}
		if runs := runner.took(); len(runs) != 0 || len(machines.removed) != 0 {
			t.Fatalf("the refused destroy ran %v and removed %v", runs, machines.removed)
		}
	})
}

const unreachableMessage = "the apply of Machine/rhel-01 proved its install marker before it registered, and Machine/rhel-01 did not answer through its identity channel or its pinned connection, so its subscription registration cannot be read and was not removed"

func TestUnreadableProvedRegistrationRefusesBeforeAnyInverse(t *testing.T) {
	t.Parallel()
	journey, runner, machines := installationJourney(t, "request-lab-rhel-registered.golden")
	setRunning(machines, false)
	runner.unreachable = true
	_, err := journey.Service.Destroy(context.Background(), lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true})
	if !slices.ContainsFunc(diagnostics.Of(err), func(reported diagnostics.Diagnostic) bool {
		return reported.Code == "install.service-unavailable" && reported.Message == unreachableMessage && reported.Object != nil && reported.Object.Name == "rhel-01" &&
			strings.HasSuffix(reported.Remediation, "then run bootwright destroy --context "+lifecycle.JourneyContext+" --unregistered rhel-01")
	}) {
		t.Fatalf("the destroy of a stopped registered Machine = %+v (%v)", diagnostics.Of(err), err)
	}
	if runs := runner.took(); !slices.Equal(runs, []string{"destroy:running:guest"}) || len(machines.removed) != 0 {
		t.Fatalf("the destroy ran %v, removed %v and proved the stop %d times", runs, machines.removed, runner.probed.probed())
	}
}

func TestNeverRegisteredInstallationIsDestroyedWithoutTheGuest(t *testing.T) {
	t.Parallel()
	journey, runner, machines := boundInstallation(t, "request-lab-rhel-registered.golden", true, "rhel-01")
	if _, err := journey.Service.Apply(context.Background(), lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}); err == nil {
		t.Fatal("an apply that failed before its marker proof completed")
	}
	if runs := runner.took(); !slices.Equal(runs, []string{"apply:running"}) {
		t.Fatalf("the apply ran %v", runs)
	}
	setRunning(machines, false)
	holdInstallationSecrets(journey, runner.request)
	result, err := journey.Service.Destroy(context.Background(), lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true})
	if err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("the destroy = %+v, %+v (%v)", result, diagnostics.Of(err), err)
	}
	if runs := runner.took(); !slices.Contains(runs, "destroy:stopped:skip") || slices.Contains(runs, "destroy:stopped:guest") || !slices.Equal(machines.removed, []string{"rhel-01"}) {
		t.Fatalf("the destroy ran %v and removed %v", runs, machines.removed)
	}
}

func TestUnregisteredFlagLetsTheRerunProceed(t *testing.T) {
	t.Parallel()
	journey, runner, machines := installationJourney(t, "request-lab-rhel-registered.golden")
	setRunning(machines, false)
	runner.unreachable = true
	destroy := lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}
	if _, err := journey.Service.Destroy(context.Background(), destroy); err == nil {
		t.Fatal("the destroy of an unanswering registered Machine completed")
	}
	if runs := runner.took(); !slices.Equal(runs, []string{"destroy:running:guest"}) {
		t.Fatalf("the refused destroy ran %v", runs)
	}
	holdInstallationSecrets(journey, runner.request)
	setRunning(machines, false)
	preview, err := journey.Service.Plan(context.Background(), lifecycle.PlanRequest{ContextName: lifecycle.JourneyContext, Verb: reconciliation.Destroy, Unregistered: []string{"rhel-01"}})
	if err != nil || !slices.ContainsFunc(preview.Steps, func(step lifecycle.PlanStep) bool {
		return step.ID == installation.BlockID("rhel-01") && strings.Contains(step.Description, "unregistered with the subscription service's own tools") && !slices.Contains(step.Impacts, "unregister-subscription rhel-01")
	}) {
		t.Fatalf("the acknowledged preview = %+v (%v)", preview, err)
	}
	destroy.Unregistered = []string{"rhel-01"}
	result, err := journey.Service.Destroy(context.Background(), destroy)
	if err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("the acknowledged destroy = %+v, %+v (%v)", result, diagnostics.Of(err), err)
	}
	if block, _ := journey.FrozenPlan(t).Block(installation.BlockID("rhel-01")); block.Relaxation != lifecycle.Unregistered {
		t.Fatalf("the acknowledged removal froze %q, want the unregistered relaxation", block.Relaxation)
	}
	if runs := runner.took(); len(runs) == 0 || slices.ContainsFunc(runs, func(run string) bool { return !strings.HasSuffix(run, ":skip") }) ||
		!slices.Equal(machines.removed, []string{"rhel-01"}) {
		t.Fatalf("the acknowledged destroy ran %v and removed %v", runs, machines.removed)
	}
}

func TestPrintedNextStepsCompleteTheManualUnregistrationWhenTheMachineRemovalLosesData(t *testing.T) {
	t.Parallel()
	journey, runner, machines := installationJourney(t, "request-lab-rhel-registered.golden")
	machines.consumes = []string{"data-loss"}
	setRunning(machines, false)
	command := "bootwright destroy --context " + lifecycle.JourneyContext + " --authorize data-loss"
	var followed []string
	for range 4 {
		followed = append(followed, command)
		holdInstallationSecrets(journey, runner.request)
		result, err := journey.Service.Destroy(context.Background(), destroyCommand(t, command))
		if err == nil {
			if result.Receipt.State != string(reconciliation.OperationDone) || !slices.Equal(machines.removed, []string{"rhel-01"}) {
				t.Fatalf("%q = %+v and removed %v", command, result, machines.removed)
			}
			return
		}
		reported := diagnostics.Of(err)
		if len(reported) == 0 {
			t.Fatalf("%q failed without a diagnostic: %v", command, err)
		}
		remedy := reported[0].Remediation
		at := strings.LastIndex(remedy, "bootwright destroy ")
		if at < 0 {
			t.Fatalf("%q = %+v names no destroy to run next", command, reported)
		}
		command = remedy[at:]
	}
	t.Fatalf("following each printed next step ran %q without completing", followed)
}

func destroyCommand(t *testing.T, command string) lifecycle.DestroyRequest {
	t.Helper()
	request := lifecycle.DestroyRequest{SkipConfirmation: true}
	fields := strings.Fields(strings.TrimPrefix(command, "bootwright destroy "))
	for index := 0; index+1 < len(fields); index += 2 {
		switch fields[index] {
		case "--context":
			request.ContextName = fields[index+1]
		case "--authorize":
			request.Authorizations = append(request.Authorizations, strings.Split(fields[index+1], ",")...)
		case "--unregistered":
			request.Unregistered = append(request.Unregistered, strings.Split(fields[index+1], ",")...)
		default:
			t.Fatalf("the next step %q has an unexpected flag %q", command, fields[index])
		}
	}
	if len(fields)%2 != 0 || request.ContextName == "" {
		t.Fatalf("the next step %q is not a destroy command", command)
	}
	return request
}

func TestUnregisteredFlagRefusesAMachineThePlanDoesNotUnregister(t *testing.T) {
	t.Parallel()
	for name, golden := range map[string]string{"registered": "request-lab-rhel-registered.golden", "not registered": "request-lab-rhel.golden"} {
		t.Run(name, func(t *testing.T) {
			journey, runner, machines := installationJourney(t, golden)
			setRunning(machines, false)
			unknown := "rhel-02"
			if name == "not registered" {
				unknown = "rhel-01"
			}
			_, err := journey.Service.Destroy(context.Background(), lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, Unregistered: []string{unknown}})
			reported := diagnostics.Of(err)
			if len(reported) != 1 || reported[0].Code != "authorize.unneeded" ||
				reported[0].Message != "this destroy removes no subscription registration from Machine/"+unknown+", so --unregistered "+unknown+" acknowledges nothing" ||
				reported[0].Remediation != "repeat bootwright destroy --context "+lifecycle.JourneyContext+" without --unregistered "+unknown {
				t.Fatalf("the destroy naming %s = %#v (%v)", unknown, reported, err)
			}
			if journey.Confirmations() != 0 || len(runner.took()) != 0 || len(machines.removed) != 0 {
				t.Fatalf("the refused destroy asked %d confirmations, ran the adapter or removed %v", journey.Confirmations(), machines.removed)
			}
			if operation := journey.Operation(t); operation.Verb != reconciliation.Apply {
				t.Fatalf("the refused destroy registered operation %+v", operation)
			}
		})
	}
}

func TestUnregisteredFlagReleasesOnlyTheNamedMachine(t *testing.T) {
	t.Parallel()
	journey, runner, machines := boundInstallation(t, "request-lab-rhel-registered.golden", false, "rhel-01", "rhel-02")
	if _, err := journey.Service.Apply(context.Background(), lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}); err != nil {
		t.Fatalf("the apply = %+v (%v)", diagnostics.Of(err), err)
	}
	runner.took()
	setRunning(machines, false)
	holdInstallationSecrets(journey, runner.request)
	_, _ = journey.Service.Destroy(context.Background(), lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, Unregistered: []string{"rhel-02"}, SkipConfirmation: true})
	runs := runner.took()
	slices.Sort(runs)
	if len(runs) != 2 || !slices.Contains(runs, "destroy:running:guest") || !slices.ContainsFunc(runs, func(run string) bool { return strings.HasSuffix(run, ":skip") }) {
		t.Fatalf("the destroy naming rhel-02 ran %v", runs)
	}
}

func TestReleasedRegistrationFinishesShutdownAfterItsAttemptLosesPowerResponse(t *testing.T) {
	journey, runner, machines := installationJourney(t, "request-lab-rhel-registered.golden")
	setRunning(machines, false)
	runner.failStop = true
	request := lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}
	if _, err := journey.Service.Destroy(context.Background(), request); err == nil {
		t.Fatal("the unproved shutdown completed")
	}
	if runs := runner.took(); !slices.Equal(runs, []string{"destroy:running:guest"}) || len(machines.removed) != 0 {
		t.Fatalf("a failed shutdown ran %v and removed %v", runs, machines.removed)
	}
	holdInstallationSecrets(journey, runner.request)
	runner.failStop = false
	result, err := journey.Service.Destroy(context.Background(), request)
	if err != nil || result.Receipt.State != string(reconciliation.OperationDone) || len(runner.took()) != 0 || !slices.Equal(machines.removed, []string{"rhel-01"}) {
		t.Fatalf("resuming shutdown = %+v (%v, %v), removed %v", result, err, diagnostics.Of(err), machines.removed)
	}
}

func TestRegisteredDestroyResumesAfterAnUnprovedColdStart(t *testing.T) {
	for _, running := range []bool{false, true} {
		t.Run(map[bool]string{true: "on", false: "off"}[running], func(t *testing.T) {
			journey, runner, machines := installationJourney(t, "request-lab-rhel-registered.golden")
			request := lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}
			setRunning(machines, false)
			runner.failStart = true
			if _, err := journey.Service.Destroy(context.Background(), request); err == nil || len(runner.took()) != 0 {
				t.Fatal("the failed cold start reached registration cleanup")
			}
			setRunning(machines, running)
			runner.failStart = false
			holdInstallationSecrets(journey, runner.request)
			result, err := journey.Service.Destroy(context.Background(), request)
			if err != nil || result.Receipt.State != string(reconciliation.OperationDone) || !slices.Contains(runner.took(), "destroy:running:guest") {
				t.Fatalf("resumed cleanup = %+v (%v, %v)", result, err, diagnostics.Of(err))
			}
		})
	}
}
