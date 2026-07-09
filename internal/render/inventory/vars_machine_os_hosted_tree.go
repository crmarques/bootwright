package inventory

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/artifacts"
	"github.com/crmarques/bootwright/internal/infra/media"
	"github.com/crmarques/bootwright/internal/render/installer"
)

func machineOSHostedTreeVars(state v1alpha1.State, source *v1alpha1.MachineInstallPackageSource) (string, map[string]any, bool) {
	hostedTree := source.GetHostedTree()
	if hostedTree == nil {
		return "", nil, false
	}
	dvd, err := media.Resolve(hostedTree.FromMedia)
	if err != nil {
		return "", nil, false
	}
	sourceID := managedOSSourceID("", dvd.Key, dvd.Original)
	server, endpointName, ok := artifacts.ResolveEndpointRef(state, hostedTree.ArtifactServerEndpoint)
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
