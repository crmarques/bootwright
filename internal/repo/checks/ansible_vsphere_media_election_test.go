package repocheck

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoYAMLFilesUnder(t *testing.T, rel string) []string {
	t.Helper()
	root := filepath.Join(repoRoot(t), rel)
	info, err := os.Stat(root)
	if err != nil {
		t.Fatalf("stat %s: %v", rel, err)
	}
	if !info.IsDir() {
		return []string{rel}
	}
	var out []string
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".yml" {
			return nil
		}
		relative, err := filepath.Rel(repoRoot(t), path)
		if err != nil {
			return err
		}
		out = append(out, relative)
		return nil
	}); err != nil {
		t.Fatalf("walk %s: %v", rel, err)
	}
	return out
}

const vsphereMediaInsertPath = "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_media_vsphere/tasks/insert.yml"
const vsphereBootStagePath = "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_vsphere/tasks/stage_agent_iso.yml"
const bootAgentMachinePlaybookPath = "ansible/collections/ansible_collections/bootwright/core/playbooks/task_container_cluster_boot_agent_machine.yml"

func TestVSphereMediaUploadIsElectedPerDatastoreTarget(t *testing.T) {
	tasks := flattenAnsibleTasks(readAnsibleTasks(t, vsphereMediaInsertPath))
	requirement := tasks[findAnsibleTask(t, tasks, "Resolve vSphere virtual media upload requirement")]
	setFact, ok := requirement["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("upload requirement task is not set_fact: %v", requirement)
	}
	expr := fmt.Sprint(setFact["bootwright_vsphere_media_upload_required"])
	for _, want := range []string{
		"bootwright_vsphere_media_upload_elected",
		"bootwright_vsphere_media_uploaded_marker.stat.exists",
		"bootwright_vsphere_media_stage_stat.stat.mtime",
	} {
		if !strings.Contains(expr, want) {
			t.Fatalf("upload requirement expression missing %q: %s", want, expr)
		}
	}
	for _, name := range []string{
		"Upload vSphere virtual media to the datastore",
		"Mark vSphere virtual media uploaded",
	} {
		task := tasks[findAnsibleTask(t, tasks, name)]
		if got := fmt.Sprint(task["when"]); !strings.Contains(got, "bootwright_vsphere_media_upload_required") {
			t.Fatalf("%s must only run for the elected uploader, got when=%v", name, task["when"])
		}
	}
}

func TestVSphereMediaNonElecteesWaitForTheUploadMarkerBeforeAttaching(t *testing.T) {
	tasks := flattenAnsibleTasks(readAnsibleTasks(t, vsphereMediaInsertPath))
	waitIdx := findAnsibleTask(t, tasks, "Wait for the elected uploader to publish the vSphere datastore media")
	freshIdx := findAnsibleTask(t, tasks, "Assert the published vSphere datastore media is not older than the staged media")
	attachIdx := findAnsibleTask(t, tasks, "Attach vSphere virtual media to the machine CD-ROM")
	if !(waitIdx < freshIdx && freshIdx < attachIdx) {
		t.Fatalf("non-electees must wait for and validate the upload marker before attaching media")
	}
	wait, ok := tasks[waitIdx]["ansible.builtin.wait_for"].(map[string]any)
	if !ok {
		t.Fatalf("upload wait task must use wait_for so it stays free-strategy safe: %v", tasks[waitIdx])
	}
	if got := fmt.Sprint(wait["path"]); !strings.Contains(got, "bootwright_vsphere_media_uploaded_marker_path") {
		t.Fatalf("upload wait path = %v, want the upload marker", wait["path"])
	}
	if _, ok := wait["timeout"]; !ok {
		t.Fatal("upload wait must bound its timeout so a failed uploader cannot hang the boot")
	}
	for _, idx := range []int{waitIdx, freshIdx} {
		if got := fmt.Sprint(tasks[idx]["when"]); !strings.Contains(got, "not (bootwright_vsphere_media_upload_elected") {
			t.Fatalf("%v must be scoped to non-electees, got when=%v", tasks[idx]["name"], tasks[idx]["when"])
		}
	}
}

func TestVSphereBootStagesTheSharedAgentISOOnce(t *testing.T) {
	tasks := readAnsibleTasks(t, vsphereBootStagePath)
	stageIdx := findAnsibleTask(t, tasks, "Stage generated agent ISO for vSphere upload")
	if got := fmt.Sprint(tasks[stageIdx]["when"]); !strings.Contains(got, "bootwright_vsphere_boot_stage_elected") {
		t.Fatalf("staging the shared agent ISO must be elected, got when=%v", tasks[stageIdx]["when"])
	}
	nested := nestedAnsibleTasks(t, tasks[stageIdx], "block")
	findAnsibleTask(t, nested, "Mark generated agent ISO staged")
	waitIdx := findAnsibleTask(t, tasks, "Wait for the elected machine to stage the agent ISO")
	assertIdx := findAnsibleTask(t, tasks, "Assert the shared staged agent ISO is present")
	if !(stageIdx < waitIdx && waitIdx < assertIdx) {
		t.Fatal("non-electees must wait for the staging marker and then confirm the staged ISO")
	}
	if _, ok := tasks[waitIdx]["ansible.builtin.wait_for"].(map[string]any); !ok {
		t.Fatalf("staging wait must use wait_for so it stays free-strategy safe: %v", tasks[waitIdx])
	}
}

func TestBootAgentMachinePlaybookUsesFreeStrategy(t *testing.T) {
	plays := readAnsiblePlays(t, bootAgentMachinePlaybookPath)
	if len(plays) != 1 {
		t.Fatalf("boot-agent-machine play count = %d, want 1", len(plays))
	}
	if got := plays[0]["strategy"]; got != "free" {
		t.Fatalf("boot-agent-machine strategy = %v, want free so slow machines never gate their peers", got)
	}
	if _, ok := plays[0]["any_errors_fatal"]; ok {
		t.Fatal("free strategy silently ignores any_errors_fatal; the boot play must not claim it")
	}
}

func TestBootPathStaysFreeStrategyCompatible(t *testing.T) {
	roots := []string{
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_vsphere",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_none",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_media_vsphere",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_media_libvirt",
		"ansible/collections/ansible_collections/bootwright/core/roles/support_ssh_readiness",
		bootAgentMachinePlaybookPath,
	}
	for _, root := range roots {
		for _, rel := range repoYAMLFilesUnder(t, root) {
			body := readRepoFile(t, rel)
			for _, forbidden := range []string{"run_once", "any_errors_fatal", "\nserial:", "  serial:"} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("%s uses %q, which the free strategy on the agent-boot play ignores or breaks", rel, strings.TrimSpace(forbidden))
				}
			}
			if strings.Contains(body, "ansible.builtin.pause") {
				t.Fatalf("%s uses pause, which the free strategy cannot run because it bypasses the host loop", rel)
			}
		}
	}
}
