package desiredstate

import (
	"errors"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

type TolerantLoad struct {
	State   v1alpha1.State
	Skipped []error
}

func (l TolerantLoad) Degraded() bool {
	return len(l.Skipped) > 0
}

func (l TolerantLoad) SkippedDetails() []string {
	out := make([]string, 0, len(l.Skipped))
	for _, err := range l.Skipped {
		out = append(out, err.Error())
	}
	return out
}

func LoadNormalizeTolerant(paths []string) (TolerantLoad, error) {
	files, err := discoverFiles(paths)
	if err != nil {
		return TolerantLoad{}, err
	}
	var skipped []error
	selectedFiles, _, _, err := selectResourceFilesCollecting(files, &skipped)
	if err != nil {
		return TolerantLoad{}, err
	}
	state, err := loadFilesCollecting(selectedFiles, &skipped)
	if err != nil {
		return TolerantLoad{}, err
	}
	if err := resolveRegisteredAddons(&state); err != nil {
		skipped = append(skipped, err)
	}
	if errs := validateAuthoredNodeNames(state); len(errs) > 0 {
		skipped = append(skipped, errors.New(strings.Join(errs, "; ")))
	}
	if errs := validateAuthoredMachineAccess(state); len(errs) > 0 {
		skipped = append(skipped, errors.New(strings.Join(errs, "; ")))
	}
	Normalize(&state)
	return TolerantLoad{State: applyEnvironmentClusterSelection(state), Skipped: skipped}, nil
}

func LoadNormalizeValidateTolerant(paths []string) (TolerantLoad, error) {
	loaded, err := LoadNormalizeTolerant(paths)
	if err != nil {
		return TolerantLoad{}, err
	}
	if verr := Validate(loaded.State); verr != nil {
		loaded.Skipped = append(loaded.Skipped, verr)
	}
	return loaded, nil
}
