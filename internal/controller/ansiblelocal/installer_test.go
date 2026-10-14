package ansiblelocal

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

type authorityArea struct {
	prerequisites.BundleArea
	location prerequisites.BundleLocation
}

func (area authorityArea) Location(context.Context) (prerequisites.BundleLocation, error) {
	return area.location, nil
}

// testLaunch is the controller runtime's launch as the guard hands it over.
var testLaunch = prerequisites.PythonLaunch{Program: "/runtime/bin/python", Shim: "/runtime/bootwright-python", Arguments: []string{"-I", "-B"}, Directory: "/runtime"}

// treeStager stages an automation tree as the stager does: a private
// directory whose open descriptor is the tree's lock, released once. It
// writes no files, because the test runners read none, and reports this
// executable's automation digest unless a test names another.
type treeStager struct {
	parent string
	digest string
	plain  bool

	mutex    sync.Mutex
	staged   []string
	released int
}

func newTreeStager(t *testing.T) *treeStager { return &treeStager{parent: t.TempDir()} }

func (s *treeStager) Stage(context.Context) (prerequisites.StagedAutomation, error) {
	tree, err := os.MkdirTemp(s.parent, "automation-")
	if err != nil {
		return prerequisites.StagedAutomation{}, err
	}
	lock, err := os.Open(tree)
	if err != nil {
		return prerequisites.StagedAutomation{}, err
	}
	digest := s.digest
	if digest == "" {
		digest = ansible.Digest()
	}
	s.mutex.Lock()
	s.staged = append(s.staged, tree)
	s.mutex.Unlock()
	var once sync.Once
	release := func() {
		once.Do(func() {
			lock.Close()
			s.mutex.Lock()
			s.released++
			s.mutex.Unlock()
		})
	}
	var held interface{ Fd() uintptr } = lock
	if s.plain {
		held = plainLock{lock.Fd()}
	}
	return prerequisites.StagedAutomation{Path: tree, Digest: digest, Lock: held, Release: release}, nil
}

func (s *treeStager) counts() (int, int) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return len(s.staged), s.released
}

// plainLock is a lock descriptor that is not an open file a run can pass to
// its Ansible.
type plainLock struct{ fd uintptr }

func (l plainLock) Fd() uintptr { return l.fd }

func targetArea() authorityArea {
	return authorityArea{location: prerequisites.BundleLocation{Path: "/var/lib/bootwright/controller/tools/area", Device: 1, Inode: 2, Writable: true}}
}

func clientInstallation(output prerequisites.RunOutput) prerequisites.ClientInstallation {
	return prerequisites.ClientInstallation{
		Target: targetArea(), Identity: strings.Repeat("a", 64), Launch: testLaunch,
		Tools:   []prerequisites.ToolDefinition{{Kind: "kubectl", Source: prerequisites.DependencySource{ID: "tool-kubectl", Bytes: 1}}},
		Publish: func(context.Context, []string) error { return nil }, Output: output,
	}
}

func TestTheRequestCarriesEachToolsAcquisitionDeadline(t *testing.T) {
	tools := []prerequisites.ToolDefinition{
		{Kind: "openshift-clients", Source: prerequisites.DependencySource{ID: "tool-openshift-clients", Bytes: 44_433_552}},
		{Kind: "kubectl", Source: prerequisites.DependencySource{ID: "tool-kubectl", Bytes: 1}},
	}
	request, err := New(newTreeStager(t)).request(context.Background(), targetArea(), strings.Repeat("a", 64), tools, prerequisites.SetupEgress{})
	if err != nil {
		t.Fatal(err)
	}
	want := []toolAcquisition{{Source: "tool-openshift-clients", Seconds: 205}, {Source: "tool-kubectl", Seconds: 121}}
	if request.Version != "controller-prerequisites-v6" || !slices.Equal(request.Acquisition, want) {
		t.Fatalf("request = %s with acquisition %v, want controller-prerequisites-v6 with %v", request.Version, request.Acquisition, want)
	}
	encoded, err := json.Marshal(request)
	if err != nil || !strings.Contains(string(encoded), `"noProxy":[]`) || strings.Contains(string(encoded), `"packages"`) || strings.Contains(string(encoded), `"native"`) {
		t.Fatalf("the request encodes %s (%v), want tools alone and an empty bypass list", encoded, err)
	}
	if _, err := New(newTreeStager(t)).request(context.Background(), targetArea(), "not-a-digest", tools, prerequisites.SetupEgress{}); err == nil {
		t.Fatal("a request without a request digest identity was frozen")
	}
}

// An installation with no target tool has nothing for the role to do, so it
// refuses before any automation is staged.
func TestAnInstallationWithoutToolsIsRefused(t *testing.T) {
	stager := newTreeStager(t)
	installation := clientInstallation(nil)
	installation.Tools = nil
	if result, err := New(stager).Clients(context.Background(), installation); err == nil || result.Outcome != "failed" {
		t.Fatalf("an installation without tools = %q (%v)", result.Outcome, err)
	}
	if staged, _ := stager.counts(); staged != 0 {
		t.Fatal("an installation without tools staged automation")
	}
}

// A target area the stage may not write refuses before any automation is
// staged or Ansible started.
func TestInstallerRefusesReadOnlyOrSealedPathCapabilities(t *testing.T) {
	for _, location := range []prerequisites.BundleLocation{{Writable: false}, {Writable: true, Sealed: true}} {
		stager := newTreeStager(t)
		installer := New(stager)
		installer.runner = func(context.Context, prerequisites.PythonLaunch, stagedTree, capabilityRequest, func(context.Context, []string) error, func(prerequisites.ProgressEvent), prerequisites.RunOutput) (prerequisites.ActionResult, error) {
			t.Fatal("Ansible ran without write authority")
			return prerequisites.ActionResult{}, nil
		}
		installation := clientInstallation(nil)
		installation.Target = authorityArea{location: location}
		result, err := installer.Clients(context.Background(), installation)
		if staged, _ := stager.counts(); err == nil || result.Outcome != "failed" || staged != 0 {
			t.Fatalf("an unauthorized target = %q (%v) after staging %d trees", result.Outcome, err, staged)
		}
	}
}

// A client installation runs its Ansible from an extraction of its own: the
// runner is handed the staged tree and the open file that locks it, the
// request names the target area as its bundle, and the extraction is
// released once the run ends.
func TestAClientInstallationRunsFromItsOwnStagedAutomation(t *testing.T) {
	stager := newTreeStager(t)
	installer := New(stager)
	var tree stagedTree
	var request capabilityRequest
	installer.runner = func(_ context.Context, launch prerequisites.PythonLaunch, given stagedTree, frozen capabilityRequest, _ func(context.Context, []string) error, _ func(prerequisites.ProgressEvent), _ prerequisites.RunOutput) (prerequisites.ActionResult, error) {
		if !reflect.DeepEqual(launch, testLaunch) {
			t.Fatalf("the runner was handed launch %+v", launch)
		}
		if _, released := stager.counts(); released != 0 {
			t.Fatal("the extraction was released while its Ansible ran")
		}
		tree, request = given, frozen
		return actionResult("changed", true), nil
	}
	if result, err := installer.Clients(context.Background(), clientInstallation(nil)); err != nil || result.Outcome != "changed" {
		t.Fatalf("client installation = %q (%v)", result.Outcome, err)
	}
	staged, released := stager.counts()
	if staged != 1 || released != 1 || tree.Path != stager.staged[0] || tree.Lock == nil || tree.Lock.Name() != stager.staged[0] {
		t.Fatalf("ran from %+v after staging %v and releasing %d", tree, stager.staged, released)
	}
	target := location(targetArea().location)
	if request.PublicationBundle != target {
		t.Fatalf("the request names publication %+v, want the target %+v", request.PublicationBundle, target)
	}
}

// An extraction whose reported digest is not this executable's automation
// digest, or whose lock cannot be handed to Ansible, is refused before
// Ansible starts and released at once.
func TestAStagedTreeTheInstallerCannotRunIsRefused(t *testing.T) {
	for name, stager := range map[string]*treeStager{
		"another digest":     {digest: strings.Repeat("0", 64)},
		"an unpassable lock": {plain: true},
	} {
		t.Run(name, func(t *testing.T) {
			stager.parent = t.TempDir()
			installer := New(stager)
			installer.runner = func(context.Context, prerequisites.PythonLaunch, stagedTree, capabilityRequest, func(context.Context, []string) error, func(prerequisites.ProgressEvent), prerequisites.RunOutput) (prerequisites.ActionResult, error) {
				t.Fatal("Ansible ran from a tree this executable cannot run")
				return prerequisites.ActionResult{}, nil
			}
			result, err := installer.Clients(context.Background(), clientInstallation(nil))
			reported := diagnostics.Of(err)
			if staged, released := stager.counts(); result.Outcome != "failed" || len(reported) != 1 || reported[0].Code != "controller.identity" || staged != 1 || released != 1 {
				t.Fatalf("installation = %q %+v after staging %d and releasing %d", result.Outcome, reported, staged, released)
			}
		})
	}
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

// A client installation's Ansible prints into the output its block's attempt
// opened, so the installer hands the runner a writer into exactly that
// output, and a nil one, when nothing is retained, stays nil.
func TestAClientInstallationHandsItsAnsibleTheOutputItWasGiven(t *testing.T) {
	for kept, output := range map[string]prerequisites.RunOutput{"with an output": &retainedOutput{name: "attempt"}, "without one": nil} {
		t.Run(kept, func(t *testing.T) {
			handed := []prerequisites.RunOutput{}
			installer := New(newTreeStager(t))
			installer.runner = func(_ context.Context, _ prerequisites.PythonLaunch, _ stagedTree, request capabilityRequest, _ func(context.Context, []string) error, _ func(prerequisites.ProgressEvent), retain prerequisites.RunOutput) (prerequisites.ActionResult, error) {
				if request.Version != requestVersion {
					t.Fatalf("the installation ran request %q", request.Version)
				}
				handed = append(handed, retain)
				return actionResult("changed", true), nil
			}
			result, err := installer.Clients(context.Background(), clientInstallation(output))
			if err != nil || result.Outcome != "changed" {
				t.Fatalf("installation = %q (%v), want the runner's changed result", result.Outcome, err)
			}
			if len(handed) != 1 || (handed[0] == nil) != (output == nil) {
				t.Fatalf("the installation handed its Ansible %#v, want a writer into %#v", handed, output)
			}
			if output != nil {
				if written, err := handed[0].Write([]byte("printed\n")); err != nil || written != 8 || string(output.(*retainedOutput).kept) != "printed\n" {
					t.Fatalf("the installation handed its Ansible a writer that did not reach its output: %d %v", written, err)
				}
			}
		})
	}
}

// The installer New returns runs the Ansible process itself, and one built
// without a runner or a stager refuses before it starts anything.
func TestTheInstallerRunsItsOwnProcessAndRefusesWithoutARunner(t *testing.T) {
	if reflect.ValueOf(New(newTreeStager(t)).runner).Pointer() != reflect.ValueOf(run).Pointer() {
		t.Fatal("New does not run the Ansible process itself")
	}
	for name, installer := range map[string]Installer{
		"no runner": {Stager: newTreeStager(t)},
		"no stager": {runner: run},
	} {
		result, err := installer.Clients(context.Background(), clientInstallation(&retainedOutput{}))
		reported := diagnostics.Of(err)
		if result.Outcome != "failed" || len(reported) != 1 || reported[0].Message != "the Ansible controller adapter is incomplete" {
			t.Fatalf("%s = %q %#v, want an incomplete-adapter failure", name, result.Outcome, reported)
		}
	}
}

// The runner copies the Ansible's output through the writer it is handed, and
// os/exec stops copying from one that fails, which could leave the Ansible
// blocked on a full pipe. So the writer it gets accepts every write whatever
// the output beneath it does.
func TestTheRunnerGetsAWriterThatNeverFails(t *testing.T) {
	broken := &retainedOutput{name: "attempt", broken: true}
	installer := New(newTreeStager(t))
	installer.runner = func(_ context.Context, _ prerequisites.PythonLaunch, _ stagedTree, _ capabilityRequest, _ func(context.Context, []string) error, _ func(prerequisites.ProgressEvent), retain prerequisites.RunOutput) (prerequisites.ActionResult, error) {
		for _, line := range []string{"first\n", "second\n"} {
			if written, err := retain.Write([]byte(line)); err != nil || written != len(line) {
				t.Fatalf("a write to a broken output returned %d (%v)", written, err)
			}
		}
		return actionResult("changed", true), nil
	}
	if result, err := installer.Clients(context.Background(), clientInstallation(broken)); err != nil || result.Outcome != "changed" {
		t.Fatalf("a broken output changed the result: %q (%v)", result.Outcome, err)
	}
}
