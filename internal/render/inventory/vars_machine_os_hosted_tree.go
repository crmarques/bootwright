package inventory

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/artifacts"
	"github.com/crmarques/bootwright/internal/infra/media"
	"github.com/crmarques/bootwright/internal/render/installer"
)

// machineOSHostedTreeVars resolves the publish + fetch coordinates for a
// hostedTree packageSource: bootwright extracts the DVD (fromMedia) once into
// the cluster artifact server's document root and the installing node fetches
// GPG-signed packages from it over the machineBoot endpoint. It returns the
// tree URL to render as installer.sourceURL and the image.installTree vars the
// Ansible extraction step consumes. ok is false when the DVD reference, a
// bootwright-managed artifact server, or its node-reachable machineBoot endpoint
// cannot be resolved; the caller then leaves sourceURL empty so the boot ISO
// install fails loudly rather than mis-installing. The scheme follows the
// machineBoot listener (author it http: the installer verifies TLS and would
// reject a self-signed artifact certificate the node does not trust).
func machineOSHostedTreeVars(state v1alpha1.State, ci v1alpha1.ClusterInstall, source *v1alpha1.MachineInstallPackageSource) (string, map[string]any, bool) {
	hostedTree := source.GetHostedTree()
	if hostedTree == nil {
		return "", nil, false
	}
	dvd, err := media.Resolve(hostedTree.FromMedia)
	if err != nil {
		return "", nil, false
	}
	sourceID := managedOSSourceID("", dvd.Key, dvd.Original)
	// machineBoot is the modeled node-reachable artifact endpoint; a managed
	// server (Config != nil) is required because bootwright extracts into its
	// document root, which it does not own for an external server.
	server, endpointName, ok := artifacts.ResolveConsumerEndpoint(state, ci, v1alpha1.ArtifactConsumerMachineBoot)
	if !ok || server.Config == nil {
		return "", nil, false
	}
	base := installer.ArtifactServerEndpointURL(state, server, endpointName)
	if base == "" {
		return "", nil, false
	}
	treeURL := base + "trees/" + sourceID + "/"
	tree := map[string]any{
		"sourceId":    sourceID,
		"fetchUrl":    treeURL,
		"publishPath": fmt.Sprintf("{{ bootwright_managed_services_dir }}/%s/public/trees/%s", server.Component.Metadata.Name, sourceID),
		"fromMedia":   machineOSHostedTreeSourceVars(dvd),
	}
	return treeURL, tree, true
}

// machineOSHostedTreeSourceVars projects the resolved DVD reference into the
// vars the Ansible extraction step reads to stage and verify the source ISO,
// mirroring machineOSInstallImageVars for the boot media.
func machineOSHostedTreeSourceVars(dvd media.Resolved) map[string]any {
	out := map[string]any{
		"kind":     dvd.Kind,
		"original": dvd.Original,
	}
	if dvd.Key != "" {
		out["key"] = dvd.Key
	}
	if dvd.Path != "" {
		out["path"] = dvd.Path
	}
	if dvd.URL != "" {
		out["url"] = dvd.URL
	}
	return out
}
