package ansiblelocal

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
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
		}, nil, nil)
		if err == nil || result.Outcome != "failed" {
			t.Fatal("unauthorized path capability accepted")
		}
	}
}

type heldExecution struct{}

func (execution heldExecution) WithPython(_ context.Context, _ prerequisites.BundleArea, _ prerequisites.ExecutionRequirement, run func(prerequisites.PythonLaunch, func() error) error) error {
	return run(prerequisites.PythonLaunch{Loader: "/bundle/python/lib/ld.so", Directory: "/bundle"}, func() error { return nil })
}

type retainedOutput struct{ name string }

func (output *retainedOutput) Write(value []byte) (int, error) { return len(value), nil }

// Setup's own Ansible prints into the run its caller opened, so each way the
// installer starts that Ansible must hand the runner exactly the output it was
// given, and a nil one, when no run could be opened, must stay nil.
func TestEachInstallerOperationHandsItsAnsibleTheOutputItWasGiven(t *testing.T) {
	area := authorityArea{location: prerequisites.BundleLocation{Path: "/bundle", Writable: true}}
	definition := prerequisites.Definition{CatalogDigest: strings.Repeat("a", 64)}
	preparation := prerequisites.NativePreparation{InventorySHA256: strings.Repeat("b", 64), AddedSources: []string{}}
	publish := func(context.Context, prerequisites.NativePreparation) error { return nil }
	operations := map[string]func(Installer, prerequisites.RunOutput) (prerequisites.ActionResult, error){
		"a preparation": func(installer Installer, output prerequisites.RunOutput) (prerequisites.ActionResult, error) {
			return installer.Prepare(context.Background(), area, prerequisites.Platform{}, definition, prerequisites.SetupEgress{}, publish, nil, output)
		},
		"a recovery": func(installer Installer, output prerequisites.RunOutput) (prerequisites.ActionResult, error) {
			return installer.Recover(context.Background(), area, prerequisites.Platform{}, definition, prerequisites.SetupEgress{}, preparation, nil, output)
		},
		"a client installation": func(installer Installer, output prerequisites.RunOutput) (prerequisites.ActionResult, error) {
			return installer.Clients(context.Background(), prerequisites.ClientInstallation{Execution: area, Target: area, Definition: definition, Release: func() error { return nil }, Publish: publish, Output: output})
		},
	}
	operationNames := map[string]string{"a preparation": "setup", "a recovery": "recover", "a client installation": "setup"}
	for name, operate := range operations {
		for kept, output := range map[string]prerequisites.RunOutput{"with a run": &retainedOutput{name: "setup-000001"}, "without a run": nil} {
			t.Run(name+" "+kept, func(t *testing.T) {
				handed := []prerequisites.RunOutput{}
				installer := New(heldExecution{})
				installer.runner = func(_ context.Context, _ prerequisites.PythonLaunch, request capabilityRequest, _ func() error, _ func(context.Context, prerequisites.NativePreparation) error, _ func(prerequisites.ProgressEvent), retain prerequisites.RunOutput) (prerequisites.ActionResult, error) {
					if request.Operation != operationNames[name] {
						t.Fatalf("%s ran operation %q, want %q", name, request.Operation, operationNames[name])
					}
					handed = append(handed, retain)
					return actionResult("changed", true), nil
				}
				result, err := operate(installer, output)
				if err != nil || result.Outcome != "changed" {
					t.Fatalf("%s = %q (%v), want the runner's changed result", name, result.Outcome, err)
				}
				if len(handed) != 1 || handed[0] != output {
					t.Fatalf("%s handed its Ansible %#v, want exactly %#v", name, handed, output)
				}
			})
		}
	}
}

// The installer New returns runs the Ansible process itself, and one built
// without a runner refuses before it starts anything.
func TestTheInstallerRunsItsOwnProcessAndRefusesWithoutARunner(t *testing.T) {
	if reflect.ValueOf(New(heldExecution{}).runner).Pointer() != reflect.ValueOf(run).Pointer() {
		t.Fatal("New does not run the Ansible process itself")
	}
	area := authorityArea{location: prerequisites.BundleLocation{Path: "/bundle", Writable: true}}
	definition := prerequisites.Definition{CatalogDigest: strings.Repeat("a", 64)}
	publish := func(context.Context, prerequisites.NativePreparation) error { return nil }
	installer := Installer{ExecutionGuard: unusedExecution{t}}
	incomplete := func(operation string, result prerequisites.ActionResult, err error) {
		t.Helper()
		reported := diagnostics.Of(err)
		if result.Outcome != "failed" || len(reported) != 1 || reported[0].Message != "the Ansible controller adapter is incomplete" {
			t.Fatalf("%s without a runner = %q %#v, want an incomplete-adapter failure", operation, result.Outcome, reported)
		}
	}
	result, err := installer.Prepare(context.Background(), area, prerequisites.Platform{}, definition, prerequisites.SetupEgress{}, publish, nil, &retainedOutput{})
	incomplete("a preparation", result, err)
	result, err = installer.Clients(context.Background(), prerequisites.ClientInstallation{Execution: area, Target: area, Definition: definition, Release: func() error { return nil }, Publish: publish})
	incomplete("a client installation", result, err)
}
