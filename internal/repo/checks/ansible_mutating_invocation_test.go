package repocheck

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge"
)

func TestAnsibleMutatingRemediesUseResolvedInvocationFacts(t *testing.T) {
	names := []string{
		converge.MutatingInvocationExtraVar,
		converge.ApplyReconcileInvocationExtraVar,
		converge.ApplyRebuildInvocationExtraVar,
		converge.ApplyFullInvocationExtraVar,
		converge.ApplyThroughBaseInvocationExtraVar,
	}
	docs := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/docs/vars-contract.md")
	producer := readRepoFile(t, "internal/cli/mutating_invocation_extra_vars.go")
	allowed := map[string]bool{}
	for _, name := range names {
		allowed[name] = true
		if !strings.Contains(docs, "`"+name+"`") {
			t.Errorf("vars contract does not publish %s", name)
		}
		if !strings.Contains(producer, "converge."+mutatingInvocationConstantName(name)) {
			t.Errorf("CLI producer does not emit %s", name)
		}
	}
	for _, command := range []string{"scope_apply_cmd.go", "scope_destroy_cmd.go"} {
		if body := readRepoFile(t, "internal/cli/"+command); !strings.Contains(body, "appendMutatingInvocationExtraVars(&plan, invocation)") {
			t.Errorf("%s does not attach resolved invocation facts before Ansible execution", command)
		}
	}
	root := filepath.Join(repoRoot(t), "ansible/collections/ansible_collections/bootwright/core/roles")
	factPattern := regexp.MustCompile(`bootwright_[a-z0-9_]*invocation`)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || (filepath.Ext(path) != ".yml" && filepath.Ext(path) != ".yaml") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for lineNo, line := range strings.Split(string(body), "\n") {
			if (strings.Contains(line, "bootwright apply") || strings.Contains(line, "bootwright destroy")) && !strings.Contains(line, "_invocation") {
				t.Errorf("%s:%d assembles a mutating remedy instead of using a resolved invocation fact: %s", path, lineNo+1, strings.TrimSpace(line))
			}
			for _, name := range factPattern.FindAllString(line, -1) {
				if !allowed[name] {
					t.Errorf("%s:%d consumes undocumented mutating invocation fact %s", path, lineNo+1, name)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func mutatingInvocationConstantName(name string) string {
	switch name {
	case converge.MutatingInvocationExtraVar:
		return "MutatingInvocationExtraVar"
	case converge.ApplyReconcileInvocationExtraVar:
		return "ApplyReconcileInvocationExtraVar"
	case converge.ApplyRebuildInvocationExtraVar:
		return "ApplyRebuildInvocationExtraVar"
	case converge.ApplyFullInvocationExtraVar:
		return "ApplyFullInvocationExtraVar"
	case converge.ApplyThroughBaseInvocationExtraVar:
		return "ApplyThroughBaseInvocationExtraVar"
	default:
		return ""
	}
}
