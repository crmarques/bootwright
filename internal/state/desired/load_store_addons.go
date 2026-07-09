package desiredstate

import (
	"fmt"
	"os"
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons/nativecatalog"
)

// resolveRegisteredAddons appends machine-registered native add-ons
// (`bootwright add-ons add`) for the binding/profile addonRefs that no
// authored ClusterAddon matches. The registered directory loads like any
// authored add-on dir — SourcePath anchors its shipped playbooks/manifests —
// and context init later snapshots it into the context input tree, after
// which the in-tree copy resolves the reference and the store is not
// consulted. A store that is absent or unreadable (a rootless run cannot
// traverse the root-owned Bootwright dir) falls through to the normal
// unresolved-reference validation error.
func resolveRegisteredAddons(state *v1alpha1.State) error {
	authored := map[string]bool{}
	for _, addon := range state.ClusterAddons {
		authored[addon.Metadata.Name] = true
	}
	referenced := map[string]bool{}
	for _, binding := range state.ClusterAddonBindings {
		for _, addon := range binding.Spec.Addons {
			referenced[addon.AddonRef.Name] = true
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
		dir := nativecatalog.InstalledDir(name)
		if _, err := os.Stat(dir); err != nil {
			continue
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
