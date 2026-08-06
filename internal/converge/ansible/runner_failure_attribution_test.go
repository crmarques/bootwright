package ansible

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFailureLog(t *testing.T, lines []string) string {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "out.log")
	if err := os.WriteFile(logPath, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}
	return logPath
}

func TestSummarizeFailureNamesTheTaskThatFailedNotTheLastTaskRun(t *testing.T) {
	got := summarizeFailure(writeFailureLog(t, []string{
		"PLAY [Check host prerequisites] *",
		"TASK [Open the host connection] *",
		"ok: [bastion]",
		"TASK [bootwright.core.check_host_preflight : Verify Ansible transport] *",
		"fatal: [bastion]: FAILED! => {\"msg\": \"transport probe failed\"}",
		"TASK [bootwright.core.check_storage_preflight : Resolve storage nodes on this host] *",
		"ok: [localhost]",
		"PLAY RECAP *",
	}), 50)
	if !strings.Contains(got, "failed task: TASK [bootwright.core.check_host_preflight : Verify Ansible transport]") {
		t.Fatalf("summary must name the task the failure happened in:\n%s", got)
	}
	if strings.Contains(got, "Resolve storage nodes on this host") && strings.Contains(got, "failed task: TASK [bootwright.core.check_storage_preflight") {
		t.Fatalf("summary must not attribute the failure to a later task that succeeded:\n%s", got)
	}
	if !strings.Contains(got, "failure: transport probe failed") {
		t.Fatalf("summary must carry the failure message:\n%s", got)
	}
}

func TestSummarizeFailurePrefersARealFailureOverAToleratedUnreachable(t *testing.T) {
	got := summarizeFailure(writeFailureLog(t, []string{
		"PLAY [Check host prerequisites] *",
		"TASK [Open the host connection] *",
		"fatal: [storage__ceph-01__srv4203]: UNREACHABLE! => {\"msg\": \"Failed to connect to the host via ssh: no route to host\", \"unreachable\": true}",
		"ok: [bastion]",
		"TASK [Refuse an unreachable host this run does not install] *",
		"fatal: [storage__ceph-01__vrt3068]: FAILED! => {\"msg\": \"vrt3068 is unreachable at 10.22.254.5 as root\"}",
		"PLAY RECAP *",
	}), 50)
	if !strings.Contains(got, "failure: vrt3068 is unreachable at 10.22.254.5 as root") {
		t.Fatalf("summary must report the host the run refuses, not a host it tolerates:\n%s", got)
	}
	if !strings.Contains(got, "failed task: TASK [Refuse an unreachable host this run does not install]") {
		t.Fatalf("summary must name the refusing task:\n%s", got)
	}
}

func TestSummarizeFailureAttributesAnUnreachableToItsOwnTask(t *testing.T) {
	got := summarizeFailure(writeFailureLog(t, []string{
		"PLAY [Apply storage cluster] *",
		"TASK [Gathering Facts] *",
		"fatal: [node-01]: UNREACHABLE! => {\"msg\": \"Failed to connect to the host via ssh: permission denied\", \"unreachable\": true}",
		"TASK [storage_cluster_cephadm : Read cluster status] *",
		"ok: [node-02]",
		"PLAY RECAP *",
	}), 50)
	if !strings.Contains(got, "failed task: TASK [Gathering Facts]") {
		t.Fatalf("an unreachable host must be attributed to the task that could not connect:\n%s", got)
	}
	if !strings.Contains(got, "permission denied") {
		t.Fatalf("summary must carry the unreachable reason:\n%s", got)
	}
}

func TestSummarizeFailureNamesTheFailingHost(t *testing.T) {
	got := summarizeFailure(writeFailureLog(t, []string{
		"TASK [bootwright.core.machine_substrate_kubevirt : Apply KubeVirt VirtualMachine] *",
		"ok: [machine__container__hub__m0]",
		"fatal: [machine__container__hub__m1]: FAILED! => {\"msg\": \"admission webhook denied the request\"}",
		"PLAY RECAP *",
	}), 50)
	if !strings.Contains(got, "failure: admission webhook denied the request (host machine__container__hub__m1)") {
		t.Fatalf("one task now covers every machine, so the summary must name the failing host:\n%s", got)
	}
}

func TestSummarizeFailureOmitsTheHostWhenOnlyAnEnrichedErrorIsPresent(t *testing.T) {
	got := summarizeFailure(writeFailureLog(t, []string{
		"TASK [bootwright.core.machine_substrate_kubevirt : Apply KubeVirt VirtualMachine] *",
		"[ERROR]: the requested kubeconfig was not found",
		"PLAY RECAP *",
	}), 50)
	if strings.Contains(got, "(host ") {
		t.Fatalf("an [ERROR]: line carries no host, so none may be invented:\n%s", got)
	}
}
