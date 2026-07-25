package preflight

import (
	"fmt"
	"net/url"
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

	resourcePath := fmt.Sprintf("/apis/k8s.cni.cncf.io/v1/namespaces/%s/network-attachment-definitions/%s", url.PathEscape(ref.Namespace), url.PathEscape(ref.Name))
	out, err := deps.CommandOutputLocalRoot("kubectl", "--kubeconfig", kubeconfigPath, "--request-timeout=5s", "get", "--raw="+resourcePath)
	if err != nil {
		evidence := strings.TrimSpace(string(out))
		if evidence == "" {
			evidence = err.Error()
		}
		if kubernetesResourceMissing(evidence, ref.Name) {
			return failCheck(checkGroupInstallerTools, name, evidence, impact, remediation)
		}
		return failCheck(checkGroupInstallerTools, name, evidence, impact, "ensure "+kubeconfigPath+" identifies a reachable Kubernetes API server and grants access to k8s.cni.cncf.io/v1")
	}
	if !strings.Contains(string(out), ref.Name) {
		return failCheck(checkGroupInstallerTools, name, strings.TrimSpace(string(out)), impact, "ensure "+kubeconfigPath+" identifies a reachable Kubernetes API server and grants access to k8s.cni.cncf.io/v1")
	}
	return okCheck(checkGroupInstallerTools, name, ref.Namespace+"/"+ref.Name)
}
