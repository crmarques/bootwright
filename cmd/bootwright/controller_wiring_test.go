package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/controller/runtimelock"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestComposedBaselineDryRunReadsNoSetupState(t *testing.T) {
	for _, flag := range []string{"", "--context="} {
		ports, stores := &setupPorts{}, &controllerPorts{}
		services := assembleServices(serviceDependencies{Controller: ports.setupDependencies(stores, stores)})
		args := []string{"setup", "--dry-run"}
		if flag != "" {
			args = append(args, flag)
		}
		var out, errOut bytes.Buffer
		code := runServices(context.Background(), args, &out, &errOut, services)
		if code != 0 || errOut.Len() != 0 || ports.reads == 0 || ports.effects != 0 || stores.effects != 0 || !strings.Contains(out.String(), "Scope     baseline") ||
			!strings.Contains(out.String(), "Outcome  planned") || !strings.Contains(out.String(), "  podman\n") {
			t.Fatal(code, out.String(), errOut.String(), ports, stores)
		}
	}
}

func TestComposedDownloadOnlyStaysBelowThePrivilegeBoundaryAndReadsNoState(t *testing.T) {
	destination := t.TempDir()
	args := []string{"setup", "--download-only", "--dest-dir", destination, "--wheel-mirror", "file:///srv/wheels"}
	if class := cli.ClassifyInvocation(args); class.RequiresRoot || class.Command != "setup" {
		t.Fatalf("setup --download-only classifies as %+v", class)
	}
	ports, stores := &setupPorts{}, &controllerPorts{}
	services := assembleServices(serviceDependencies{Controller: ports.setupDependencies(stores, stores)})
	var out, errOut bytes.Buffer
	code := runServices(context.Background(), args, &out, &errOut, services)
	if code != 0 || errOut.Len() != 0 || ports.reads != 0 || ports.effects != 0 || stores.effects != 0 || !reflect.DeepEqual(ports.exports, []string{destination}) {
		t.Fatal(code, out.String(), errOut.String(), ports, stores)
	}
	if !strings.Contains(out.String(), "Controller wheel download") || !strings.Contains(out.String(), "Outcome  downloaded") || !strings.Contains(out.String(), "ansible_core-2.21.5-py3-none-any.whl") {
		t.Fatal(out.String())
	}
}

func TestTheDryRunJSONListsEveryLockedFileOfTheEmbeddedLock(t *testing.T) {
	lock, err := runtimelock.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	ports, stores := &setupPorts{}, &controllerPorts{}
	deps := ports.setupDependencies(stores, stores)
	deps.Lock = runtimeLock(lock)
	var out, errOut bytes.Buffer
	if code := runServices(context.Background(), []string{"setup", "--dry-run", "--output", "json"}, &out, &errOut, assembleServices(serviceDependencies{Controller: deps})); code != 0 || errOut.Len() != 0 {
		t.Fatal(code, out.String(), errOut.String())
	}
	var envelope struct {
		Result struct {
			Files []map[string]string `json:"files"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err, out.String())
	}
	want := 1
	for _, abi := range lock.PythonABIs() {
		files, _ := lock.Files(abi)
		want += len(files)
	}
	files := envelope.Result.Files
	if len(files) != want {
		t.Fatalf("the dry run lists %d files, the lock names %d", len(files), want)
	}
	installer := lock.Installer()
	for index, file := range files {
		last := index == len(files)-1
		if (file["abi"] == "") != last || len(file["sha256"]) != 64 || !strings.HasPrefix(file["url"], "https://files.pythonhosted.org/") || file["filename"] == "" {
			t.Fatalf("file %d is %v", index, file)
		}
		if last && (file["filename"] != installer.Filename || file["sha256"] != installer.SHA256 || file["url"] != installer.URL) {
			t.Fatalf("the last file %v is not the installer %+v", file, installer)
		}
	}
	for key := range files[0] {
		if !slices.Contains([]string{"abi", "filename", "url", "sha256"}, key) {
			t.Fatalf("a listed file carries the unexpected field %q", key)
		}
	}
}

// controllerPorts is the store as a dry run must never reach it: every call
// is an effect.
type controllerPorts struct{ effects int }

func (p *controllerPorts) ReadController(context.Context, string, func(prerequisites.StorageView) error) error {
	p.effects++
	return errors.New("unexpected controller read")
}
func (p *controllerPorts) ReadSetup(context.Context) (prerequisites.SetupView, error) {
	p.effects++
	return prerequisites.SetupView{}, errors.New("unexpected setup read")
}
func (p *controllerPorts) MutateSetup(context.Context, func(prerequisites.SetupTransaction) error) error {
	p.effects++
	return errors.New("unexpected setup mutation")
}
func (p *controllerPorts) HoldRuntime(context.Context, string) (prerequisites.RuntimeHold, error) {
	p.effects++
	return nil, errors.New("unexpected runtime hold")
}

func TestComposedControllerSuppliesEveryPort(t *testing.T) {
	presenter := cli.NewControllerPresenter(io.Discard, io.Discard, nil)
	process := processDependencies{Progress: presenter, Presenter: presenter}
	composed, release := localControllerDependencies(testRepository(t.TempDir()), process)
	defer release()
	deps := reflect.ValueOf(composed)
	fields := deps.Type()
	for index := range fields.NumField() {
		if deps.Field(index).IsNil() {
			t.Fatalf("composition left %s unsupplied", fields.Field(index).Name)
		}
	}
	if fields.NumField() != 12 {
		t.Fatalf("controller port count = %d; update this gate with the port it covers", fields.NumField())
	}
	if _, reports := composed.Host.(prerequisites.FIPSInspector); !reports {
		t.Fatal("the composed host inspector reports no FIPS mode")
	}
	if _, releases := composed.Host.(prerequisites.ReleaseInspector); !releases {
		t.Fatal("the composed host inspector reads no OS release")
	}
	if composed.Lock.Digest == "" || len(composed.Lock.Files) == 0 {
		t.Fatal("the composed runtime lock is empty")
	}
	if _, reports := composed.Python.(lifecycle.RuntimePresence); !reports {
		t.Fatal("the composed runtime guard reports no runtime presence")
	}
}

func TestUnsuppliedControllerPortsRemainUnavailable(t *testing.T) {
	services := assembleServices(serviceDependencies{})
	for _, args := range [][]string{{"setup"}, {"setup", "--dry-run"}, {"preflight", "controller"}} {
		var out, errOut bytes.Buffer
		code := runServices(context.Background(), args, &out, &errOut, services)
		if code != 1 || out.Len() != 0 || !strings.HasPrefix(errOut.String(), "  error  cli.not-implemented\n    bootwright ") {
			t.Fatal(args, code, out.String(), errOut.String())
		}
	}
}
