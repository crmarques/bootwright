package ceph

import (
	"net/url"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const PackageVersion = "ceph-packages-v1"

type PackageSource struct {
	CertificateRef  string              `json:"certificateRef"`
	Host            string              `json:"host"`
	Media           string              `json:"media,omitempty"`
	MediaSHA256     string              `json:"mediaSHA256,omitempty"`
	MediaSize       int64               `json:"mediaSize,omitempty"`
	Profile         string              `json:"profile"`
	PublicationPath string              `json:"publicationPath,omitempty"`
	Subscription    *SubscriptionSource `json:"subscription,omitempty"`
	URL             string              `json:"url,omitempty"`
}

type SubscriptionSource struct {
	ContentBaseURL string `json:"contentBaseURL"`
	Hostname       string `json:"hostname"`
}

type PackageRequest struct {
	LicenseAccepted                 bool            `json:"licenseAccepted"`
	PackageRepositoryTrustBundleRef string          `json:"packageRepositoryTrustBundleRef,omitempty"`
	RepositoryProxy                 string          `json:"repositoryProxy"`
	Sources                         []PackageSource `json:"sources"`
	Version                         string          `json:"version"`
}

func (p *PackageRequest) secretReferences() []string {
	if p == nil {
		return nil
	}
	references := []string{p.PackageRepositoryTrustBundleRef}
	for _, source := range p.Sources {
		references = append(references, source.CertificateRef)
	}
	return references
}

type PackageTree struct {
	Context, Media, MediaSHA256, Path string
	MediaSize                         int64
}

func packageSources(c api.Catalog, r Request, contextName string) (*PackageRequest, error) {
	cluster, _ := c.Find(api.CephCluster, r.Cluster)
	entitlement, _ := c.Find(api.Entitlement, cluster.Spec().Get("entitlementRef").Text())
	accepted := entitlement.Spec().Get("license", "accept").Bool()
	if !accepted {
		return nil, failure("lifecycle.unsupported", "the frozen IBM package entitlement has no accepted license", "review the provider license and declare Entitlement.spec.license.accept")
	}
	result := &PackageRequest{LicenseAccepted: accepted, PackageRepositoryTrustBundleRef: cluster.Spec().Get("packageRepositoryTrustBundleRef").Text(), Version: PackageVersion, Sources: []PackageSource{}}
	proxy, err := packageRepositoryProxy(c, cluster, r.PackageRepository)
	if err != nil {
		return nil, err
	}
	result.RepositoryProxy = proxy
	for _, host := range r.Hosts {
		m, _ := c.Find(api.Machine, host.Machine)
		if host.Provided {
			m = bootstrapMachine(c, r)
		}
		profile, ok := c.Find(api.MachineInstallProfile, api.MachineInstallProfileRef(m))
		if !ok || profile.Spec().Get("os", "family").Text() != "rhel" || profile.Spec().Get("os", "architecture").Text() != "x86_64" {
			return nil, failure("lifecycle.unsupported", "Ceph package acquisition requires the owned RHEL x86_64 installation profile", "select the hosted DVD or subscription profile used to install each Ceph Machine")
		}
		var source PackageSource
		if profile.Spec().Has("installer", "anaconda", "packageSource", "subscription") {
			source, err = subscriptionPackageSource(c, profile, host)
		} else {
			source, err = treePackageSource(c, profile, contextName)
		}
		if err != nil {
			return nil, err
		}
		source.Host, source.Profile = host.Name, profile.Name()
		result.Sources = append(result.Sources, source)
	}
	return result, nil
}

func treePackageSource(c api.Catalog, profile api.Object, contextName string) (PackageSource, error) {
	tree := profile.Spec().Get("installer", "anaconda", "packageSource", "hostedTree")
	media, local := strings.CutPrefix(tree.Get("fromMedia").Text(), "local-media:")
	if !local || !managedos.ValidMediaName(media) {
		return PackageSource{}, failure("lifecycle.unsupported", "Ceph dependencies require the installed Machine's pinned hosted DVD or its Satellite subscription", "import the provider DVD and select hostedTree, or select subscription to take them from the Satellite the Machine registers with")
	}
	selection := tree.Get("artifactServerEndpoint")
	server, err := artifactserver.Selected(c, selection, profile.Identity())
	if err != nil {
		return PackageSource{}, err
	}
	publication, err := artifactserver.PublicPath(c, server, selection, contextName, "os", profile.Name(), "tree", profile.Identity())
	if err != nil {
		return PackageSource{}, err
	}
	endpoint, err := packageEndpoint(c, server, selection, profile.Identity())
	if err != nil {
		return PackageSource{}, err
	}
	certificate := server.Spec().Get("tls", "secretRef").Text()
	if certificate == "" {
		return PackageSource{}, failure("api.required", "the DVD package source requires its managed server's TLS certificate", "declare ArtifactServer.spec.tls.secretRef and its HTTPS endpoint")
	}
	return PackageSource{Media: media, PublicationPath: publication.Path, URL: endpoint + "/os/" + profile.Name() + "/tree", CertificateRef: certificate}, nil
}

func subscriptionPackageSource(c api.Catalog, profile api.Object, host Host) (PackageSource, error) {
	if host.Provided {
		return PackageSource{}, failure("lifecycle.unsupported", "provided Ceph host "+host.Name+" takes its dependencies from the bootstrap Machine's hosted DVD, and "+profile.Identity()+" installs from a subscription",
			"install the bootstrap Machine with a hostedTree profile, or make "+host.Name+" a Machine this context installs")
	}
	entitlement, found := c.Find(api.Entitlement, profile.Spec().Get("subscription", "entitlementRef").Text())
	satellite := entitlement.Spec().Get("rhsm", "satellite")
	source := PackageSource{CertificateRef: satellite.Get("trustBundleRef").Text(),
		Subscription: &SubscriptionSource{ContentBaseURL: satellite.Get("contentBaseURL").Text(), Hostname: satellite.Get("hostname").Text()}}
	if !found || entitlement.Spec().Get("rhsm", "management").Text() == "external" || source.CertificateRef == "" || source.Subscription.ContentBaseURL == "" || source.Subscription.Hostname == "" {
		return PackageSource{}, failure("lifecycle.unsupported", "Ceph dependencies from a subscription package source come from the Satellite of a managed registration, and "+profile.Identity()+" declares none",
			"name a managed Entitlement that declares spec.rhsm.satellite with its trustBundleRef in spec.subscription.entitlementRef on "+profile.Identity()+", or select hostedTree")
	}
	if dottedPath(source.Subscription.ContentBaseURL) {
		return PackageSource{}, failure("lifecycle.unsupported", "the Satellite content root of "+entitlement.Identity()+" holds a '.', '..' or backslash path segment, which package acquisition refuses",
			"set spec.rhsm.satellite.contentBaseURL on "+entitlement.Identity()+" to the content root without such a segment")
	}
	return source, nil
}

func dottedPath(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return true
	}
	decoded, err := url.PathUnescape(parsed.EscapedPath())
	return err != nil || strings.Contains(decoded, `\`) || slices.ContainsFunc(strings.Split(decoded, "/"), func(part string) bool { return part == "." || part == ".." })
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
		url, err := infrastructureservices.ResolveEndpointURL(c, server, endpoint.Get("name").Text(), identity)
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

func bindPackageSources(e lifecycle.Execution, packages *PackageRequest, readers DependencyProofs) (*PackageRequest, error) {
	if packages == nil {
		return nil, failure("lifecycle.state", "cluster apply has no frozen package source", "")
	}
	if readers == nil {
		return nil, failure("lifecycle.state", "Ceph DVD proof reader is not bound", "")
	}
	proofs := e.ApplyProved
	if len(proofs) == 0 {
		proofs = e.Proved
	}
	bound := *packages
	bound.Sources = slices.Clone(bound.Sources)
	for i, source := range bound.Sources {
		if source.Subscription != nil {
			continue
		}
		tree, err := readers.PackageTree(proofs, source.Profile)
		if err != nil {
			return nil, err
		}
		if tree.Context == "" || tree.Media != source.Media || tree.Path != source.PublicationPath || tree.MediaSize <= 0 || len(tree.MediaSHA256) != 64 || strings.Trim(tree.MediaSHA256, "0123456789abcdef") != "" {
			return nil, failure("lifecycle.state", "the DVD package source differs from its original apply publication", "restore the frozen hosted DVD publication")
		}
		bound.Sources[i].MediaSHA256, bound.Sources[i].MediaSize = tree.MediaSHA256, tree.MediaSize
	}
	return &bound, nil
}

func packageCertificateVariable(index int) string { return "packageCA" + strconv.Itoa(index) }

func bootstrapMachine(c api.Catalog, r Request) api.Object {
	for _, host := range r.Hosts {
		if host.Name == r.Bootstrap {
			m, _ := c.Find(api.Machine, host.Machine)
			return m
		}
	}
	return api.Object{}
}
