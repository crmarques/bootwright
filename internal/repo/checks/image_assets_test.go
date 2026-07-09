package repocheck

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var readmeImageSrc = regexp.MustCompile(`<img[^>]*\bsrc="([^"]+)"`)

func TestReadmeImagesResolve(t *testing.T) {
	root := repoRoot(t)
	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}

	checked := 0
	for _, match := range readmeImageSrc.FindAllStringSubmatch(string(readme), -1) {
		src := match[1]
		if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
			continue
		}
		checked++
		path := filepath.Join(root, filepath.FromSlash(src))
		if _, err := os.Stat(path); err != nil {
			t.Errorf("README image %q does not resolve to a file: %v", src, err)
		}
	}
	if checked == 0 {
		t.Fatal("expected at least one local image reference in README.md")
	}
}
