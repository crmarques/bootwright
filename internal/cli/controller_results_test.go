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

func TestControllerReportsAndExplicitContextDispatch(t *testing.T) {
	for _, command := range [][]string{{"bastion", "setup", "--dry-run"}, {"preflight", "bastion"}} {
		for _, flag := range []string{"", "--context=", "--context=example"} {
			args := append([]string{}, command...)
			if flag != "" {
				args = append(args, flag)
			}
			dryRun := command[0] == "bastion"
			outcome := "ready"
			if dryRun {
				outcome = "planned"
			}
			report := controllerReport(outcome, dryRun)
			name := ""
			if flag == "--context=example" {
				name, report.ContextName, report.Machine = "example", "example", "bastion"
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
				if request.ContextName != name || !request.DryRun {
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
	code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), []string{"preflight", "bastion"})
	if code != 1 || !strings.Contains(out.String(), "Outcome  not-ready") || !strings.Contains(errOut.String(), "controller.prerequisites") {
		t.Fatal(code, out.String(), errOut.String())
	}
}

func TestControllerPlanFailureStopsOutputWithoutFallback(t *testing.T) {
	var errOut bytes.Buffer
	presenter := NewControllerPlanPresenter(rejectingWriter{})
	err := presenter.PresentControllerPlan(context.Background(), *controllerReport("planned", true))
	record := &dispatchRecord{result: commandResult{controller: controllerReport("incomplete", false)}, err: err}
	var out bytes.Buffer
	code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), []string{"bastion", "setup"})
	if code != 1 || out.Len() != 0 || errOut.Len() != 0 {
		t.Fatal(code, out.String(), errOut.String())
	}
}

func TestControllerConfirmationUsesSetupScope(t *testing.T) {
	var out bytes.Buffer
	confirmation := NewConfirmation(func(context.Context, []byte) (int, error) { return 0, errors.New("unexpected input") }, &out, func() (bool, error) { return false, nil })
	err := confirmation.Confirm(context.Background(), "bastion setup", "baseline")
	diagnostics := diagnostics.Of(err)
	if len(diagnostics) != 1 || diagnostics[0].Code != "controller.setup" || !strings.Contains(diagnostics[0].Message, "--yes") || out.Len() != 0 {
		t.Fatal(diagnostics, out.String())
	}
}

func TestControllerInvocationPrivilegeAndHelp(t *testing.T) {
	for _, args := range [][]string{{"bastion", "setup", "--dry-run"}, {"bastion", "setup", "--dry-run", "--context="}} {
		if ClassifyInvocation(args).RequiresRoot {
			t.Fatal("baseline dry-run requested privilege", args)
		}
	}
	for _, args := range [][]string{{"bastion", "setup"}, {"bastion", "setup", "--dry-run", "--context=example"}, {"preflight", "bastion"}} {
		if !ClassifyInvocation(args).RequiresRoot {
			t.Fatal("effectful invocation omitted privilege", args)
		}
	}
	for _, args := range [][]string{{"bastion", "setup", "--help"}, {"preflight", "bastion", "--help"}} {
		var out bytes.Buffer
		code := New(Config{Out: &out}).Run(context.Background(), args)
		if code != 0 || strings.Contains(out.String(), "default: current") || !strings.Contains(out.String(), "baseline") {
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
			err := confirmation.Confirm(context.Background(), "bastion setup", "baseline")
			// The plan is presented before the prompt, so the prompt names the
			// bastion scope rather than a context transition.
			if !strings.Contains(out.String(), "Confirm bastion setup for baseline? [y/N] ") {
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
	diagnostics := diagnostics.Of(confirmation.Confirm(ctx, "bastion setup", "baseline"))
	if len(diagnostics) != 1 || diagnostics[0].Code != "controller.setup" || out.Len() != 0 {
		t.Fatal(diagnostics, out.String())
	}
}

func TestControllerSetupForwardsConfirmationSuppressionAndNoOp(t *testing.T) {
	for _, testCase := range []struct {
		args []string
		skip bool
	}{
		{args: []string{"bastion", "setup"}},
		{args: []string{"bastion", "setup", "--yes"}, skip: true},
		{args: []string{"bastion", "setup", "--yes", "--context=example"}, skip: true},
	} {
		// A verified no-op reports "unchanged"; "ready" is the preflight outcome.
		report := controllerReport("unchanged", false)
		if strings.HasSuffix(testCase.args[len(testCase.args)-1], "example") {
			report.ContextName, report.Machine = "example", "bastion"
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
		if err := NewControllerPlanPresenter(&out).PresentControllerPlan(context.Background(), *report); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "  "+token+"  execution-bundle  ") {
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
	code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), []string{"bastion", "setup"})
	if code != 1 {
		t.Fatalf("exit = %d", code)
	}
	for _, want := range []string{"Progress\n", "  [DONE]     execution-bundle\n", "  [UNKNOWN]  container-runtime\n", "  [SKIPPED]  controller-binding\n"} {
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
	if code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), []string{"bastion", "setup"}); code != 0 || errOut.Len() != 0 {
		t.Fatal(code, errOut.String())
	}
	if strings.Contains(out.String(), "Progress\n") || !strings.Contains(out.String(), "Readiness  all required prerequisites verified") {
		t.Fatalf("result = %q", out.String())
	}
}
