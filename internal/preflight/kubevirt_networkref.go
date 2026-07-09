package preflight

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

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

func kubeVirtNetworkRefCheck(attachmentName string, ref v1alpha1.KubeVirtNetworkRef, kubeconfigPath string, deps Deps) Check {
	name := attachmentName + " network"
	remediation := fmt.Sprintf("create %s %q (apiGroup %q) for namespace %q on the host cluster and wait for its NetworkAttachmentDefinition to be ready",
		ref.EffectiveKind(), ref.Name, ref.EffectiveAPIGroup(), ref.Namespace)
	impact := "KubeVirt VMs attach to this network by NetworkAttachmentDefinition <namespace>/<name>; without it the child nodes have no network"

	args := []string{"--kubeconfig", kubeconfigPath, "--request-timeout=5s", "get", "networkattachmentdefinition.k8s.cni.cncf.io", ref.Name, "-o", "name"}
	if ref.Namespace != "" {
		args = append(args, "-n", ref.Namespace)
	}
	out, err := deps.CommandOutputLocalRoot("kubectl", args...)
	if err != nil {
		evidence := strings.TrimSpace(string(out))
		if evidence == "" {
			evidence = err.Error()
		}
		if kubeconfigUnreadable(evidence) {
			return failCheck(checkGroupInstallerTools, name, evidence, impact, "ensure "+kubeconfigPath+" is a readable, valid kubeconfig (bootwright manages it under the root-owned workspace)")
		}
		return failCheck(checkGroupInstallerTools, name, evidence, impact, remediation)
	}
	if !strings.Contains(string(out), "networkattachmentdefinition.k8s.cni.cncf.io/"+ref.Name) {
		return failCheck(checkGroupInstallerTools, name, strings.TrimSpace(string(out)), impact, remediation)
	}
	return okCheck(checkGroupInstallerTools, name, ref.Namespace+"/"+ref.Name)
}
