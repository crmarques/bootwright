package listing

import (
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/acquisition"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/dnsserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/infrastructureservices/mirrorfill"
	"github.com/crmarques/bootwright/internal/infrastructureservices/ntpserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/proxy"
	"github.com/crmarques/bootwright/internal/infrastructureservices/registry"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
)

type ImagePull struct {
	Kind, Origin, Pulled, Machine, Consumer string
}

func ImagePulls(catalog api.Catalog) ([]ImagePull, error) {
	compiled := map[api.Kind]string{
		api.ArtifactServer: artifactserver.DefaultImage(), api.DNSServer: dnsserver.Definition(nil).Image,
		api.NTPServer: ntpserver.Definition().Image, api.Proxy: proxy.Definition().Image, api.Registry: registry.Definition(nil).Image,
	}
	var pulls []ImagePull
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
			pulls = append(pulls, ImagePull{Kind: "service image", Origin: reference, Pulled: pulled, Machine: machine, Consumer: service.Identity()})
		}
	}
	for _, pull := range libvirt.EmulatorPulls(catalog) {
		pulls = append(pulls, ImagePull{Kind: "emulated BMC image", Origin: pull.Origin, Pulled: pull.Pulled, Machine: pull.Host, Consumer: pull.Provider})
	}
	return pulls, nil
}

func imageAcquisitions(catalog api.Catalog) ([]Row, error) {
	pulls, err := ImagePulls(catalog)
	if err != nil {
		return nil, err
	}
	var rows []Row
	for _, pull := range pulls {
		rows = append(rows, imageAcquisition(pull.Kind, pull.Pulled, []string{pull.Machine}, pull.Consumer))
	}
	images, err := MirrorImages(catalog)
	if err != nil || len(images) == 0 {
		return rows, err
	}
	mirror, _ := infrastructureservices.ImageMirrorOf(catalog)
	for _, image := range images {
		rows = append(rows, imageAcquisition("mirror image", image.Source, []string{mirror.Registry.Spec().Get("machineRef").Text()}, mirror.Registry.Identity()))
	}
	return rows, nil
}

func MirrorImages(catalog api.Catalog) ([]mirrorfill.Image, error) {
	mirror, declared := infrastructureservices.ImageMirrorOf(catalog)
	if !declared || !mirror.Managed || mirror.Location == "" {
		return nil, nil
	}
	pulls, err := ImagePulls(catalog)
	if err != nil {
		return nil, err
	}
	var images []mirrorfill.Image
	for _, pull := range pulls {
		mirrored, beneath := strings.CutPrefix(pull.Pulled, mirror.Location+"/")
		image := acquisition.ParseImageReference(mirrored)
		source := image.Pinned()
		if !beneath || image.Digest == "" || slices.ContainsFunc(images, func(known mirrorfill.Image) bool { return known.Source == source }) {
			continue
		}
		images = append(images, mirrorfill.Image{Digest: "sha256:" + image.Digest, Repository: image.Repository, Source: source})
	}
	return images, nil
}

func imageAcquisition(kind, reference string, machines []string, consumer string) Row {
	image := acquisition.ParseImageReference(reference).Qualified()
	version := image.Tag
	if image.Digest != "" {
		version = "sha256:" + image.Digest
	}
	return Row{Kind: kind, Artifact: image.Repository, Version: version, Origin: image.Repository, Class: originClass("https://" + image.Repository), SHA256: image.Digest, Machines: machines, Consumer: consumer}
}
