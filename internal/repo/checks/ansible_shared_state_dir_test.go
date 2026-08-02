package repocheck

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const sharedMachineStateDir = "/etc/bootwright"

var sharedStateInstallDir = regexp.MustCompile(`install -d -m ([0-7]{3,4})([^\n]*)`)

func sharedStateMarkerExprs(t *testing.T) map[string]bool {
	t.Helper()
	exprs := map[string]bool{
		"bootwright_node_access.markerPath":          true,
		"bootwright_component.osInstall.marker.path": true,
	}
	facts := map[string]string{}
	walkAnsibleTaskFiles(t, func(rel string) {
		collectSetFactStrings(readAnsibleTasks(t, rel), facts)
	})
	for range facts {
		grew := false
		for name, value := range facts {
			if exprs[name] {
				continue
			}
			if !strings.HasPrefix(strings.TrimSpace(value), sharedMachineStateDir+"/") && !referencesAnyExpr(value, exprs) {
				continue
			}
			exprs[name] = true
			grew = true
		}
		if !grew {
			break
		}
	}
	return exprs
}

func collectSetFactStrings(tasks []map[string]any, out map[string]string) {
	for _, task := range tasks {
		for _, key := range []string{"ansible.builtin.set_fact", "set_fact"} {
			body, ok := task[key].(map[string]any)
			if !ok {
				continue
			}
			for name, value := range body {
				if s, ok := value.(string); ok {
					out[name] = s
				}
			}
		}
		for _, key := range []string{"block", "rescue", "always"} {
			raw, ok := task[key].([]any)
			if !ok {
				continue
			}
			children := make([]map[string]any, 0, len(raw))
			for _, item := range raw {
				if child, ok := item.(map[string]any); ok {
					children = append(children, child)
				}
			}
			collectSetFactStrings(children, out)
		}
	}
}

func referencesAnyExpr(value string, exprs map[string]bool) bool {
	for expr := range exprs {
		if strings.Contains(value, expr) {
			return true
		}
	}
	return false
}

func namesSharedStateDir(path string, exprs map[string]bool) bool {
	trimmed := strings.TrimSpace(path)
	if trimmed == sharedMachineStateDir || strings.HasPrefix(trimmed, sharedMachineStateDir+"/") {
		return true
	}
	if !strings.Contains(path, "dirname") {
		return false
	}
	return referencesAnyExpr(path, exprs)
}

func assertWorldTraversable(t *testing.T, where, mode string) {
	t.Helper()
	bits, err := strconv.ParseUint(strings.TrimSpace(mode), 8, 32)
	if err != nil {
		t.Errorf("%s sets %s mode %q, which is not an octal literal; the shared directory's mode is a contract and must be readable at a glance", where, sharedMachineStateDir, mode)
		return
	}
	if bits&0o005 != 0o005 {
		t.Errorf("%s tightens %s to mode %q, which denies the unprivileged orchestration account the traverse+read it needs; the pre-install ownership probe reads the install marker before it is entitled to root, so a root-only parent makes a Bootwright-owned host look foreign. Carry confidentiality on the file's own mode instead", where, sharedMachineStateDir, mode)
	}
}

func TestSharedMachineStateDirectoryStaysWorldTraversable(t *testing.T) {
	exprs := sharedStateMarkerExprs(t)
	if !exprs["bootwright_os_marker_path"] || !exprs["bootwright_ceph_osd_marker_path"] {
		t.Fatalf("marker-path discovery found %v; it must reach the OS install and Ceph OSD markers or the guard covers nothing", exprs)
	}

	directories := 0
	installs := 0
	walkAnsibleTaskFiles(t, func(rel string) {
		for _, task := range flattenAnsibleTasks(readAnsibleTasks(t, rel)) {
			for _, key := range []string{"ansible.builtin.file", "file"} {
				body, ok := task[key].(map[string]any)
				if !ok {
					continue
				}
				path, _ := body["path"].(string)
				state, _ := body["state"].(string)
				if state != "directory" || !namesSharedStateDir(path, exprs) {
					continue
				}
				directories++
				mode, ok := body["mode"].(string)
				if !ok {
					continue
				}
				assertWorldTraversable(t, rel+" task \""+asTaskName(task)+"\"", mode)
			}
		}
		for _, match := range sharedStateInstallDir.FindAllStringSubmatch(readRepoFile(t, rel), -1) {
			if !namesSharedStateDir(match[2], exprs) {
				continue
			}
			installs++
			assertWorldTraversable(t, rel+" `install -d`", match[1])
		}
	})
	if directories == 0 || installs == 0 {
		t.Fatalf("found %d file-module and %d `install -d` writers of %s; the guard must see both kinds or it has stopped matching", directories, installs, sharedMachineStateDir)
	}
}

func TestManagedOSMarkerProbeFallsBackToPrivilegedRead(t *testing.T) {
	rel := bootwrightCollectionRoleRoot + "/machine_os_identity/tasks/probe_existing.yml"
	tasks := readAnsibleTasks(t, rel)
	task := tasks[findAnsibleTask(t, tasks, "Read managed OS install marker before install")]
	body, ok := task["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("%s: marker probe is not an ansible.builtin.command task", rel)
	}
	argv, ok := body["argv"].([]any)
	if !ok || len(argv) == 0 {
		t.Fatalf("%s: marker probe has no argv", rel)
	}
	remote, _ := argv[len(argv)-1].(string)
	if !strings.Contains(remote, "sudo -n cat") {
		t.Fatalf("%s: the marker read must fall back to `sudo -n cat` so a root-only %s cannot make a Bootwright-owned host read as foreign, got %q", rel, sharedMachineStateDir, remote)
	}
	if !strings.Contains(remote, "cat ") || !strings.Contains(remote, "||") {
		t.Fatalf("%s: the marker read must try the unprivileged `cat` first and escalate only on failure, got %q", rel, remote)
	}
}

func asTaskName(task map[string]any) string {
	name, _ := task["name"].(string)
	return name
}
