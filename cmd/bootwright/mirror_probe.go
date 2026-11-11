package main

import (
	"context"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/bundlelocal"
	"github.com/crmarques/bootwright/internal/controller/clients"
	environmentpreflight "github.com/crmarques/bootwright/internal/environment/preflight"
	"github.com/crmarques/bootwright/internal/infrastructureservices/mirrorfill"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

type materialLender interface {
	WithMaterial(context.Context, lifecycle.MaterialRequest, func(context.Context, map[string]secrets.Material) error) error
}

type mirrorReader func(context.Context, []bundlelocal.MirrorFile, clients.Request, []byte) ([]bundlelocal.MirrorReading, error)

type mirrorProbe struct {
	material materialLender
	read     mirrorReader
}

func readMirror(ctx context.Context, files []bundlelocal.MirrorFile, request clients.Request, anchor []byte) ([]bundlelocal.MirrorReading, error) {
	return bundlelocal.ProbeMirror(ctx, files, request.Egress, anchor)
}

func (p mirrorProbe) ProbeMirror(ctx context.Context, selected string, catalog api.Catalog) ([]environmentpreflight.MirrorFile, error) {
	selection, err := controller.Select(catalog)
	if err != nil {
		return nil, err
	}
	tools, err := controller.SelectTools(catalog)
	if err != nil {
		return nil, err
	}
	mirrored, err := mirroredClients(tools)
	if err != nil {
		return nil, err
	}
	var files []environmentpreflight.MirrorFile
	var probed []bundlelocal.MirrorFile
	for _, client := range mirrored {
		files = append(files, environmentpreflight.MirrorFile{Tool: client.tool, URL: client.location, Publisher: client.source, Bytes: client.bytes})
		probed = append(probed, bundlelocal.MirrorFile{URL: client.location, Bytes: client.bytes})
	}
	if len(probed) == 0 {
		return nil, nil
	}
	request := clients.NewRequest(selection, tools)
	var readings []bundlelocal.MirrorReading
	_, reference := controller.ArtifactMirror(catalog)
	if reference == "" {
		readings, err = p.read(ctx, probed, request, nil)
	} else {
		err = p.material.WithMaterial(ctx, lifecycle.MaterialRequest{ContextName: selected, Secrets: []string{reference}}, func(inner context.Context, material map[string]secrets.Material) error {
			anchor, err := clients.MirrorAnchor(material, reference)
			if err != nil {
				return err
			}
			defer clear(anchor)
			readings, err = p.read(inner, probed, request, anchor)
			return err
		})
	}
	if err != nil {
		return nil, err
	}
	for index, reading := range readings[:min(len(readings), len(files))] {
		files[index].Status, files[index].Served, files[index].Failure = reading.Status, reading.Served, reading.Failure
	}
	return files, nil
}

type mirroredClient struct {
	tool, location, source, sha256 string
	bytes                          int64
}

func mirroredClients(tools []controller.ToolRequest) ([]mirroredClient, error) {
	var clients []mirroredClient
	for _, tool := range tools {
		if !controller.PublicOrigin(tool) || tool.Mirror == "" || tool.SHA256 == "" {
			continue
		}
		location, err := bundlelocal.SourceURL(tool)
		if err != nil {
			return nil, err
		}
		origin := tool
		origin.Mirror = ""
		source, err := bundlelocal.SourceURL(origin)
		if err != nil {
			return nil, err
		}
		clients = append(clients, mirroredClient{tool: tool.Kind + " " + strings.TrimPrefix(tool.Version, "v"), location: location, source: source, sha256: tool.SHA256, bytes: tool.Bytes})
	}
	return clients, nil
}

func mirrorFileArtifacts(catalog api.Catalog) ([]mirrorfill.Artifact, error) {
	tools, err := controller.SelectTools(catalog)
	if err != nil {
		return nil, err
	}
	mirrored, err := mirroredClients(tools)
	if err != nil {
		return nil, err
	}
	artifacts := make([]mirrorfill.Artifact, 0, len(mirrored))
	for _, client := range mirrored {
		artifacts = append(artifacts, mirrorfill.Artifact{Bytes: client.bytes, Location: client.location, SHA256: client.sha256, Source: client.source})
	}
	return artifacts, nil
}
