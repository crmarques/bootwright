package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	storagepreflight "github.com/crmarques/bootwright/internal/storage/preflight"
)

func TestAClusterPreflightUnknownWritesOneCompleteJSONResult(t *testing.T) {
	record := &dispatchRecord{result: commandResult{storagePreflight: pendingStoragePreflight()}, err: diagnostics.NewFailure("preflight.unknown", "storage readiness is not proved", "")}
	var out, errOut bytes.Buffer
	exit := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), []string{"preflight", "--stage", "clusters", "--clusters", "ceph", "--output", "json"})
	if exit != 1 || errOut.Len() != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, errOut.String())
	}
	decoder := json.NewDecoder(&out)
	var envelope struct {
		OK     bool
		Result struct {
			Checks  []storageCheckResult
			Summary storageSummaryResult
		}
	}
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.OK || len(envelope.Result.Checks) != 2 || envelope.Result.Summary.Pending != 1 {
		t.Fatalf("%+v", envelope)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("extra output: %v", err)
	}
}
func TestAClusterPreflightHumanFailureKeepsChecksAndEscapesText(t *testing.T) {
	report := pendingStoragePreflight()
	report.Checks[0].Reason = "not ready\x1b[31m\nforged"
	record := &dispatchRecord{result: commandResult{storagePreflight: report}, err: diagnostics.NewFailure("preflight.unknown", "storage readiness is not proved", "")}
	var out, errOut bytes.Buffer
	exit := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), []string{"preflight", "--stage", "clusters", "--clusters", "ceph"})
	if exit != 1 || !strings.Contains(out.String(), "pending") || !strings.Contains(errOut.String(), "preflight.unknown") || strings.Contains(out.String(), "\x1b") || strings.Contains(out.String(), "\nforged") {
		t.Fatalf("%d %q %q", exit, out.String(), errOut.String())
	}
}

func TestClusterStagePreflightHumanFailureKeepsBothKindsOfChecksUnderOneDiagnostic(t *testing.T) {
	report := clustersPreflightReport(true)
	record := &dispatchRecord{result: commandResult{storagePreflight: report}, err: diagnostics.NewFailureWithRemediation("preflight.unknown", "cluster readiness is not proved; inspect the failed, unknown and pending checks", "", "resolve the reported prerequisites and repeat bootwright preflight --stage clusters")}
	var out, errOut bytes.Buffer
	exit := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), []string{"preflight", "--stage", "clusters", "--dry-run"})
	if exit != 1 || !strings.HasPrefix(out.String(), "  ○ Preflight synthetic\n    Stages  clusters\n") || !strings.Contains(out.String(), "resolution.api") || !strings.Contains(out.String(), "capacity.memory") || strings.Count(errOut.String(), "preflight.unknown") != 1 {
		t.Fatalf("%d %q %q", exit, out.String(), errOut.String())
	}
}

func TestInfrastructureStagesAndDefaultPreflightHumanFailureKeepEveryCheckUnderOneDiagnostic(t *testing.T) {
	for _, row := range []struct {
		args, title string
		report      *storagepreflight.Report
		code        string
	}{
		{"preflight --stage controller,infrastructure-services,substrates", "  ! Preflight synthetic\n    Stages  controller, infrastructure-services, substrates\n", infraPreflightReport(true), "preflight.failed"},
		{"preflight --dry-run", "  ○ Preflight synthetic\n    Stages  controller, infrastructure-services, substrates, machines, clusters\n", allPreflightReport(true), "preflight.unknown"},
	} {
		record := &dispatchRecord{result: commandResult{storagePreflight: row.report}, err: diagnostics.NewFailureWithRemediation(row.code, "readiness is not proved", "", "resolve the reported prerequisites")}
		var out, errOut bytes.Buffer
		exit := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), strings.Fields(row.args))
		if exit != 1 || !strings.HasPrefix(out.String(), row.title) || strings.Count(out.String(), "\n  ○ ") != len(row.report.Checks) || strings.Count(errOut.String(), row.code) != 1 {
			t.Fatalf("%s: %d %q %q", row.args, exit, out.String(), errOut.String())
		}
	}
}

func TestPreflightHumanOutputStylesStatusIdentityAndStateThroughTheSharedRenderer(t *testing.T) {
	var out bytes.Buffer
	if err := writeStoragePreflightText(WithPresentation(&out, PresentationOptions{Color: "always"}), infraPreflightReport(true)); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"  \x1b[38;5;160m!\x1b[0m Preflight synthetic",
		"\x1b[1;97mcontroller.listener-firewall\x1b[0m",
		"\x1b[38;5;160mfailed\x1b[0m",
		"\x1b[38;5;35mpassed\x1b[0m",
		"\x1b[1;38;5;69mChecks\x1b[0m",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("preflight text lacks %q:\n%q", want, out.String())
		}
	}
	out.Reset()
	if err := writeStoragePreflightText(WithPresentation(&out, PresentationOptions{Color: "never", ASCII: true}), allPreflightReport(true)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "\x1b") || !strings.HasPrefix(out.String(), "  o Preflight synthetic\n") || strings.Contains(out.String(), "·") {
		t.Fatalf("plain ASCII preflight text = %q", out.String())
	}
}
