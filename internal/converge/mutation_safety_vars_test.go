package converge

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var mutationSafetyVarPattern = regexp.MustCompile(`\bbootwright_[a-z0-9_]+\b`)

func TestMutationSafetyVarsStayClosedAcrossGoAnsibleAndDocs(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	registered := map[string]mutationSafetyVarClass{}
	validClasses := map[mutationSafetyVarClass]bool{
		mutationSafetyVarIntent:        true,
		mutationSafetyVarAuthorization: true,
		mutationSafetyVarScope:         true,
		mutationSafetyVarExecution:     true,
	}
	for _, contract := range mutationSafetyVars {
		if contract.Name == "" || !validClasses[contract.Class] {
			t.Errorf("invalid mutation safety variable contract: %#v", contract)
			continue
		}
		if prior, exists := registered[contract.Name]; exists {
			t.Errorf("mutation safety variable %s is registered twice (%s and %s)", contract.Name, prior, contract.Class)
		}
		registered[contract.Name] = contract.Class
	}

	goVars := mutationSafetyVarNames(t, filepath.Join(root, "internal"), func(path string, entry fs.DirEntry) bool {
		return strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") && entry.Name() != "mutation_safety_vars.go"
	})
	ansibleVars := mutationSafetyVarNames(t, filepath.Join(root, "ansible", "collections", "ansible_collections", "bootwright", "core"), func(path string, _ fs.DirEntry) bool {
		return strings.HasSuffix(path, ".yml") || strings.HasSuffix(path, ".yaml")
	})
	docsPath := filepath.Join(root, "ansible", "collections", "ansible_collections", "bootwright", "core", "docs", "vars-contract.md")
	docs, err := os.ReadFile(docsPath)
	if err != nil {
		t.Fatalf("read %s: %v", docsPath, err)
	}

	for name := range goVars {
		if mutationSafetyVarCandidate(name) {
			if _, exists := registered[name]; !exists {
				t.Errorf("Go produces mutation-control variable %s without registering it in mutationSafetyVars", name)
			}
		}
	}
	for name := range registered {
		if !goVars[name] {
			t.Errorf("registered mutation-control variable %s has no Go producer", name)
		}
		if !ansibleVars[name] {
			t.Errorf("registered mutation-control variable %s has no Ansible consumer", name)
		}
		if !strings.Contains(string(docs), "`"+name+"`") {
			t.Errorf("registered mutation-control variable %s is absent from %s", name, docsPath)
		}
	}
}

func mutationSafetyVarNames(t *testing.T, root string, include func(string, fs.DirEntry) bool) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !include(path, entry) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, name := range mutationSafetyVarPattern.FindAllString(string(data), -1) {
			out[name] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}
	return out
}

func mutationSafetyVarCandidate(name string) bool {
	for _, prefix := range []string{
		"bootwright_arbiter_",
		"bootwright_destroy_",
		"bootwright_task_",
	} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	for _, exact := range []string{
		"bootwright_agent_node_cluster_name",
		"bootwright_agent_node_machine_name",
		"bootwright_apply_mode",
		"bootwright_infra_component_apply_skip_records",
		"bootwright_infra_component_destroy_scope_records",
		"bootwright_infra_component_reclaim_records",
		"bootwright_infra_component_service_scope",
		"bootwright_infra_destroy_context_sweep",
		"bootwright_install_wait_target",
		"bootwright_machine_infra_records_only",
		"bootwright_machine_task_cluster_name",
		"bootwright_machine_task_machine_name",
		"bootwright_machine_task_provider_host_name",
	} {
		if name == exact {
			return true
		}
	}
	for _, token := range strings.Split(strings.TrimPrefix(name, "bootwright_"), "_") {
		if mutationSafetyVarTokens[token] {
			return true
		}
	}
	return false
}

var mutationSafetyVarTokens = map[string]bool{
	"acknowledge": true,
	"allow":       true,
	"approve":     true,
	"authorize":   true,
	"authorized":  true,
	"confirmed":   true,
	"delete":      true,
	"deprovision": true,
	"destroy":     true,
	"drop":        true,
	"erase":       true,
	"evict":       true,
	"force":       true,
	"format":      true,
	"only":        true,
	"overwrite":   true,
	"permit":      true,
	"prune":       true,
	"purge":       true,
	"rebuild":     true,
	"reclaim":     true,
	"recreate":    true,
	"reimage":     true,
	"reinstall":   true,
	"remove":      true,
	"replace":     true,
	"reset":       true,
	"scope":       true,
	"skip":        true,
	"terminate":   true,
	"undefine":    true,
	"uninstall":   true,
	"wipe":        true,
	"zap":         true,
}

func TestMutationSafetyVarCandidateCoversFutureProvidersAndTasks(t *testing.T) {
	for _, name := range []string{
		"bootwright_vsphere_wipe_disks",
		"bootwright_kubevirt_guest_scope",
		"bootwright_redfish_force",
		"bootwright_task_future_provider_machine",
	} {
		if !mutationSafetyVarCandidate(name) {
			t.Errorf("future mutation-control variable %s escaped registry classification", name)
		}
	}
	if mutationSafetyVarCandidate("bootwright_vsphere_datacenter") {
		t.Error("ordinary rendered provider data must not enter the mutation-control registry")
	}
}
