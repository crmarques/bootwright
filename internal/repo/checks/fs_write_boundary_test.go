package repocheck

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var mutatingFSCalls = []string{
	"os.WriteFile(", "os.Create(", "os.CreateTemp(",
	"os.Mkdir(", "os.MkdirAll(", "os.MkdirTemp(",
	"os.OpenFile(", "os.Rename(", "os.Truncate(",
	"os.Remove(", "os.RemoveAll(", "os.Symlink(", "os.Link(",
	"os.Chmod(", "os.Chown(", "os.WriteString(",
	"ioutil.WriteFile(", "ioutil.TempFile(", "ioutil.TempDir(",
}

var fsWriteAllowedPrefixes = []string{
	"internal/host/",
}

var fsWriteAllowedPackages = map[string]bool{
	"internal/addons/nativecatalog": true,
	"internal/addons/oc":            true,
	"internal/cli":                  true,
	"internal/converge":             true,
	"internal/converge/ansible":     true,
	"internal/converge/bundle":      true,
	"internal/converge/workflow":    true,
	"internal/infra/media":          true,
	"internal/render":               true,
	"internal/secrets":              true,
	"internal/sshtrust":             true,
	"internal/workspace":            true,
}

func TestFilesystemWritesConfinedToHostAndAllowlist(t *testing.T) {
	root := repoRoot(t)
	offenders := map[string][]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if skipGoImportDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !strings.HasPrefix(rel, "api/") && !strings.HasPrefix(rel, "cmd/") && !strings.HasPrefix(rel, "internal/") {
			return nil
		}
		pkg := filepath.ToSlash(filepath.Dir(rel))
		if fsWriteAllowedPackages[pkg] {
			return nil
		}
		for _, prefix := range fsWriteAllowedPrefixes {
			if strings.HasPrefix(rel, prefix) {
				return nil
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(data)
		for _, call := range mutatingFSCalls {
			if strings.Contains(text, call) {
				offenders[rel] = append(offenders[rel], strings.TrimSuffix(call, "("))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) == 0 {
		return
	}
	files := make([]string, 0, len(offenders))
	for f := range offenders {
		files = append(files, f)
	}
	sort.Strings(files)
	var b strings.Builder
	for _, f := range files {
		b.WriteString("\n  ")
		b.WriteString(f)
		b.WriteString(" (")
		b.WriteString(strings.Join(offenders[f], ", "))
		b.WriteString(")")
	}
	t.Fatalf("direct filesystem writes outside internal/host and the fs-write allowlist; route through internal/host/safefs or managedroot, or add the package to fsWriteAllowedPackages deliberately:%s", b.String())
}
