package ansiblelocal

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/controller"
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
	if request.Version != "controller-prerequisites-v5" || !slices.Equal(request.Acquisition, want) || request.NativeStaging != 0 {
		t.Fatalf("request = %s with acquisition %v and staging %d, want controller-prerequisites-v5 with %v and none", request.Version, request.Acquisition, request.NativeStaging, want)
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

// Each native package carries its own acquisition deadline, in the request's
// package order and before the tools', and the request freezes the bound its
// packages are staged under together, from their declared bytes.
func TestRequestFreezesNativeAcquisition(t *testing.T) {
	platform := prerequisites.Platform{OS: "fedora", Release: "43", Architecture: "amd64"}
	repository := "https://packages.example.test/fedora"
	plan := prerequisites.NativeResolvedPlan{
		Format: "bootwright.native-plan-v1", Platform: platform, Solver: "dnf5", SolverVersion: "5.2.0",
		Requests: controller.DefaultDependencyVersions(), Requirements: prerequisites.NativeRequirements{ContainerRuntime: true}, Roots: []prerequisites.NativeRoot{}, Packages: []prerequisites.NativePackage{},
		Repositories: []prerequisites.NativeRepository{{ID: "base", BaseURL: repository, MetadataSHA256: strings.Repeat("d", 64)}},
		Actions:      []prerequisites.NativeAction{}, BeforeSHA256: strings.Repeat("e", 64), AfterSHA256: strings.Repeat("e", 64),
	}
	for _, key := range []string{"nmstate", "openssh", "podman"} {
		for _, name := range prerequisites.NativeRootNames()[key] {
			identity := prerequisites.NativeIdentity{Name: name, Version: "1.2.3", Release: "1.fc43", Architecture: "x86_64"}
			bytes := int64(1)
			if len(plan.Packages) == 0 {
				bytes = 100 << 20
			}
			plan.Roots = append(plan.Roots, prerequisites.NativeRoot{Key: key, Requested: "latest", Package: identity})
			plan.Packages = append(plan.Packages, prerequisites.NativePackage{Name: name, Version: identity.Version, Release: identity.Release, Architecture: identity.Architecture, Signer: strings.Repeat("f", 40),
				Source: prerequisites.DependencySource{ID: "native-" + name, URL: repository + "/" + name + ".rpm", SHA256: strings.Repeat("a", 64), Bytes: bytes}})
		}
	}
	plan, err := prerequisites.CanonicalNativePlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	area := authorityArea{location: prerequisites.BundleLocation{Path: "/bundle", Writable: true}}
	definition := prerequisites.Definition{CatalogDigest: strings.Repeat("a", 64), Native: &plan, NativeRequirements: plan.Requirements,
		Tools: []prerequisites.ToolDefinition{{Kind: "kubectl", Source: prerequisites.DependencySource{ID: "tool-kubectl", Bytes: 1}}}}
	request, err := New(unusedExecution{t}).request(context.Background(), area, nil, "setup", platform, definition, prerequisites.SetupEgress{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want, total := []toolAcquisition{}, int64(0)
	for _, item := range request.Packages {
		seconds := int64(121)
		if item.Source.Bytes == 100<<20 {
			seconds = 320
		}
		want, total = append(want, toolAcquisition{Source: item.Source.ID, Seconds: seconds}), total+item.Source.Bytes
	}
	want = append(want, toolAcquisition{Source: "tool-kubectl", Seconds: 121})
	if len(request.Packages) < 2 || !slices.Equal(request.Acquisition, want) {
		t.Fatalf("acquisition = %v, want %v", request.Acquisition, want)
	}
	if staging := int64(acquisitionDeadline(total).Seconds()); request.NativeStaging != staging || staging != 321 {
		t.Fatalf("native staging = %d, want %d seconds for %d bytes", request.NativeStaging, staging, total)
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

// retainedOutput keeps what reaches it; a broken one refuses every write, as a
// run on a full device does.
type retainedOutput struct {
	name   string
	kept   []byte
	broken bool
}

func (output *retainedOutput) Write(value []byte) (int, error) {
	if output.broken {
		return 0, errors.New("no space left on the device")
	}
	output.kept = append(output.kept, value...)
	return len(value), nil
}

// locatedOutput is a setup run, which names its own directory.
type locatedOutput struct{ retainedOutput }

func (output *locatedOutput) Location() string {
	return "/var/lib/bootwright/controller/runs/" + output.name
}

// Setup's own Ansible prints into the run its caller opened, so each way the
// installer starts that Ansible must hand the runner a writer into exactly
// the output it was given, and a nil one, when no run could be opened, must
// stay nil.
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
				if len(handed) != 1 || (handed[0] == nil) != (output == nil) {
					t.Fatalf("%s handed its Ansible %#v, want a writer into %#v", name, handed, output)
				}
				if output != nil {
					if written, err := handed[0].Write([]byte("printed\n")); err != nil || written != 8 || string(output.(*retainedOutput).kept) != "printed\n" {
						t.Fatalf("%s handed its Ansible a writer that did not reach its output: %d %v", name, written, err)
					}
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

// The runner copies the Ansible's output through the writer it is handed, and
// os/exec stops copying from one that fails, which could leave the Ansible
// blocked on a full pipe. So the writer it gets accepts every write whatever
// the output beneath it does.
func TestTheRunnerGetsAWriterThatNeverFails(t *testing.T) {
	area := authorityArea{location: prerequisites.BundleLocation{Path: "/bundle", Writable: true}}
	definition := prerequisites.Definition{CatalogDigest: strings.Repeat("a", 64)}
	publish := func(context.Context, prerequisites.NativePreparation) error { return nil }
	for name, operate := range map[string]func(Installer, prerequisites.RunOutput) (prerequisites.ActionResult, error){
		"setup": func(installer Installer, output prerequisites.RunOutput) (prerequisites.ActionResult, error) {
			return installer.Prepare(context.Background(), area, prerequisites.Platform{}, definition, prerequisites.SetupEgress{}, publish, nil, output)
		},
		"a client installation": func(installer Installer, output prerequisites.RunOutput) (prerequisites.ActionResult, error) {
			return installer.Clients(context.Background(), prerequisites.ClientInstallation{Execution: area, Target: area, Definition: definition, Release: func() error { return nil }, Publish: publish, Output: output})
		},
	} {
		broken := &retainedOutput{name: "setup-000001", broken: true}
		installer := New(heldExecution{})
		installer.runner = func(_ context.Context, _ prerequisites.PythonLaunch, _ capabilityRequest, _ func() error, _ func(context.Context, prerequisites.NativePreparation) error, _ func(prerequisites.ProgressEvent), retain prerequisites.RunOutput) (prerequisites.ActionResult, error) {
			for _, line := range []string{"first\n", "second\n"} {
				if written, err := retain.Write([]byte(line)); err != nil || written != len(line) {
					t.Fatalf("%s: a write to a broken output returned %d (%v)", name, written, err)
				}
			}
			return actionResult("changed", true), nil
		}
		if result, err := operate(installer, broken); err != nil || result.Outcome != "changed" {
			t.Fatalf("%s: a broken output changed the result: %q (%v)", name, result.Outcome, err)
		}
	}
}

// A failure of setup's own Ansible that its run's output explains leads its
// remedy with where that output is and that only root reads it, before the
// correction it already gave; one whose output no run kept is unchanged.
func TestASetupAnsibleFailureNamesItsRunOutput(t *testing.T) {
	area := authorityArea{location: prerequisites.BundleLocation{Path: "/bundle", Writable: true}}
	definition := prerequisites.Definition{CatalogDigest: strings.Repeat("a", 64)}
	publish := func(context.Context, prerequisites.NativePreparation) error { return nil }
	unchanged := "Preserve the controller state and restore the qualified controller execution environment, then rerun bootwright setup."
	for kept, test := range map[string]struct {
		output prerequisites.RunOutput
		want   string
	}{
		"a run":                       {&locatedOutput{retainedOutput{name: "setup-000003"}}, "Read /var/lib/bootwright/controller/runs/setup-000003/run.output as root for what the Ansible printed, then preserve the controller state and restore the qualified controller execution environment, then rerun bootwright setup."},
		"no run":                      {nil, unchanged},
		"an output that names no run": {&retainedOutput{name: "setup-000004"}, unchanged},
	} {
		installer := New(heldExecution{})
		installer.runner = func(context.Context, prerequisites.PythonLaunch, capabilityRequest, func() error, func(context.Context, prerequisites.NativePreparation) error, func(prerequisites.ProgressEvent), prerequisites.RunOutput) (prerequisites.ActionResult, error) {
			// The runner's own refusal of an Ansible that did not complete
			// (runner_linux_amd64.go).
			return actionResult("failed", false), failure("controller.setup", "Ansible did not complete the authorized dependency operation")
		}
		_, err := installer.Prepare(context.Background(), area, prerequisites.Platform{}, definition, prerequisites.SetupEgress{}, publish, nil, test.output)
		reported := diagnostics.Of(err)
		if len(reported) != 1 || reported[0].Message != "Ansible did not complete the authorized dependency operation" || reported[0].Remediation != test.want {
			t.Fatalf("with %s: refusal = %#v, want the remedy %q", kept, reported, test.want)
		}
	}
}
