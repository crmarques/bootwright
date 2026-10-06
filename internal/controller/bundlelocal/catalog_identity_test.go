package bundlelocal

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

var (
	fedoraPlatform = prerequisites.Platform{OS: "fedora", Release: "43", Architecture: "amd64"}
	rhelPlatform   = prerequisites.Platform{OS: "rhel", Release: "9.8", Architecture: "amd64"}
)

// compiledExecution is the provided execution foundation the compiled catalog
// holds for platform.
func compiledExecution(t *testing.T, platform prerequisites.Platform) prerequisites.ExecutionRequirement {
	t.Helper()
	record, err := compiledCatalog()
	if err != nil {
		t.Fatal(err)
	}
	native, ok := selectNative(record, platform)
	if !ok {
		t.Fatalf("the compiled catalog holds no foundation for %s %s", platform.OS, platform.Release)
	}
	return cloneExecution(native.Execution)
}

// pinnedResolution is one complete resolution of platform from fixed
// publisher identities, carrying automation, whose only compiled input is the
// provided execution foundation every resolution of that platform carries.
func pinnedResolution(t *testing.T, platform prerequisites.Platform, automation string) prerequisites.Definition {
	t.Helper()
	versions := controller.DefaultDependencyVersions()
	source := func(id, url string, size int64) prerequisites.DependencySource {
		digest := sha256.Sum256([]byte(id))
		return prerequisites.DependencySource{ID: id, URL: url, SHA256: hex.EncodeToString(digest[:]), Bytes: size}
	}
	bootstrap := prerequisites.BootstrapDefinition{
		Format: "bootwright.controller.bootstrap-v1", Platform: platform,
		PythonIntent: versions.Python, AnsibleIntent: versions.Ansible, PythonVersion: "3.13.15", AnsibleVersion: "2.21.4",
		PythonExecutable: "python/bin/python3.13", SitePackages: "python/lib/python3.13/site-packages/",
		Sources: []prerequisites.DependencySource{
			source("python-3.13.15", "https://github.com/astral-sh/python-build-standalone/releases/download/20260901/cpython-3.13.15%2B20260901-x86_64-unknown-linux-gnu-install_only_stripped.tar.gz", 34810677),
			source("wheel-ansible-core-2.21.4", "https://files.pythonhosted.org/packages/ansible_core-2.21.4-py3-none-any.whl", 2457154),
			source("wheel-urllib3-2.7.0", "https://files.pythonhosted.org/packages/urllib3-2.7.0-py3-none-any.whl", 129956),
		},
		Wheels: []prerequisites.BootstrapWheel{{Name: "ansible-core", Version: "2.21.4", SourceID: "wheel-ansible-core-2.21.4"}, {Name: "urllib3", Version: "2.7.0", SourceID: "wheel-urllib3-2.7.0"}},
		Metadata: []prerequisites.DependencySource{
			source("python-metadata", pythonMetadataURL, 4096),
			source("ansible-metadata", ansibleIndexURL, 8192),
		},
		ProjectionSHA256: strings.Repeat("b", 64), FileCount: 4096, ExpandedBytes: 1 << 26,
		Execution: compiledExecution(t, platform), ExecutionPackages: executionPackageOwners(),
		AutomationDigest: automation,
	}
	bootstrap.Execution.PythonExecutable = bootstrap.PythonExecutable
	bootstrap, err := prerequisites.CanonicalBootstrap(bootstrap)
	if err != nil {
		t.Fatal(err)
	}
	solver, solverVersion, release, repository := "dnf5", "5.2.0", "1.fc43", "https://dl.fedoraproject.org/pub/fedora/linux/updates/43/Everything/x86_64"
	if platform.OS == "rhel" {
		solver, solverVersion, release, repository = "dnf4", "4.14.0", "1.el9", "https://cdn-ubi.redhat.com/content/public/ubi/dist/ubi9/9/x86_64/appstream/os"
	}
	native := prerequisites.NativeResolvedPlan{
		Format: "bootwright.native-plan-v1", Platform: platform, Solver: solver, SolverVersion: solverVersion, Requests: versions,
		Requirements: prerequisites.NativeRequirements{ContainerRuntime: true}, Roots: []prerequisites.NativeRoot{},
		Repositories: []prerequisites.NativeRepository{{ID: "updates", BaseURL: repository, MetadataSHA256: strings.Repeat("d", 64)}},
		Packages:     []prerequisites.NativePackage{}, Actions: []prerequisites.NativeAction{}, BeforeSHA256: strings.Repeat("e", 64), AfterSHA256: strings.Repeat("e", 64),
	}
	for _, root := range []struct{ key, name string }{{"podman", "podman"}, {"openssh", "openssh-clients"}, {"nmstate", "nmstate"}} {
		identity := prerequisites.NativeIdentity{Name: root.name, Version: "1.2.3", Release: release, Architecture: "x86_64"}
		native.Roots = append(native.Roots, prerequisites.NativeRoot{Key: root.key, Requested: "latest", Package: identity})
		native.Packages = append(native.Packages, prerequisites.NativePackage{Name: identity.Name, Version: identity.Version, Release: identity.Release, Architecture: identity.Architecture, Signer: strings.Repeat("f", 40), Source: source("native-"+root.name, repository+"/Packages/"+root.name+".rpm", 4096)})
	}
	native, err = prerequisites.CanonicalNativePlan(native)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := prerequisites.NewResolvedDefinition(bootstrap, native)
	if err != nil {
		t.Fatal(err)
	}
	return definition
}

// The compiled execution foundation flows into every bootstrap digest and is
// what a retained resolution is held against, so its values never move with
// the catalog around it. The digests are those of the foundations before the
// catalog stopped pinning dependency artifacts.
func TestTheCompiledExecutionFoundationIsUnchanged(t *testing.T) {
	for platform, want := range map[prerequisites.Platform]string{
		fedoraPlatform: "b0f5d93f779697a212ff4dd56ffd54887414a6083259086b09452e69af3a790a",
		rhelPlatform:   "de17167fe4981efb779e592e6ad59c13abdca80f769e5b39650f70e736bf198f",
	} {
		encoded, err := json.Marshal(compiledExecution(t, platform))
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(encoded)
		if got := hex.EncodeToString(digest[:]); got != want {
			t.Errorf("the %s %s execution foundation digest is %s, want %s", platform.OS, platform.Release, got, want)
		}
	}
}

// A resolved bundle's identity reads nothing of the catalog but the provided
// execution foundation, so dropping the catalog's dependency pins moves no
// bundle and no retained resolution. The digests are those this resolution had
// while the catalog still pinned them.
func TestAResolvedBundleIdentityDoesNotReadTheCatalogPins(t *testing.T) {
	for platform, want := range map[prerequisites.Platform][2]string{
		fedoraPlatform: {"becd7778c6da7298fbe8ea4052a52bf4b75fb54137f728db6656a886e18dfec6", "44a6a51c2139cb0d55367f5d2b049515ae7d69826cb7db35ddacfa89defa03b8"},
		rhelPlatform:   {"9923479b54af98a1b6828217c4c59eab29896a035cc5d91e7ec2ecc5ff115e3a", "9793fcb8b2d76639486ff60135baf993d1c9cb6e7c0d862658272fa704bbd8ce"},
	} {
		definition := pinnedResolution(t, platform, strings.Repeat("c", 64))
		if definition.CatalogDigest != want[0] || definition.ResolutionDigest != want[1] {
			t.Errorf("%s %s resolves to bundle %s and resolution %s, want %s and %s", platform.OS, platform.Release, definition.CatalogDigest, definition.ResolutionDigest, want[0], want[1])
		}
	}
}

// The catalog admits exactly the releases whose foundation it holds, on
// amd64, and refuses every other before anything is resolved.
func TestTheCatalogAdmitsOnlyItsCompiledFoundations(t *testing.T) {
	for _, platform := range []prerequisites.Platform{fedoraPlatform, rhelPlatform} {
		if err := (Catalog{}).Admit(platform); err != nil {
			t.Fatalf("%+v was refused: %v", platform, err)
		}
	}
	for _, platform := range []prerequisites.Platform{{OS: "rhel", Release: "9.7", Architecture: "amd64"}, {OS: "fedora", Release: "44", Architecture: "amd64"}, {OS: "rhel", Release: "9.8", Architecture: "arm64"}, {OS: "centos", Release: "9", Architecture: "amd64"}} {
		if err := (Catalog{}).Admit(platform); err == nil {
			t.Fatalf("admitted unqualified platform %+v", platform)
		}
	}
}
