package repocheck

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var retiredAuthoredVocabulary = []*regexp.Regexp{
	regexp.MustCompile(`kind:\s*Playbook\b`),
	regexp.MustCompile("`Playbook`"),
	regexp.MustCompile(`\bspec\.hooks\b`),
	regexp.MustCompile(`\bfailureMode\b`),
	regexp.MustCompile(`\bbootwright_hook_`),
	regexp.MustCompile(`\bspec\.ceph\.management\b`),
	regexp.MustCompile(`\bspec\.controlPlane\b`),
	regexp.MustCompile(`\bspec\.compute\[`),
	regexp.MustCompile(`\brequiredOverride\b`),
	regexp.MustCompile(`--converge-drifted`),
	regexp.MustCompile(`--expect-new`),
	regexp.MustCompile(`--confirm-data-loss`),
	regexp.MustCompile(`--include-unowned`),
	regexp.MustCompile(`--skip-unreachable`),
	regexp.MustCompile(`bootwright check `),
	regexp.MustCompile(`diff --live`),
	regexp.MustCompile(`(^|[^\w-])--force([^\w-]|$)`),
}

var externalToolForceContexts = []string{
	"wipefs",
	"vgremove",
	"pvremove",
	"lvremove",
	"cephadm",
	"ceph ",
	"rm-cluster",
	"subscription-manager",
	"virtctl",
	"kubectl",
	"podman",
	"git ",
	"--force-conflicts",
	"--force --fsid",
	"--force --grace-period",
}

func lineAllowsRetiredForce(line string) bool {
	for _, tool := range externalToolForceContexts {
		if strings.Contains(line, tool) {
			return true
		}
	}
	return false
}

func TestAgentStoreAvoidsRetiredAuthoredVocabulary(t *testing.T) {
	root := repoRoot(t)
	base := filepath.Join(root, ".agents")
	var findings []string
	err := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		for _, line := range strings.Split(string(body), "\n") {
			for _, pattern := range retiredAuthoredVocabulary {
				if !pattern.MatchString(line) {
					continue
				}
				if strings.Contains(pattern.String(), "--force") && lineAllowsRetiredForce(line) {
					continue
				}
				findings = append(findings, rel+": "+pattern.String()+": "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk .agents: %v", err)
	}
	if len(findings) == 0 {
		return
	}
	sort.Strings(findings)
	for _, f := range findings {
		t.Errorf("retired authored vocabulary in the agent store: %s", f)
	}
}
