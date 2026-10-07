// Package bundlelocal acquires and verifies the resolved Controller
// dependency closure. Workspace remains the sole owner of bundle filesystem
// effects.
package bundlelocal

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"io"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

//go:embed catalog.json
var catalogData []byte

// Catalog admits the releases whose provided execution foundation this
// executable was compiled against. It pins no dependency release: setup
// resolves every one of them.
type Catalog struct{}

// ValidateEgress admits the complete explicit acquisition policy without
// reading trust files, consulting DNS or creating an HTTP transport.
func (Catalog) ValidateEgress(egress prerequisites.SetupEgress) error {
	_, err := explicitProxy(egress)
	return err
}

// Admit refuses, without reading the host, a platform whose provided
// execution foundation is not compiled into this executable.
func (Catalog) Admit(platform prerequisites.Platform) error {
	record, err := compiledCatalog()
	if err != nil {
		return err
	}
	if _, found := selectNative(record, platform); !found || platform.Architecture != "amd64" {
		return unsupportedPlatform()
	}
	return nil
}

// catalogRecord is the compiled catalog: the provided execution foundation of
// each admitted release, which every resolution of that release carries and
// every retained one must still match.
type catalogRecord struct {
	Format     string         `json:"format"`
	Projection string         `json:"projection"`
	Native     []nativeRecord `json:"native"`
}

// nativeRecord is one admitted release. Packages attributes each foundation
// file to the package build that provides it on a qualified host, so a refusal
// can name what to reinstall. It sits beside the execution requirement, never
// inside it, because that requirement is what every resolution carries and
// its digests bind; the attribution binds nothing.
type nativeRecord struct {
	OS        string                             `json:"os"`
	Release   string                             `json:"release"`
	Execution prerequisites.ExecutionRequirement `json:"execution"`
	Packages  []foundationPackage                `json:"packages"`
}

// foundationPackage is one package build and the foundation files it provides.
type foundationPackage struct {
	Name  string   `json:"name"`
	Build string   `json:"build"`
	Files []string `json:"files"`
}

// foundationBuilds names the package builds that provide a foundation, in
// catalog order, as a check states what it requires.
func foundationBuilds(packages []foundationPackage) string {
	builds := make([]string, 0, len(packages))
	for _, pkg := range packages {
		builds = append(builds, pkg.Name+" "+pkg.Build)
	}
	return strings.Join(builds, ", ")
}

// described names a package build, or, for a build only setup's
// qualification names, the package as setup qualified it.
func (pkg foundationPackage) described() string {
	if pkg.Build == "" {
		return pkg.Name + " as setup qualified it"
	}
	return pkg.Name + " " + pkg.Build
}

// upstreamVersion is the version a compiled build pins, without its release.
func (pkg foundationPackage) upstreamVersion() string {
	index := strings.LastIndex(pkg.Build, "-")
	if index <= 0 {
		return ""
	}
	return pkg.Build[:index]
}

// compiledAttribution is the package attribution of the compiled record whose
// execution requirement is the one given, apart from the interpreter path a
// resolution adds. A requirement setup qualified in that record's shape keeps
// its package names and files under the paths it pins, but no build, which
// only the receipt names; one in no compiled record's shape has none.
func compiledAttribution(requirement prerequisites.ExecutionRequirement) []foundationPackage {
	native, exact, found := compiledNativeFor(requirement)
	if !found {
		return nil
	}
	if exact {
		return native.Packages
	}
	renamed := make(map[string]string, len(native.Execution.Files))
	for index, file := range native.Execution.Files {
		renamed[file.Path] = requirement.Files[index].Path
	}
	packages := make([]foundationPackage, 0, len(native.Packages))
	for _, pkg := range native.Packages {
		files := make([]string, 0, len(pkg.Files))
		for _, name := range pkg.Files {
			files = append(files, renamed[name])
		}
		packages = append(packages, foundationPackage{Name: pkg.Name, Files: files})
	}
	return packages
}

// compiledNativeFor is the compiled record whose execution requirement is the
// one given, exactly or in its shape, apart from the interpreter path.
func compiledNativeFor(requirement prerequisites.ExecutionRequirement) (native nativeRecord, exact, found bool) {
	record, err := compiledCatalog()
	if err != nil {
		return nativeRecord{}, false, false
	}
	requirement.PythonExecutable = ""
	for _, native := range record.Native {
		if equalExecution(native.Execution, requirement) {
			return native, true, true
		}
	}
	for _, native := range record.Native {
		if sameFoundationShape(native.Execution, requirement) {
			return native, false, true
		}
	}
	return nativeRecord{}, false, false
}

func sameFoundationShape(compiled, requirement prerequisites.ExecutionRequirement) bool {
	if compiled.Loader != requirement.Loader || compiled.LockPath != requirement.LockPath || len(compiled.Files) != len(requirement.Files) {
		return false
	}
	for index, file := range compiled.Files {
		if !prerequisites.FoundationPathShape(file.Path, requirement.Files[index].Path) {
			return false
		}
	}
	return true
}

func cloneExecution(value prerequisites.ExecutionRequirement) prerequisites.ExecutionRequirement {
	value.Files = slices.Clone(value.Files)
	value.Links = slices.Clone(value.Links)
	value.Preload = slices.Clone(value.Preload)
	return value
}

func equalExecution(a, b prerequisites.ExecutionRequirement) bool {
	return a.PythonExecutable == b.PythonExecutable && a.Loader == b.Loader && a.LockPath == b.LockPath && slices.Equal(a.Files, b.Files) && slices.Equal(a.Links, b.Links) && slices.Equal(a.Preload, b.Preload)
}

func compiledCatalog() (catalogRecord, error) {
	var record catalogRecord
	decoder := json.NewDecoder(bytes.NewReader(catalogData))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&record) != nil || decoder.Decode(new(json.RawMessage)) != io.EOF || record.Format != "bootwright.controller.catalog-v1" || record.Projection != "python-wheel-files-v1" {
		return catalogRecord{}, bundleFailure("compiled dependency catalog is invalid")
	}
	return record, nil
}

func selectNative(record catalogRecord, platform prerequisites.Platform) (nativeRecord, bool) {
	for _, native := range record.Native {
		if native.OS == platform.OS && native.Release == platform.Release {
			return native, true
		}
	}
	return nativeRecord{}, false
}

func unsupportedPlatform() error {
	return diagnostics.NewFailureWithRemediation("controller.unsupported", "controller setup requires qualified RHEL 9.8 or Fedora 43 on Linux amd64", "", "Use a qualified installed controller release and architecture.")
}

func bundleFailure(message string) error {
	return &prerequisites.ScopedFailure{Message: message, Correction: "Restore approved dependency sources or the exact retained bundle"}
}
