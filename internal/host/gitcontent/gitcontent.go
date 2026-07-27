package gitcontent

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	SchemeHTTPS = "https"
	SchemeSSH   = "ssh"
	SchemeLocal = "local"
)

var CommitRe = regexp.MustCompile(`^[0-9a-f]{40}$`)

var scpLikeRe = regexp.MustCompile(`^[^/@]+@[^/:]+:.+$`)

func Scheme(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("url is required")
	}
	switch {
	case strings.HasPrefix(value, "https://"):
		return SchemeHTTPS, nil
	case strings.HasPrefix(value, "ssh://"):
		return SchemeSSH, nil
	case strings.HasPrefix(value, "file://"):
		return SchemeLocal, nil
	case strings.HasPrefix(value, "http://"):
		return "", fmt.Errorf("url %q uses http; use https so the fetch is encrypted", value)
	case strings.Contains(value, "::"):
		return "", fmt.Errorf("url %q uses a git transport helper, which is not supported", value)
	case strings.Contains(value, "://"):
		parsed, err := url.Parse(value)
		if err != nil {
			return "", fmt.Errorf("url %q is not a valid URL: %w", value, err)
		}
		return "", fmt.Errorf("url scheme %q is not supported; use https, ssh, file, or an absolute local path", parsed.Scheme)
	case scpLikeRe.MatchString(value):
		return SchemeSSH, nil
	case filepath.IsAbs(value):
		return SchemeLocal, nil
	default:
		return "", fmt.Errorf("url %q must be an https, ssh, or file URL, or an absolute local path", value)
	}
}

func IsLocal(raw string) bool {
	scheme, err := Scheme(raw)
	return err == nil && scheme == SchemeLocal
}

func LocalPath(raw string) string {
	value := strings.TrimSpace(raw)
	return strings.TrimPrefix(value, "file://")
}

func ValidateSubdir(subdir string) error {
	value := strings.TrimSpace(subdir)
	if value == "" {
		return nil
	}
	if filepath.IsAbs(value) {
		return fmt.Errorf("subdir %q must be a relative path", subdir)
	}
	clean := filepath.Clean(value)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("subdir %q must stay within the repository", subdir)
	}
	return nil
}
