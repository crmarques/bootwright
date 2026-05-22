package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"go.yaml.in/yaml/v3"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/provisioning/render"
)

const (
	installerFreshnessFresh   = "fresh"
	installerFreshnessStale   = "stale"
	installerFreshnessMissing = "missing"
	installerFreshnessUnknown = "unknown"
)

type effectiveStateFreshness struct {
	State string
	Path  string
	Error string
}

func freshnessForInstaller(freshness effectiveStateFreshness, installerPath string) effectiveStateFreshness {
	exists, err := regularFileExists(installerPath)
	if err != nil {
		return effectiveStateFreshness{State: installerFreshnessUnknown, Path: installerPath, Error: err.Error()}
	}
	if !exists {
		return effectiveStateFreshness{State: installerFreshnessMissing}
	}
	return freshness
}

func loadEffectiveStateFreshness(current v1alpha1.State, stateDir string) effectiveStateFreshness {
	path := filepath.Join(stateDir, "effective-state.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return effectiveStateFreshness{State: installerFreshnessUnknown, Path: path, Error: err.Error()}
	}
	var rendered v1alpha1.State
	if err := yaml.Unmarshal(data, &rendered); err != nil {
		return effectiveStateFreshness{State: installerFreshnessUnknown, Path: path, Error: err.Error()}
	}
	expected := stateFreshnessShape(current)
	rendered = stateFreshnessShape(rendered)
	if reflect.DeepEqual(rendered, expected) {
		return effectiveStateFreshness{State: installerFreshnessFresh, Path: path}
	}
	return effectiveStateFreshness{State: installerFreshnessStale, Path: path}
}

func stateFreshnessShape(state v1alpha1.State) v1alpha1.State {
	render.ApplySharedOverlays(&state)
	for i := range state.Environments {
		state.Environments[i].SourcePath = ""
	}
	for i := range state.Hosts {
		state.Hosts[i].SourcePath = ""
	}
	for i := range state.NetworkConfigs {
		state.NetworkConfigs[i].SourcePath = ""
	}
	for i := range state.InfraProviders {
		state.InfraProviders[i].SourcePath = ""
	}
	for i := range state.ClusterInfras {
		state.ClusterInfras[i].SourcePath = ""
	}
	for i := range state.ContainerClusters {
		state.ContainerClusters[i].SourcePath = ""
	}
	return state
}

func regularFileExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat %s: %w", path, err)
	}
	if info.IsDir() {
		return false, fmt.Errorf("%s is a directory; expected a file", path)
	}
	return true, nil
}
