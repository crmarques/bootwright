package storagecontract

import (
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

// resolved is an intended setup for scope whose receipt carries the complete
// resolution it publishes, as every setup's does, so a retained resolution
// names its bundle. revision, one hexadecimal digit, sets the automation and so
// the bundle and the receipt apart from another setup's.
func resolved(t *testing.T, scope prerequisites.SetupContext, revision string) prerequisites.HostState {
	t.Helper()
	definition := resolution(t, strings.Repeat(revision, 64))
	value := intent(t, scope)
	value.Receipt.ID = "setup-" + strings.Repeat(revision, 32)
	value.Receipt.CatalogDigest, value.Receipt.Definition = definition.CatalogDigest, &definition
	value.Receipt.Sources = slices.Clone(definition.Sources)
	value.Receipt.PlanDigest = digest(t, value)
	return value
}

// reresolved is a later setup of the bundle earlier names, solved again
// against the package inventory revision names, as a retry after a failed
// setup is: the same bundle under a new resolution and a new receipt.
func reresolved(t *testing.T, earlier prerequisites.HostState, revision string) prerequisites.HostState {
	t.Helper()
	native := *earlier.Receipt.Definition.Native
	native.BeforeSHA256, native.AfterSHA256 = strings.Repeat(revision, 64), strings.Repeat(revision, 64)
	native, err := prerequisites.CanonicalNativePlan(native)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := prerequisites.NewResolvedDefinition(*earlier.Receipt.Definition.Bootstrap, native)
	if err != nil || definition.CatalogDigest != earlier.Receipt.CatalogDigest || definition.ResolutionDigest == earlier.Receipt.Definition.ResolutionDigest {
		t.Fatalf("the solve does not name the same bundle under a new resolution: %v", err)
	}
	value := earlier
	value.Receipt.ID = "setup-" + strings.Repeat(revision, 32)
	value.Receipt.Definition = &definition
	value.Receipt.Sources = slices.Clone(definition.Sources)
	value.Receipt.Actions = slices.Clone(earlier.Receipt.Actions)
	value.Receipt.PlanDigest = digest(t, value)
	return value
}

func resolution(t *testing.T, automation string) prerequisites.Definition {
	t.Helper()
	platform := prerequisites.Platform{OS: "fedora", Release: "43", Architecture: "amd64"}
	source := func(id, url string) prerequisites.DependencySource {
		return prerequisites.DependencySource{ID: id, URL: url, SHA256: strings.Repeat("a", 64), Bytes: 64}
	}
	bootstrap, err := prerequisites.CanonicalBootstrap(prerequisites.BootstrapDefinition{
		Format: "bootwright.controller.bootstrap-v1", Platform: platform, PythonIntent: "latest", AnsibleIntent: "latest",
		PythonVersion: "3.14.7", AnsibleVersion: "2.21.4", PythonExecutable: "python/bin/python3.14", SitePackages: "python/lib/python3.14/site-packages/",
		Sources: []prerequisites.DependencySource{
			source("python", "https://github.com/astral-sh/python-build-standalone/releases/download/20260901/cpython-3.14.7%2B20260901-x86_64-unknown-linux-gnu-install_only_stripped.tar.gz"),
			source("ansible", "https://files.pythonhosted.org/packages/ansible_core-2.21.4-py3-none-any.whl"),
			source("urllib3", "https://files.pythonhosted.org/packages/urllib3-2.7.0-py3-none-any.whl"),
		},
		Wheels: []prerequisites.BootstrapWheel{{Name: "ansible-core", Version: "2.21.4", SourceID: "ansible"}, {Name: "urllib3", Version: "2.7.0", SourceID: "urllib3"}},
		Metadata: []prerequisites.DependencySource{
			source("python-metadata", "https://raw.githubusercontent.com/astral-sh/uv/main/crates/uv-python/download-metadata.json"),
			source("ansible-metadata", "https://pypi.org/pypi/ansible-core/json"),
		},
		ProjectionSHA256: strings.Repeat("b", 64), FileCount: 10, ExpandedBytes: 100, AutomationDigest: automation,
		Execution:         prerequisites.ExecutionRequirement{PythonExecutable: "python/bin/python3.14", Files: []prerequisites.InstalledFile{}, Links: []prerequisites.InstalledLink{}, Preload: []string{}},
		ExecutionPackages: []string{"glibc", "libgcc"},
	})
	if err != nil {
		t.Fatal(err)
	}
	native := prerequisites.NativeResolvedPlan{
		Format: "bootwright.native-plan-v1", Platform: platform, Solver: "dnf5", SolverVersion: "5.2.0", Requests: controller.DefaultDependencyVersions(),
		Requirements: prerequisites.NativeRequirements{ContainerRuntime: true},
		Repositories: []prerequisites.NativeRepository{{ID: "base", BaseURL: "https://packages.example.test/fedora", MetadataSHA256: strings.Repeat("d", 64)}},
		Roots:        []prerequisites.NativeRoot{}, Packages: []prerequisites.NativePackage{}, Actions: []prerequisites.NativeAction{},
		BeforeSHA256: strings.Repeat("e", 64), AfterSHA256: strings.Repeat("e", 64),
	}
	for _, root := range []struct{ key, name string }{{"podman", "podman"}, {"openssh", "openssh-clients"}, {"nmstate", "nmstate"}} {
		identity := prerequisites.NativeIdentity{Name: root.name, Version: "1.2.3", Release: "1.fc43", Architecture: "x86_64"}
		native.Roots = append(native.Roots, prerequisites.NativeRoot{Key: root.key, Requested: "latest", Package: identity})
		native.Packages = append(native.Packages, prerequisites.NativePackage{
			Name: identity.Name, Version: identity.Version, Release: identity.Release, Architecture: identity.Architecture,
			Signer: strings.Repeat("f", 40), Source: source(root.name, "https://packages.example.test/fedora/"+root.name+".rpm"),
		})
	}
	if native, err = prerequisites.CanonicalNativePlan(native); err != nil {
		t.Fatal(err)
	}
	definition, err := prerequisites.NewResolvedDefinition(bootstrap, native)
	if err != nil {
		t.Fatal(err)
	}
	return definition
}
