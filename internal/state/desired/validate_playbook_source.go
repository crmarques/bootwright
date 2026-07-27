package desiredstate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/host/gitcontent"
)

func validatePlaybookSource(prefix string, source *v1alpha1.PlaybookSource, secrets map[string]v1alpha1.Secret) []string {
	if source == nil {
		return nil
	}
	hasPath := strings.TrimSpace(source.Path) != ""
	hasGit := source.Git != nil
	switch {
	case hasPath && hasGit:
		return []string{fmt.Sprintf("%s.source must set exactly one of path or git, not both", prefix)}
	case !hasPath && !hasGit:
		return []string{fmt.Sprintf("%s.source must set path or git", prefix)}
	case hasGit:
		return validatePlaybookGitSource(prefix+".source.git", *source.Git, secrets)
	}
	value := strings.TrimSpace(source.Path)
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

func validatePlaybookGitSource(prefix string, git v1alpha1.PlaybookGitSource, secrets map[string]v1alpha1.Secret) []string {
	var errs []string
	scheme, err := gitcontent.Scheme(git.URL)
	if err != nil {
		errs = append(errs, fmt.Sprintf("%s.%v", prefix, err))
	}
	if strings.TrimSpace(git.Ref) == "" {
		errs = append(errs, fmt.Sprintf("%s.ref is required; name a commit, tag, or branch", prefix))
	}
	if err := gitcontent.ValidateSubdir(git.Subdir); err != nil {
		errs = append(errs, fmt.Sprintf("%s.%v", prefix, err))
	}
	if git.SecretRef == nil {
		return errs
	}
	name := git.SecretRef.Name
	declared, ok := secrets[name]
	if !ok {
		errs = append(errs, fmt.Sprintf("%s.secretRef %q does not match any Secret", prefix, name))
		return errs
	}
	switch declared.Spec.Type {
	case v1alpha1.SecretTypeToken, v1alpha1.SecretTypeUsernamePassword:
		if scheme == gitcontent.SchemeSSH {
			errs = append(errs, fmt.Sprintf("%s.secretRef %q is a %s secret, which authenticates https; use an sshKeyPair secret for an ssh url", prefix, name, declared.Spec.Type))
		}
	case v1alpha1.SecretTypeSSHKeyPair:
		if scheme == gitcontent.SchemeHTTPS {
			errs = append(errs, fmt.Sprintf("%s.secretRef %q is an sshKeyPair secret, which authenticates ssh; use a token or usernamePassword secret for an https url", prefix, name))
		}
	default:
		errs = append(errs, fmt.Sprintf("%s.secretRef %q has type %q; git authentication needs %s, %s, or %s", prefix, name, declared.Spec.Type,
			v1alpha1.SecretTypeToken, v1alpha1.SecretTypeUsernamePassword, v1alpha1.SecretTypeSSHKeyPair))
	}
	if scheme == gitcontent.SchemeLocal {
		errs = append(errs, fmt.Sprintf("%s.secretRef is not used for a local repository; remove it", prefix))
	}
	return errs
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
