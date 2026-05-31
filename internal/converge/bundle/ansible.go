package bundle

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed ansible_bundle.zip
var bundleArchiveFS embed.FS

const bundleArchiveRelPath = "ansible_bundle.zip"

const AnsibleCfgRelPath = "ansible.cfg"

const bundleDigestRelPath = ".bootwright-bundle.sha256"
const bundleVersionRelPath = ".bootwright-bundle.version"

var errEmptyAnsibleBundle = errors.New("embedded ansible bundle is empty")

var RoleRelPaths = []string{
	filepath.Join("roles", "bastion"),
	filepath.Join("roles", "shared"),
	filepath.Join("roles", "providers"),
	filepath.Join("roles", "cluster_infra"),
	filepath.Join("roles", "openshift"),
	filepath.Join("roles", "storage"),
}

func RolesPath(bundleDir string) string {
	paths := make([]string, 0, len(RoleRelPaths))
	for _, rel := range RoleRelPaths {
		paths = append(paths, filepath.Join(bundleDir, rel))
	}
	return strings.Join(paths, string(os.PathListSeparator))
}

const CollectionsRelPath = "collections"

const FilterPluginsRelPath = "filter_plugins"

type AnsibleBundleResult struct {
	Dir    string
	Reused bool
	Files  int
	Bytes  int64
}

func EnsureAnsibleBundle(dest string, bundleVersion string) (AnsibleBundleResult, error) {
	archive, err := openAnsibleBundleArchive()
	if err != nil {
		return AnsibleBundleResult{}, err
	}
	digest, stats, err := ansibleBundleDigest(archive)
	if err != nil {
		return AnsibleBundleResult{}, err
	}
	result := AnsibleBundleResult{Dir: dest, Files: stats.files, Bytes: stats.bytes}
	if existingBundleMatches(dest, digest, bundleVersion) {
		result.Reused = true
		return result, nil
	}
	if err := extractAnsibleBundle(archive, dest, digest, bundleVersion); err != nil {
		return AnsibleBundleResult{}, err
	}
	return result, nil
}

func IsEmptyAnsibleBundle(err error) bool {
	return errors.Is(err, errEmptyAnsibleBundle)
}

func ExtractAnsibleBundle(dest string, bundleVersion string) error {
	result, err := EnsureAnsibleBundle(dest, bundleVersion)
	if err != nil {
		return err
	}
	if result.Reused {
		archive, err := openAnsibleBundleArchive()
		if err != nil {
			return err
		}
		digest, _, err := ansibleBundleDigest(archive)
		if err != nil {
			return err
		}
		return extractAnsibleBundle(archive, dest, digest, bundleVersion)
	}
	return nil
}

func openAnsibleBundleArchive() (*zip.Reader, error) {
	data, err := bundleArchiveFS.ReadFile(bundleArchiveRelPath)
	if err != nil {
		return nil, fmt.Errorf("locate embedded ansible bundle: %w", err)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w (rebuild bootwright via 'make build'): %w", errEmptyAnsibleBundle, err)
	}
	if findArchiveFile(archive, AnsibleCfgRelPath) == nil {
		return nil, fmt.Errorf("%w (rebuild bootwright via 'make build'): missing %s", errEmptyAnsibleBundle, AnsibleCfgRelPath)
	}
	return archive, nil
}

func extractAnsibleBundle(archive *zip.Reader, dest string, digest string, bundleVersion string) error {
	if err := os.RemoveAll(dest); err != nil {
		return fmt.Errorf("clear bundle destination %s: %w", dest, err)
	}
	for _, f := range sortedArchiveFiles(archive) {
		rel, ok := cleanArchivePath(f.Name)
		if !ok {
			return fmt.Errorf("embedded bundle contains unsafe path %q", f.Name)
		}
		target := filepath.Join(dest, rel)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create %s: %w", target, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(target), err)
		}
		data, err := readArchiveFile(f)
		if err != nil {
			return fmt.Errorf("read embedded %s: %w", rel, err)
		}
		if err := os.WriteFile(target, data, archiveFileMode(f)); err != nil {
			return fmt.Errorf("write %s: %w", target, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dest, bundleDigestRelPath), []byte(digest+"\n"), 0o644); err != nil {
		return fmt.Errorf("write bundle digest marker: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dest, bundleVersionRelPath), []byte(bundleVersion+"\n"), 0o644); err != nil {
		return fmt.Errorf("write bundle version marker: %w", err)
	}
	return nil
}

type ansibleBundleStats struct {
	files int
	bytes int64
}

func ansibleBundleDigest(archive *zip.Reader) (string, ansibleBundleStats, error) {
	hash := sha256.New()
	var stats ansibleBundleStats
	for _, f := range sortedArchiveFiles(archive) {
		if f.FileInfo().IsDir() {
			continue
		}
		rel, ok := cleanArchivePath(f.Name)
		if !ok {
			return "", ansibleBundleStats{}, fmt.Errorf("embedded bundle contains unsafe path %q", f.Name)
		}
		data, err := readArchiveFile(f)
		if err != nil {
			return "", ansibleBundleStats{}, fmt.Errorf("read embedded %s: %w", rel, err)
		}
		executable := "0"
		if f.Mode()&0o111 != 0 {
			executable = "1"
		}
		fmt.Fprintf(hash, "%s\x00%s\x00%d\x00", path.Clean(rel), executable, len(data))
		if _, err := hash.Write(data); err != nil {
			return "", ansibleBundleStats{}, err
		}
		if _, err := hash.Write([]byte{0}); err != nil {
			return "", ansibleBundleStats{}, err
		}
		stats.files++
		stats.bytes += int64(len(data))
	}
	return hex.EncodeToString(hash.Sum(nil)), stats, nil
}

func sortedArchiveFiles(archive *zip.Reader) []*zip.File {
	files := append([]*zip.File(nil), archive.File...)
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files
}

func findArchiveFile(archive *zip.Reader, rel string) *zip.File {
	clean := path.Clean(rel)
	for _, f := range archive.File {
		if path.Clean(f.Name) == clean && !f.FileInfo().IsDir() {
			return f
		}
	}
	return nil
}

func cleanArchivePath(raw string) (string, bool) {
	clean := path.Clean(strings.TrimPrefix(raw, "/"))
	if clean == "." || strings.HasPrefix(clean, "../") || clean == ".." {
		return "", false
	}
	return filepath.FromSlash(clean), true
}

func readArchiveFile(f *zip.File) ([]byte, error) {
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func archiveFileMode(f *zip.File) os.FileMode {
	mode := f.Mode().Perm()
	if mode == 0 {
		mode = 0o644
	}
	if f.Mode()&0o111 != 0 {
		mode |= 0o111
	}
	return mode
}

func existingBundleMatches(dest string, digest string, bundleVersion string) bool {
	data, err := os.ReadFile(filepath.Join(dest, bundleDigestRelPath))
	if err != nil || strings.TrimSpace(string(data)) != digest {
		return false
	}
	data, err = os.ReadFile(filepath.Join(dest, bundleVersionRelPath))
	if err != nil || strings.TrimSpace(string(data)) != strings.TrimSpace(bundleVersion) {
		return false
	}
	for _, rel := range []string{
		AnsibleCfgRelPath,
		filepath.Join("playbooks", "checks", "preflight.yml"),
		filepath.Join("playbooks", "targets", "infra", "apply.yml"),
	} {
		info, err := os.Stat(filepath.Join(dest, rel))
		if err != nil || info.IsDir() {
			return false
		}
	}
	return true
}
