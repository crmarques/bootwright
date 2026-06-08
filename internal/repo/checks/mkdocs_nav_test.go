package repocheck

import (
	"regexp"
	"testing"
)

var mkdocsNavDocPattern = regexp.MustCompile(`[A-Za-z0-9._/-]+\.md`)

func TestMkdocsNavTargetsExist(t *testing.T) {
	body := readRepoFile(t, "mkdocs.yml")
	seen := map[string]bool{}
	for _, target := range mkdocsNavDocPattern.FindAllString(body, -1) {
		if seen[target] {
			continue
		}
		seen[target] = true
		if !repoFileExists(t, "docs/"+target) {
			t.Errorf("mkdocs.yml references %q but docs/%s does not exist; mkdocs build --strict would fail at publish", target, target)
		}
	}
}
