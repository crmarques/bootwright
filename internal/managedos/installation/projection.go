package installation

import (
	"encoding/json"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const (
	profileRefField = "spec.os.installProfileRef"
	releaseFamily   = "rhel"
)

func (Capability) ProjectedPaths() []lifecycle.ProjectedPath {
	return []lifecycle.ProjectedPath{{Kind: api.MachineInstallProfile, Path: "spec.os.version"}}
}

func (Capability) Project(applied api.Catalog, block reconciliation.Block, evidence json.RawMessage) ([]lifecycle.Projection, error) {
	request, err := DecodeRequest(block.Request)
	if err != nil {
		return nil, err
	}
	observed, err := decodeEvidence(evidence, block.RequestDigest)
	if err != nil {
		return nil, err
	}
	name := request.Identity.Object
	machine, found := applied.Find(api.Machine, name)
	if !found {
		return nil, nil
	}
	profile := machine.Spec().Get("os", "installProfileRef").Text()
	if profile == "" || observed.OS == (OSEvidence{}) {
		return nil, nil
	}
	return []lifecycle.Projection{{
		Kind: api.MachineInstallProfile, Name: profile,
		Observed: releaseFragment(observed.OS), Notes: releaseNotes(name, observed.OS),
		Holder: &lifecycle.ProjectionHolder{Kind: api.Machine, Name: name, Field: profileRefField},
	}}, nil
}

func releaseFragment(system OSEvidence) api.Value {
	if !updateRelease.MatchString(system.VersionID) {
		return api.MapValue()
	}
	return api.MapValue(api.FieldValue{Name: "os", Value: api.MapValue(
		api.FieldValue{Name: "version", Value: api.StringValue(system.VersionID)},
	)})
}

func releaseNotes(machine string, system OSEvidence) []string {
	var notes []string
	if system.Kernel != "" {
		notes = append(notes, machine+": kernel "+system.Kernel)
	}
	if system.ID != releaseFamily {
		notes = append(notes, machine+": /etc/os-release ID "+named(system.ID)+" is not "+releaseFamily)
	}
	if !updateRelease.MatchString(system.VersionID) {
		notes = append(notes, machine+": /etc/os-release VERSION_ID "+named(system.VersionID)+" is not <major>.<minor>")
	}
	return notes
}

func named(value string) string {
	if value == "" {
		return "none"
	}
	return value
}
