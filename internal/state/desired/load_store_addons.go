package desiredstate

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons/nativecatalog"
)

func resolveRegisteredAddons(state *v1alpha1.State) error {
	authored := map[string]bool{}
	for _, addon := range state.ClusterAddons {
		authored[addon.Metadata.Name] = true
	}
	referenced := map[string]bool{}
	for _, binding := range state.ClusterAddonBindings {
		for _, ref := range binding.Spec.AddonRefs {
			referenced[ref.Name] = true
		}
		for _, config := range binding.Spec.AddonConfigs {
			referenced[config.AddonRef.Name] = true
		}
	}
	for _, profile := range state.ClusterAddonProfiles {
		for _, ref := range profile.Spec.AddonRefs {
			referenced[ref.Name] = true
		}
	}
	var missing []string
	for name := range referenced {
		if name != "" && !authored[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	for _, name := range missing {
		if nativecatalog.ValidateStoreName(name) != nil {
			continue
		}
		dir := nativecatalog.InstalledDir(name)
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		marker, found, err := nativecatalog.ReadMarker(dir)
		if err != nil {
			return fmt.Errorf("read registered add-on marker %s: %w", name, err)
		}
		if !found || marker.Name != name {
			return fmt.Errorf("add-on %q has a store directory at %s but no valid registration marker; it may be a partial or interrupted `bootwright add-ons add`. Remove it and re-register with: bootwright add-ons add --name %s", name, dir, name)
		}
		loaded, err := Load([]string{dir})
		if err != nil {
			return fmt.Errorf("load registered add-on %s: %w", name, err)
		}
		for _, addon := range loaded.ClusterAddons {
			if addon.Metadata.Name == name {
				state.ClusterAddons = append(state.ClusterAddons, addon)
			}
		}
	}
	return nil
}

func unresolvedAddonRemedy(name string) string {
	inCatalog := false
	if entries, err := nativecatalog.Entries(); err == nil {
		for _, entry := range entries {
			if entry.Name == name {
				inCatalog = true
				break
			}
		}
	}
	_, statErr := os.Stat(nativecatalog.InstalledDir(name))
	return addonRemedyForState(name, inCatalog, statErr)
}

func addonRemedyForState(name string, inCatalog bool, statErr error) string {
	if !inCatalog {
		return ""
	}
	if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
		return fmt.Sprintf("; a registered copy may exist but the add-on store %s is only readable as root — re-run as root, or snapshot the add-on into the input with bootwright context init/update", nativecatalog.StoreDir())
	}
	return fmt.Sprintf("; the native catalog ships it — register it with: bootwright add-ons add --name %s", name)
}
