//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	p "github.com/crmarques/bootwright/internal/controller/prerequisites"
)

func syntheticResolution(t *testing.T) p.Definition {
	t.Helper()
	platform := p.Platform{OS: "fedora", Release: "43", Architecture: "amd64"}
	versions := controller.DefaultDependencyVersions()
	source := func(id, url string) p.DependencySource {
		return p.DependencySource{ID: id, URL: url, SHA256: strings.Repeat("a", 64), Bytes: 64}
	}
	bootstrap, err := p.CanonicalBootstrap(p.BootstrapDefinition{
		Format: "bootwright.controller.bootstrap-v1", Platform: platform, PythonIntent: "latest", AnsibleIntent: "latest", PythonVersion: "3.14.7", AnsibleVersion: "2.21.4", PythonExecutable: "python/bin/python3.14", SitePackages: "python/lib/python3.14/site-packages/",
		Sources:          []p.DependencySource{source("python", "https://github.com/astral-sh/python-build-standalone/releases/download/20260901/cpython-3.14.7%2B20260901-x86_64-unknown-linux-gnu-install_only_stripped.tar.gz"), source("ansible", "https://files.pythonhosted.org/packages/ansible_core-2.21.4-py3-none-any.whl"), source("urllib3", "https://files.pythonhosted.org/packages/urllib3-2.7.0-py3-none-any.whl")},
		Wheels:           []p.BootstrapWheel{{Name: "ansible-core", Version: "2.21.4", SourceID: "ansible"}, {Name: "urllib3", Version: "2.7.0", SourceID: "urllib3"}},
		Metadata:         []p.DependencySource{source("python-metadata", "https://raw.githubusercontent.com/astral-sh/uv/main/crates/uv-python/download-metadata.json"), source("ansible-metadata", "https://pypi.org/pypi/ansible-core/json")},
		ProjectionSHA256: strings.Repeat("b", 64), FileCount: 10, ExpandedBytes: 100, AutomationDigest: strings.Repeat("c", 64),
		Execution:         p.ExecutionRequirement{PythonExecutable: "python/bin/python3.14", Files: []p.InstalledFile{}, Links: []p.InstalledLink{}, Preload: []string{}},
		ExecutionPackages: []string{"glibc", "libgcc"},
	})
	if err != nil {
		t.Fatal(err)
	}
	native := p.NativeResolvedPlan{Format: "bootwright.native-plan-v1", Platform: platform, Solver: "dnf5", SolverVersion: "5.2.0", Requests: versions, Requirements: p.NativeRequirements{ContainerRuntime: true}, Repositories: []p.NativeRepository{{ID: "base", BaseURL: "https://packages.example.test/fedora", MetadataSHA256: strings.Repeat("d", 64)}}, Roots: []p.NativeRoot{}, Packages: []p.NativePackage{}, Actions: []p.NativeAction{}, BeforeSHA256: strings.Repeat("e", 64), AfterSHA256: strings.Repeat("e", 64)}
	for _, root := range []struct{ key, name string }{{"podman", "podman"}, {"openssh", "openssh-clients"}, {"nmstate", "nmstate"}} {
		identity := p.NativeIdentity{Name: root.name, Version: "1.2.3", Release: "1.fc43", Architecture: "x86_64"}
		native.Roots = append(native.Roots, p.NativeRoot{Key: root.key, Requested: "latest", Package: identity})
		native.Packages = append(native.Packages, p.NativePackage{Name: identity.Name, Version: identity.Version, Release: identity.Release, Architecture: identity.Architecture, Signer: strings.Repeat("f", 40), Source: source(root.name, "https://packages.example.test/fedora/"+root.name+".rpm")})
	}
	native, err = p.CanonicalNativePlan(native)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := p.NewResolvedDefinition(bootstrap, native, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return definition
}

func TestControllerResolvedDefinitionPublicationAndIsolation(t *testing.T) {
	store, _ := fixture(t)
	scope := p.SetupContext{}
	value := syntheticControllerState(t, scope)
	definition := syntheticResolution(t)
	value.Receipt.Definition = &definition
	value.Receipt.CatalogDigest = definition.CatalogDigest
	value.Receipt.Sources = slices.Clone(definition.Sources)
	var err error
	value.Receipt.PlanDigest, err = p.SetupPlanDigest(value.Host, value.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	publishControllerState(t, store, scope, value)
	read := func(mutate bool) error {
		return store.ReadController(context.Background(), "", func(view p.StorageView) error {
			if len(view.State.RetainedDefinitions) != 1 || view.State.Receipt.Definition == nil || !p.SameDefinition(*view.State.Receipt.Definition, definition) || !p.SameDefinition(view.State.RetainedDefinitions[0], definition) {
				t.Fatal("frozen definition did not survive publication")
			}
			if mutate {
				view.State.Receipt.Definition.Bootstrap.Sources[0].URL = "changed"
				view.State.RetainedDefinitions[0].Native.Packages[0].Source.SHA256 = "changed"
			}
			return nil
		})
	}
	if err = read(true); err != nil {
		t.Fatal(err)
	}
	if err = read(false); err != nil {
		t.Fatal(err)
	}
	complete := completeControllerState(value)
	publishControllerState(t, store, scope, complete)
	changed := cloneControllerState(complete)
	changed.Receipt.Definition.Native.BeforeSHA256 = strings.Repeat("0", 64)
	if p.SameDefinition(*value.Receipt.Definition, *changed.Receipt.Definition) {
		t.Fatal("clone retained aliases")
	}
	if err := validateControllerState(changed); err == nil {
		t.Fatal("modified native transaction was accepted")
	}
}

func TestControllerResolvedDefinitionEncodingRemainsBoundedAndCanonical(t *testing.T) {
	definition := syntheticResolution(t)
	value := syntheticControllerState(t, p.SetupContext{})
	value.Receipt.Definition = &definition
	value.Receipt.CatalogDigest = definition.CatalogDigest
	value.Receipt.Sources = slices.Clone(definition.Sources)
	value.Receipt.PlanDigest, _ = p.SetupPlanDigest(value.Host, value.Receipt)
	value, err := retainControllerSources(p.HostState{}, value)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := encodeRecord(controllerRecord(value), maxControllerState)
	if err != nil {
		t.Fatal(err)
	}
	decoded, _, err := decodeControllerRecord(encoded)
	if err != nil || !p.SameDefinition(*decoded.Receipt.Definition, definition) {
		t.Fatalf("full resolution round-trip failed: %v", err)
	}
	for _, malformed := range [][]byte{bytes.Replace(encoded, []byte(`"toolRequests":[]`), []byte(`"toolRequests":null`), 1), bytes.Replace(encoded, []byte(`"pythonVersion":"3.14.7"`), []byte(`"pythonVersion":"3.14.8"`), 1)} {
		if bytes.Equal(encoded, malformed) {
			t.Fatal("fixture did not alter canonical bytes")
		}
		if _, _, err := decodeControllerRecord(malformed); err == nil {
			t.Fatal("malformed frozen definition accepted")
		}
	}
	if _, err := encodeRecord(controllerRecord(value), len(encoded)-1); err == nil {
		t.Fatal("encoding exceeded exact size bound")
	}
}
