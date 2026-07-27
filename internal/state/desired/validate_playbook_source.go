package desiredstate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func validatePlaybookSource(prefix string, source *v1alpha1.PlaybookSource) []string {
	if source == nil {
		return nil
	}
	value := strings.TrimSpace(source.Path)
	if value == "" {
		return []string{fmt.Sprintf("%s.source must set path", prefix)}
	}
	if !filepath.IsAbs(value) {
		return []string{fmt.Sprintf("%s.source.path %q must be an absolute directory outside the input tree", prefix, source.Path)}
	}
	info, err := os.Lstat(value)
	if err != nil {
		return []string{fmt.Sprintf("%s.source.path %q does not exist: %v", prefix, source.Path, err)}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return []string{fmt.Sprintf("%s.source.path %q must not be a symlink", prefix, source.Path)}
	}
	if !info.IsDir() {
		return []string{fmt.Sprintf("%s.source.path %q must name a directory", prefix, source.Path)}
	}
	return nil
}

func validateSourcedContent(prefix string, source *v1alpha1.PlaybookSource, playbook, rolesPath, collectionsPath string) []string {
	var errs []string
	for _, entry := range []struct {
		field string
		value string
		file  bool
	}{
		{"playbook", playbook, true},
		{"rolesPath", rolesPath, false},
		{"collectionsPath", collectionsPath, false},
	} {
		if strings.TrimSpace(entry.value) == "" {
			if entry.file {
				errs = append(errs, fmt.Sprintf("%s.playbook is required", prefix))
			}
			continue
		}
		if filepath.IsAbs(entry.value) {
			errs = append(errs, fmt.Sprintf("%s.%s %q must be a relative path inside the source", prefix, entry.field, entry.value))
			continue
		}
		clean := filepath.Clean(entry.value)
		if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			errs = append(errs, fmt.Sprintf("%s.%s %q must stay within the source root", prefix, entry.field, entry.value))
		}
	}
	if strings.TrimSpace(playbook) != "" && !isYAMLFile(filepath.Clean(playbook)) {
		errs = append(errs, fmt.Sprintf("%s.playbook %q is not a .yaml or .yml file", prefix, playbook))
	}
	if !v1alpha1.PlaybookSourceIsSet(source) {
		return errs
	}
	if strings.TrimSpace(source.Path) == "" {
		return errs
	}
	root := source.Path
	if strings.TrimSpace(playbook) != "" {
		errs = append(errs, validateContainedFile(prefix+".playbook", root, playbook, true)...)
	}
	if strings.TrimSpace(rolesPath) != "" {
		errs = append(errs, validateContainedDir(prefix+".rolesPath", root, rolesPath)...)
	}
	if strings.TrimSpace(collectionsPath) != "" {
		errs = append(errs, validateContainedDir(prefix+".collectionsPath", root, collectionsPath)...)
	}
	return errs
}
