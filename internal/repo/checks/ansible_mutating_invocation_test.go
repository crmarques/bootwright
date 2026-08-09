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
		converge.ApplyReclaimInvocationExtraVar,
		converge.ApplyFullInvocationExtraVar,
		converge.ApplyThroughBaseInvocationExtraVar,
		converge.ArbiterDegradedInvocationExtraVar,
		converge.ArbiterSameSiteInvocationExtraVar,
		converge.ArbiterUnreachableInvocationExtraVar,
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
		if body := readRepoFile(t, "internal/cli/"+command); !strings.Contains(body, "appendMutatingInvocationExtraVars(&plan, invocation,") {
			t.Errorf("%s does not attach resolved invocation facts before Ansible execution", command)
		}
	}
	gate := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/osd_filter_gate.yml")
	for _, want := range []string{converge.ApplyReclaimInvocationExtraVar, converge.ApplyReclaimDevicesExtraVar, converge.ApplyReclaimInvocationSentinel} {
		if !strings.Contains(gate, want) {
			t.Errorf("dynamic OSD filter gate does not consume registered runtime reclaim value %s", want)
		}
	}
	root := filepath.Join(repoRoot(t), "ansible/collections/ansible_collections/bootwright/core/roles")
	strictResolvedRemedyPaths := map[string]bool{
		filepath.Join(root, "machine_substrate_libvirt/tasks/machine.yml"): true,
		filepath.Join(root, "machine_substrate_vsphere/tasks/gate.yml"):    true,
		filepath.Join(root, "machine_substrate_vsphere/tasks/probe.yml"):   true,
		filepath.Join(root, "ownership_record/tasks/apply_mode_gate.yml"):  true,
	}
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
		if strictResolvedRemedyPaths[path] {
			for _, forbidden := range []string{
				"_invocation | default",
				"re-run apply",
				"then re-apply",
				"same --mode create command",
				"same bootwright apply invocation",
			} {
				if strings.Contains(string(body), forbidden) {
					t.Errorf("%s contains generic apply remedy %q instead of a required resolved invocation fact", path, forbidden)
				}
			}
		}
		for lineNo, line := range strings.Split(string(body), "\n") {
			if (strings.Contains(line, "bootwright apply") || strings.Contains(line, "bootwright destroy") || strings.Contains(line, "bootwright storage-cluster replace-arbiter")) && !strings.Contains(line, "_invocation") {
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
	case converge.ApplyReclaimInvocationExtraVar:
		return "ApplyReclaimInvocationExtraVar"
	case converge.ApplyFullInvocationExtraVar:
		return "ApplyFullInvocationExtraVar"
	case converge.ApplyThroughBaseInvocationExtraVar:
		return "ApplyThroughBaseInvocationExtraVar"
	case converge.ArbiterDegradedInvocationExtraVar:
		return "ArbiterDegradedInvocationExtraVar"
	case converge.ArbiterSameSiteInvocationExtraVar:
		return "ArbiterSameSiteInvocationExtraVar"
	case converge.ArbiterUnreachableInvocationExtraVar:
		return "ArbiterUnreachableInvocationExtraVar"
	default:
		return ""
	}
}

func TestArbiterBackendsCarryEvidenceInsteadOfMutatingArgv(t *testing.T) {
	root := repoRoot(t)
	paths := []string{
		filepath.Join(root, "internal/converge/storage_replace_arbiter.go"),
		filepath.Join(root, "internal/converge/workflow/apply_mode_preflight.go"),
	}
	storageRoot := filepath.Join(root, "internal/storage/arbiter")
	err := filepath.WalkDir(storageRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && filepath.Ext(path) == ".go" && !strings.HasSuffix(path, "_test.go") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	runnable := regexp.MustCompile(`(?i)bootwright\s+(apply|destroy)\s+--|bootwright\s+storage-cluster\s+replace-arbiter\s+--|\[\]string\s*\{\s*"bootwright"`)
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if match := runnable.Find(body); match != nil {
			t.Errorf("%s assembles mutating Bootwright argv %q; return typed evidence to internal/cli", path, match)
		}
		if strings.Contains(string(body), "TiebreakerReplacementCommand") {
			t.Errorf("%s restored the removed context-free replacement command helper", path)
		}
	}
}
