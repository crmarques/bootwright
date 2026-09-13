package ansiblelocal

import (
	"context"
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
