package listing

import (
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/evidence"
	"github.com/crmarques/bootwright/internal/controller/tooltable"
	"github.com/crmarques/bootwright/internal/infrastructureservices/mirrorfill"
)

type MirroredClient struct {
	Tool, Location, Source, SHA256 string
	CatalogArtifact                string
	Bytes                          int64
}

func (t Tools) MirroredClients(tools []evidence.ToolRequest) ([]MirroredClient, error) {
	var clients []MirroredClient
	for _, tool := range tools {
		if !tooltable.PublicOrigin(tool) || tool.Mirror == "" || tool.SHA256 == "" {
			continue
		}
		location, err := t.SourceURL(tool)
		if err != nil {
			return nil, err
		}
		origin := tool
		origin.Mirror = ""
		source, err := t.SourceURL(origin)
		if err != nil {
			return nil, err
		}
		clients = append(clients, MirroredClient{Tool: tool.Kind + " " + strings.TrimPrefix(tool.Version, "v"), Location: location, Source: source, SHA256: tool.SHA256, Bytes: tool.Bytes})
	}
	return clients, nil
}

func (t Tools) MirrorFiles(catalog api.Catalog) ([]mirrorfill.Artifact, error) {
	tools, err := controller.SelectTools(catalog)
	if err != nil {
		return nil, err
	}
	mirrored, err := t.MirroredClients(tools)
	if err != nil {
		return nil, err
	}
	applyCatalogSources(catalog, mirrored)
	artifacts := make([]mirrorfill.Artifact, 0, len(mirrored))
	for _, client := range mirrored {
		artifacts = append(artifacts, mirrorfill.Artifact{Bytes: client.Bytes, Location: client.Location, SHA256: client.SHA256, Source: client.Source, CatalogArtifact: client.CatalogArtifact})
	}
	return artifacts, nil
}

func applyCatalogSources(catalog api.Catalog, clients []MirroredClient) {
	environments := catalog.OfKind(api.Environment)
	if len(environments) != 1 {
		return
	}
	name := environments[0].Spec().Get("mirror", "artifacts", "toolSources", "helm", "catalogArtifact").Text()
	if name == "" {
		return
	}
	for index, client := range clients {
		if strings.HasPrefix(client.Tool, "helm ") {
			clients[index].CatalogArtifact = name
			clients[index].Source = ""
		}
	}
}
