package bundlelocal

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestCatalogRejectsExecutionFoundationSubstitution(t *testing.T) {
	for _, change := range []func(*prerequisites.ExecutionRequirement){
		func(value *prerequisites.ExecutionRequirement) { value.Loader = "/unapproved/loader" },
		func(value *prerequisites.ExecutionRequirement) { value.LockPath = "/unapproved/lock" },
		func(value *prerequisites.ExecutionRequirement) { value.Files[0].SHA256 = strings.Repeat("a", 64) },
		func(value *prerequisites.ExecutionRequirement) { value.Links[0].Target = "unapproved" },
		func(value *prerequisites.ExecutionRequirement) { value.Preload[0] = "/unapproved/library" },
	} {
		definition := resolvedDefinitionFixture(t)
		bootstrap := *definition.Bootstrap
		change(&bootstrap.Execution)
		if canonical, err := prerequisites.CanonicalBootstrap(bootstrap); err == nil {
			if substituted, err := prerequisites.NewResolvedDefinition(canonical, *definition.Native); err == nil {
				if _, err := validateDefinition(substituted); !errors.Is(err, prerequisites.ErrBootstrapIncompatible) {
					t.Fatalf("accepted a substituted execution foundation: %v", err)
				}
			}
		}
		if _, err := validateDefinition(resolvedDefinitionFixture(t)); err != nil {
			t.Fatal("caller changed the compiled execution profile", err)
		}
	}
}

func TestDryInspectionNeverAcquiresOrExecutes(t *testing.T) {
	definition := resolvedDefinitionFixture(t)
	m := &Manager{
		fetch: func(context.Context, prerequisites.DependencySource, prerequisites.SetupEgress) ([]byte, error) {
			t.Fatal("inspection acquired a dependency")
			return nil, nil
		},
		probe: func(context.Context, prerequisites.BundleArea, prerequisites.Definition) error {
			t.Fatal("dry inspection executed a process")
			return nil
		},
	}
	area := newMemoryArea()
	area.sealed = true
	inspection, err := m.Inspect(t.Context(), area, definition, false)
	if err != nil || inspection.Ready || !inspection.Recoverable || !inspection.Sealed || area.writes != 0 {
		t.Fatalf("dry inspection: %+v %v writes=%d", inspection, err, area.writes)
	}
	definition.CatalogDigest = strings.Repeat("0", 64)
	inspection, err = m.Inspect(t.Context(), area, definition, false)
	if err == nil || !inspection.Sealed {
		t.Fatal("invalid definition discarded sealed authority evidence", inspection, err)
	}
}

func TestCatalogRejectsUnsupportedEgressBeforeEffects(t *testing.T) {
	for _, egress := range []prerequisites.SetupEgress{{}, {HTTPSProxy: "http://proxy.example:3128", NoProxy: []string{".example.test", "192.0.2.0/24"}}} {
		if err := (Catalog{}).ValidateEgress(egress); err != nil {
			t.Fatal(err)
		}
	}
	for _, egress := range []prerequisites.SetupEgress{{HTTPProxy: "http://proxy.example:3128"}, {HTTPSProxy: "http://credentials:secret@proxy.example:3128"}, {HTTPSProxy: "http://proxy.example:3128", NoProxy: []string{"https://invalid.example"}}} {
		if err := (Catalog{}).ValidateEgress(egress); err == nil {
			t.Fatal("admitted egress that cannot acquire qualified HTTPS dependencies")
		}
	}
}

func TestPreparationRejectsUnapprovedBytesBeforeFilePublication(t *testing.T) {
	definition := resolvedDefinitionFixture(t)
	m := &Manager{
		fetch: func(context.Context, prerequisites.DependencySource, prerequisites.SetupEgress) ([]byte, error) {
			return []byte("changed upstream payload"), nil
		},
		probe: func(context.Context, prerequisites.BundleArea, prerequisites.Definition) error {
			t.Fatal("unapproved bytes were executed")
			return nil
		},
	}
	area := newMemoryArea()
	if _, err := m.Prepare(t.Context(), area, nil, definition, prerequisites.SetupEgress{}, nil); err == nil {
		t.Fatal("accepted source integrity failure")
	}
	if len(area.files) != 0 {
		t.Fatal("published an unapproved artifact")
	}
}

// TestBundleFailureRemedyNamesOnlyWhatSetupAccepts drives a real setup bundle
// failure. Setup selects no context and consumes no --context value, so its
// remedy repeats setup as it is invoked.
func TestBundleFailureRemedyNamesOnlyWhatSetupAccepts(t *testing.T) {
	definition := resolvedDefinitionFixture(t)
	m := &Manager{
		fetch: func(context.Context, prerequisites.DependencySource, prerequisites.SetupEgress) ([]byte, error) {
			return []byte("changed upstream payload"), nil
		},
		probe: func(context.Context, prerequisites.BundleArea, prerequisites.Definition) error { return nil },
	}
	_, err := m.Prepare(t.Context(), newMemoryArea(), nil, definition, prerequisites.SetupEgress{}, nil)
	found := diagnostics.Of(err)
	if len(found) != 1 || found[0].Code != "controller.setup" || found[0].Message != "dependency source changed before bundle publication" {
		t.Fatalf("setup bundle failure: %+v", found)
	}
	remedy := found[0].Remediation
	if !strings.Contains(remedy, "bootwright setup") || strings.Contains(remedy, "context") {
		t.Fatalf("remedy asks setup for an input it does not consume: %q", remedy)
	}
	if remedy != "Restore approved dependency sources or the exact retained bundle, then rerun bootwright setup." {
		t.Fatalf("remedy: %q", remedy)
	}
}

// A bundle holds what one resolution froze, so a definition without its
// resolved Python and Ansible closure names no bundle: validation, inspection
// and preparation refuse it before any area is read or written.
func TestABundleDefinitionWithoutABootstrapRefuses(t *testing.T) {
	resolved := resolvedDefinitionFixture(t)
	unresolved := prerequisites.Definition{
		CatalogDigest: resolved.CatalogDigest, PythonVersion: resolved.PythonVersion, AnsibleVersion: resolved.AnsibleVersion,
		Sources: resolved.Sources, Execution: resolved.Execution, NativeRequirements: resolved.NativeRequirements, Native: resolved.Native,
	}
	m := &Manager{
		fetch: func(context.Context, prerequisites.DependencySource, prerequisites.SetupEgress) ([]byte, error) {
			t.Fatal("a definition without a bootstrap acquired a dependency")
			return nil, nil
		},
		probe: func(context.Context, prerequisites.BundleArea, prerequisites.Definition) error {
			t.Fatal("a definition without a bootstrap executed a process")
			return nil
		},
	}
	if err := m.Validate(unresolved); err == nil {
		t.Fatal("validation admitted a definition without a bootstrap")
	}
	area := newMemoryArea()
	if _, err := m.Inspect(t.Context(), area, unresolved, true); err == nil {
		t.Fatal("inspection admitted a definition without a bootstrap")
	}
	if _, err := m.Prepare(t.Context(), area, nil, unresolved, prerequisites.SetupEgress{}, nil); err == nil {
		t.Fatal("preparation admitted a definition without a bootstrap")
	}
	if area.writes != 0 || len(area.files) != 0 {
		t.Fatal("a definition without a bootstrap wrote to its area")
	}
}
