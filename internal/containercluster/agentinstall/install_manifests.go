package agentinstall

import (
	"fmt"
	"regexp"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// installManifests admits only compiler-owned install-time objects.
func installManifests(cluster api.Object) []InstallManifest {
	result := []InstallManifest{}
	config := cluster.Spec().Get("networking", "ovnKubernetesConfig")
	if !config.Present() || config.Len() == 0 {
		return result
	}
	result = append(result, InstallManifest{File: "bootwright-00-ovn-network.yaml", APIVersion: "operator.openshift.io/v1", Kind: "Network", Name: "cluster", Content: map[string]any{"apiVersion": "operator.openshift.io/v1", "kind": "Network", "metadata": map[string]any{"name": "cluster"}, "spec": map[string]any{"defaultNetwork": map[string]any{"type": "OVNKubernetes", "ovnKubernetesConfig": nativeValue(config)}}}})
	return result
}

var manifestFilePattern = regexp.MustCompile(`^bootwright-[0-9]{2}-[a-z0-9-]+\.yaml$`)

func validateInstallManifests(manifests []InstallManifest) error {
	previous := ""
	for _, m := range manifests {
		if !manifestFilePattern.MatchString(m.File) || m.File <= previous {
			return fmt.Errorf("install-time manifest filenames must be ordered unique top-level Bootwright YAML names")
		}
		previous = m.File
		if m.Kind != "Network" || m.APIVersion != "operator.openshift.io/v1" || m.Name != "cluster" {
			return fmt.Errorf("install-time manifest has unsupported kind or identity")
		}
		metadata, ok := m.Content["metadata"].(map[string]any)
		if !ok || metadata["name"] != m.Name || m.Content["apiVersion"] != m.APIVersion || m.Content["kind"] != m.Kind {
			return fmt.Errorf("install-time manifest content differs from its identity")
		}
	}
	return nil
}
