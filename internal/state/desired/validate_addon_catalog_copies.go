package desiredstate

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons/nativecatalog"
)

func validateAddonCatalogCopies(state v1alpha1.State) []string {
	var errs []string
	seen := map[string]bool{}
	for _, addon := range state.ClusterAddons {
		if addon.SourcePath == "" {
			continue
		}
		dir := filepath.Dir(addon.SourcePath)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		errs = append(errs, validateAddonCatalogCopy(addon.Metadata.Name, dir)...)
	}
	sort.Strings(errs)
	return errs
}

func validateAddonCatalogCopy(name, dir string) []string {
	marker, found, err := nativecatalog.ReadMarker(dir)
	if err != nil {
		return []string{fmt.Sprintf("ClusterAddon/%s carries a catalog registration marker at %s that cannot be read (%v); a copy Bootwright cannot identify cannot be proven current, so re-register it with `bootwright add-ons add --name %s --yes` and re-snapshot the context with `bootwright context update`", name, filepath.Join(dir, nativecatalog.MarkerName), err, name)}
	}
	if !found {
		return nil
	}
	fix := fmt.Sprintf("`sudo bootwright add-ons add --name %s --version %s --yes` re-registers this build's copy in the machine-local store, then `sudo bootwright context update --name <context> -f <input dir> --yes` re-snapshots it into the context input", marker.Name, marker.Version)

	onDisk, err := nativecatalog.DirDigest(dir)
	if err != nil {
		return []string{fmt.Sprintf("ClusterAddon/%s at %s cannot be digested (%v); it carries a catalog registration marker, so Bootwright must be able to prove what it holds. %s", name, dir, err, fix)}
	}
	if marker.ContentDigest != "" && onDisk != marker.ContentDigest {
		return []string{fmt.Sprintf("ClusterAddon/%s at %s no longer matches the registration marker beside it (%s on disk, %s recorded); the copy was edited or partially written after registration, and its playbooks are what apply actually runs. %s", name, dir, onDisk, marker.ContentDigest, fix)}
	}

	catalogDigest, err := nativecatalog.ReleaseDigest(marker.Name, marker.Version)
	if err != nil {
		return []string{fmt.Sprintf("ClusterAddon/%s at %s was registered from catalog entry %s %s, which this Bootwright build no longer offers (%v); run `bootwright add-ons list` for what it does offer", name, dir, marker.Name, marker.Version, err)}
	}
	if catalogDigest == onDisk {
		return nil
	}
	return []string{fmt.Sprintf("ClusterAddon/%s at %s predates this Bootwright build: it holds %s of catalog entry %s %s, and this build embeds %s. Registering and snapshotting are separate steps that neither a rebuild nor an apply repeats, so a fix shipped in the binary's catalog never reaches the run — the playbook that executes is this copy, and its failure will name line numbers from the older file. %s", name, dir, onDisk, marker.Name, marker.Version, catalogDigest, fix)}
}
