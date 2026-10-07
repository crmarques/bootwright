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
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestComposedBaselineDryRunUsesOnlyPlatformAndCatalog(t *testing.T) {
	for _, flag := range []string{"", "--context="} {
		ports := &controllerPorts{}
		services := assembleServices(serviceDependencies{Controller: controllerDependencies{
			Storage: ports, Host: ports, Catalog: ports, Bundle: ports,
			Bootstrap: bootstrapPort{ports}, Native: nativePort{ports}, NativeInspector: nativePort{ports},
			Foundation: foundationPort{ports},
		}})
		args := []string{"setup", "--dry-run"}
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
func (p *controllerPorts) Admit(prerequisites.Platform) error             { p.catalog++; return nil }
func (p *controllerPorts) ValidateEgress(prerequisites.SetupEgress) error { p.egress++; return nil }
func (p *controllerPorts) Identity(context.Context) (controller.InstalledHostIdentity, error) {
	p.effects++
	return controller.InstalledHostIdentity{}, errors.New("unexpected identity inspection")
}

// bootstrapPort and nativePort are the resolution ports a dry run never
// reaches: each call is an effect.
type bootstrapPort struct{ ports *controllerPorts }

func (b bootstrapPort) Resolve(context.Context, prerequisites.Platform, controller.DependencyVersions, prerequisites.SetupEgress) (prerequisites.BootstrapDefinition, []diagnostics.Diagnostic, error) {
	b.ports.effects++
	return prerequisites.BootstrapDefinition{}, nil, errors.New("unexpected bootstrap resolution")
}

type nativePort struct{ ports *controllerPorts }

// foundationPort is the execution foundation inspector a dry run never
// reaches: its call is an effect.
type foundationPort struct{ ports *controllerPorts }

func (f foundationPort) Inspect(context.Context, prerequisites.Platform) (prerequisites.FoundationInspection, error) {
	f.ports.effects++
	return prerequisites.FoundationInspection{}, errors.New("unexpected foundation inspection")
}

func (n nativePort) Resolve(context.Context, prerequisites.Platform, prerequisites.NativeRequirements, controller.DependencyVersions, prerequisites.SetupEgress) (prerequisites.NativeResolvedPlan, error) {
	n.ports.effects++
	return prerequisites.NativeResolvedPlan{}, errors.New("unexpected native resolution")
}

func (n nativePort) Check(context.Context, prerequisites.NativeResolvedPlan) (prerequisites.NativePresence, error) {
	n.ports.effects++
	return prerequisites.NativePresence{}, errors.New("unexpected native inspection")
}

func (n nativePort) OperatorRoots(context.Context, prerequisites.Platform, []string) (prerequisites.OperatorPresence, error) {
	n.ports.effects++
	return prerequisites.OperatorPresence{}, errors.New("unexpected operator inspection")
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
func (p *controllerPorts) Validate(prerequisites.Definition) error {
	p.effects++
	return errors.New("unexpected bundle validation")
}
func (p *controllerPorts) Rebase(context.Context, prerequisites.BundleArea, prerequisites.BootstrapDefinition) (prerequisites.BootstrapDefinition, error) {
	p.effects++
	return prerequisites.BootstrapDefinition{}, errors.New("unexpected bundle rebase")
}

func (p *controllerPorts) Prepare(context.Context, prerequisites.BundleArea, prerequisites.BundleArea, prerequisites.Definition, prerequisites.SetupEgress, func(prerequisites.ProgressEvent)) (prerequisites.BundleInspection, error) {
	p.effects++
	return prerequisites.BundleInspection{}, errors.New("unexpected bundle preparation")
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
	if fields.NumField() != 13 {
		t.Fatalf("controller port count = %d; update this gate with the port it covers", fields.NumField())
	}
	if _, reports := composed.Host.(prerequisites.FIPSInspector); !reports {
		t.Fatal("the composed host inspector reports no FIPS mode")
	}
	if _, reads := composed.NativeInspector.(prerequisites.NativeInventory); !reads {
		t.Fatal("the composed native inspector reads no package inventory")
	}
}

func TestUnsuppliedControllerPortsRemainUnavailable(t *testing.T) {
	services := assembleServices(serviceDependencies{})
	for _, args := range [][]string{{"setup"}, {"setup", "--dry-run"}, {"preflight", "controller"}} {
		var out, errOut bytes.Buffer
		code := runServices(context.Background(), args, &out, &errOut, services)
		if code != 1 || out.Len() != 0 || !strings.HasPrefix(errOut.String(), "[FAIL] cli.not-implemented: bootwright ") {
			t.Fatal(args, code, out.String(), errOut.String())
		}
	}
}
