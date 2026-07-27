package hooks

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func ContentDigest(addonSourcePath string, hook v1alpha1.ClusterAddonHook) (string, error) {
	base := filepath.Dir(addonSourcePath)
	sum := sha256.New()
	digestPath := func(rel string) error {
		if strings.TrimSpace(rel) == "" {
			return nil
		}
		root := filepath.Join(base, rel)
		if _, statErr := os.Lstat(root); errors.Is(statErr, fs.ErrNotExist) {
			return nil
		}
		return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return fmt.Errorf("scan hook content %s: %w", path, err)
			}
			if entry.IsDir() {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return fmt.Errorf("read hook content %s: %w", path, readErr)
			}
			relPath, _ := filepath.Rel(root, path)
			sum.Write([]byte(relPath))
			sum.Write([]byte{0})
			sum.Write(data)
			sum.Write([]byte{0})
			return nil
		})
	}
	paths := []string{hook.Playbook, hook.RolesPath, hook.CollectionsPath}
	for _, manifest := range hook.Manifests {
		paths = append(paths, manifest.Path)
	}
	for _, rel := range paths {
		if err := digestPath(rel); err != nil {
			return "", err
		}
	}
	return "sha256:" + hex.EncodeToString(sum.Sum(nil)), nil
}
