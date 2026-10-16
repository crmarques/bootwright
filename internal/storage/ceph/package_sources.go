package ceph

import (
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const PackageVersion = "ceph-packages-v2"

type PackageSource struct {
	CertificateRef  string `json:"certificateRef"`
	Host            string `json:"host"`
	Media           string `json:"media"`
	MediaSHA256     string `json:"mediaSHA256,omitempty"`
	MediaSize       int64  `json:"mediaSize,omitempty"`
	Profile         string `json:"profile"`
	PublicationPath string `json:"publicationPath"`
	URL             string `json:"url"`
}

type PackageRequest struct {
	LicenseAccepted bool            `json:"licenseAccepted"`
	Sources         []PackageSource `json:"sources"`
	Version         string          `json:"version"`
}

type PackageTree struct {
	Context, Media, MediaSHA256, Path string
	MediaSize                         int64
}

func packageSources(c api.Catalog, r Request, contextName string) (*PackageRequest, error) {
	cluster, _ := c.Find(api.StorageCluster, r.Cluster)
	entitlement, _ := c.Find(api.Entitlement, cluster.Spec().Get("ceph", "entitlementRef").Text())
	accepted := entitlement.Spec().Get("license", "accept").Bool()
	if !accepted {
		return nil, failure("lifecycle.unsupported", "the frozen IBM package entitlement has no accepted license", "review the provider license and declare Entitlement.spec.license.accept")
	}
	result := &PackageRequest{LicenseAccepted: accepted, Version: PackageVersion, Sources: []PackageSource{}}
	for _, host := range r.Hosts {
		m, _ := c.Find(api.Machine, host.Machine)
		profile, ok := c.Find(api.MachineInstallProfile, m.Spec().Get("os", "installProfileRef").Text())
		if !ok || profile.Spec().Get("os", "family").Text() != "rhel" || profile.Spec().Get("os", "architecture").Text() != "x86_64" {
			return nil, failure("lifecycle.unsupported", "Ceph package acquisition requires the owned RHEL x86_64 DVD installation profile", "select the hosted DVD profile used to install each Ceph Machine")
		}
		tree := profile.Spec().Get("installer", "anaconda", "packageSource", "hostedTree")
		media, local := strings.CutPrefix(tree.Get("fromMedia").Text(), "local-media:")
		if !local || !managedos.ValidMediaName(media) {
			return nil, failure("lifecycle.unsupported", "Ceph dependencies require the installed Machine's pinned hosted DVD", "import the provider DVD and select hostedTree; subscription registration is not an implemented package source")
		}
		selection := tree.Get("artifactServerEndpoint")
		server, err := artifactserver.Selected(c, selection, profile.Identity())
		if err != nil {
			return nil, err
		}
		publication, err := artifactserver.PublicPath(c, server, selection, contextName, "os", profile.Name(), "tree", profile.Identity())
		if err != nil {
			return nil, err
		}
		endpoint, err := packageEndpoint(c, server, selection, profile.Identity())
		if err != nil {
			return nil, err
		}
		certificate := server.Spec().Get("tls", "secretRef").Text()
		if certificate == "" {
			return nil, failure("api.required", "the DVD package source requires its managed server's TLS certificate", "declare ArtifactServer.spec.tls.secretRef and its HTTPS endpoint")
		}
		result.Sources = append(result.Sources, PackageSource{Host: host.Name, Profile: profile.Name(), Media: media, PublicationPath: publication.Path, URL: endpoint + "/os/" + profile.Name() + "/tree", CertificateRef: certificate})
	}
	return result, nil
}

func packageEndpoint(c api.Catalog, server api.Object, selection api.Value, identity string) (string, error) {
	addressRef := ""
	for _, endpoint := range server.Spec().Get("endpoints").Items() {
		if endpoint.Get("name").Text() == selection.Get("endpointRef").Text() || selection.Get("endpointRef").Text() == "" && server.Spec().Get("endpoints").Len() == 1 {
			addressRef = endpoint.Get("addressRef").Text()
		}
	}
	var candidates []string
	for _, endpoint := range server.Spec().Get("endpoints").Items() {
		if endpoint.Get("addressRef").Text() != addressRef || addressRef == "" {
			continue
		}
		url, err := artifactserver.EndpointURL(c, server, endpoint.Get("name").Text(), identity)
		if err != nil {
			return "", err
		}
		if strings.HasPrefix(url, "https://") {
			candidates = append(candidates, url)
		}
	}
	slices.Sort(candidates)
	candidates = slices.Compact(candidates)
	if len(candidates) != 1 {
		return "", failure("lifecycle.unsupported", "the hosted DVD has no unique HTTPS endpoint at its declared address", "declare one HTTPS endpoint for the hostedTree server and address; package downloads require verified TLS")
	}
	return candidates[0], nil
}

func bindPackageSources(e lifecycle.Execution, r *Request, readers DependencyProofs) error {
	if r.Packages == nil {
		return failure("lifecycle.state", "cluster apply has no frozen package source", "")
	}
	if readers == nil {
		return failure("lifecycle.state", "Ceph DVD proof reader is not bound", "")
	}
	proofs := e.ApplyProved
	if len(proofs) == 0 {
		proofs = e.Proved
	}
	bound := *r.Packages
	bound.Sources = slices.Clone(bound.Sources)
	for i, source := range bound.Sources {
		tree, err := readers.PackageTree(proofs, source.Profile)
		if err != nil {
			return err
		}
		if tree.Context == "" || tree.Media != source.Media || tree.Path != source.PublicationPath || tree.MediaSize <= 0 || len(tree.MediaSHA256) != 64 || strings.Trim(tree.MediaSHA256, "0123456789abcdef") != "" {
			return failure("lifecycle.state", "the DVD package source differs from its original apply publication", "restore the frozen hosted DVD publication")
		}
		bound.Sources[i].MediaSHA256, bound.Sources[i].MediaSize = tree.MediaSHA256, tree.MediaSize
	}
	r.Packages = &bound
	return nil
}

func packageCertificateVariable(index int) string { return "packageCA" + strconv.Itoa(index) }
