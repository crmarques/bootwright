package listing

import (
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/acquisition"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/clients"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
)

func (t Tools) controllerAcquisitions(catalog api.Catalog) ([]Row, string, error) {
	selection, err := controller.Select(catalog)
	if err != nil {
		return nil, "", err
	}
	tools, err := controller.SelectTools(catalog)
	if err != nil {
		return nil, "", err
	}
	machine, consumer := selection.MachineName(), ""
	if environments := catalog.OfKind(api.Environment); len(environments) == 1 {
		consumer = environments[0].Identity()
	}
	var rows []Row
	for _, requirement := range clients.NewRequest(selection, tools).Packages {
		rows = append(rows, Row{Kind: "host package", Artifact: requirement, Class: classHostRepositories, Origin: "the controller's dnf repositories", Machines: []string{machine}, Consumer: consumer})
	}
	for _, tool := range tools {
		source, err := t.SourceURL(tool)
		if err != nil {
			return nil, "", err
		}
		version := strings.TrimPrefix(tool.Version, "v")
		rows = append(rows, Row{Kind: "client", Artifact: tool.Kind, Version: version, Origin: source, Class: originClass(source), SHA256: tool.SHA256, Bytes: tool.Bytes, Machines: []string{machine}, Consumer: consumer})
		contacts, err := t.Contacts(tool)
		if err != nil {
			return nil, "", err
		}
		for _, contact := range contacts {
			if contact.Purpose == "download" {
				continue
			}
			rows = append(rows, Row{Kind: "client " + contact.Purpose, Artifact: tool.Kind, Version: version, Origin: contact.Host, Class: acquisition.OriginClass(contact.Host), Machines: []string{machine}, Consumer: consumer})
		}
	}
	return rows, machine, nil
}

func (t Tools) mirrorFillAcquisitions(catalog api.Catalog) ([]Row, error) {
	mirror, declared := infrastructureservices.ArtifactMirrorOf(catalog)
	if !declared || !mirror.Managed {
		return nil, nil
	}
	tools, err := controller.SelectTools(catalog)
	if err != nil {
		return nil, err
	}
	mirrored, err := t.MirroredClients(tools)
	if err != nil {
		return nil, err
	}
	applyCatalogSources(catalog, mirrored)
	host := []string{mirror.Server.Spec().Get("machineRef").Text()}
	var rows []Row
	for _, client := range mirrored {
		artifact, version, _ := strings.Cut(client.Tool, " ")
		origin, class := client.Source, originClass(client.Source)
		if client.CatalogArtifact != "" {
			origin, class = client.CatalogArtifact, "artifact catalog"
		}
		rows = append(rows, Row{Kind: "mirror file", Artifact: artifact, Version: version, Origin: origin, Class: class,
			SHA256: client.SHA256, Bytes: client.Bytes, Machines: host, Consumer: mirror.Server.Identity()})
	}
	return rows, nil
}
