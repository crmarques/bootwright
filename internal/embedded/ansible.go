package embedded

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

//go:embed all:bundle
var bundleFS embed.FS

const bundleRoot = "bundle"

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
	sub, err := fs.Sub(bundleFS, bundleRoot)
	if err != nil {
		return AnsibleBundleResult{}, fmt.Errorf("locate embedded ansible bundle: %w", err)
	}
	if _, err := fs.Stat(sub, AnsibleCfgRelPath); err != nil {
		return AnsibleBundleResult{}, fmt.Errorf("%w (rebuild bootwright via 'make build'): %w", errEmptyAnsibleBundle, err)
	}
	digest, stats, err := ansibleBundleDigest(sub)
	if err != nil {
		return AnsibleBundleResult{}, err
	}
	result := AnsibleBundleResult{Dir: dest, Files: stats.files, Bytes: stats.bytes}
	if existingBundleMatches(dest, digest, bundleVersion) {
		result.Reused = true
		return result, nil
	}
	if err := extractAnsibleBundle(sub, dest, digest, bundleVersion); err != nil {
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
		sub, err := fs.Sub(bundleFS, bundleRoot)
		if err != nil {
			return fmt.Errorf("locate embedded ansible bundle: %w", err)
		}
		digest, _, err := ansibleBundleDigest(sub)
		if err != nil {
			return err
		}
		return extractAnsibleBundle(sub, dest, digest, bundleVersion)
	}
	return nil
}

func extractAnsibleBundle(sub fs.FS, dest string, digest string, bundleVersion string) error {
	if err := os.RemoveAll(dest); err != nil {
		return fmt.Errorf("clear bundle destination %s: %w", dest, err)
	}
	if err := fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == "PLACEHOLDER" {
			return nil
		}
		target := filepath.Join(dest, path)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(sub, path)
		if err != nil {
			return fmt.Errorf("read embedded %s: %w", path, err)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(target), err)
		}
		mode := os.FileMode(0o644)
		if info, err := d.Info(); err == nil && info.Mode()&0o111 != 0 {
			mode = 0o755
		}
		if err := os.WriteFile(target, data, mode); err != nil {
			return fmt.Errorf("write %s: %w", target, err)
		}
		return nil
	}); err != nil {
		return err
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

func ansibleBundleDigest(sub fs.FS) (string, ansibleBundleStats, error) {
	hash := sha256.New()
	var stats ansibleBundleStats
	err := fs.WalkDir(sub, ".", func(rel string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if rel == "PLACEHOLDER" || d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(sub, rel)
		if err != nil {
			return fmt.Errorf("read embedded %s: %w", rel, err)
		}
		executable := "0"
		if info, err := d.Info(); err == nil && info.Mode()&0o111 != 0 {
			executable = "1"
		}
		fmt.Fprintf(hash, "%s\x00%s\x00%d\x00", path.Clean(rel), executable, len(data))
		if _, err := hash.Write(data); err != nil {
			return err
		}
		if _, err := hash.Write([]byte{0}); err != nil {
			return err
		}
		stats.files++
		stats.bytes += int64(len(data))
		return nil
	})
	if err != nil {
		return "", ansibleBundleStats{}, err
	}
	return hex.EncodeToString(hash.Sum(nil)), stats, nil
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
