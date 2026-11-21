package listing

import (
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/evidence"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
)

type Row struct {
	Kind     string
	Artifact string
	Version  string
	Origin   string
	Class    string
	SHA256   string
	Bytes    int64
	Machines []string
	Consumer string
}

type Contact struct{ Host, Purpose string }

type Tools struct {
	SourceURL func(evidence.ToolRequest) (string, error)
	Contacts  func(evidence.ToolRequest) ([]Contact, error)
}

const (
	classHostRepositories = "host repositories"
	classMediaStore       = "media store"
)

var releaseSignatureStores = []string{"https://storage.googleapis.com/openshift-release/official/signatures/openshift/release",
	"https://mirror.openshift.com/pub/openshift-v4/signatures/openshift/release"}

var openShiftReleaseRepositories = []string{infrastructureservices.OpenShiftReleaseRepository, infrastructureservices.OpenShiftComponentRepository}

func (t Tools) List(catalog api.Catalog) ([]Row, error) {
	var acquired []Row
	controllerRows, controllerMachine, err := t.controllerAcquisitions(catalog)
	if err != nil {
		return nil, err
	}
	acquired = append(acquired, controllerRows...)
	fills, err := t.mirrorFillAcquisitions(catalog)
	if err != nil {
		return nil, err
	}
	acquired = append(acquired, fills...)
	images, err := imageAcquisitions(catalog)
	if err != nil {
		return nil, err
	}
	acquired = append(acquired, images...)
	releases, err := t.mirrorReleaseAcquisitions(catalog)
	if err != nil {
		return nil, err
	}
	acquired = append(acquired, releases...)
	acquired = append(acquired, clusterAcquisitions(catalog, controllerMachine)...)
	acquired = append(acquired, storageAcquisitions(catalog, controllerMachine)...)
	acquired = append(acquired, operatorAcquisitions(catalog)...)
	acquired = append(acquired, machineRepositoryAcquisitions(catalog)...)
	for _, image := range catalog.OfKind(api.MachineImage) {
		acquired = append(acquired, Row{Kind: "installation media", Artifact: image.Spec().Get("bootMedia").Text(), Class: classMediaStore,
			Origin: "the file or URL media add named", SHA256: strings.TrimPrefix(image.Spec().Get("checksum").Text(), "sha256:"), Machines: []string{controllerMachine}, Consumer: image.Identity()})
	}
	for _, profile := range catalog.OfKind(api.MachineInstallProfile) {
		if dvd := profile.Spec().Get("installer", "anaconda", "packageSource", "hostedTree", "fromMedia").Text(); dvd != "" {
			acquired = append(acquired, Row{Kind: "installation media", Artifact: dvd, Class: classMediaStore,
				Origin: "the file or URL media add named", Machines: []string{controllerMachine}, Consumer: profile.Identity()})
		}
	}
	return withRegistryHosts(acquired), nil
}
