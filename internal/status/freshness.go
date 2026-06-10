package status

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"go.yaml.in/yaml/v3"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

const (
	InstallerFreshnessFresh   = "fresh"
	InstallerFreshnessStale   = "stale"
	InstallerFreshnessMissing = "missing"
	InstallerFreshnessUnknown = "unknown"
)

type EffectiveStateFreshness struct {
	State string
	Path  string
	Error string
}

func FreshnessForInstaller(freshness EffectiveStateFreshness, installerPath string) EffectiveStateFreshness {
	exists, err := regularFileExists(installerPath)
	if err != nil {
		return EffectiveStateFreshness{State: InstallerFreshnessUnknown, Path: installerPath, Error: err.Error()}
	}
	if !exists {
		return EffectiveStateFreshness{State: InstallerFreshnessMissing}
	}
	return freshness
}

func LoadEffectiveStateFreshness(current v1alpha1.State, renderedDir string) EffectiveStateFreshness {
	path := filepath.Join(renderedDir, "effective-state.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return EffectiveStateFreshness{State: InstallerFreshnessUnknown, Path: path, Error: err.Error()}
	}
	var rendered v1alpha1.State
	if err := yaml.Unmarshal(data, &rendered); err != nil {
		return EffectiveStateFreshness{State: InstallerFreshnessUnknown, Path: path, Error: err.Error()}
	}
	expected := stateFreshnessShape(current)
	rendered = stateFreshnessShape(rendered)
	if reflect.DeepEqual(rendered, expected) {
		return EffectiveStateFreshness{State: InstallerFreshnessFresh, Path: path}
	}
	return EffectiveStateFreshness{State: InstallerFreshnessStale, Path: path}
}

func stateFreshnessShape(state v1alpha1.State) v1alpha1.State {
	// Clear computed bookkeeping (SourcePath, normalize-injected DefaultedRefs)
	// on every loaded resource so freshness compares only the authored fields.
	// Both carry `yaml:"-"`, so they are absent from the round-tripped
	// effective-state.yaml; clearing them on the in-memory side too keeps the
	// DeepEqual honest. Reflecting over State's per-kind slices ensures a newly
	// added kind can never be silently omitted from this normalization (the
	// previous explicit list missed all storage and add-on kinds, which made
	// status falsely report "stale" for any storage/add-on-bearing state).
	v := reflect.ValueOf(&state).Elem()
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		if field.Kind() != reflect.Slice {
			continue
		}
		for j := 0; j < field.Len(); j++ {
			elem := field.Index(j)
			if elem.Kind() != reflect.Struct {
				continue
			}
			for _, name := range []string{"SourcePath", "DefaultedRefs"} {
				if f := elem.FieldByName(name); f.IsValid() && f.CanSet() {
					f.Set(reflect.Zero(f.Type()))
				}
			}
		}
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
