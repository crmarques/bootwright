package main

import (
	"encoding/json"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
)

const nativeResolutionHelper = "collections/ansible_collections/bootwright/core/plugins/module_utils/native_resolution.py"

// pythonTable returns the body of one top-level dictionary literal of the
// embedded solver helper, between its opening brace and the closing brace at
// the start of a line.
func pythonTable(t *testing.T, source, name string) string {
	t.Helper()
	start := strings.Index(source, "\n"+name+" = {\n")
	if start < 0 {
		t.Fatalf("the solver helper declares no %s table", name)
	}
	body := source[start+len("\n"+name+" = {\n"):]
	end := strings.Index(body, "\n}\n")
	if end < 0 {
		t.Fatalf("the solver helper's %s table does not close", name)
	}
	return body[:end]
}

var (
	pythonRootEntry = regexp.MustCompile(`"([a-z-]+)":\s*\(([^)]*)\)`)
	pythonString    = regexp.MustCompile(`"([^"]*)"`)
	pythonPair      = regexp.MustCompile(`"([a-z-]+)":\s*"([A-Za-z]+)"`)
)

// The solver's root table is written twice, once in Go, which proves a plan,
// and once in the Python helper, which solves it, and the provider host names
// two of its closures again. The installation block names none: the controller
// stage owns the installer-media closure. Every copy must name the same
// packages in the same order, and the hypervisor must share the libvirt intent
// on both sides.
func TestControllerClosuresAgreeAcrossGoAndPython(t *testing.T) {
	source := string(ansible.Automation()[nativeResolutionHelper])
	if source == "" {
		t.Fatal("the solver helper is not embedded")
	}
	roots := map[string][]string{}
	body := pythonTable(t, source, "ROOTS")
	for _, entry := range pythonRootEntry.FindAllStringSubmatch(body, -1) {
		packages := []string{}
		for _, name := range pythonString.FindAllStringSubmatch(entry[2], -1) {
			packages = append(packages, name[1])
		}
		roots[entry[1]] = packages
	}
	if len(roots) == 0 || strings.Count(body, ":") != len(roots) {
		t.Fatalf("read %d ROOTS entries from a table of %d", len(roots), strings.Count(body, ":"))
	}
	native := prerequisites.NativeRootNames()
	if !slices.Equal(slices.Sorted(maps.Keys(roots)), slices.Sorted(maps.Keys(native))) {
		t.Fatalf("Python roots %v, Go roots %v", slices.Sorted(maps.Keys(roots)), slices.Sorted(maps.Keys(native)))
	}
	for key, packages := range native {
		if !slices.Equal(roots[key], packages) {
			t.Errorf("root %s: Python names %v, Go names %v", key, roots[key], packages)
		}
	}
	if want := append(slices.Clone(roots["libvirt"]), roots["hypervisor"]...); !slices.Equal(libvirt.HypervisorPackages(), want) {
		t.Errorf("a provider host installs %v, the solver's libvirt and hypervisor roots are %v", libvirt.HypervisorPackages(), want)
	}

	intents := map[string]string{}
	for _, pair := range pythonPair.FindAllStringSubmatch(pythonTable(t, source, "ROOT_VERSIONS"), -1) {
		intents[pair[1]] = pair[2]
	}
	if !slices.Equal(slices.Sorted(maps.Keys(intents)), slices.Sorted(maps.Keys(native))) || intents["hypervisor"] != intents["libvirt"] {
		t.Fatalf("Python root intents %v", intents)
	}
	// Go proves the same intents: a plan whose every root asks for the
	// release of the intent the Python table names for its key, with each
	// intent a distinct exact release, is one Go accepts.
	versions := controller.DefaultDependencyVersions()
	versions.Podman, versions.OpenSSH, versions.NMState, versions.Libvirt, versions.InstallerMedia = "1.0.1", "1.0.2", "1.0.3", "1.0.4", "1.0.5"
	encoded, err := json.Marshal(versions)
	if err != nil {
		t.Fatal(err)
	}
	byField := map[string]string{}
	if err := json.Unmarshal(encoded, &byField); err != nil {
		t.Fatal(err)
	}
	plan := prerequisites.NativeResolvedPlan{
		Format: "bootwright.native-plan-v1", Platform: prerequisites.Platform{OS: "fedora", Release: "43", Architecture: "amd64"},
		Solver: "dnf5", SolverVersion: "5.2.0", Requests: versions,
		Requirements: prerequisites.NativeRequirements{ContainerRuntime: true, LibvirtClient: true, Hypervisor: true, InstallerMedia: true},
		Roots:        []prerequisites.NativeRoot{}, Packages: []prerequisites.NativePackage{}, Actions: []prerequisites.NativeAction{},
		Repositories: []prerequisites.NativeRepository{{ID: "base", BaseURL: "https://packages.example.test/fedora", MetadataSHA256: strings.Repeat("d", 64)}},
		BeforeSHA256: strings.Repeat("e", 64), AfterSHA256: strings.Repeat("e", 64),
	}
	for key, packages := range native {
		requested := byField[intents[key]]
		if requested == "" {
			t.Fatalf("root %s names the intent %q, which DependencyVersions does not carry", key, intents[key])
		}
		for _, name := range packages {
			identity := prerequisites.NativeIdentity{Name: name, Version: requested, Release: "1.fc43", Architecture: "x86_64"}
			plan.Roots = append(plan.Roots, prerequisites.NativeRoot{Key: key, Requested: requested, Package: identity})
			plan.Packages = append(plan.Packages, prerequisites.NativePackage{
				Name: name, Version: requested, Release: "1.fc43", Architecture: "x86_64", Signer: strings.Repeat("f", 40),
				Source: prerequisites.DependencySource{ID: name, URL: "https://packages.example.test/fedora/" + name + ".rpm", SHA256: strings.Repeat("a", 64), Bytes: 64},
			})
		}
	}
	if _, err := prerequisites.CanonicalNativePlan(plan); err != nil {
		t.Fatalf("Go refuses the root intents the Python helper solves with: %v", err)
	}
}
