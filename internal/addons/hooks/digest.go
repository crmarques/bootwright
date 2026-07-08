package hooks

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// ContentDigest is a best-effort sha256 over a hook's shipped content: the
// playbook file, the vendored roles/collections trees, and every manifest
// template. Missing files contribute nothing (validation requires them for a
// real apply); the digest folds into the add-on DesiredHash and the per-hook
// record so an edit to shipped content re-runs the add-on under run: onChange.
func ContentDigest(addonSourcePath string, hook v1alpha1.ClusterAddonHook) string {
	base := filepath.Dir(addonSourcePath)
	sum := sha256.New()
	digestPath := func(rel string) {
		if strings.TrimSpace(rel) == "" {
			return
		}
		root := filepath.Join(base, rel)
		_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			relPath, _ := filepath.Rel(root, path)
			sum.Write([]byte(relPath))
			sum.Write([]byte{0})
			sum.Write(data)
			sum.Write([]byte{0})
			return nil
		})
	}
	digestPath(hook.Playbook)
	digestPath(hook.RolesPath)
	digestPath(hook.CollectionsPath)
	for _, manifest := range hook.Manifests {
		digestPath(manifest.Path)
	}
	return "sha256:" + hex.EncodeToString(sum.Sum(nil))
}
