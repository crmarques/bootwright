// Package bundlelocal acquires and verifies the compiled Controller dependency
// closure. Workspace remains the sole owner of bundle filesystem effects.
package bundlelocal

import (
	"cmp"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"slices"

	automation "github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate"
)

//go:embed catalog.json
var catalogData []byte

// Catalog selects only the releases whose exact dependency artifacts are
// recorded in the embedded catalog. A selected version is never a range.
type Catalog struct{}

// ValidateEgress admits the complete explicit acquisition policy without
// reading trust files, consulting DNS or creating an HTTP transport.
func (Catalog) ValidateEgress(egress prerequisites.SetupEgress) error {
	_, err := explicitProxy(egress)
	return err
}

type catalogRecord struct {
	Bootstrap      *prerequisites.BootstrapDefinition
	Format         string                           `json:"format"`
	Projection     string                           `json:"projection"`
	PythonVersion  string                           `json:"pythonVersion"`
	AnsibleVersion string                           `json:"ansibleVersion"`
	Baseline       []prerequisites.DependencySource `json:"baseline"`
	Native         []nativeRecord                   `json:"native"`
}

type nativeRecord struct {
	OS              string                             `json:"os"`
	Release         string                             `json:"release"`
	Packages        []prerequisites.NativePackage      `json:"packages"`
	RuntimeFiles    []prerequisites.InstalledFile      `json:"runtimeFiles"`
	RuntimeLinks    []prerequisites.InstalledLink      `json:"runtimeLinks"`
	Execution       prerequisites.ExecutionRequirement `json:"execution"`
	LibvirtPackages []prerequisites.NativePackage      `json:"libvirtPackages"`
	LibvirtFiles    []prerequisites.InstalledFile      `json:"libvirtFiles"`
	LibvirtLinks    []prerequisites.InstalledLink      `json:"libvirtLinks"`
}

func (Catalog) Select(platform prerequisites.Platform, requirements prerequisites.NativeRequirements) (prerequisites.Definition, error) {
	record, digest, err := compiledCatalog()
	if err != nil {
		return prerequisites.Definition{}, err
	}
	if platform.Architecture != "amd64" {
		return prerequisites.Definition{}, unsupportedPlatform()
	}
	native, found := selectNative(record, platform)
	if !found {
		return prerequisites.Definition{}, unsupportedPlatform()
	}
	packages, err := selectedNativePackages(native, requirements)
	if err != nil {
		return prerequisites.Definition{}, err
	}
	definition := prerequisites.Definition{
		CatalogDigest: nativeCatalogDigest(digest, platform, requirements), PythonVersion: record.PythonVersion,
		AnsibleVersion: record.AnsibleVersion, Sources: slices.Clone(record.Baseline),
		Execution:          cloneExecution(native.Execution),
		NativeRequirements: requirements,
	}
	if requirements.ContainerRuntime {
		definition.Runtime.SELinuxMode = "enforcing"
		definition.Runtime.LockPath = "/usr/lib/sysimage/rpm/.rpm.lock"
		if platform.OS == "rhel" {
			definition.Runtime.LockPath = "/var/lib/rpm/.rpm.lock"
		}
		for _, pkg := range packages {
			definition.Sources = append(definition.Sources, pkg.Source)
			if pkg.Name == "podman" {
				definition.Runtime.Version = pkg.Version
			}
		}
		definition.Runtime.Files = slices.Clone(native.RuntimeFiles)
		definition.Runtime.Links = slices.Clone(native.RuntimeLinks)
		if requirements.LibvirtClient {
			definition.Runtime.Files = uniqueFiles(append(definition.Runtime.Files, native.LibvirtFiles...))
			definition.Runtime.Links = uniqueLinks(append(definition.Runtime.Links, native.LibvirtLinks...))
		}
	}
	slices.SortFunc(definition.Sources, func(a, b prerequisites.DependencySource) int { return cmp.Compare(a.ID, b.ID) })
	return definition, nil
}

func nativeCatalogDigest(digest string, platform prerequisites.Platform, requirements prerequisites.NativeRequirements) string {
	selection, _ := json.Marshal(struct {
		Platform     prerequisites.Platform
		Requirements prerequisites.NativeRequirements
	}{platform, requirements})
	hash := sha256.New()
	hash.Write([]byte("bootwright.controller.native-selection-v1\x00"))
	hash.Write([]byte(digest))
	hash.Write([]byte{0})
	hash.Write(selection)
	return hex.EncodeToString(hash.Sum(nil))
}

func uniqueFiles(files []prerequisites.InstalledFile) []prerequisites.InstalledFile {
	slices.SortFunc(files, func(a, b prerequisites.InstalledFile) int { return cmp.Compare(a.Path, b.Path) })
	return slices.Compact(files)
}

func uniqueLinks(links []prerequisites.InstalledLink) []prerequisites.InstalledLink {
	slices.SortFunc(links, func(a, b prerequisites.InstalledLink) int { return cmp.Compare(a.Path, b.Path) })
	return slices.Compact(links)
}

func selectedNativePackages(native nativeRecord, requirements prerequisites.NativeRequirements) ([]prerequisites.NativePackage, error) {
	if requirements.LibvirtClient && (!requirements.ContainerRuntime || len(native.LibvirtPackages) == 0) {
		return nil, desiredstate.NewFailureWithRemediation("controller.unsupported", "selected libvirt controller dependencies have no qualified native source for this bastion release", "", "Use the qualified Fedora bastion profile for libvirt preparation; authenticated RHEL AppStream acquisition is not implemented.")
	}
	if !requirements.ContainerRuntime {
		return nil, nil
	}
	packages := slices.Clone(native.Packages)
	if requirements.LibvirtClient {
		packages = append(packages, native.LibvirtPackages...)
	}
	slices.SortFunc(packages, func(a, b prerequisites.NativePackage) int { return cmp.Compare(a.Source.ID, b.Source.ID) })
	return packages, nil
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

// NativePackages returns an independent exact closure for the native installer.
// Absence, compatibility and hook prerequisites require separate host evidence.
func (Catalog) NativePackages(platform prerequisites.Platform, requirements prerequisites.NativeRequirements) ([]prerequisites.NativePackage, error) {
	record, _, err := compiledCatalog()
	if err != nil {
		return nil, err
	}
	native, found := selectNative(record, platform)
	if !found || platform.Architecture != "amd64" {
		return nil, unsupportedPlatform()
	}
	return selectedNativePackages(native, requirements)
}

func compiledCatalog() (catalogRecord, string, error) {
	var record catalogRecord
	if json.Unmarshal(catalogData, &record) != nil || record.Format != "bootwright.controller.catalog-v1" || record.Projection != "python-wheel-files-v1" {
		return record, "", bundleFailure("compiled dependency catalog is invalid")
	}
	digest := sha256.New()
	digest.Write([]byte("bootwright.controller.catalog-with-automation-v1\x00"))
	digest.Write(catalogData)
	digest.Write([]byte{0})
	digest.Write([]byte(automation.Digest()))
	return record, hex.EncodeToString(digest.Sum(nil)), nil
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
	return desiredstate.NewFailureWithRemediation("controller.unsupported", "controller setup requires qualified RHEL 9.8 or Fedora 43 on Linux amd64", "", "Use a qualified installed bastion release and architecture.")
}

func bundleFailure(message string) error {
	return desiredstate.NewFailureWithRemediation("controller.setup", message, "", "Restore approved dependency sources or the exact retained bundle, then rerun setup with the same explicit context.")
}
