package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

func TestComposedBaselineDryRunUsesOnlyPlatformAndCatalog(t *testing.T) {
	for _, flag := range []string{"", "--context="} {
		ports := &controllerPorts{}
		services := assembleServices(serviceDependencies{Controller: controllerDependencies{Storage: ports, Host: ports, Catalog: ports, Bundle: ports}})
		args := []string{"bastion", "setup", "--dry-run"}
		if flag != "" {
			args = append(args, flag)
		}
		var out, errOut bytes.Buffer
		code := runServices(context.Background(), args, &out, &errOut, services)
		if code != 0 || errOut.Len() != 0 || ports.platform != 1 || ports.catalog != 1 || ports.egress != 1 || ports.effects != 0 || !strings.Contains(out.String(), "Scope     baseline") || !strings.Contains(out.String(), "Outcome  planned") {
			t.Fatal(code, out.String(), errOut.String(), ports)
		}
	}
}

type controllerPorts struct{ platform, catalog, egress, effects int }

func (p *controllerPorts) Platform(context.Context) (prerequisites.Platform, error) {
	p.platform++
	return prerequisites.Platform{OS: "fedora", Release: "43", Architecture: "amd64"}, nil
}
func (p *controllerPorts) Select(prerequisites.Platform, prerequisites.NativeRequirements) (prerequisites.Definition, error) {
	p.catalog++
	return prerequisites.Definition{CatalogDigest: strings.Repeat("a", 64), PythonVersion: "3.13.15", AnsibleVersion: "2.21.4", Runtime: prerequisites.RuntimeRequirement{Version: "5.8.4"}}, nil
}
func (p *controllerPorts) ValidateEgress(prerequisites.SetupEgress) error { p.egress++; return nil }
func (p *controllerPorts) Identity(context.Context) (controller.InstalledHostIdentity, error) {
	p.effects++
	return controller.InstalledHostIdentity{}, errors.New("unexpected identity inspection")
}
func (p *controllerPorts) Runtime(context.Context, prerequisites.RuntimeRequirement) (prerequisites.RuntimeInspection, error) {
	p.effects++
	return prerequisites.RuntimeInspection{}, errors.New("unexpected runtime inspection")
}
func (p *controllerPorts) ReadController(context.Context, string, func(prerequisites.StorageView) error) error {
	p.effects++
	return errors.New("unexpected controller read")
}
func (p *controllerPorts) MutateController(context.Context, prerequisites.SetupContext, bool, func(prerequisites.StorageTransaction) error) error {
	p.effects++
	return errors.New("unexpected controller mutation")
}
func (p *controllerPorts) Inspect(context.Context, prerequisites.BundleArea, prerequisites.Definition, bool) (prerequisites.BundleInspection, error) {
	p.effects++
	return prerequisites.BundleInspection{}, errors.New("unexpected bundle inspection")
}
func (p *controllerPorts) Prepare(context.Context, prerequisites.BundleArea, prerequisites.Definition, prerequisites.SetupEgress, func(prerequisites.ProgressEvent)) error {
	p.effects++
	return errors.New("unexpected bundle preparation")
}

func TestComposedControllerSuppliesEveryPort(t *testing.T) {
	process := processDependencies{Progress: cli.NewControllerProgressPresenter(io.Discard), Presenter: cli.NewControllerPlanPresenter(io.Discard)}
	deps := reflect.ValueOf(localControllerDependencies(testRepository(t.TempDir()), process))
	fields := deps.Type()
	for index := range fields.NumField() {
		if deps.Field(index).IsNil() {
			t.Fatalf("composition left %s unsupplied", fields.Field(index).Name)
		}
	}
	if fields.NumField() != 11 {
		t.Fatalf("controller port count = %d; update this gate with the port it covers", fields.NumField())
	}
}

func TestUnsuppliedControllerPortsRemainUnavailable(t *testing.T) {
	services := assembleServices(serviceDependencies{})
	for _, args := range [][]string{{"bastion", "setup"}, {"bastion", "setup", "--dry-run"}, {"preflight", "bastion"}} {
		var out, errOut bytes.Buffer
		code := runServices(context.Background(), args, &out, &errOut, services)
		if code != 1 || out.Len() != 0 || !strings.HasPrefix(errOut.String(), "[FAIL] cli.not-implemented: bootwright ") {
			t.Fatal(args, code, out.String(), errOut.String())
		}
	}
}
