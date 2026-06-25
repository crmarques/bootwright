package preflight

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// kubeVirtNetworkRefChecks verifies that every kubevirt networkAttachment on a
// provider resolves to a usable network on the host cluster before its VMs boot.
func kubeVirtNetworkRefChecks(provider v1alpha1.InfraProvider, kubeconfigPath string, deps Deps) []Check {
	var checks []Check
	for _, attachment := range provider.Spec.NetworkAttachments {
		if attachment.KubeVirt == nil {
			continue
		}
		checks = append(checks, kubeVirtNetworkRefCheck(attachment.Name, attachment.KubeVirt.NetworkRef, kubeconfigPath, deps))
	}
	return checks
}

// kubeVirtNetworkRefCheck asserts the NetworkAttachmentDefinition the VM's
// multus networkName resolves actually exists. This is the one artifact the VM
// consumes in every case: a referenced NAD directly, or the NAD that a (C)UDN
// derives under the same name in the selected namespace. Probing it is uniform
// over apiGroup/kind, so the check never needs per-kind code — which is what
// keeps Bootwright resilient to UDN/CUDN API changes.
func kubeVirtNetworkRefCheck(attachmentName string, ref v1alpha1.KubeVirtNetworkRef, kubeconfigPath string, deps Deps) Check {
	name := attachmentName + " network"
	remediation := fmt.Sprintf("create %s %q (apiGroup %q) for namespace %q on the host cluster and wait for its NetworkAttachmentDefinition to be ready",
		ref.EffectiveKind(), ref.Name, ref.EffectiveAPIGroup(), ref.Namespace)
	impact := "KubeVirt VMs attach to this network by NetworkAttachmentDefinition <namespace>/<name>; without it the child nodes have no network"

	args := []string{"--kubeconfig", kubeconfigPath, "get", "networkattachmentdefinition.k8s.cni.cncf.io", ref.Name, "-o", "name"}
	if ref.Namespace != "" {
		args = append(args, "-n", ref.Namespace)
	}
	out, err := deps.CommandOutput("kubectl", args...)
	if err != nil {
		evidence := strings.TrimSpace(string(out))
		if evidence == "" {
			evidence = err.Error()
		}
		return failCheck(checkGroupInstallerTools, name, evidence, impact, remediation)
	}
	if !strings.Contains(string(out), "networkattachmentdefinition.k8s.cni.cncf.io/"+ref.Name) {
		return failCheck(checkGroupInstallerTools, name, strings.TrimSpace(string(out)), impact, remediation)
	}
	return okCheck(checkGroupInstallerTools, name, ref.Namespace+"/"+ref.Name)
}
