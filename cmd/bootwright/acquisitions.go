package main

import (
	"net/url"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/acquisition"
	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/bundlelocal"
	"github.com/crmarques/bootwright/internal/controller/clients"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/dnsserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/infrastructureservices/mirrorfill"
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

var releaseSignatureStores = []string{"https://storage.googleapis.com/openshift-release/official/signatures/openshift/release",
	"https://mirror.openshift.com/pub/openshift-v4/signatures/openshift/release"}

var openShiftReleaseRepositories = []string{infrastructureservices.OpenShiftReleaseRepository, infrastructureservices.OpenShiftComponentRepository}

func listAcquisitions(catalog api.Catalog) ([]cli.Acquisition, error) {
	var acquired []cli.Acquisition
	controllerRows, controllerMachine, err := controllerAcquisitions(catalog)
	if err != nil {
		return nil, err
	}
	acquired = append(acquired, controllerRows...)
	fills, err := mirrorFillAcquisitions(catalog)
	if err != nil {
		return nil, err
	}
	acquired = append(acquired, fills...)
	images, err := imageAcquisitions(catalog)
	if err != nil {
		return nil, err
	}
	acquired = append(acquired, images...)
	releases, err := mirrorReleaseAcquisitions(catalog)
	if err != nil {
		return nil, err
	}
	acquired = append(acquired, releases...)
	acquired = append(acquired, clusterAcquisitions(catalog, controllerMachine)...)
	acquired = append(acquired, storageAcquisitions(catalog, controllerMachine)...)
	acquired = append(acquired, operatorAcquisitions(catalog)...)
	acquired = append(acquired, machineRepositoryAcquisitions(catalog)...)
	for _, image := range catalog.OfKind(api.MachineImage) {
		acquired = append(acquired, cli.Acquisition{Kind: "installation media", Artifact: image.Spec().Get("bootMedia").Text(), Class: classMediaStore,
			Origin: "the file or URL media add named", SHA256: strings.TrimPrefix(image.Spec().Get("checksum").Text(), "sha256:"), Machines: []string{controllerMachine}, Consumer: image.Identity()})
	}
	return withRegistryHosts(acquired), nil
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

func mirrorFillAcquisitions(catalog api.Catalog) ([]cli.Acquisition, error) {
	mirror, declared := infrastructureservices.ArtifactMirrorOf(catalog)
	if !declared || !mirror.Managed {
		return nil, nil
	}
	tools, err := controller.SelectTools(catalog)
	if err != nil {
		return nil, err
	}
	mirrored, err := mirroredClients(tools)
	if err != nil {
		return nil, err
	}
	host := []string{mirror.Server.Spec().Get("machineRef").Text()}
	var rows []cli.Acquisition
	for _, client := range mirrored {
		artifact, version, _ := strings.Cut(client.tool, " ")
		rows = append(rows, cli.Acquisition{Kind: "mirror file", Artifact: artifact, Version: version, Origin: client.source, Class: originClass(client.source),
			SHA256: client.sha256, Bytes: client.bytes, Machines: host, Consumer: mirror.Server.Identity()})
	}
	return rows, nil
}

type imagePull struct {
	kind, origin, pulled, machine, consumer string
}

func imagePulls(catalog api.Catalog) ([]imagePull, error) {
	compiled := map[api.Kind]string{
		api.ArtifactServer: artifactserver.DefaultImage(), api.DNSServer: dnsserver.Definition(nil).Image,
		api.NTPServer: ntpserver.Definition().Image, api.Proxy: proxy.Definition().Image, api.Registry: registryDefinition().Image,
	}
	var pulls []imagePull
	for _, kind := range []api.Kind{api.ArtifactServer, api.DNSServer, api.NTPServer, api.Proxy, api.Registry} {
		for _, service := range catalog.OfKind(kind) {
			if service.Spec().Get("management").Text() != "managed" {
				continue
			}
			reference, err := managedservice.ImageFor(service.Spec(), service.Identity(), compiled[kind])
			if err != nil {
				return nil, err
			}
			machine, pulled := service.Spec().Get("machineRef").Text(), reference
			if !service.Spec().Get("image", "local").Present() {
				pulled, _ = managedservice.MirroredImage(catalog, reference, managedservice.ServiceConsumer(service))
			}
			pulls = append(pulls, imagePull{kind: "service image", origin: reference, pulled: pulled, machine: machine, consumer: service.Identity()})
		}
	}
	for _, provider := range catalog.OfKind(api.InfraProvider) {
		if substrate.Variant(provider) != substrate.ArmLibvirt {
			continue
		}
		host := substrate.HostMachine(provider)
		var pulled []string
		for _, machine := range substrate.HostedMachines(catalog, provider.Name()) {
			reference, _ := managedservice.MirroredImage(catalog, libvirt.EmulatorImage(), managedservice.EmulatedBMCConsumer(machine, host))
			if !slices.Contains(pulled, reference) {
				pulled = append(pulled, reference)
				pulls = append(pulls, imagePull{kind: "emulated BMC image", origin: libvirt.EmulatorImage(), pulled: reference, machine: host, consumer: provider.Identity()})
			}
		}
	}
	return pulls, nil
}

func imageAcquisitions(catalog api.Catalog) ([]cli.Acquisition, error) {
	pulls, err := imagePulls(catalog)
	if err != nil {
		return nil, err
	}
	var rows []cli.Acquisition
	for _, pull := range pulls {
		rows = append(rows, imageAcquisition(pull.kind, pull.pulled, []string{pull.machine}, pull.consumer))
	}
	images, err := mirrorImageArtifacts(catalog)
	if err != nil || len(images) == 0 {
		return rows, err
	}
	mirror, _ := infrastructureservices.ImageMirrorOf(catalog)
	for _, image := range images {
		rows = append(rows, imageAcquisition("mirror image", image.Source, []string{mirror.Registry.Spec().Get("machineRef").Text()}, mirror.Registry.Identity()))
	}
	return rows, nil
}

func mirrorImageArtifacts(catalog api.Catalog) ([]mirrorfill.Image, error) {
	mirror, declared := infrastructureservices.ImageMirrorOf(catalog)
	if !declared || !mirror.Managed || mirror.Location == "" {
		return nil, nil
	}
	pulls, err := imagePulls(catalog)
	if err != nil {
		return nil, err
	}
	var images []mirrorfill.Image
	for _, pull := range pulls {
		mirrored, beneath := strings.CutPrefix(pull.pulled, mirror.Location+"/")
		repository, digest, pinned := strings.Cut(mirrored, "@sha256:")
		if tag := strings.LastIndex(repository, ":"); tag > strings.LastIndex(repository, "/") {
			repository = repository[:tag]
		}
		source := repository + "@sha256:" + digest
		if !beneath || !pinned || slices.ContainsFunc(images, func(image mirrorfill.Image) bool { return image.Source == source }) {
			continue
		}
		images = append(images, mirrorfill.Image{Digest: "sha256:" + digest, Repository: repository, Source: source})
	}
	return images, nil
}

func mirrorReleases(catalog api.Catalog) ([]mirrorfill.Release, error) {
	if mirror, declared := infrastructureservices.ImageMirrorOf(catalog); !declared || !mirror.Managed {
		return nil, nil
	}
	tools, err := controller.SelectTools(catalog)
	if err != nil {
		return nil, err
	}
	var releases []mirrorfill.Release
	for _, cluster := range catalog.OfKind(api.OpenShiftCluster) {
		digest, managed := agentinstall.MirroredRelease(catalog, cluster)
		if !managed {
			continue
		}
		version := cluster.Spec().Get("distribution", "release", "version").Text()
		index := slices.IndexFunc(tools, func(tool controller.ToolRequest) bool {
			return tool.Kind == "openshift-clients" && tool.Compatibility == "openshift" && tool.Version == version
		})
		location := ""
		if index >= 0 {
			if location, err = bundlelocal.SourceURL(tools[index]); err != nil {
				return nil, err
			}
		}
		releases = append(releases, mirrorfill.Release{Client: mirrorfill.ReleaseClientFor(version, location), Cluster: cluster.Name(), Digest: digest,
			PullSecretRef: cluster.Spec().Get("install", "pullSecretRef").Text(), Version: version})
	}
	return releases, nil
}

func mirrorReleaseAcquisitions(catalog api.Catalog) ([]cli.Acquisition, error) {
	releases, err := mirrorReleases(catalog)
	if err != nil || len(releases) == 0 {
		return nil, err
	}
	mirror, _ := infrastructureservices.ImageMirrorOf(catalog)
	host := []string{mirror.Registry.Spec().Get("machineRef").Text()}
	var rows []cli.Acquisition
	for _, release := range releases {
		if err := release.Refusal(); err != nil {
			return nil, err
		}
		consumer := api.NewObject(api.OpenShiftCluster, release.Cluster, api.Value{}, api.Value{}).Identity()
		for _, repository := range openShiftReleaseRepositories {
			rows = append(rows, cli.Acquisition{Kind: "mirror release", Artifact: repository, Version: release.Version, Origin: repository, Class: originClass("https://" + repository),
				Machines: host, Consumer: consumer})
		}
		rows = append(rows,
			cli.Acquisition{Kind: "mirror release client", Artifact: "openshift-clients", Version: release.Version, Origin: release.Client.URL, Class: originClass(release.Client.URL),
				Machines: host, Consumer: consumer},
			cli.Acquisition{Kind: "mirror release client checksums", Artifact: "openshift-clients", Version: release.Version, Origin: release.Client.Checksums,
				Class: originClass(release.Client.Checksums), Machines: host, Consumer: consumer})
		for _, store := range releaseSignatureStores {
			rows = append(rows, cli.Acquisition{Kind: "mirror release signatures", Artifact: infrastructureservices.OpenShiftReleaseRepository, Version: release.Version,
				Origin: store, Class: originClass(store), Machines: host, Consumer: consumer})
		}
	}
	return rows, nil
}

func clusterAcquisitions(catalog api.Catalog, controllerMachine string) []cli.Acquisition {
	var rows []cli.Acquisition
	for _, cluster := range catalog.OfKind(api.OpenShiftCluster) {
		machines := append([]string{controllerMachine}, clusterNodes(cluster)...)
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

func clusterNodes(cluster api.Object) []string {
	var machines []string
	for _, node := range cluster.Spec().Get("nodes").Items() {
		if machine := node.Get("machineRef").Text(); machine != "" {
			machines = append(machines, machine)
		}
	}
	return machines
}

func imageAcquisition(kind, reference string, machines []string, consumer string) cli.Acquisition {
	repository, digest, _ := strings.Cut(reference, "@sha256:")
	if first, _, _ := strings.Cut(repository, "/"); !strings.ContainsAny(first, ".:") && first != "localhost" {
		repository = "docker.io/" + repository
	}
	version := ""
	if separator := strings.LastIndex(repository, ":"); separator > strings.LastIndex(repository, "/") {
		repository, version = repository[:separator], repository[separator+1:]
	}
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
