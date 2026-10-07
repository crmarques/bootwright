package main

import (
	"bytes"
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/privilege"
	"github.com/crmarques/bootwright/internal/managedos/media"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// proxyEnvironment replaces every spelling of the route variables, so the
// host's own proxy settings never reach the classification under test.
func proxyEnvironment(t *testing.T, values map[string]string) {
	t.Helper()
	for _, name := range controller.ProxyEnvironmentNames {
		t.Setenv(name, values[name])
		t.Setenv(strings.ToLower(name), "")
	}
}

var (
	routeFromURL      = []string{"media", "add", "--name", "image.iso", "--from-url", "https://images.example/image.iso", "--sha256", strings.Repeat("a1", 32)}
	routeFromFile     = []string{"media", "add", "--name", "image.iso", "--from-file", "/images/image.iso"}
	routeHostPreview  = []string{"preflight", "controller"}
	routeContextCheck = []string{"preflight", "controller", "--context", "lab"}
)

// A local import or a context-backed preflight takes nothing from the invoking
// environment, so sudo is never asked to forward a route it would not use and
// a sudoers rule without SETENV keeps working for them.
func TestOnlyContextFreeAcquisitionForwardsTheInvokingRoute(t *testing.T) {
	proxyEnvironment(t, map[string]string{"HTTPS_PROXY": "http://proxy.example:3128", "NO_PROXY": "images.internal"})
	for _, test := range []struct {
		args []string
		want []string
	}{
		{routeFromURL, []string{"HTTPS_PROXY=http://proxy.example:3128", "NO_PROXY=images.internal"}},
		{routeHostPreview, []string{"HTTPS_PROXY=http://proxy.example:3128", "NO_PROXY=images.internal"}},
		{routeFromFile, nil},
		{routeContextCheck, nil},
	} {
		route, refusal := privilege.AmbientRoute(cli.ClassifyInvocation(test.args).AmbientRoute, os.LookupEnv)
		if refusal != nil {
			t.Fatalf("%v: refused %+v", test.args, *refusal)
		}
		if got := privilege.RouteAssignments(route); !slices.Equal(got, test.want) {
			t.Errorf("%v: forwarded %q, want %q", test.args, got, test.want)
		}
	}
}

// An unqualified route refuses only the invocations that would acquire over
// it; a command that never reads it cannot fail because of it.
func TestAnUnqualifiedRouteRefusesOnlyContextFreeAcquisition(t *testing.T) {
	proxyEnvironment(t, map[string]string{"HTTP_PROXY": "http://proxy.example:3128"})
	for _, test := range []struct {
		args    []string
		refuses bool
	}{
		{routeFromURL, true},
		{routeHostPreview, true},
		{routeFromFile, false},
		{routeContextCheck, false},
	} {
		_, refusal := privilege.AmbientRoute(cli.ClassifyInvocation(test.args).AmbientRoute, os.LookupEnv)
		if (refusal != nil) != test.refuses {
			t.Errorf("%v: refusal %+v, want refusal %v", test.args, refusal, test.refuses)
		}
		if refusal != nil && refusal.Code != "controller.unsupported" {
			t.Errorf("%v: refusal reported %+v", test.args, *refusal)
		}
	}
}

// A JSON invocation writes exactly one document, so what its services report
// while they work, a power verb's log location included, reaches no stream;
// the same command in text mode names that location as its own field.
func TestAJSONInvocationWritesNoProgress(t *testing.T) {
	ctx := context.Background()
	const location = "/var/lib/bootwright/contexts/lab/state/runs/run-9d2e4f6a8b0c1d3e5f7a9b1c3d5e7f9a"
	start := []string{"machine", "start", "--name", "rhel-01"}
	classification := cli.ClassifyInvocation(append(slices.Clone(start), "--output", "json"))
	if !classification.JSON {
		t.Fatalf("classification = %+v, want JSON", classification)
	}
	var out, errOut bytes.Buffer
	process, hooks := interactiveProcess(classification, nil, &out, &errOut, controller.Route{})
	process.LifecycleProgress.ReportLogLocation(ctx, location)
	process.LifecycleProgress.ReportProgress(ctx, lifecycle.ProgressEvent{
		Block: "machine-power-rhel-01", Description: "start rhel-01", Status: "running", Position: 1, Total: 1,
	})
	hooks.finish()
	if out.Len() != 0 || errOut.Len() != 0 {
		t.Fatalf("a JSON invocation reported progress: stdout %q, stderr %q", out.String(), errOut.String())
	}
	process, hooks = interactiveProcess(cli.ClassifyInvocation(start), start, &out, &errOut, controller.Route{})
	process.LifecycleProgress.ReportLogLocation(ctx, location)
	hooks.finish()
	if want := "\n  Logs  " + location + "\n"; out.String() != want || errOut.Len() != 0 {
		t.Fatalf("a text invocation reported stdout %q, stderr %q, want stdout %q", out.String(), errOut.String(), want)
	}
}

// The media reporter an interactive invocation composes streams through the
// invocation's own progress, so a JSON media list writes its one document and
// no check row, and a text one opens Checks with the image being read.
func TestAJSONMediaListWritesNoProgress(t *testing.T) {
	ctx := context.Background()
	list := []string{"media", "list", "--checksums"}
	verify := media.ProgressEvent{Check: true, Step: "verify", Label: "Verify demo.iso", Status: "running", Position: 1, Total: 1}
	classification := cli.ClassifyInvocation(append(slices.Clone(list), "--output", "json"))
	if !classification.JSON {
		t.Fatalf("classification = %+v, want JSON", classification)
	}
	var out, errOut bytes.Buffer
	process, hooks := interactiveProcess(classification, nil, &out, &errOut, controller.Route{})
	deps := localMediaDependencies(nil, nil, process.LifecycleProgress, process.MediaPresenter, controller.Route{}, nil)
	deps.Progress.ReportProgress(ctx, verify)
	hooks.finish()
	if out.Len() != 0 || errOut.Len() != 0 {
		t.Fatalf("a JSON media list reported progress: stdout %q, stderr %q", out.String(), errOut.String())
	}
	process, hooks = interactiveProcess(cli.ClassifyInvocation(list), list, &out, &errOut, controller.Route{})
	deps = localMediaDependencies(nil, nil, process.LifecycleProgress, process.MediaPresenter, controller.Route{}, nil)
	deps.Progress.ReportProgress(ctx, verify)
	hooks.finish()
	if want := "\nChecks\n  [RUNNING]  [1/1] Verify demo.iso\n"; out.String() != want || errOut.Len() != 0 {
		t.Fatalf("a text media list reported stdout %q, stderr %q, want stdout %q", out.String(), errOut.String(), want)
	}
}
