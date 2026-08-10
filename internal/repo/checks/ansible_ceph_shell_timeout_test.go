package repocheck

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	convergeansible "github.com/crmarques/bootwright/internal/converge/ansible"
	"go.yaml.in/yaml/v3"
)

const cephShellCommandFloor = 125

type cephShellTimeoutClass struct {
	name       string
	variable   string
	mutating   bool
	candidates int
}

type cephShellWalkContext struct {
	rescue                any
	inheritedIgnoreErrors bool
	inheritedNoLog        bool
}

func TestAnsibleBoundsEveryCephadmShellCommand(t *testing.T) {
	root := repoRoot(t)
	base := filepath.Join(root, "ansible/collections/ansible_collections/bootwright/core")
	classes := cephShellTimeoutClasses()
	var candidates int
	var offenders []string
	err := filepath.WalkDir(base, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || (filepath.Ext(path) != ".yml" && filepath.Ext(path) != ".yaml") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		for {
			var document any
			if err := decoder.Decode(&document); err != nil {
				if err == io.EOF {
					break
				}
				return fmt.Errorf("parse %s: %w", rel, err)
			}
			walkCephShellCommands(document, cephShellWalkContext{}, func(task map[string]any, context cephShellWalkContext) {
				if problem := validateCephSurfaceInclude(task, context, rel); problem != "" {
					offenders = append(offenders, fmt.Sprintf("%s: %s: %s", rel, fmt.Sprint(task["name"]), problem))
				}
				argv, candidate, taskProblem := cephShellTaskArgv(task)
				if taskProblem != "" {
					offenders = append(offenders, fmt.Sprintf("%s: %s: %s", rel, fmt.Sprint(task["name"]), taskProblem))
					return
				}
				if !candidate {
					return
				}
				candidates++
				name := fmt.Sprint(task["name"])
				class, problem := validateCephShellTimeout(argv, task, classes)
				if problem != "" {
					offenders = append(offenders, fmt.Sprintf("%s: %s: %s", rel, name, problem))
					return
				}
				class.candidates++
				if context.inheritedNoLog {
					offenders = append(offenders, fmt.Sprintf("%s: %s: an enclosing no_log can censor the timeout relay; protect only the command task", rel, name))
				}
				if context.inheritedIgnoreErrors {
					offenders = append(offenders, fmt.Sprintf("%s: %s: an enclosing ignore_errors can swallow rc 124/137", rel, name))
				}
				if context.rescue != nil {
					if problem := validateCephTimeoutRescue(context.rescue, class, name); problem != "" {
						offenders = append(offenders, fmt.Sprintf("%s: %s: rescue cannot relay rc 124/137 safely: %s", rel, name, problem))
					}
				} else if value, noLog := task["no_log"]; noLog && ansibleControlMayEnable(value) {
					offenders = append(offenders, fmt.Sprintf("%s: %s: no_log can censor rc 124/137 before the runner sees it; wrap the command in the fail-closed Ceph timeout relay", rel, name))
				}
				if value, exists := task["ignore_errors"]; exists && ansibleControlMayEnable(value) {
					offenders = append(offenders, fmt.Sprintf("%s: %s: ignore_errors can swallow rc 124/137", rel, name))
				}
				if value, exists := task["failed_when"]; exists && !failedWhenRetainsCephTimeout(value) {
					offenders = append(offenders, fmt.Sprintf("%s: %s: failed_when=%q can swallow rc 124/137", rel, name, fmt.Sprint(value)))
				}
				if ansibleTaskLoops(task) {
					loopControl, _ := task["loop_control"].(map[string]any)
					if !cephTimeoutCodesNamed(loopControl["break_when"]) {
						offenders = append(offenders, fmt.Sprintf("%s: %s: a loop must break on rc 124/137 before it starts another item", rel, name))
					}
				}
				if _, retried := task["retries"]; retried && !cephTimeoutCodesNamed(task["until"]) {
					offenders = append(offenders, fmt.Sprintf("%s: %s: a retry must end on rc 124/137 before it starts another attempt", rel, name))
				}
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", base, err)
	}
	if candidates < cephShellCommandFloor {
		t.Fatalf("found %d cephadm shell commands, want at least %d: the recursive scanner no longer covers the audited command surface", candidates, cephShellCommandFloor)
	}
	for _, class := range classes {
		if class.candidates == 0 {
			offenders = append(offenders, fmt.Sprintf("timeout class %s (%s) matched no command", class.name, class.variable))
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("every cephadm shell invocation must use its named finite timeout class and kill escalation, and every timeout must reach the runner as rc 124/137:\n%s", strings.Join(offenders, "\n"))
	}
	assertCephTimeoutDefaults(t, root, classes)
	assertCephTimeoutRuntimeContract(t, root, classes)
	assertCephTimeoutRelay(t, root)
}

func cephShellTimeoutClasses() map[string]*cephShellTimeoutClass {
	return map[string]*cephShellTimeoutClass{
		"probe":         {name: "probe", variable: "bootwright_ceph_probe_timeout_seconds"},
		"inventory":     {name: "inventory", variable: "bootwright_ceph_inventory_probe_timeout_seconds"},
		"configuration": {name: "configuration", variable: "bootwright_ceph_config_timeout_seconds", mutating: true},
		"orchestration": {name: "orchestration", variable: "bootwright_ceph_orchestration_timeout_seconds", mutating: true},
		"removal":       {name: "removal", variable: "bootwright_ceph_removal_timeout_seconds", mutating: true},
		"tool":          {name: "tool", variable: "bootwright_ceph_tool_timeout_seconds"},
		"batch":         {name: "batch", variable: "bootwright_ceph_operation_batch_timeout_seconds", mutating: true},
	}
}

func TestCephShellTimeoutGuardRejectsUnsafeShapes(t *testing.T) {
	classes := cephShellTimeoutClasses()
	tests := []struct {
		name string
		argv any
		task map[string]any
	}{
		{
			name: "unbounded probe",
			argv: []any{"cephadm", "shell", "--", "ceph", "health"},
		},
		{
			name: "unknown command",
			argv: []any{"timeout", "--kill-after={{ bootwright_ceph_timeout_kill_after_seconds }}", "{{ bootwright_ceph_probe_timeout_seconds }}", "cephadm", "shell", "--", "ceph", "osd", "destroy", "1"},
		},
		{
			name: "mutation under probe ceiling",
			argv: []any{"timeout", "--kill-after={{ bootwright_ceph_timeout_kill_after_seconds }}", "{{ bootwright_ceph_probe_timeout_seconds }}", "cephadm", "shell", "--", "ceph", "config", "set", "global", "key", "value"},
		},
		{
			name: "unregistered dynamic argv",
			argv: "{{ ['cephadm', 'shell', '--'] + unregistered_command }}",
		},
		{
			name: "unbounded registered dynamic argv",
			argv: "{{ ['cephadm', 'shell', '--'] + bootwright_ceph_op_command }}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, problem := validateCephShellTimeout(tt.argv, tt.task, classes); problem == "" {
				t.Fatalf("unsafe argv passed the guard: %v", tt.argv)
			}
		})
	}
	for _, unsafe := range []any{false, "result.rc == 0", "result.rc in [124]", "result.rc != 0 and result.rc not in [124, 137]", "not (result.rc in [124, 137])", "false or (result.rc in [124, 137])"} {
		if failedWhenRetainsCephTimeout(unsafe) {
			t.Fatalf("failed_when=%v was treated as timeout preserving", unsafe)
		}
	}
	for _, safe := range []any{true, "result.rc != 0", "result.rc in [124, 137]", "result.rc | default(0) | int in [124, 137]"} {
		if !failedWhenRetainsCephTimeout(safe) {
			t.Fatalf("failed_when=%v was treated as timeout swallowing", safe)
		}
	}
	var caught any
	walkCephShellCommands(map[string]any{
		"block":  []any{map[string]any{"ansible.builtin.command": map[string]any{"argv": []any{"cephadm", "shell"}}}},
		"rescue": []any{map[string]any{"ansible.builtin.debug": map[string]any{"msg": "caught"}}},
	}, cephShellWalkContext{}, func(task map[string]any, context cephShellWalkContext) {
		if _, ok := task["ansible.builtin.command"]; ok {
			caught = context.rescue
		}
	})
	if caught == nil {
		t.Fatal("a command under block/rescue was not marked as catchable")
	}
	if problem := validateCephTimeoutRescue(caught, classes["probe"], "Probe Ceph health"); problem == "" {
		t.Fatal("a rescue without the timeout relay passed the guard")
	}
	safe := []any{
		map[string]any{
			"ansible.builtin.include_tasks": "{{ role_path }}/tasks/ceph_command_timeout.yml",
			"vars": map[string]any{
				"bootwright_ceph_timeout_task":           "Probe Ceph health",
				"bootwright_ceph_timeout_seconds":        "{{ bootwright_ceph_probe_timeout_seconds }}",
				"bootwright_ceph_timeout_exit_code":      "{{ ansible_failed_result.rc }}",
				"bootwright_ceph_timeout_state_changing": false,
			},
			"when": "ansible_failed_result.rc | int in [124, 137]",
		},
		map[string]any{"ansible.builtin.fail": map[string]any{"msg": "A hidden Ceph command failed."}},
	}
	if problem := validateCephTimeoutRescue(safe, classes["probe"], "Probe Ceph health"); problem != "" {
		t.Fatalf("the fail-closed timeout relay was rejected: %s", problem)
	}
	unsafeTerminal := append([]any(nil), safe...)
	unsafeTerminal[1] = map[string]any{
		"ansible.builtin.fail": map[string]any{"msg": "A hidden Ceph command failed."},
		"when":                 false,
	}
	if problem := validateCephTimeoutRescue(unsafeTerminal, classes["probe"], "Probe Ceph health"); problem == "" {
		t.Fatal("a conditional terminal rescue failure passed the guard")
	}
	for name, task := range map[string]map[string]any{
		"shell module":       {"ansible.builtin.shell": "timeout 120 cephadm shell -- ceph health"},
		"short command name": {"command": map[string]any{"argv": []any{"timeout", "120", "cephadm", "shell", "--", "ceph", "health"}}},
		"free form command":  {"ansible.builtin.command": "timeout 120 cephadm shell -- ceph health"},
	} {
		if _, candidate, problem := cephShellTaskArgv(task); candidate || problem == "" {
			t.Fatalf("%s escaped the structured-argv guard: candidate=%v problem=%q", name, candidate, problem)
		}
	}
	if !cephTimeoutCodesNamed("ready or (result.rc | default(0) | int in [124, 137])") {
		t.Fatal("a positive top-level timeout stop was rejected")
	}
	for _, unsafe := range []any{"not (result.rc in [124, 137])", "ready and (result.rc in [124, 137])", "floor(result.rc in [124, 137])"} {
		if cephTimeoutCodesNamed(unsafe) {
			t.Fatalf("retry/loop expression %q was treated as a positive timeout stop", unsafe)
		}
	}
	includeRole := map[string]any{
		"ansible.builtin.include_role": map[string]any{"name": "bootwright.core.storage_cluster_cephadm"},
		"ignore_errors":                true,
	}
	if problem := validateCephSurfaceInclude(includeRole, cephShellWalkContext{}, "ansible/playbook.yml"); problem == "" {
		t.Fatal("ignore_errors on the Ceph role boundary escaped the guard")
	}
	includeTasks := map[string]any{"ansible.builtin.include_tasks": "phases/bootstrap.yml"}
	if problem := validateCephSurfaceInclude(includeTasks, cephShellWalkContext{rescue: []any{}}, "ansible/roles/storage_cluster_cephadm/tasks/main.yml"); problem == "" {
		t.Fatal("rescue around a Ceph role task include escaped the guard")
	}
	unknownEntrypoint := map[string]any{
		"ansible.builtin.include_role": map[string]any{
			"name":       "bootwright.core.storage_cluster_cephadm",
			"tasks_from": "future_mutation.yml",
		},
	}
	if problem := validateCephSurfaceInclude(unknownEntrypoint, cephShellWalkContext{}, "ansible/playbook.yml"); problem == "" {
		t.Fatal("an unregistered Ceph role entrypoint escaped the timeout-contract guard")
	}
	healthMute := []any{"timeout", "--kill-after={{ bootwright_ceph_timeout_kill_after_seconds }}", "{{ bootwright_ceph_probe_timeout_seconds }}", "cephadm", "shell", "--", "ceph", "health", "mute", "OSD_DOWN"}
	if _, problem := validateCephShellTimeout(healthMute, nil, classes); problem == "" {
		t.Fatal("a state-changing ceph health subcommand passed as a read-only probe")
	}
}

func validateCephSurfaceInclude(task map[string]any, context cephShellWalkContext, path string) string {
	includeTasks := false
	for _, module := range []string{"ansible.builtin.include_tasks", "ansible.builtin.import_tasks"} {
		if value, exists := task[module]; exists &&
			(strings.Contains(filepath.ToSlash(path), "/roles/storage_cluster_cephadm/tasks/") || strings.Contains(fmt.Sprint(value), "storage_cluster_cephadm")) {
			includeTasks = true
		}
	}
	includeRole := false
	for _, module := range []string{"ansible.builtin.include_role", "ansible.builtin.import_role"} {
		if value, exists := task[module]; exists && strings.Contains(fmt.Sprint(value), "storage_cluster_cephadm") {
			includeRole = true
			body, _ := value.(map[string]any)
			entrypoint := strings.TrimSpace(fmt.Sprint(body["tasks_from"]))
			if entrypoint == "<nil>" {
				entrypoint = ""
			}
			allowed := map[string]bool{"": true, "main.yml": true, "destroy.yml": true, "destroy_release.yml": true, "replace_arbiter.yml": true, "revoke_node_access.yml": true}
			if !allowed[entrypoint] {
				return fmt.Sprintf("Ceph role entrypoint %q is not registered with the runtime timeout contract", entrypoint)
			}
		}
	}
	if !includeTasks && !includeRole {
		return ""
	}
	if context.rescue != nil {
		return "a rescue around this include can swallow a timeout from a nested cephadm shell command"
	}
	if context.inheritedIgnoreErrors {
		return "an enclosing ignore_errors can swallow a timeout from a nested cephadm shell command"
	}
	if context.inheritedNoLog {
		return "an enclosing no_log can censor a nested Ceph timeout and its safe relay"
	}
	if value, exists := task["ignore_errors"]; exists && ansibleControlMayEnable(value) {
		return "ignore_errors can swallow a timeout from a nested cephadm shell command"
	}
	if value, exists := task["no_log"]; exists && ansibleControlMayEnable(value) {
		return "no_log can censor a nested Ceph timeout and its safe relay"
	}
	if _, exists := task["failed_when"]; exists {
		return "failed_when on an include cannot safely preserve a nested rc 124/137"
	}
	return ""
}

func walkCephShellCommands(value any, context cephShellWalkContext, visit func(map[string]any, cephShellWalkContext)) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			walkCephShellCommands(item, context, visit)
		}
	case map[string]any:
		visit(typed, context)
		localRescue, hasRescue := typed["rescue"]
		inheritedNoLog := context.inheritedNoLog
		if value, exists := typed["no_log"]; exists && ansibleControlMayEnable(value) {
			inheritedNoLog = true
		}
		inheritedIgnoreErrors := context.inheritedIgnoreErrors
		if value, exists := typed["ignore_errors"]; exists && ansibleControlMayEnable(value) {
			inheritedIgnoreErrors = true
		}
		for key, item := range typed {
			nested := context
			nested.inheritedNoLog = inheritedNoLog
			nested.inheritedIgnoreErrors = inheritedIgnoreErrors
			if key == "block" && hasRescue {
				nested.rescue = localRescue
			}
			walkCephShellCommands(item, nested, visit)
		}
	}
}

func cephShellTaskArgv(task map[string]any) (any, bool, string) {
	for _, module := range []string{"command", "ansible.builtin.shell", "shell", "ansible.builtin.raw", "raw", "action", "local_action"} {
		if value, exists := task[module]; exists && valueMentionsCephShell(value) {
			return nil, false, fmt.Sprintf("%s cannot safely express a classified cephadm shell argv; use ansible.builtin.command with argv", module)
		}
	}
	value, exists := task["ansible.builtin.command"]
	if !exists {
		return nil, false, ""
	}
	command, ok := value.(map[string]any)
	if !ok {
		if valueMentionsCephShell(value) {
			return nil, false, "free-form ansible.builtin.command cannot safely express a classified cephadm shell argv; use argv"
		}
		return nil, false, ""
	}
	argv, exists := command["argv"]
	if !exists {
		if valueMentionsCephShell(command) {
			return nil, false, "ansible.builtin.command invokes cephadm shell without structured argv"
		}
		return nil, false, ""
	}
	if !cephShellArgv(argv, task) {
		return nil, false, ""
	}
	return argv, true, ""
}

func valueMentionsCephShell(value any) bool {
	raw := fmt.Sprint(value)
	return strings.Contains(raw, "bootwright_ceph_shell_prefix") ||
		(strings.Contains(raw, "cephadm") && strings.Contains(raw, "shell"))
}

func ansibleControlMayEnable(value any) bool {
	if boolean, ok := value.(bool); ok {
		return boolean
	}
	return !strings.EqualFold(strings.TrimSpace(fmt.Sprint(value)), "false")
}

func validateCephTimeoutRescue(value any, class *cephShellTimeoutClass, taskName string) string {
	tasks, ok := value.([]any)
	if !ok || len(tasks) != 2 {
		return "the rescue must contain exactly the timeout relay and an unconditional terminal failure"
	}
	relay, ok := tasks[0].(map[string]any)
	if !ok || !strings.HasSuffix(fmt.Sprint(relay["ansible.builtin.include_tasks"]), "/tasks/ceph_command_timeout.yml") {
		return "the first rescue task must include the shared ceph_command_timeout.yml relay"
	}
	if !cephTimeoutCodesNamed(relay["when"]) {
		return "the timeout relay must run for both rc 124 and rc 137"
	}
	if value, exists := relay["ignore_errors"]; exists && fmt.Sprint(value) != "false" {
		return "the timeout relay must not ignore its failure"
	}
	for _, key := range []string{"failed_when", "rescue"} {
		if _, exists := relay[key]; exists {
			return fmt.Sprintf("the timeout relay must not set %s", key)
		}
	}
	vars, ok := relay["vars"].(map[string]any)
	if !ok {
		return "the timeout relay has no safe metadata variables"
	}
	if got := strings.TrimSpace(fmt.Sprint(vars["bootwright_ceph_timeout_task"])); got != taskName {
		return fmt.Sprintf("the timeout relay reports task %q, want exact task %q", got, taskName)
	}
	if !strings.Contains(fmt.Sprint(vars["bootwright_ceph_timeout_seconds"]), class.variable) {
		return fmt.Sprintf("the timeout relay does not report the %s class duration", class.name)
	}
	if !strings.Contains(fmt.Sprint(vars["bootwright_ceph_timeout_exit_code"]), "ansible_failed_result.rc") {
		return "the timeout relay does not report the failed command's exit code"
	}
	stateChanging, ok := vars["bootwright_ceph_timeout_state_changing"].(bool)
	if !ok || stateChanging != class.mutating {
		return fmt.Sprintf("the timeout relay does not report state_changing=%t", class.mutating)
	}
	terminal, ok := tasks[1].(map[string]any)
	if !ok {
		return "the rescue has no terminal failure"
	}
	if _, ok := terminal["ansible.builtin.fail"].(map[string]any); !ok {
		return "the rescue must end with ansible.builtin.fail for non-timeout failures"
	}
	for _, key := range []string{"when", "failed_when", "rescue"} {
		if _, exists := terminal[key]; exists {
			return fmt.Sprintf("the terminal rescue failure must not set %s", key)
		}
	}
	if value, exists := terminal["ignore_errors"]; exists && fmt.Sprint(value) != "false" {
		return "the terminal rescue failure must not ignore its failure"
	}
	return ""
}

func cephShellArgv(value any, task map[string]any) bool {
	argv := ansibleArgvStrings(value)
	if cephadmShellPosition(argv) >= 0 {
		return true
	}
	raw := fmt.Sprint(value)
	if strings.Contains(raw, "bootwright_ceph_shell_prefix") {
		return true
	}
	return strings.Contains(raw, "cephadm") && strings.Contains(raw, "shell")
}

func validateCephShellTimeout(value any, task map[string]any, classes map[string]*cephShellTimeoutClass) (*cephShellTimeoutClass, string) {
	argv := ansibleArgvStrings(value)
	if len(argv) > 0 {
		shell := cephadmShellPosition(argv)
		if shell < 0 {
			return nil, "the argv is not a recognizable cephadm shell command"
		}
		child := cephShellChild(argv, shell)
		className := cephShellStaticClass(child)
		class := classes[className]
		if class == nil {
			return nil, fmt.Sprintf("child command %q has no fail-closed timeout classification", strings.Join(child, " "))
		}
		if problem := validateCephShellWrapper(argv, class.variable); problem != "" {
			return nil, problem
		}
		if got := convergeansible.CephShellCommandStateChanging(argv); got != class.mutating {
			return nil, fmt.Sprintf("%s classification disagrees with the runner's state-changing classification", class.name)
		}
		return class, ""
	}
	raw := fmt.Sprint(value)
	className := ""
	switch {
	case strings.Contains(raw, "bootwright_ceph_op_command"):
		className = "orchestration"
		want := "{{['timeout','--kill-after='~(bootwright_ceph_timeout_kill_after_seconds|string),bootwright_ceph_orchestration_timeout_seconds|string,'cephadm','shell','--']+bootwright_ceph_op_command}}"
		if normalizedAnsibleExpression(raw) != want {
			return nil, "dynamic rendered-operation argv must use the exact registered orchestration timeout prefix"
		}
	case strings.Contains(raw, "'host', 'rm'"):
		className = "removal"
		want := "{{['timeout','--kill-after='~(bootwright_ceph_timeout_kill_after_seconds|string),bootwright_ceph_removal_timeout_seconds|string,'cephadm','shell','--','ceph','orch','host','rm',bootwright_arbiter_live_node]+(['--offline','--force']if(bootwright_arbiter_retire_offline|default(false)|bool)else[])}}"
		if normalizedAnsibleExpression(raw) != want {
			return nil, "dynamic host-removal argv must use the exact registered removal timeout shape"
		}
	case strings.Contains(raw, "bootwright_ceph_shell_prefix"):
		className = "orchestration"
		vars, ok := task["vars"].(map[string]any)
		if !ok {
			return nil, "dynamic cephadm shell prefix has no task-local argv definition"
		}
		prefix := ansibleArgvStrings(vars["bootwright_ceph_shell_prefix"])
		if problem := validateCephShellWrapper(prefix, classes[className].variable); problem != "" {
			return nil, "dynamic cephadm shell prefix " + problem
		}
		return classes[className], ""
	default:
		return nil, "dynamic cephadm shell argv has no fail-closed timeout classification"
	}
	return classes[className], ""
}

func validateCephShellWrapper(argv []string, timeoutVariable string) string {
	if len(argv) < 5 || argv[0] != "timeout" || argv[3] != "cephadm" || argv[4] != "shell" {
		return fmt.Sprintf("argv %q must start with timeout, kill escalation, named duration, cephadm, shell", strings.Join(argv, " "))
	}
	if !strings.HasPrefix(argv[1], "--kill-after=") || !strings.Contains(argv[1], "bootwright_ceph_timeout_kill_after_seconds") {
		return "timeout wrapper must use --kill-after={{ bootwright_ceph_timeout_kill_after_seconds }}"
	}
	if !validCephTimeoutOperand(argv[2], timeoutVariable) {
		return fmt.Sprintf("timeout duration %q must use {{ %s }}", argv[2], timeoutVariable)
	}
	return ""
}

func validCephTimeoutOperand(value, timeoutVariable string) bool {
	normalized := normalizedAnsibleExpression(value)
	if normalized == "{{"+timeoutVariable+"}}" {
		return true
	}
	pattern := regexp.MustCompile(`^\{\{\[` + regexp.QuoteMeta(timeoutVariable) + `\|int,([1-9][0-9]*)\]\|min\}\}$`)
	return pattern.MatchString(normalized)
}

func cephadmShellPosition(argv []string) int {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == "cephadm" && argv[i+1] == "shell" {
			return i
		}
	}
	return -1
}

func cephShellChild(argv []string, shell int) []string {
	for i := shell + 2; i < len(argv); i++ {
		if argv[i] == "--" {
			return argv[i+1:]
		}
	}
	return nil
}

func cephShellStaticClass(child []string) string {
	if len(child) == 0 {
		return ""
	}
	if child[0] == "crushtool" || hasArgvExact(child, "bash", "-c", "command -v python3 >/dev/null 2>&1") {
		return "tool"
	}
	if child[0] == "bash" {
		return "batch"
	}
	if child[0] == "radosgw-admin" {
		if hasArgvPrefix(child[1:], "realm", "list") || hasArgvPrefix(child[1:], "zonegroup", "list") || hasArgvPrefix(child[1:], "zone", "list") || hasArgvPrefix(child[1:], "user", "info") {
			return "probe"
		}
		return ""
	}
	if child[0] != "ceph" {
		return ""
	}
	args := make([]string, 0, len(child)-1)
	for _, arg := range child[1:] {
		if !strings.HasPrefix(arg, "--connect-timeout=") {
			args = append(args, arg)
		}
	}
	if hasArgvPrefix(args, "orch", "device", "ls") {
		return "inventory"
	}
	if hasArgvExact(args, "health") || hasArgvExact(args, "health", "--format", "json") ||
		hasArgvExact(args, "fsid") || hasArgvExact(args, "status") ||
		hasArgvExact(args, "versions", "--format", "json") {
		return "probe"
	}
	for _, prefix := range [][]string{
		{"osd", "stat"}, {"osd", "tree"}, {"osd", "metadata"}, {"osd", "getcrushmap"},
		{"osd", "pool", "ls"}, {"osd", "crush", "rule", "dump"},
		{"osd", "erasure-code-profile", "ls"}, {"osd", "erasure-code-profile", "get"},
		{"fs", "ls"}, {"fs", "get"}, {"mon", "dump"}, {"mgr", "module", "ls"},
		{"mgr", "services"}, {"config", "dump"}, {"config", "get"}, {"config-key", "get"},
		{"orch", "ls"}, {"orch", "ps"}, {"orch", "host", "ls"}, {"log", "last"}, {"cephadm", "get-user"},
	} {
		if hasArgvPrefix(args, prefix...) {
			return "probe"
		}
	}
	for _, prefix := range [][]string{
		{"config", "set"}, {"config-key", "set"}, {"osd", "setcrushmap"}, {"osd", "pool", "set"},
		{"cephadm", "registry-login"}, {"cephadm", "set-ssh-config"}, {"cephadm", "set-user"},
		{"mgr", "module", "enable"}, {"mgr", "module", "disable"},
		{"orch", "accept", "call-home-enabled"}, {"orch", "deny", "call-home-enabled"},
	} {
		if hasArgvPrefix(args, prefix...) {
			return "configuration"
		}
	}
	for _, prefix := range [][]string{
		{"orch", "apply"}, {"orch", "redeploy"}, {"orch", "reconfig"}, {"orch", "host", "add"}, {"mon", "set_new_tiebreaker"},
	} {
		if hasArgvPrefix(args, prefix...) {
			return "orchestration"
		}
	}
	for _, prefix := range [][]string{
		{"osd", "pool", "rm"}, {"osd", "erasure-code-profile", "rm"},
		{"fs", "fail"}, {"fs", "rm"}, {"mon", "rm"}, {"orch", "device", "zap"},
		{"orch", "host", "drain"}, {"orch", "host", "rm"}, {"orch", "daemon", "rm"},
	} {
		if hasArgvPrefix(args, prefix...) {
			return "removal"
		}
	}
	return ""
}

func hasArgvPrefix(argv []string, prefix ...string) bool {
	if len(argv) < len(prefix) {
		return false
	}
	for i := range prefix {
		if argv[i] != prefix[i] {
			return false
		}
	}
	return true
}

func hasArgvExact(argv []string, expected ...string) bool {
	return len(argv) == len(expected) && hasArgvPrefix(argv, expected...)
}

func failedWhenRetainsCephTimeout(value any) bool {
	if boolean, ok := value.(bool); ok {
		return boolean
	}
	expression := normalizedAnsibleExpression(fmt.Sprint(value))
	pattern := regexp.MustCompile(`^\(?[A-Za-z_][A-Za-z0-9_]*\.rc(?:(?:\|default\(0\))?\|int)?(?:!=0|in\[124,137\])\)?$`)
	return pattern.MatchString(expression)
}

func cephTimeoutCodesNamed(value any) bool {
	expression := fmt.Sprint(value)
	pattern := regexp.MustCompile(`(?:^|\bor\s*|\[\s*)\(*\s*[A-Za-z_][A-Za-z0-9_]*\.rc\s*(?:(?:\|\s*default\(0\)\s*)?\|\s*int\s*)?in\s*\[\s*124\s*,\s*137\s*\]\s*\)*(?:\s*$|\s*\bor\b|\s*\])`)
	return pattern.MatchString(expression)
}

func normalizedAnsibleExpression(value string) string {
	return strings.Join(strings.Fields(value), "")
}

func ansibleTaskLoops(task map[string]any) bool {
	for key := range task {
		if key == "loop" || strings.HasPrefix(key, "with_") {
			return true
		}
	}
	return false
}

func assertCephTimeoutDefaults(t *testing.T, root string, classes map[string]*cephShellTimeoutClass) {
	t.Helper()
	path := filepath.Join(root, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/defaults/main.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var defaults map[string]any
	if err := yaml.Unmarshal(data, &defaults); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	wants := map[string]int{
		"bootwright_ceph_probe_timeout_seconds":           120,
		"bootwright_ceph_inventory_probe_timeout_seconds": 300,
		"bootwright_ceph_config_timeout_seconds":          300,
		"bootwright_ceph_orchestration_timeout_seconds":   600,
		"bootwright_ceph_removal_timeout_seconds":         1800,
		"bootwright_ceph_tool_timeout_seconds":            300,
		"bootwright_ceph_operation_batch_timeout_seconds": 1800,
		"bootwright_ceph_timeout_kill_after_seconds":      15,
	}
	for _, class := range classes {
		if _, ok := wants[class.variable]; !ok {
			t.Fatalf("timeout class %s has no finite default contract", class.name)
		}
	}
	for name, want := range wants {
		if got := defaults[name]; got != want {
			t.Errorf("%s = %v, want finite default %d", name, got, want)
		}
	}
}

func assertCephTimeoutRuntimeContract(t *testing.T, root string, classes map[string]*cephShellTimeoutClass) {
	t.Helper()
	tasksDir := filepath.Join(root, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks")
	path := filepath.Join(tasksDir, "timeout_contract.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var tasks []map[string]any
	if err := yaml.Unmarshal(data, &tasks); err != nil || len(tasks) != 1 {
		t.Fatalf("parse one timeout contract task from %s: tasks=%d err=%v", path, len(tasks), err)
	}
	assertion, ok := tasks[0]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no fail-closed assertion", path)
	}
	conditions := fmt.Sprint(assertion["that"])
	for _, class := range classes {
		if !strings.Contains(conditions, class.variable+" | int > 0") {
			t.Errorf("%s does not reject a zero or negative %s", path, class.variable)
		}
	}
	for _, required := range []string{
		"bootwright_ceph_operation_batch_timeout_seconds | int <= 1800",
		"bootwright_ceph_timeout_kill_after_seconds | int > 0",
		"bootwright_ceph_timeout_kill_after_seconds | int <= 60",
	} {
		if !strings.Contains(conditions, required) {
			t.Errorf("%s does not enforce %q", path, required)
		}
	}
	if !strings.Contains(fmt.Sprint(assertion["fail_msg"]), "bootwright_mutating_invocation") {
		t.Errorf("%s does not return the exact resolved retry invocation", path)
	}
	for _, entrypoint := range []string{"main.yml", "destroy.yml", "destroy_release.yml", "replace_arbiter.yml", "revoke_node_access.yml"} {
		entryPath := filepath.Join(tasksDir, entrypoint)
		entryData, err := os.ReadFile(entryPath)
		if err != nil {
			t.Fatalf("read %s: %v", entryPath, err)
		}
		var entryTasks []map[string]any
		if err := yaml.Unmarshal(entryData, &entryTasks); err != nil || len(entryTasks) == 0 {
			t.Fatalf("parse timeout-protected entrypoint %s: tasks=%d err=%v", entryPath, len(entryTasks), err)
		}
		if got := fmt.Sprint(entryTasks[0]["ansible.builtin.import_tasks"]); got != "timeout_contract.yml" {
			t.Errorf("%s starts with import_tasks=%q, want timeout_contract.yml before any Ceph command", entryPath, got)
		}
	}
	healthPath := filepath.Join(root, "ansible/collections/ansible_collections/bootwright/core/playbooks/check_storage_health.yml")
	healthData, err := os.ReadFile(healthPath)
	if err != nil {
		t.Fatalf("read %s: %v", healthPath, err)
	}
	var healthPlays []map[string]any
	if err := yaml.Unmarshal(healthData, &healthPlays); err != nil || len(healthPlays) != 1 {
		t.Fatalf("parse health timeout contract from %s: plays=%d err=%v", healthPath, len(healthPlays), err)
	}
	healthTasks, ok := healthPlays[0]["tasks"].([]any)
	if !ok || len(healthTasks) == 0 {
		t.Fatalf("%s has no health tasks", healthPath)
	}
	healthContract, ok := healthTasks[0].(map[string]any)["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("%s does not validate its timeout before the first probe", healthPath)
	}
	healthConditions := fmt.Sprint(healthContract["that"])
	for _, required := range []string{
		"bootwright_ceph_probe_timeout_seconds | int > 0",
		"bootwright_ceph_timeout_kill_after_seconds | int > 0",
		"bootwright_ceph_timeout_kill_after_seconds | int <= 60",
	} {
		if !strings.Contains(healthConditions, required) {
			t.Errorf("%s does not enforce %q", healthPath, required)
		}
	}
	if message := fmt.Sprint(healthContract["fail_msg"]); strings.Contains(message, "bootwright_mutating_invocation") || !strings.Contains(message, "No state-changing command") {
		t.Errorf("%s must keep a health-probe timeout command-free, got %q", healthPath, message)
	}
}

func assertCephTimeoutRelay(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/ceph_command_timeout.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := string(data)
	for _, required := range []string{
		"ansible.builtin.fail:",
		"BOOTWRIGHT_CEPH_COMMAND_TIMEOUT=",
		"bootwright_ceph_timeout_task",
		"bootwright_ceph_timeout_seconds",
		"bootwright_ceph_timeout_exit_code",
		"bootwright_ceph_timeout_state_changing",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("Ceph timeout relay %s is missing %q", path, required)
		}
	}
}
