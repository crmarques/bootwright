package main

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
)

const hostedRoot = "/var/lib/bootwright-hosted"

func TestEveryDomainNamesTheOneHostedRoot(t *testing.T) {
	for owner, root := range map[string]string{
		"managedservice.HostedRoot": managedservice.HostedRoot,
		"artifactserver.HostedRoot": artifactserver.HostedRoot,
		"agentinstall.HostedRoot":   agentinstall.HostedRoot,
	} {
		if root != hostedRoot {
			t.Errorf("%s = %q, not the hosted root %q every context's hosted content lies beneath", owner, root, hostedRoot)
		}
	}
	assets := ansible.Automation()
	for name, spelled := range map[string]string{
		"plugins/module_utils/hosted_area.py":                  `ROOT = "` + hostedRoot + `"` + "\n",
		"roles/substrate_libvirt_machine/defaults/main.yml":    "substrate_libvirt_machine_hosted: " + hostedRoot + "\n",
		"roles/managedos_install_anaconda/defaults/main.yml":   "managedos_install_anaconda_work_parent: " + hostedRoot + "\n",
		"plugins/module_utils/physical_erase_controller.py":    "ROOT = Path(hosted_area.ROOT)\n",
		"roles/infra_artifact_server_nginx/tasks/apply.yml":    "== '" + hostedRoot + "/' ~ bootwright_artifact_server_request.identity.context\n",
		"roles/infra_managed_service/tasks/apply.yml":          "== '" + hostedRoot + "/' ~ bootwright_managed_service_request.identity.context\n",
		"roles/containercluster_media_agent/tasks/apply.yml":   "== '" + hostedRoot + "/' ~ bootwright_cluster_media_request.identity.context\n",
		"roles/containercluster_install_agent/tasks/apply.yml": "== '" + hostedRoot + "/' ~ bootwright_cluster_install_request.identity.context\n",
	} {
		if !strings.Contains(string(assets[automationCollection+name]), spelled) {
			t.Errorf("%s no longer spells the hosted root as %q", name, strings.TrimSpace(spelled))
		}
	}
}
