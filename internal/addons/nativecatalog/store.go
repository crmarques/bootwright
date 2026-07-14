package nativecatalog

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/host/managedroot"
	"github.com/crmarques/bootwright/internal/host/safefs"
	"github.com/crmarques/bootwright/internal/workspace"
)

const (
	DirName = "add-ons"

	MarkerName = ".bootwright-addon"

	dirMode  = 0o700
	fileMode = 0o600
)

func StoreDir() string {
	return filepath.Join(workspace.RootDir(), DirName)
}

func InstalledDir(name string) string {
	return filepath.Join(StoreDir(), name)
}

func ValidateStoreName(name string) error {
	if name == "" || name != filepath.Base(name) || strings.HasPrefix(name, ".") || strings.ContainsAny(name, `/\:`) {
		return fmt.Errorf("invalid add-on name %q", name)
	}
	return nil
}

func ensureStoreDir() error {
	if _, err := managedroot.Ensure(workspace.RootDir(), dirMode); err != nil {
		return err
	}
	if err := os.MkdirAll(StoreDir(), dirMode); err != nil {
		return fmt.Errorf("create add-ons store: %w", err)
	}
	return os.Chmod(StoreDir(), dirMode)
}

type Marker struct {
	Name          string
	Version       string
	ContentDigest string
}

type Installed struct {
	Marker   Marker
	Dir      string
	Modified bool
}

func Digest(files []File) string {
	sum := sha256.New()
	for _, file := range files {
		sum.Write([]byte(file.Path))
		sum.Write([]byte{0})
		sum.Write(file.Data)
		sum.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(sum.Sum(nil))
}

func Install(release Release) (dir string, err error) {
	if err := ValidateStoreName(release.Entry.Name); err != nil {
		return "", err
	}
	files, err := Files(release)
	if err != nil {
		return "", err
	}
	for _, file := range files {
		if err := validateCatalogFilePath(file.Path); err != nil {
			return "", err
		}
	}
	if err := ensureStoreDir(); err != nil {
		return "", err
	}
	dir = InstalledDir(release.Entry.Name)
	if err := os.RemoveAll(dir); err != nil {
		return "", fmt.Errorf("replace registered add-on %s: %w", release.Entry.Name, err)
	}
	for _, file := range files {
		rel := filepath.Clean(file.Path)
		dest := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(dest), dirMode); err != nil {
			return "", err
		}
		if err := safefs.AtomicWriteFile(dest, file.Data, fileMode); err != nil {
			return "", err
		}
	}
	marker := fmt.Sprintf("name=%s\nversion=%s\ncontentDigest=%s\n", release.Entry.Name, release.Version, Digest(files))
	if err := safefs.AtomicWriteFile(filepath.Join(dir, MarkerName), []byte(marker), fileMode); err != nil {
		return "", err
	}
	return dir, nil
}

func validateCatalogFilePath(path string) error {
	rel := filepath.Clean(path)
	if path == "" || rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("embedded add-on file path %q escapes the add-on directory", path)
	}
	if filepath.Base(rel) == MarkerName {
		return fmt.Errorf("embedded add-on file path %q uses reserved marker name %s", path, MarkerName)
	}
	return nil
}

func ReadMarker(dir string) (marker Marker, found bool, err error) {
	data, err := os.ReadFile(filepath.Join(dir, MarkerName))
	if errors.Is(err, os.ErrNotExist) {
		return Marker{}, false, nil
	}
	if err != nil {
		return Marker{}, false, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "name":
			marker.Name = value
		case "version":
			marker.Version = value
		case "contentDigest":
			marker.ContentDigest = value
		}
	}
	return marker, true, nil
}

func InstalledAddons() ([]Installed, error) {
	entries, err := os.ReadDir(StoreDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Installed
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(StoreDir(), entry.Name())
		marker, found, err := ReadMarker(dir)
		if err != nil || !found {
			continue
		}
		out = append(out, Installed{
			Marker:   marker,
			Dir:      dir,
			Modified: installedDigest(dir) != marker.ContentDigest,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Marker.Name < out[j].Marker.Name })
	return out, nil
}

func installedDigest(dir string) string {
	var files []File
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() == MarkerName {
			return nil
		}
		if d.Type() != 0 {
			return fmt.Errorf("%s is not a regular file", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files = append(files, File{Path: filepath.ToSlash(rel), Data: data})
		return nil
	})
	if err != nil {
		return ""
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return Digest(files)
}

func Remove(name string) error {
	if err := ValidateStoreName(name); err != nil {
		return err
	}
	dir := InstalledDir(name)
	_, found, err := ReadMarker(dir)
	if err != nil {
		return err
	}
	if !found {
		if _, statErr := os.Stat(dir); errors.Is(statErr, os.ErrNotExist) {
			return fmt.Errorf("add-on %q is not registered", name)
		}
		return fmt.Errorf("%s was not registered by bootwright add-ons add; remove it manually", dir)
	}
	return os.RemoveAll(dir)
}

func ReferencedStoreAddons(state v1alpha1.State) map[string]string {
	prefix := StoreDir() + string(filepath.Separator)
	out := map[string]string{}
	for _, addon := range state.ClusterAddons {
		if strings.HasPrefix(addon.SourcePath, prefix) {
			out[addon.Metadata.Name] = filepath.Dir(addon.SourcePath)
		}
	}
	return out
}
