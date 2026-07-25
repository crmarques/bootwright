package repocheck

import (
	"strings"
	"testing"
)

func TestHostVirtctlUsesMaterializedHostKubeconfig(t *testing.T) {
	body := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/playbooks/task_host_virtctl_provision.yml")
	if !strings.Contains(body, "bootwright_kubevirt_host_kubeconfigs[bootwright_task_host_cluster_name]") {
		t.Fatal("host virtctl playbook does not use the materialized host kubeconfig map")
	}
	if strings.Contains(body, "bootwright_clusters_dir") {
		t.Fatal("host virtctl playbook constructs a durable cluster-secret path")
	}
}
