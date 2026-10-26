package agentinstall

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func runtimeRefusals(contextName, continuation string, request InstallRequest) map[string]error {
	if continuation == "" {
		continuation = "bootwright apply --context " + contextName
	}
	refusals := map[string]error{}
	for index, node := range request.Nodes {
		for reason, test := range map[string]struct{ message, remedy string }{
			"media-missing":    {"the cluster published no boot image for this node", "repeat " + continuation + " so the media block publishes the cluster image"},
			"private-delivery": {"the private boot image could not be fetched through its verified artifact listener", "restore the artifact listener and its certificate trust, then repeat " + continuation},
			"controller-read":  {"the management controller did not answer a complete node state read", "restore the declared controller connection and its trust, then repeat the command"},
			"media-insert":     {"the management controller refused the private boot image or its transport trust", "inspect the retained adapter output, correct the controller's virtual-media trust, then repeat " + continuation},
			"media-eject":      {"the management controller did not prove the boot image released", "restore the controller connection and virtual-media trust, then repeat the command"},
			"node-unreachable": {"the node did not answer its required installation ports within the frozen boot budget", "repair the node's installation network and boot path, then repeat " + continuation},
		} {
			refusals[nodeRefusal(reason, index)] = &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{{
				Severity: "error", Code: "lifecycle.state",
				Object:      &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: string(api.Machine), Name: node.Machine},
				Message:     "node " + node.Name + " (Machine/" + node.Machine + ", " + node.Address + ") of ContainerCluster/" + request.Identity.Cluster + ": " + test.message,
				Remediation: test.remedy,
			}}}
		}
	}
	return refusals
}
