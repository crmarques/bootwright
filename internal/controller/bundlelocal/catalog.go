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

type nativeRecord struct {
	OS        string                             `json:"os"`
	Release   string                             `json:"release"`
	Execution prerequisites.ExecutionRequirement `json:"execution"`
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
