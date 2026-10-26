package agentinstall

import (
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/substrate"
)

func mediaLocation(catalog api.Catalog, cluster api.Object, controller, contextName string, needs *Requirements) (string, Publication, string, machine.Placement, error) {
	arms := clusterArms(catalog, cluster)
	if len(arms) > 0 && len(arms) == cluster.Spec().Get("nodes").Len() && !slices.ContainsFunc(arms, func(arm string) bool { return arm != substrate.ArmKubeVirt }) {
		if cluster.Spec().Has("install", "agent", "redfishVirtualMedia") {
			return "", Publication{}, "", machine.Placement{}, refusalAt("api.invariant", "$.spec.install.agent.redfishVirtualMedia", "a push-only cluster publishes no Redfish virtual media", "remove spec.install.agent.redfishVirtualMedia")
		}
		return "push", Publication{Path: WorkRoot(contextName, cluster.Name()) + "/" + ImageFile}, "", machine.Placement{Connection: machine.ConnectionLocal, Machine: controller}, nil
	}
	selection := cluster.Spec().Get("install", "agent", "redfishVirtualMedia", "artifactServerEndpoint")
	server, err := artifactserver.Selected(catalog, selection, cluster.Identity())
	if err != nil {
		return "", Publication{}, "", machine.Placement{}, err
	}
	published, certificate, err := artifactserver.PrivatePath(catalog, server, selection, contextName, consumerPrefix, cluster.Name(), cluster.Identity())
	if err != nil {
		return "", Publication{}, "", machine.Placement{}, err
	}
	placement, err := artifactserver.PlacementFor(catalog, server, controller)
	if err == nil {
		needs.ArtifactServers = append(needs.ArtifactServers, server.Name())
	}
	return "fetch", Publication{Path: published.Path, URL: published.URL}, certificate, placement, err
}

func bindPushedMedia(media MediaRequest, needs Requirements, install *InstallRequest) error {
	if media.DeliveryKind != "push" {
		return nil
	}
	definition, err := mediaDefinition(media, needs, reconciliation.Apply)
	if err != nil {
		return err
	}
	install.ImageIdentity, err = reconciliation.RequestDigest(definition)
	install.PublicationRoot = Publication{}
	return err
}
