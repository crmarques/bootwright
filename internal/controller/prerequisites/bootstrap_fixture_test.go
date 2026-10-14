package prerequisites

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
)

// resolvingFixture is one canonical earlier-build resolution, which records an
// earlier build wrote stay readable from until X89 deletes them.
type resolvingFixture struct {
	bootstrap BootstrapDefinition
	native    NativeResolvedPlan
}

func dynamicFixture(t *testing.T) (struct{}, *resolvingFixture) {
	t.Helper()
	platform := Platform{OS: "fedora", Release: "43", Architecture: "amd64"}
	repository := "https://packages.example.test/fedora"
	r := &resolvingFixture{}
	versions := controller.DefaultDependencyVersions()
	source := func(id, url string) DependencySource {
		return DependencySource{ID: id, URL: url, SHA256: strings.Repeat("a", 64), Bytes: 64}
	}
	bootstrap := BootstrapDefinition{
		Format: "bootwright.controller.bootstrap-v1", Platform: platform,
		PythonIntent: versions.Python, AnsibleIntent: versions.Ansible, PythonVersion: "3.14.7", AnsibleVersion: "2.21.4",
		PythonExecutable: "python/bin/python3.14", SitePackages: "python/lib/python3.14/site-packages/",
		Sources:          []DependencySource{source("python-3.14.7", "https://github.com/astral-sh/python-build-standalone/releases/download/20260901/cpython-3.14.7%2B20260901-x86_64-unknown-linux-gnu-install_only_stripped.tar.gz"), source("ansible-2.21.4", "https://files.pythonhosted.org/packages/ansible_core-2.21.4-py3-none-any.whl"), source("urllib3-2.7.0", "https://files.pythonhosted.org/packages/urllib3-2.7.0-py3-none-any.whl")},
		Wheels:           []BootstrapWheel{{Name: "ansible-core", Version: "2.21.4", SourceID: "ansible-2.21.4"}, {Name: "urllib3", Version: "2.7.0", SourceID: "urllib3-2.7.0"}},
		Metadata:         []DependencySource{source("python-metadata", "https://raw.githubusercontent.com/astral-sh/uv/main/crates/uv-python/download-metadata.json"), source("ansible-metadata", "https://pypi.org/simple/ansible-core/")},
		ProjectionSHA256: strings.Repeat("b", 64), FileCount: 10, ExpandedBytes: 100,
		Execution:         ExecutionRequirement{PythonExecutable: "python/bin/python3.14", Files: []InstalledFile{}, Links: []InstalledLink{}, Preload: []string{}},
		ExecutionPackages: []string{"glibc", "libgcc"},
		AutomationDigest:  strings.Repeat("c", 64),
	}
	var err error
	if r.bootstrap, err = CanonicalBootstrap(bootstrap); err != nil {
		t.Fatal(err)
	}
	native := NativeResolvedPlan{Format: "bootwright.native-plan-v1", Platform: platform, Solver: "dnf5", SolverVersion: "5.2.0", Requests: versions, Requirements: NativeRequirements{ContainerRuntime: true}, Roots: []NativeRoot{}, Repositories: []NativeRepository{{ID: "base", BaseURL: repository, MetadataSHA256: strings.Repeat("d", 64)}}, Packages: []NativePackage{}, Actions: []NativeAction{}, BeforeSHA256: strings.Repeat("e", 64), AfterSHA256: strings.Repeat("e", 64)}
	for _, root := range []struct{ key, name string }{{"podman", "podman"}, {"openssh", "openssh-clients"}, {"nmstate", "nmstate"}} {
		identity := NativeIdentity{Name: root.name, Version: "1.2.3", Release: "1.fc43", Architecture: "x86_64"}
		native.Roots = append(native.Roots, NativeRoot{Key: root.key, Requested: "latest", Package: identity})
		native.Packages = append(native.Packages, NativePackage{Name: identity.Name, Version: identity.Version, Release: identity.Release, Architecture: identity.Architecture, Signer: strings.Repeat("f", 40), Source: source(root.name, repository+"/"+root.name+".rpm")})
	}
	if r.native, err = CanonicalNativePlan(native); err != nil {
		t.Fatal(err)
	}
	return struct{}{}, r
}
