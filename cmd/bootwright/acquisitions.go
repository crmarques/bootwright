package main

import (
	"net/url"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/acquisition"
	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/bundlelocal"
	"github.com/crmarques/bootwright/internal/controller/clients"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/dnsserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/infrastructureservices/ntpserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/proxy"
	cephnative "github.com/crmarques/bootwright/internal/storage/ceph"
	"github.com/crmarques/bootwright/internal/substrate"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
)

const (
	classHostRepositories = "host repositories"
	classMediaStore       = "media store"
)

var openShiftReleaseRepositories = []string{"quay.io/openshift-release-dev/ocp-release", "quay.io/openshift-release-dev/ocp-v4.0-art-dev"}

func listAcquisitions(catalog api.Catalog) ([]cli.Acquisition, error) {
	var acquired []cli.Acquisition
	controllerRows, controllerMachine, err := controllerAcquisitions(catalog)
	if err != nil {
		return nil, err
	}
	acquired = append(acquired, controllerRows...)
	services, err := serviceAcquisitions(catalog)
	if err != nil {
		return nil, err
	}
	acquired = append(acquired, services...)
	acquired = append(acquired, emulatorAcquisitions(catalog)...)
	acquired = append(acquired, clusterAcquisitions(catalog, controllerMachine)...)
	acquired = append(acquired, storageAcquisitions(catalog, controllerMachine)...)
	for _, image := range catalog.OfKind(api.MachineImage) {
		acquired = append(acquired, cli.Acquisition{Kind: "installation media", Artifact: image.Spec().Get("bootMedia").Text(), Class: classMediaStore,
			Origin: "the file or URL media add named", SHA256: strings.TrimPrefix(image.Spec().Get("checksum").Text(), "sha256:"), Machines: []string{controllerMachine}, Consumer: image.Identity()})
	}
	return acquired, nil
}

func controllerAcquisitions(catalog api.Catalog) ([]cli.Acquisition, string, error) {
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
	var rows []cli.Acquisition
	for _, requirement := range clients.NewRequest(selection, tools).Packages {
		rows = append(rows, cli.Acquisition{Kind: "host package", Artifact: requirement, Class: classHostRepositories, Origin: "the controller's dnf repositories", Machines: []string{machine}, Consumer: consumer})
	}
	for _, tool := range tools {
		source, err := bundlelocal.SourceURL(tool)
		if err != nil {
			return nil, "", err
		}
		version := strings.TrimPrefix(tool.Version, "v")
		rows = append(rows, cli.Acquisition{Kind: "client", Artifact: tool.Kind, Version: version, Origin: source, Class: originClass(source), SHA256: tool.SHA256, Bytes: tool.Bytes, Machines: []string{machine}, Consumer: consumer})
		contacts, err := bundlelocal.Contacts(tool)
		if err != nil {
			return nil, "", err
		}
		for _, contact := range contacts {
			if contact.Purpose == "download" {
				continue
			}
			rows = append(rows, cli.Acquisition{Kind: "client " + contact.Purpose, Artifact: tool.Kind, Version: version, Origin: contact.Host, Class: acquisition.OriginClass(contact.Host), Machines: []string{machine}, Consumer: consumer})
		}
	}
	return rows, machine, nil
}

func serviceAcquisitions(catalog api.Catalog) ([]cli.Acquisition, error) {
	compiled := map[api.Kind]string{
		api.ArtifactServer: artifactserver.DefaultImage(), api.DNSServer: dnsserver.Definition(nil).Image,
		api.NTPServer: ntpserver.Definition().Image, api.Proxy: proxy.Definition().Image,
	}
	var rows []cli.Acquisition
	for _, kind := range []api.Kind{api.ArtifactServer, api.DNSServer, api.NTPServer, api.Proxy} {
		for _, service := range catalog.OfKind(kind) {
			if service.Spec().Get("management").Text() != "managed" {
				continue
			}
			reference, err := managedservice.ImageFor(service.Spec(), service.Identity(), compiled[kind])
			if err != nil {
				return nil, err
			}
			if !service.Spec().Get("image", "local").Present() {
				reference, _ = managedservice.MirroredImage(catalog, reference)
			}
			rows = append(rows, imageAcquisition("service image", reference, []string{service.Spec().Get("machineRef").Text()}, service.Identity()))
		}
	}
	return rows, nil
}

func emulatorAcquisitions(catalog api.Catalog) []cli.Acquisition {
	referenced := map[string]bool{}
	for _, machine := range catalog.OfKind(api.Machine) {
		referenced[machine.Spec().Get("substrate", "providerRef").Text()] = true
	}
	var rows []cli.Acquisition
	for _, provider := range catalog.OfKind(api.InfraProvider) {
		if substrate.Variant(provider) != substrate.ArmLibvirt || !referenced[provider.Name()] {
			continue
		}
		reference, _ := managedservice.MirroredImage(catalog, libvirt.EmulatorImage())
		rows = append(rows, imageAcquisition("emulated BMC image", reference, []string{substrate.HostMachine(provider)}, provider.Identity()))
	}
	return rows
}

func clusterAcquisitions(catalog api.Catalog, controllerMachine string) []cli.Acquisition {
	var rows []cli.Acquisition
	for _, cluster := range catalog.OfKind(api.OpenShiftCluster) {
		machines := []string{controllerMachine}
		for _, node := range cluster.Spec().Get("nodes").Items() {
			if machine := node.Get("machineRef").Text(); machine != "" {
				machines = append(machines, machine)
			}
		}
		distribution := cluster.Spec().Get("distribution")
		release := distribution.Get("release")
		if distribution.Get("type").Text() == "openshift" {
			for _, repository := range openShiftReleaseRepositories {
				if agentinstall.NeverContacted(catalog, cluster, repository) {
					continue
				}
				rows = append(rows, cli.Acquisition{Kind: "release", Artifact: repository, Version: release.Get("version").Text(), Origin: repository, Class: originClass("https://" + repository),
					Machines: machines, Consumer: cluster.Identity()})
			}
		} else {
			rows = append(rows, cli.Acquisition{Kind: "release", Artifact: "the release image its installer names", Version: release.Get("version").Text(), Origin: "quay.io", Class: acquisition.ClassPublic,
				Machines: machines, Consumer: cluster.Identity()})
		}
		for _, source := range agentinstall.ImageDigestSources(catalog, cluster) {
			entry, _ := source.(map[string]any)
			mirrors, _ := entry["mirrors"].([]any)
			for _, mirror := range mirrors {
				location, _ := mirror.(string)
				origin, _ := entry["source"].(string)
				rows = append(rows, cli.Acquisition{Kind: "release mirror", Artifact: origin, Origin: location, Class: originClass("https://" + location),
					Machines: machines, Consumer: cluster.Identity()})
			}
		}
	}
	return rows
}

func storageAcquisitions(catalog api.Catalog, controllerMachine string) []cli.Acquisition {
	var rows []cli.Acquisition
	for _, cluster := range catalog.OfKind(api.CephCluster) {
		spec := cluster.Spec()
		if spec.Get("management").Text() != "managed" {
			continue
		}
		hosts := []string{}
		for _, node := range spec.Get("topology", "nodes").Items() {
			if machine := node.Get("machineRef").Text(); machine != "" {
				hosts = append(hosts, machine)
			}
		}
		images := []api.Value{spec.Get("image")}
		for _, field := range spec.Get("images").Fields() {
			images = append(images, field.Value)
		}
		for _, image := range images {
			base := image.Get("base").Text()
			if base == "" {
				continue
			}
			version, digest := image.Get("version").Text(), ""
			if strings.HasPrefix(strings.ToLower(version), "sha256:") {
				digest = strings.ToLower(strings.TrimPrefix(strings.ToLower(version), "sha256:"))
			}
			rows = append(rows, cli.Acquisition{Kind: "storage image", Artifact: base, Version: version, Origin: base, Class: originClass("https://" + base), SHA256: digest,
				Machines: append([]string{controllerMachine}, hosts...), Consumer: cluster.Identity()})
		}
		repository := spec.Get("packageRepository").Text()
		if repository == "" {
			repository = cephnative.DefaultPackageRepository
		}
		rows = append(rows, cli.Acquisition{Kind: "storage packages", Artifact: "cephadm and its packages", Version: spec.Get("packageVersion").Text(), Origin: repository, Class: originClass(repository),
			Machines: hosts, Consumer: cluster.Identity()})
	}
	return rows
}

func imageAcquisition(kind, reference string, machines []string, consumer string) cli.Acquisition {
	repository, digest, _ := strings.Cut(reference, "@sha256:")
	if first, _, _ := strings.Cut(repository, "/"); !strings.ContainsAny(first, ".:") && first != "localhost" {
		repository = "docker.io/" + repository
	}
	version := ""
	if digest != "" {
		version = "sha256:" + digest
	}
	return cli.Acquisition{Kind: kind, Artifact: repository, Version: version, Origin: repository, Class: originClass("https://" + repository), SHA256: digest, Machines: machines, Consumer: consumer}
}

func originClass(location string) string {
	parsed, err := url.Parse(location)
	if err != nil || parsed.Host == "" {
		return acquisition.ClassDeclared
	}
	return acquisition.OriginClass(parsed.Host)
}
