package repocheck

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// mkdocs cannot read assets outside docs_dir, so the README's images/ and the
// site's docs/assets/images/ keep their own copies of the shared logo/overview
// art. Any asset present in both directories must stay byte-identical so the
// GitHub README and the published site never drift to different diagrams.
func TestSharedImageAssetsStayIdentical(t *testing.T) {
	root := repoRoot(t)
	repoImages := filepath.Join(root, "images")
	docsImages := filepath.Join(root, "docs", "assets", "images")
	entries, err := os.ReadDir(repoImages)
	if err != nil {
		t.Fatalf("read %s: %v", repoImages, err)
	}
	shared := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		docsCopy := filepath.Join(docsImages, entry.Name())
		if _, err := os.Stat(docsCopy); err != nil {
			continue
		}
		shared++
		a, err := os.ReadFile(filepath.Join(repoImages, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		b, err := os.ReadFile(docsCopy)
		if err != nil {
			t.Fatalf("read %s: %v", docsCopy, err)
		}
		if !bytes.Equal(a, b) {
			t.Fatalf("images/%s and docs/assets/images/%s have drifted; keep the shared asset identical", entry.Name(), entry.Name())
		}
	}
	if shared == 0 {
		t.Fatal("expected at least one shared asset between images/ and docs/assets/images/")
	}
}
