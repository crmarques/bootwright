package repocheck

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The GitHub README is rendered from the repo root and now points at the
// published site's assets under docs/assets/images/ instead of keeping a
// duplicate top-level images/ copy. Guard that every local image the README
// references still resolves to a real file so the README art never 404s.
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
