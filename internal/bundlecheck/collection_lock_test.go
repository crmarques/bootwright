package bundlecheck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestAnsibleCollectionLockMatchesEmbeddedManifest(t *testing.T) {
	if !repoFileExists(t, "internal/embedded/bundle/collections/ansible_collections") {
		t.Skip("embedded collection bundle has not been synced")
	}

	var requirements struct {
		Collections []struct {
			Name string `yaml:"name"`
			Type string `yaml:"type"`
		} `yaml:"collections"`
	}
	if err := yaml.Unmarshal([]byte(readRepoFile(t, "ansible/collections/requirements.yml")), &requirements); err != nil {
		t.Fatalf("decode requirements.yml: %v", err)
	}

	var lock struct {
		Collections []struct {
			Name                string `yaml:"name"`
			Version             string `yaml:"version"`
			Source              string `yaml:"source"`
			FilesManifestSHA256 string `yaml:"filesManifestSha256"`
		} `yaml:"collections"`
	}
	if err := yaml.Unmarshal([]byte(readRepoFile(t, "ansible/collections/requirements.lock.yml")), &lock); err != nil {
		t.Fatalf("decode requirements.lock.yml: %v", err)
	}

	type collectionLockEntry struct {
		Name                string
		Version             string
		Source              string
		FilesManifestSHA256 string
	}
	locked := map[string]collectionLockEntry{}
	for _, entry := range lock.Collections {
		if entry.Name == "" || entry.Version == "" || entry.Source == "" {
			t.Fatalf("collection lock entry is incomplete: %+v", entry)
		}
		if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(entry.FilesManifestSHA256) {
			t.Fatalf("collection lock entry %s has invalid filesManifestSha256 %q", entry.Name, entry.FilesManifestSHA256)
		}
		locked[entry.Source] = collectionLockEntry{
			Name:                entry.Name,
			Version:             entry.Version,
			Source:              entry.Source,
			FilesManifestSHA256: entry.FilesManifestSHA256,
		}
	}

	for _, requirement := range requirements.Collections {
		if requirement.Type != "url" {
			t.Fatalf("collection requirement %s must remain exact URL pinned", requirement.Name)
		}
		namespace, name, version, ok := parseGalaxyTarballURL(requirement.Name)
		if !ok {
			t.Fatalf("collection requirement is not an exact Galaxy tarball URL: %s", requirement.Name)
		}
		lockEntry, ok := locked[requirement.Name]
		if !ok {
			t.Fatalf("collection requirement %s is missing from requirements.lock.yml", requirement.Name)
		}
		wantName := namespace + "." + name
		if lockEntry.Name != wantName || lockEntry.Version != version {
			t.Fatalf("collection lock entry for %s got %s %s, want %s %s", requirement.Name, lockEntry.Name, lockEntry.Version, wantName, version)
		}

		var manifest struct {
			CollectionInfo struct {
				Namespace string `json:"namespace"`
				Name      string `json:"name"`
				Version   string `json:"version"`
			} `json:"collection_info"`
			FileManifestFile struct {
				ChksumType   string `json:"chksum_type"`
				ChksumSHA256 string `json:"chksum_sha256"`
			} `json:"file_manifest_file"`
		}
		manifestPath := filepath.Join("internal/embedded/bundle/collections/ansible_collections", namespace, name, "MANIFEST.json")
		if err := json.Unmarshal([]byte(readRepoFile(t, manifestPath)), &manifest); err != nil {
			t.Fatalf("decode %s: %v", manifestPath, err)
		}
		if manifest.CollectionInfo.Namespace != namespace || manifest.CollectionInfo.Name != name || manifest.CollectionInfo.Version != version {
			t.Fatalf("%s collection_info = %s.%s %s, want %s.%s %s", manifestPath, manifest.CollectionInfo.Namespace, manifest.CollectionInfo.Name, manifest.CollectionInfo.Version, namespace, name, version)
		}
		if manifest.FileManifestFile.ChksumType != "sha256" || manifest.FileManifestFile.ChksumSHA256 != lockEntry.FilesManifestSHA256 {
			t.Fatalf("%s file manifest checksum = %s:%s, want sha256:%s", manifestPath, manifest.FileManifestFile.ChksumType, manifest.FileManifestFile.ChksumSHA256, lockEntry.FilesManifestSHA256)
		}
	}
	if len(locked) != len(requirements.Collections) {
		t.Fatalf("requirements.lock.yml has %d entries, requirements.yml has %d", len(locked), len(requirements.Collections))
	}
}

func parseGalaxyTarballURL(url string) (string, string, string, bool) {
	match := regexp.MustCompile(`^https://galaxy\.ansible\.com/download/([a-z]+)-([a-z0-9_]+)-([0-9][A-Za-z0-9._-]*)\.tar\.gz$`).FindStringSubmatch(url)
	if match == nil {
		return "", "", "", false
	}
	return match[1], match[2], match[3], true
}

func repoFileExists(t *testing.T, rel string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(repoRoot(t), rel))
	return err == nil
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate repository root")
		}
		dir = parent
	}
}
