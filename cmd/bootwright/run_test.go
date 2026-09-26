package main

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/controller"
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
		var out, errOut bytes.Buffer
		route, refused := ambientRoute(cli.ClassifyInvocation(test.args), &out, &errOut)
		if refused != 0 || out.Len() != 0 || errOut.Len() != 0 {
			t.Fatalf("%v: refused %d, stdout %q, stderr %q", test.args, refused, out.String(), errOut.String())
		}
		if got := routeAssignments(route); !slices.Equal(got, test.want) {
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
		var out, errOut bytes.Buffer
		_, refused := ambientRoute(cli.ClassifyInvocation(test.args), &out, &errOut)
		if (refused != 0) != test.refuses {
			t.Errorf("%v: refused %d, want refusal %v", test.args, refused, test.refuses)
		}
		if test.refuses && !strings.Contains(errOut.String(), "controller.unsupported") {
			t.Errorf("%v: refusal reported %q", test.args, errOut.String())
		}
		if !test.refuses && (out.Len() != 0 || errOut.Len() != 0) {
			t.Errorf("%v: stdout %q, stderr %q", test.args, out.String(), errOut.String())
		}
	}
}
