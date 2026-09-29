package ansiblelocal

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

type authorityArea struct {
	prerequisites.BundleArea
	location prerequisites.BundleLocation
}

func (area authorityArea) Read(ctx context.Context, path string, maximum int) ([]byte, error) {
	return ansible.Assets()[strings.TrimPrefix(path, "automation/")], nil
}
func (area authorityArea) Location(context.Context) (prerequisites.BundleLocation, error) {
	return area.location, nil
}

type unusedExecution struct{ t *testing.T }

func (execution unusedExecution) WithPython(context.Context, prerequisites.BundleArea, prerequisites.ExecutionRequirement, func(prerequisites.PythonLaunch, func() error) error) error {
	execution.t.Fatal("Ansible executed without write authority")
	return nil
}

func TestTheRequestCarriesEachToolsAcquisitionDeadline(t *testing.T) {
	area := authorityArea{location: prerequisites.BundleLocation{Path: "/bundle", Writable: true}}
	definition := prerequisites.Definition{CatalogDigest: strings.Repeat("a", 64), Tools: []prerequisites.ToolDefinition{
		{Kind: "openshift-clients", Source: prerequisites.DependencySource{ID: "tool-openshift-clients", Bytes: 44_433_552}},
		{Kind: "kubectl", Source: prerequisites.DependencySource{ID: "tool-kubectl", Bytes: 1}},
	}}
	request, err := New(unusedExecution{t}).request(context.Background(), area, nil, "setup", prerequisites.Platform{}, definition, prerequisites.SetupEgress{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []toolAcquisition{{Source: "tool-openshift-clients", Seconds: 205}, {Source: "tool-kubectl", Seconds: 121}}
	if request.Version != "controller-prerequisites-v4" || !slices.Equal(request.Acquisition, want) {
		t.Fatalf("request = %s with acquisition %v, want controller-prerequisites-v4 with %v", request.Version, request.Acquisition, want)
	}
	definition.Tools = nil
	request, err = New(unusedExecution{t}).request(context.Background(), area, nil, "setup", prerequisites.Platform{}, definition, prerequisites.SetupEgress{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(request)
	if err != nil || !strings.Contains(string(encoded), `"acquisition":[]`) {
		t.Fatalf("a request without tools encodes %s (%v), want \"acquisition\":[]", encoded, err)
	}
}

func TestInstallerRefusesReadOnlyOrSealedPathCapabilities(t *testing.T) {
	for _, location := range []prerequisites.BundleLocation{{Writable: false}, {Writable: true, Sealed: true}} {
		installer := New(unusedExecution{t})
		result, err := installer.Prepare(context.Background(), authorityArea{location: location}, prerequisites.Platform{}, prerequisites.Definition{}, prerequisites.SetupEgress{}, func(context.Context, prerequisites.NativePreparation) error {
			t.Fatal("unauthorized preparation published")
			return nil
		}, nil)
		if err == nil || result.Outcome != "failed" {
			t.Fatal("unauthorized path capability accepted")
		}
	}
}
