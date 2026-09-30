package prerequisites

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"
)

// preparedSetup is a confirmed setup whose container runtime is installed by
// the controller Ansible.
func preparedSetup(t *testing.T, f *fixture) (*Report, error) {
	t.Helper()
	return f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
}

// recoveredSetup is a setup whose native result was lost and whose repetition
// recovers the recorded transaction with the controller Ansible instead of
// installing again.
func recoveredSetup(t *testing.T, f *fixture) (*Report, error) {
	t.Helper()
	installer := f.service.runtime.(*testRuntimeInstaller)
	installer.err = failure("controller.unknown", "native result was lost", "resolve the native transaction")
	installer.result = ActionResult{Outcome: "unknown", Evidence: object(map[string]any{"installationEntered": true})}
	if _, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true}); code(err) != "controller.unknown" {
		t.Fatalf("the interrupted setup = %v", err)
	}
	f.host.runtime = RuntimeInspection{Present: true, Ready: true}
	return f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
}

// A preparation and a recovery each keep what their Ansible printed in a run
// of their own, named before that Ansible starts and again on the result.
func TestAPreparationAndARecoveryEachKeepTheirAnsibleOutput(t *testing.T) {
	for _, journey := range []struct {
		name  string
		run   func(*testing.T, *fixture) (*Report, error)
		runs  int
		print string
	}{
		{"preparation", preparedSetup, 1, "TASK [install the native packages]\n"},
		{"recovery", recoveredSetup, 2, "TASK [recover the recorded native transaction]\n"},
	} {
		t.Run(journey.name, func(t *testing.T) {
			f, installer := explicitRuntimeFixture(t)
			progress := &recordingProgress{owner: f}
			f.service.options.Progress = progress
			installer.started = func() {
				if len(progress.locations) == 0 || progress.locations[len(progress.locations)-1] != f.store.runs[len(f.store.runs)-1].Location() {
					t.Errorf("the Ansible started before its run was named: %v", progress.locations)
				}
			}
			report, err := journey.run(t, f)
			if err != nil || report.Outcome != "changed" {
				t.Fatalf("setup = %#v (%v)", report, err)
			}
			if len(f.store.runs) != journey.runs {
				t.Fatalf("setup kept %d runs, want %d", len(f.store.runs), journey.runs)
			}
			run := f.store.runs[len(f.store.runs)-1]
			if run.output.String() != journey.print || !run.closed {
				t.Fatalf("the run kept %q, closed %t", run.output.String(), run.closed)
			}
			if report.LogLocation != run.Location() || len(progress.locations) != journey.runs || progress.locations[journey.runs-1] != run.Location() {
				t.Fatalf("the result names %q and progress named %v, want %q", report.LogLocation, progress.locations, run.Location())
			}
			if last := installer.outputs[len(installer.outputs)-1]; last != RunOutput(run) {
				t.Fatalf("the Ansible wrote to %v, not to its run", last)
			}
		})
	}
}

// A dry run, a no-op setup and a setup that starts no Ansible open no run.
func TestASetupThatStartsNoAnsibleOpensNoRun(t *testing.T) {
	f := newFixture(t)
	progress := &recordingProgress{owner: f}
	f.service.options.Progress = progress
	if report, err := f.service.Setup(context.Background(), SetupRequest{DryRun: true}); err != nil || report.LogLocation != "" {
		t.Fatalf("dry run = %#v (%v)", report, err)
	}
	if report, err := preparedSetup(t, f); err != nil || report.Outcome != "changed" || report.LogLocation != "" {
		t.Fatalf("a bundle-only setup = %#v (%v)", report, err)
	}
	if report, err := preparedSetup(t, f); err != nil || report.Outcome != "unchanged" || report.LogLocation != "" {
		t.Fatalf("a no-op setup = %#v (%v)", report, err)
	}
	if len(f.store.runs) != 0 || len(progress.locations) != 0 {
		t.Fatalf("setup opened %d runs and named %v", len(f.store.runs), progress.locations)
	}
	runtime, installer := explicitRuntimeFixture(t)
	if _, err := preparedSetup(t, runtime); err != nil || installer.calls != 1 {
		t.Fatalf("setup = %v", err)
	}
	if report, err := preparedSetup(t, runtime); err != nil || report.Outcome != "unchanged" || report.LogLocation != "" || len(runtime.store.runs) != 1 {
		t.Fatalf("a no-op setup after the runtime = %#v (%v), %d runs", report, err, len(runtime.store.runs))
	}
}

// retainedJourney is what one setup journey leaves behind that retention must
// not move: its outcome and failure, the receipt, and the progress read from
// that receipt.
type retainedJourney struct {
	outcome  string
	code     string
	receipt  string
	progress []ActionProgress
}

func journeyEvidence(t *testing.T, f *fixture, report *Report, err error) retainedJourney {
	t.Helper()
	receipt, encoding := json.Marshal(f.store.state)
	if encoding != nil || report == nil {
		t.Fatalf("the journey left %#v (%v)", report, encoding)
	}
	return retainedJourney{outcome: report.Outcome, code: code(err), receipt: string(receipt), progress: slices.Clone(report.Progress)}
}

// A full area, an unwritable parent, an existing run name, a link in place of
// runs and a run whose writes and close fail each leave the outcome and the
// receipt byte-identical to a setup that kept its run, for a preparation, a
// failed preparation and a recovery.
func TestARetentionFaultChangesNeitherTheOutcomeNorTheReceipt(t *testing.T) {
	faults := map[string]func(*memoryStorage){
		"a full area":               func(m *memoryStorage) { m.runFault = errors.New("setup run numbers are exhausted") },
		"an unwritable parent":      func(m *memoryStorage) { m.runFault = errors.New("held state directory changed") },
		"an existing run name":      func(m *memoryStorage) { m.runFault = errors.New("it could not be created exclusively") },
		"a link in place of runs":   func(m *memoryStorage) { m.runFault = errors.New("it is not a private directory") },
		"a failing write and close": func(m *memoryStorage) { m.brokenRuns = true },
	}
	journeys := map[string]func(*testing.T, *fixture) (*Report, error){
		"preparation": preparedSetup,
		"recovery":    recoveredSetup,
		"failed preparation": func(t *testing.T, f *fixture) (*Report, error) {
			installer := f.service.runtime.(*testRuntimeInstaller)
			installer.err = failure("controller.unsupported", "provided native foundation is incompatible", "prepare the qualified foundation")
			installer.result = ActionResult{Outcome: "failed", Evidence: object(map[string]any{"installationEntered": false})}
			return preparedSetup(t, f)
		},
	}
	for journeyName, journey := range journeys {
		baseline, _ := explicitRuntimeFixture(t)
		report, err := journey(t, baseline)
		want := journeyEvidence(t, baseline, report, err)
		if len(baseline.store.runs) == 0 || report.LogLocation == "" {
			t.Fatalf("the %s baseline kept no run", journeyName)
		}
		for faultName, fault := range faults {
			t.Run(journeyName+" with "+faultName, func(t *testing.T) {
				f, _ := explicitRuntimeFixture(t)
				fault(&f.store)
				progress := &recordingProgress{owner: f}
				f.service.options.Progress = progress
				report, err := journey(t, f)
				if got := journeyEvidence(t, f, report, err); !reflect.DeepEqual(got, want) {
					t.Fatalf("the journey left %+v\nwant %+v", got, want)
				}
				if f.store.runFault != nil && (report.LogLocation != "" || len(progress.locations) != 0) {
					t.Fatalf("a run that never opened was named: %q %v", report.LogLocation, progress.locations)
				}
			})
		}
	}
}
