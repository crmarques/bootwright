package bundlelocal

import (
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

func TestCatalogIsClosedAndItsNamespaceBindsNativeSelection(t *testing.T) {
	digests := map[string]bool{}
	for _, platform := range []prerequisites.Platform{{OS: "rhel", Release: "9.8", Architecture: "amd64"}, {OS: "fedora", Release: "43", Architecture: "amd64"}} {
		for _, runtime := range []bool{false, true} {
			definition, err := (Catalog{}).Select(platform, prerequisites.NativeRequirements{ContainerRuntime: runtime})
			if err != nil {
				t.Fatal(err)
			}
			if digests[definition.CatalogDigest] {
				t.Fatal("distinct native selections share a mutable namespace")
			}
			digests[definition.CatalogDigest] = true
			if definition.PythonVersion != "3.13.15" || definition.AnsibleVersion != "2.21.4" {
				t.Fatal("baseline version changed unexpectedly")
			}
			if _, err := validateDefinition(definition); err != nil {
				t.Fatal(err)
			}
			if !runtime && len(definition.Sources) != 11 {
				t.Fatal("baseline includes sources outside the Python and wheel closure")
			}
			if len(definition.Execution.Files) != 8 || len(definition.Execution.Preload) != 7 || definition.Execution.Loader != "/usr/lib64/ld-linux-x86-64.so.2" {
				t.Fatal("initial Python execution closure is incomplete")
			}
			seen := map[string]bool{}
			for _, source := range definition.Sources {
				if seen[source.ID] || !validPath(source.ID) || strings.Contains(source.ID, "/") || len(source.SHA256) != 64 || source.Bytes <= 0 {
					t.Fatalf("invalid catalog source %q", source.ID)
				}
				seen[source.ID] = true
			}
			definition.Sources[0].URL = "https://invalid.example/changed"
			again, err := (Catalog{}).Select(platform, prerequisites.NativeRequirements{ContainerRuntime: runtime})
			if err != nil || again.Sources[0].URL == definition.Sources[0].URL {
				t.Fatal("catalog exposed mutable source aliases")
			}
			if _, err := validateDefinition(definition); err == nil {
				t.Fatal("accepted source substitution")
			}
		}
	}
	for _, platform := range []prerequisites.Platform{{OS: "rhel", Release: "9.7", Architecture: "amd64"}, {OS: "fedora", Release: "44", Architecture: "amd64"}, {OS: "rhel", Release: "9.8", Architecture: "arm64"}, {OS: "centos", Release: "9", Architecture: "amd64"}} {
		if _, err := (Catalog{}).Select(platform, prerequisites.NativeRequirements{}); err == nil {
			t.Fatalf("accepted unqualified platform %+v", platform)
		}
	}
}

func TestCatalogRejectsExecutionFoundationSubstitution(t *testing.T) {
	platform := prerequisites.Platform{OS: "fedora", Release: "43", Architecture: "amd64"}
	for _, change := range []func(*prerequisites.ExecutionRequirement){
		func(value *prerequisites.ExecutionRequirement) { value.Loader = "/unapproved/loader" },
		func(value *prerequisites.ExecutionRequirement) { value.LockPath = "/unapproved/lock" },
		func(value *prerequisites.ExecutionRequirement) { value.Files[0].SHA256 = strings.Repeat("a", 64) },
		func(value *prerequisites.ExecutionRequirement) { value.Links[0].Target = "unapproved" },
		func(value *prerequisites.ExecutionRequirement) { value.Preload[0] = "/unapproved/library" },
	} {
		definition, err := (Catalog{}).Select(platform, prerequisites.NativeRequirements{})
		if err != nil {
			t.Fatal(err)
		}
		change(&definition.Execution)
		if _, err := validateDefinition(definition); err == nil {
			t.Fatal("accepted a substituted execution foundation")
		}
		again, err := (Catalog{}).Select(platform, prerequisites.NativeRequirements{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := validateDefinition(again); err != nil {
			t.Fatal("caller changed the compiled execution profile", err)
		}
	}
}

func TestLibvirtSelectionHasSeparateCompleteNativeNamespace(t *testing.T) {
	platform := prerequisites.Platform{OS: "fedora", Release: "43", Architecture: "amd64"}
	baseline, err := (Catalog{}).Select(platform, prerequisites.NativeRequirements{ContainerRuntime: true})
	if err != nil {
		t.Fatal(err)
	}
	requirements := prerequisites.NativeRequirements{ContainerRuntime: true, LibvirtClient: true}
	selected, err := (Catalog{}).Select(platform, requirements)
	if err != nil || selected.CatalogDigest == baseline.CatalogDigest {
		t.Fatal("selected native tools share the baseline namespace", err)
	}
	packages, err := (Catalog{}).NativePackages(platform, requirements)
	if err != nil || len(packages) != 136 || len(selected.Sources) != len(packages)+11 {
		t.Fatal("libvirt package closure is incomplete", len(packages), err)
	}
	paths := map[string]bool{}
	for _, file := range selected.Runtime.Files {
		if paths[file.Path] {
			t.Fatal("conflicting selected native file evidence", file.Path)
		}
		paths[file.Path] = true
	}
	if !paths["/usr/bin/virsh"] || !paths["/usr/bin/ssh"] || !paths["/usr/bin/nmstatectl"] || !paths["/usr/bin/podman"] {
		t.Fatal("selected native CLI evidence is incomplete")
	}
	if _, err := validateDefinition(selected); err != nil {
		t.Fatal(err)
	}
	platform.OS, platform.Release = "rhel", "9.8"
	if _, err := (Catalog{}).Select(platform, requirements); err == nil {
		t.Fatal("RHEL libvirt selection admitted without authenticated source authority")
	}
}

func TestDryInspectionNeverAcquiresOrExecutes(t *testing.T) {
	definition, err := (Catalog{}).Select(prerequisites.Platform{OS: "fedora", Release: "43", Architecture: "amd64"}, prerequisites.NativeRequirements{})
	if err != nil {
		t.Fatal(err)
	}
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
	definition, err := (Catalog{}).Select(prerequisites.Platform{OS: "fedora", Release: "43", Architecture: "amd64"}, prerequisites.NativeRequirements{})
	if err != nil {
		t.Fatal(err)
	}
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
	if err := m.Prepare(t.Context(), area, nil, definition, prerequisites.SetupEgress{}, nil); err == nil {
		t.Fatal("accepted source integrity failure")
	}
	if len(area.files) != 0 {
		t.Fatal("published an unapproved artifact")
	}
}
