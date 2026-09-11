package bundlelocal

import (
	"errors"
	automation "github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/controller"
	"net/url"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

func resolvedDefinitionFixture(t *testing.T) prerequisites.Definition {
	t.Helper()
	record, _, err := compiledCatalog()
	if err != nil {
		t.Fatal(err)
	}
	platform := prerequisites.Platform{OS: "fedora", Release: "43", Architecture: "amd64"}
	profile, ok := selectNative(record, platform)
	if !ok {
		t.Fatal("fixture platform missing")
	}
	versions := controller.DefaultDependencyVersions()
	bootstrap := prerequisites.BootstrapDefinition{Format: "bootwright.controller.bootstrap-v1", Platform: platform, PythonIntent: "latest", AnsibleIntent: "latest", PythonVersion: record.PythonVersion, AnsibleVersion: record.AnsibleVersion, PythonExecutable: "python/bin/python3.13", SitePackages: sitePackages, Sources: slices.Clone(record.Baseline), Execution: cloneExecution(profile.Execution), ExecutionPackages: executionPackageOwners(), AutomationDigest: automation.Digest(), ProjectionSHA256: strings.Repeat("a", 64), FileCount: 1, ExpandedBytes: 1}
	bootstrap.Execution.PythonExecutable = bootstrap.PythonExecutable
	for _, source := range bootstrap.Sources[1:] {
		endpoint, _ := url.Parse(source.URL)
		parts := strings.Split(path.Base(endpoint.Path), "-")
		bootstrap.Wheels = append(bootstrap.Wheels, prerequisites.BootstrapWheel{Name: normalizeWheelName(parts[0]), Version: parts[1], SourceID: source.ID})
	}
	bootstrap.Metadata = []prerequisites.DependencySource{{ID: "python-metadata", URL: pythonMetadataURL, SHA256: strings.Repeat("a", 64), Bytes: 1}, {ID: "ansible-metadata", URL: "https://pypi.org/pypi/ansible-core/json", SHA256: strings.Repeat("a", 64), Bytes: 1}}
	bootstrap, err = prerequisites.CanonicalBootstrap(bootstrap)
	if err != nil {
		t.Fatal(err)
	}
	native := prerequisites.NativeResolvedPlan{Format: "bootwright.native-plan-v1", Platform: platform, Solver: "dnf5", SolverVersion: "5.4.0", Requests: versions, Roots: []prerequisites.NativeRoot{}, Repositories: []prerequisites.NativeRepository{{ID: "fixture", BaseURL: "https://dl.fedoraproject.org", MetadataSHA256: strings.Repeat("a", 64)}}, Packages: []prerequisites.NativePackage{}, Actions: []prerequisites.NativeAction{}, BeforeSHA256: strings.Repeat("a", 64), AfterSHA256: strings.Repeat("a", 64)}
	for _, pkg := range profile.Packages {
		key := ""
		switch pkg.Name {
		case "openssh-clients":
			key = "openssh"
		case "nmstate":
			key = "nmstate"
		}
		if key == "" {
			continue
		}
		native.Packages = append(native.Packages, pkg)
		native.Roots = append(native.Roots, prerequisites.NativeRoot{Key: key, Requested: "latest", Package: prerequisites.NativeIdentity{Name: pkg.Name, Epoch: pkg.Epoch, Version: pkg.Version, Release: pkg.Release, Architecture: pkg.Architecture}})
	}
	native, err = prerequisites.CanonicalNativePlan(native)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := prerequisites.NewResolvedDefinition(bootstrap, native, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return definition
}

func TestBootstrapIncompatibilityNeverMasksCorruptResolution(t *testing.T) {
	for _, change := range []string{"automation", "execution", "corruption"} {
		t.Run(change, func(t *testing.T) {
			definition := resolvedDefinitionFixture(t)
			if change == "corruption" {
				definition.Bootstrap.Sources[0].SHA256 = strings.Repeat("f", 64)
			} else {
				if change == "automation" {
					definition.Bootstrap.AutomationDigest = strings.Repeat("f", 64)
				} else {
					definition.Bootstrap.Execution.Files[0].SHA256 = strings.Repeat("f", 64)
				}
				bootstrap, err := prerequisites.CanonicalBootstrap(*definition.Bootstrap)
				if err != nil {
					t.Fatal(err)
				}
				definition, err = prerequisites.NewResolvedDefinition(bootstrap, *definition.Native, nil, nil)
				if err != nil {
					t.Fatal(err)
				}
			}
			_, err := validateDefinition(definition)
			if err == nil || errors.Is(err, prerequisites.ErrBootstrapIncompatible) != (change != "corruption") {
				t.Fatalf("wrong incompatibility boundary: %v", err)
			}
		})
	}
}

func TestDefinitionRequiresCompleteSelectedNativeAndToolClosure(t *testing.T) {
	platform := prerequisites.Platform{OS: "fedora", Release: "43", Architecture: "amd64"}
	requirements := prerequisites.NativeRequirements{ContainerRuntime: true, LibvirtClient: true}
	tool := toolFixture(t, "kubectl", []byte("qualified synthetic binary"))
	fresh := func() prerequisites.Definition {
		t.Helper()
		base, err := (Catalog{}).Select(platform, requirements)
		if err != nil {
			t.Fatal(err)
		}
		definition, err := prerequisites.WithTools(base, []prerequisites.ToolDefinition{tool})
		if err != nil {
			t.Fatal(err)
		}
		return definition
	}
	definition := fresh()
	record, err := validateDefinition(definition)
	if err != nil || len(record.Native) != 1 || record.Native[0].OS != platform.OS {
		t.Fatal("selected definition did not retain its exact native profile", err)
	}
	packages, err := (Catalog{}).NativePackages(platform, requirements)
	if err != nil || !slices.Equal(record.Native[0].Packages, packages) {
		t.Fatal("inspection does not recognize exactly the selected native packages", err)
	}
	for name, change := range map[string]func(*prerequisites.Definition){
		"omit-native-sources": func(value *prerequisites.Definition) {
			baseline, err := (Catalog{}).Select(platform, prerequisites.NativeRequirements{})
			if err != nil {
				t.Fatal(err)
			}
			value.Sources = append(baseline.Sources, tool.Source)
			slices.SortFunc(value.Sources, func(a, b prerequisites.DependencySource) int { return strings.Compare(a.ID, b.ID) })
		},
		"omit-libvirt-selection": func(value *prerequisites.Definition) { value.NativeRequirements.LibvirtClient = false },
		"omit-runtime-selection": func(value *prerequisites.Definition) { value.NativeRequirements.ContainerRuntime = false },
		"omit-runtime-proof":     func(value *prerequisites.Definition) { value.Runtime = prerequisites.RuntimeRequirement{} },
		"omit-runtime-member":    func(value *prerequisites.Definition) { value.Runtime.Files = value.Runtime.Files[1:] },
		"runtime-lock":           func(value *prerequisites.Definition) { value.Runtime.LockPath = "/unapproved/lock" },
		"runtime-policy":         func(value *prerequisites.Definition) { value.Runtime.SELinuxMode = "disabled" },
		"base-digest":            func(value *prerequisites.Definition) { value.BaseCatalogDigest = strings.Repeat("a", 64) },
		"target-path":            func(value *prerequisites.Definition) { value.Tools[0].Files[0].Path = "tools/unapproved" },
		"target-source":          func(value *prerequisites.Definition) { value.Tools[0].Source.SHA256 = strings.Repeat("b", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			changed := fresh()
			change(&changed)
			if _, err := validateDefinition(changed); err == nil {
				t.Fatal("accepted incomplete or substituted dependency closure")
			}
		})
	}
}

func TestBaselineDefinitionDoesNotAttributeNativeRPMs(t *testing.T) {
	definition, err := (Catalog{}).Select(prerequisites.Platform{OS: "fedora", Release: "43", Architecture: "amd64"}, prerequisites.NativeRequirements{})
	if err != nil {
		t.Fatal(err)
	}
	for _, withEmptyTools := range []bool{false, true} {
		if withEmptyTools {
			definition, err = prerequisites.WithTools(definition, nil)
			if err != nil {
				t.Fatal(err)
			}
		}
		record, err := validateDefinition(definition)
		if err != nil || len(record.Native) != 1 || len(record.Native[0].Packages) != 0 {
			t.Fatal("baseline inspection admitted native RPM content", err)
		}
	}
}
