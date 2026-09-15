package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func controllerReport(outcome string, dryRun bool) *prerequisites.Report {
	status, observed := "ready", "qualified"
	if outcome == "planned" || outcome == "incomplete" || outcome == "not-ready" {
		status, observed = "not-ready", "missing or unverified"
	}
	if dryRun {
		status, observed = "unverified", "unverified"
	}
	return &prerequisites.Report{Platform: prerequisites.Platform{OS: "fedora", Release: "43", Architecture: "amd64"}, DryRun: dryRun, Outcome: outcome,
		Checks: []prerequisites.Check{{ID: "execution-bundle", Required: "qualified Python and Ansible", Observed: observed, Status: status}}, Actions: []string{"Prepare the qualified execution bundle"}, Dependencies: []string{"qualified-source.tar.gz"}}
}

// Only preflight consumes a context. Setup prepares the prerequisites every
// context shares, so an explicit --context cannot reach its request and cannot
// change what it prepares.
func TestControllerReportsAndExplicitContextDispatch(t *testing.T) {
	for _, command := range [][]string{{"setup", "--dry-run"}, {"preflight", "controller"}} {
		for _, flag := range []string{"", "--context=", "--context=example"} {
			args := append([]string{}, command...)
			if flag != "" {
				args = append(args, flag)
			}
			dryRun := command[0] == "setup"
			outcome := "ready"
			if dryRun {
				outcome = "planned"
			}
			report := controllerReport(outcome, dryRun)
			name := ""
			if flag == "--context=example" && !dryRun {
				name, report.ContextName, report.Machine = "example", "example", "controller"
			}
			record := &dispatchRecord{result: commandResult{controller: report}}
			var out, errOut bytes.Buffer
			code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), args)
			if code != 0 || errOut.Len() != 0 || !strings.Contains(out.String(), "Outcome  "+outcome) {
				t.Fatal(args, code, out.String(), errOut.String())
			}
			switch request := record.request.(type) {
			case prerequisites.CheckRequest:
				if request.ContextName != name {
					t.Fatal(request)
				}
			case prerequisites.SetupRequest:
				if !request.DryRun {
					t.Fatal(request)
				}
			default:
				t.Fatal("unexpected request", record.request)
			}
		}
	}
}

func TestControllerNegativeReportPreservesStreamsAndSafeDiagnostics(t *testing.T) {
	record := &dispatchRecord{result: commandResult{controller: controllerReport("not-ready", false)}, err: diagnostics.NewFailure("controller.prerequisites", "required prerequisites are missing", "")}
	var out, errOut bytes.Buffer
	code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), []string{"preflight", "controller"})
	if code != 1 || !strings.Contains(out.String(), "Outcome  not-ready") || !strings.Contains(errOut.String(), "controller.prerequisites") {
		t.Fatal(code, out.String(), errOut.String())
	}
}

func TestControllerPlanFailureStopsOutputWithoutFallback(t *testing.T) {
	var errOut bytes.Buffer
	presenter := NewControllerPresenter(rejectingWriter{}, nil)
	err := presenter.PresentControllerPlan(context.Background(), *controllerReport("planned", true))
	record := &dispatchRecord{result: commandResult{controller: controllerReport("incomplete", false)}, err: err}
	var out bytes.Buffer
	code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), []string{"setup"})
	if code != 1 || out.Len() != 0 || errOut.Len() != 0 {
		t.Fatal(code, out.String(), errOut.String())
	}
}

func TestControllerConfirmationUsesSetupScope(t *testing.T) {
	var out bytes.Buffer
	confirmation := NewConfirmation(func(context.Context, []byte) (int, error) { return 0, errors.New("unexpected input") }, &out, func() (bool, error) { return false, nil })
	err := confirmation.Confirm(context.Background(), "setup", "this host")
	diagnostics := diagnostics.Of(err)
	if len(diagnostics) != 1 || diagnostics[0].Code != "controller.setup" || !strings.Contains(diagnostics[0].Message, "--yes") || out.Len() != 0 {
		t.Fatal(diagnostics, out.String())
	}
}

func TestControllerInvocationPrivilegeAndHelp(t *testing.T) {
	// Setup selects no context, so every dry run stays below the privilege
	// boundary even when an ignored --context is supplied.
	for _, args := range [][]string{{"setup", "--dry-run"}, {"setup", "--dry-run", "--context="}, {"setup", "--dry-run", "--context=example"}} {
		if ClassifyInvocation(args).RequiresRoot {
			t.Fatal("setup dry-run requested privilege", args)
		}
	}
	for _, args := range [][]string{{"setup"}, {"preflight", "controller"}, {"preflight", "controller", "--context=example"}} {
		if !ClassifyInvocation(args).RequiresRoot {
			t.Fatal("effectful invocation omitted privilege", args)
		}
	}
	for _, args := range [][]string{{"setup", "--help"}, {"preflight", "controller", "--help"}} {
		var out bytes.Buffer
		code := New(Config{Out: &out}).Run(context.Background(), args)
		if code != 0 || strings.Contains(out.String(), "default: current") || !strings.Contains(out.String(), "context") {
			t.Fatal(code, out.String())
		}
	}
}

func TestControllerSetupConfirmationJourneys(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		answer   string
		accepted bool
	}{
		{name: "accepted", answer: "y\n", accepted: true},
		{name: "accepted in full", answer: "yes\n", accepted: true},
		{name: "declined", answer: "n\n"},
		{name: "declined by default", answer: "\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var out bytes.Buffer
			remaining := []byte(testCase.answer)
			confirmation := NewConfirmation(func(_ context.Context, buffer []byte) (int, error) {
				if len(remaining) == 0 {
					return 0, errors.New("read beyond the single answer")
				}
				buffer[0], remaining = remaining[0], remaining[1:]
				return 1, nil
			}, &out, func() (bool, error) { return true, nil })
			err := confirmation.Confirm(context.Background(), "setup", "this host")
			// The plan is presented before the prompt, so the prompt names the
			// controller scope rather than a context transition.
			if !strings.Contains(out.String(), "Confirm controller setup on this host? [y/N] ") {
				t.Fatalf("prompt = %q", out.String())
			}
			if testCase.accepted {
				if err != nil {
					t.Fatalf("accepted answer %q = %v", testCase.answer, err)
				}
				return
			}
			diagnostics := diagnostics.Of(err)
			if len(diagnostics) != 1 || diagnostics[0].Code != "controller.setup" {
				t.Fatalf("declined answer %q = %v", testCase.answer, diagnostics)
			}
			if len(remaining) != 0 {
				t.Fatalf("confirmation read ahead, %d bytes left", len(remaining))
			}
		})
	}
}

func TestControllerSetupConfirmationStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out bytes.Buffer
	confirmation := NewConfirmation(func(context.Context, []byte) (int, error) { return 0, errors.New("unexpected input") }, &out, func() (bool, error) { return true, nil })
	diagnostics := diagnostics.Of(confirmation.Confirm(ctx, "setup", "this host"))
	if len(diagnostics) != 1 || diagnostics[0].Code != "controller.setup" || out.Len() != 0 {
		t.Fatal(diagnostics, out.String())
	}
}

func TestControllerSetupForwardsConfirmationSuppressionAndNoOp(t *testing.T) {
	for _, testCase := range []struct {
		args []string
		skip bool
	}{
		{args: []string{"setup"}},
		{args: []string{"setup", "--yes"}, skip: true},
		{args: []string{"setup", "--yes", "--context=example"}, skip: true},
	} {
		// A verified no-op reports "unchanged"; "ready" is the preflight outcome.
		report := controllerReport("unchanged", false)
		if strings.HasSuffix(testCase.args[len(testCase.args)-1], "example") {
			report.ContextName, report.Machine = "example", "controller"
		}
		record := &dispatchRecord{result: commandResult{controller: report}}
		var out, errOut bytes.Buffer
		code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), testCase.args)
		request, ok := record.request.(prerequisites.SetupRequest)
		if !ok || request.SkipConfirmation != testCase.skip || request.DryRun {
			t.Fatal(testCase.args, record.request)
		}
		// A verified no-op still reports its ordered result at success.
		if code != 0 || errOut.Len() != 0 || !strings.Contains(out.String(), "Outcome  unchanged") {
			t.Fatal(testCase.args, code, out.String(), errOut.String())
		}
	}
}

// Readiness lines must use the output contract's status vocabulary, not the
// service's internal readiness names.
func TestControllerChecksUseContractStatusTokens(t *testing.T) {
	for status, token := range map[string]string{"ready": "[OK]", "not-ready": "[FAIL]", "unverified": "[UNKNOWN]"} {
		report := controllerReport("planned", false)
		report.Checks[0].Status = status
		var out bytes.Buffer
		if err := NewControllerPresenter(&out, nil).PresentControllerPlan(context.Background(), *report); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "  "+token+"  Execution bundle  ") {
			t.Fatalf("status %q rendered as %q, want %s", status, out.String(), token)
		}
		for _, internal := range []string{"[ready]", "[not-ready]", "[unverified]"} {
			if strings.Contains(out.String(), internal) {
				t.Fatalf("internal readiness name %s reached the public result", internal)
			}
		}
	}
}

// An interrupted setup has already shown its plan, so its result must report
// which approved actions took effect rather than only that it did not finish.
func TestControllerIncompleteSetupReportsPerActionProgress(t *testing.T) {
	report := controllerReport("incomplete", false)
	report.PlanPresented = true
	report.Progress = []prerequisites.ActionProgress{
		{ID: "execution-bundle", Phase: "observed", Outcome: "changed"},
		{ID: "container-runtime", Phase: "intent"},
		{ID: "controller-binding", Phase: "planned"},
	}
	record := &dispatchRecord{result: commandResult{controller: report}, err: diagnostics.NewFailure("controller.unknown", "a setup action has an unresolved effect outcome", "")}
	var out, errOut bytes.Buffer
	code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), []string{"setup"})
	if code != 1 {
		t.Fatalf("exit = %d", code)
	}
	for _, want := range []string{"\nProgress\n", "  [DONE]     Execution bundle\n", "  [UNKNOWN]  Container runtime\n", "  [SKIPPED]  Controller binding\n"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("result %q lacks %q", out.String(), want)
		}
	}
	if !strings.Contains(errOut.String(), "controller.unknown") {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

// A completed setup reports readiness instead, with no progress group.
func TestControllerCompletedSetupReportsReadinessOnly(t *testing.T) {
	report := controllerReport("changed", false)
	report.PlanPresented = true
	report.Progress = []prerequisites.ActionProgress{{ID: "execution-bundle", Phase: "observed", Outcome: "changed"}}
	record := &dispatchRecord{result: commandResult{controller: report}}
	var out, errOut bytes.Buffer
	if code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), []string{"setup"}); code != 0 || errOut.Len() != 0 {
		t.Fatal(code, errOut.String())
	}
	if strings.Contains(out.String(), "Progress\n") || !strings.Contains(out.String(), "Readiness  all required prerequisites verified") {
		t.Fatalf("result = %q", out.String())
	}
}

// Resolution rows open the headline before the plan, and the plan then follows
// them as its own block without repeating it.
func TestControllerResolutionRowsPrecedeThePlanUnderOneHeadline(t *testing.T) {
	var out bytes.Buffer
	presenter := NewControllerPresenter(&out, nil)
	ctx := context.Background()
	presenter.ReportProgress(ctx, prerequisites.ProgressEvent{Phase: prerequisites.ResolutionPhase, Action: "Python and Ansible", Status: "running", Step: 1, Steps: 2})
	presenter.ReportProgress(ctx, prerequisites.ProgressEvent{Phase: prerequisites.ResolutionPhase, Action: "Python and Ansible", Status: "ok", Detail: "Python 3.14.7, Ansible 2.21.4", Step: 1, Steps: 2})
	presenter.ReportProgress(ctx, prerequisites.ProgressEvent{Phase: prerequisites.ResolutionPhase, Action: "Native packages", Status: "ok", Detail: "no changes", Step: 2, Steps: 2})
	if err := presenter.PresentControllerPlan(ctx, *controllerReport("planned", false)); err != nil {
		t.Fatal(err)
	}
	presenter.ReportProgress(ctx, prerequisites.ProgressEvent{Phase: prerequisites.SetupPhase, Action: "execution-bundle", Status: "running", Step: 1, Steps: 1})
	presenter.ReportProgress(ctx, prerequisites.ProgressEvent{Phase: prerequisites.SetupPhase, Action: "execution-bundle", Status: "running", Detail: "acquiring python.tar.gz, source 1 of 1", Step: 1, Steps: 1})
	presenter.ReportProgress(ctx, prerequisites.ProgressEvent{Phase: prerequisites.SetupPhase, Action: "execution-bundle", Status: "changed", Step: 1, Steps: 1})
	rendered := out.String()
	prefix := "Controller setup\n\nResolving\n" +
		"  [RUNNING]  Python and Ansible (1/2)\n" +
		"  [OK]       Python and Ansible: Python 3.14.7, Ansible 2.21.4 (1/2)\n" +
		"  [OK]       Native packages: no changes (2/2)\n" +
		"\n  Scope     baseline\n"
	if !strings.HasPrefix(rendered, prefix) {
		t.Fatalf("result = %q, want prefix %q", rendered, prefix)
	}
	suffix := "\nProgress\n" +
		"  [RUNNING]  Execution bundle (1/1)\n" +
		"  [RUNNING]  Execution bundle: acquiring python.tar.gz, source 1 of 1 (1/1)\n" +
		"  [DONE]     Execution bundle (1/1)\n"
	if strings.Count(rendered, "Controller setup\n") != 1 || !strings.Contains(rendered, "\nChecks\n") || !strings.HasSuffix(rendered, suffix) {
		t.Fatalf("result = %q", rendered)
	}
}

// A setup opens with its scope, streams each check as it settles, resolves,
// and only then presents the plan, which repeats neither the scope nor the
// checks.
func TestControllerScopeChecksResolutionAndPlanStreamInOrder(t *testing.T) {
	var out bytes.Buffer
	presenter := NewControllerPresenter(&out, nil)
	ctx := context.Background()
	report := controllerReport("planned", false)
	if err := presenter.PresentControllerScope(ctx, prerequisites.InspectionPhase, *report); err != nil {
		t.Fatal(err)
	}
	presenter.ReportProgress(ctx, prerequisites.ProgressEvent{Phase: prerequisites.InspectionPhase, Action: "host", Status: "ok", Detail: "fedora 43/amd64"})
	presenter.ReportProgress(ctx, prerequisites.ProgressEvent{Phase: prerequisites.InspectionPhase, Action: "execution-bundle", Status: "running", Detail: "verifying the retained bundle"})
	presenter.ReportProgress(ctx, prerequisites.ProgressEvent{Phase: prerequisites.InspectionPhase, Action: "execution-bundle", Status: "failed", Detail: "required qualified Python and Ansible; observed missing or unverified"})
	presenter.ReportProgress(ctx, prerequisites.ProgressEvent{Phase: prerequisites.ResolutionPhase, Action: "Native packages", Status: "ok", Detail: "no changes", Step: 1, Steps: 1})
	if err := presenter.PresentControllerPlan(ctx, *report); err != nil {
		t.Fatal(err)
	}
	want := "Controller setup\n\n  Scope     baseline\n  Platform  fedora 43/amd64\n" +
		"\nChecks\n" +
		"  [OK]       Host: fedora 43/amd64\n" +
		"  [RUNNING]  Execution bundle: verifying the retained bundle\n" +
		"  [FAIL]     Execution bundle: required qualified Python and Ansible; observed missing or unverified\n" +
		"\nResolving\n" +
		"  [OK]       Native packages: no changes (1/1)\n" +
		"\nDependencies\n  qualified-source.tar.gz\n" +
		"\nPlanned changes\n  1. Prepare the qualified execution bundle\n"
	if out.String() != want {
		t.Fatalf("result = %q, want %q", out.String(), want)
	}
}

// Readiness streams the same checks under its own headline.
func TestControllerReadinessStreamsUnderItsOwnHeadline(t *testing.T) {
	var out bytes.Buffer
	presenter := NewControllerPresenter(&out, nil)
	ctx := context.Background()
	if err := presenter.PresentControllerScope(ctx, prerequisites.ReadinessPhase, *controllerReport("ready", false)); err != nil {
		t.Fatal(err)
	}
	presenter.ReportProgress(ctx, prerequisites.ProgressEvent{Phase: prerequisites.ReadinessPhase, Action: "host", Status: "ok", Detail: "fedora 43/amd64"})
	want := "Controller readiness\n\n  Scope     baseline\n  Platform  fedora 43/amd64\n\nChecks\n  [OK]       Host: fedora 43/amd64\n"
	if out.String() != want {
		t.Fatalf("result = %q, want %q", out.String(), want)
	}
}

// The runner terminates a row a terminal is still rewriting before it writes
// the result, and a ready setup adds only its plan and outcome to the streamed
// checks.
func TestRunnerFinishesProgressBeforeTheReadyResult(t *testing.T) {
	report := controllerReport("unchanged", false)
	report.Actions, report.ProgressPresented = nil, true
	record := &dispatchRecord{result: commandResult{controller: report}}
	var out bytes.Buffer
	config := Config{Out: &out, Services: dispatchSpies(record), FinishProgress: func() { out.WriteString("<finished>") }}
	code := New(config).Run(context.Background(), []string{"setup"})
	want := "<finished>\nPlanned changes\n  none\n\n  Outcome  unchanged\n  Next     bootwright preflight controller\n"
	if code != 0 || out.String() != want {
		t.Fatalf("code=%d result=%q, want %q", code, out.String(), want)
	}
}

// A resolution failure has already streamed its rows, so the result adds only
// its outcome instead of a second headline and an unresolved plan.
func TestControllerResolutionFailureReportsOutcomeOnly(t *testing.T) {
	report := controllerReport("planned", false)
	report.ProgressPresented = true
	record := &dispatchRecord{result: commandResult{controller: report}, err: diagnostics.NewFailure("controller.setup", "publisher metadata is unavailable", "")}
	var out, errOut bytes.Buffer
	code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), []string{"setup"})
	want := "\n  Outcome  planned\n  Next     bootwright setup\n"
	if code != 1 || out.String() != want || !strings.Contains(errOut.String(), "controller.setup") {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
}
