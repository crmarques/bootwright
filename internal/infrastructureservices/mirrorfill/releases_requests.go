package mirrorfill

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
)

const (
	ReleaseKind            = string(api.OpenShiftCluster)
	ReleaseImplementation  = "mirror-release-v1"
	ReleaseEvidenceVersion = "mirror-release-evidence-v1"
	ReleaseArchitecture    = "x86_64"
	MaxComponents          = 512
	releaseChecksums       = "sha256sum.txt"
	releasePublisher       = "https://mirror.openshift.com/pub/openshift-v4/clients/ocp/"
)

type ReleaseClient struct {
	Checksums string `json:"checksums"`
	File      string `json:"file"`
	URL       string `json:"url"`
}

type Release struct {
	Client        ReleaseClient
	Cluster       string
	Digest        string
	PullSecretRef string
	Version       string
}

type Releases func(api.Catalog) ([]Release, error)

func (r Release) Refusal() error {
	cluster := "OpenShiftCluster/" + r.Cluster
	if r.PullSecretRef == "" {
		return refusal("api.required", cluster+" names no pull secret, which the copy of its release into the managed image mirror reads quay.io with",
			"set spec.install.pullSecretRef on "+cluster)
	}
	if r.Client.URL == "" {
		return refusal("lifecycle.state", "the controller stage selects no oc for release "+r.Version+" of "+cluster+", which the mirror host copies that release with",
			"set spec.distribution.release.version on "+cluster+" to a release this build installs")
	}
	return nil
}

type ReleaseIdentity struct {
	Cluster     string `json:"cluster"`
	Context     string `json:"context"`
	Environment string `json:"environment"`
	Registry    string `json:"registry"`
}

type ReleaseImage struct {
	Components string `json:"components"`
	Digest     string `json:"digest,omitempty"`
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Version    string `json:"version"`
}

type ReleaseRequest struct {
	Client        ReleaseClient              `json:"client"`
	Egress        managedservice.Egress      `json:"egress"`
	Identity      ReleaseIdentity            `json:"identity"`
	Mirror        managedservice.ImageSource `json:"mirror"`
	Placement     machine.Placement          `json:"placement"`
	PullSecretRef string                     `json:"pullSecretRef"`
	Release       ReleaseImage               `json:"release"`
	Version       string                     `json:"version"`
}

var releaseVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?$`)

func (r ReleaseRequest) Canonical() ([]byte, error) {
	return reconciliation.Freeze(r, "mirror release")
}

func DecodeReleaseRequest(data []byte) (ReleaseRequest, error) {
	request, err := reconciliation.ThawVersion[ReleaseRequest](data, "mirror release", ReleaseImplementation)
	if err != nil {
		return ReleaseRequest{}, err
	}
	if err := request.validate(); err != nil {
		return ReleaseRequest{}, err
	}
	return request, nil
}

func ReleaseTag(version string) string { return version + "-" + ReleaseArchitecture }

func ReleaseClientFor(version, location string) ReleaseClient {
	file := "openshift-client-linux-" + version + ".tar.gz"
	return ReleaseClient{Checksums: releasePublisher + version + "/" + releaseChecksums, File: file, URL: location}
}

func (r ReleaseRequest) validate() error {
	invalid := refusal("lifecycle.state", "the frozen mirror release request names a release or client it could not copy into its mirror", "destroy it with the build that applied it")
	release, client, mirror := r.Release, r.Client, r.Mirror
	if r.Identity.Cluster == "" || r.Identity.Environment == "" || r.Identity.Registry == "" || r.PullSecretRef == "" ||
		!releaseVersionPattern.MatchString(release.Version) || len(release.Version) > 64 || release.Tag != ReleaseTag(release.Version) ||
		release.Repository != infrastructureservices.OpenShiftReleaseRepository || release.Components != infrastructureservices.OpenShiftComponentRepository ||
		release.Digest != "" && !imageDigestPattern.MatchString(release.Digest) {
		return invalid
	}
	if client != ReleaseClientFor(release.Version, client.URL) || !strings.HasPrefix(client.URL, "https://") || !strings.HasSuffix(client.URL, "/"+client.File) ||
		strings.ContainsAny(client.URL, " \t\r\n\"'\\?#") || len(client.URL) > 512 {
		return invalid
	}
	if !mirrorHostPattern.MatchString(mirror.Host) || mirror.CredentialsRef == "" || mirror.CredentialsType != "usernamePassword" || mirror.TrustBundleRef == "" {
		return invalid
	}
	return nil
}

func (r ReleaseRequest) Source() string {
	if r.Release.Digest != "" {
		return r.Release.Repository + "@" + r.Release.Digest
	}
	return r.Release.Repository + ":" + r.Release.Tag
}

func (r ReleaseRequest) SecretReferences() []string {
	return append(append(r.Placement.SecretReferences(), r.Mirror.SecretReferences()...), r.PullSecretRef)
}

func (r ReleaseRequest) SecretClasses() secrets.Classes {
	return r.Placement.SecretClasses().Merge(r.Mirror.SecretClasses()).Add(secrets.AccessCredential, r.PullSecretRef)
}

func ReleaseContentDigest() string {
	digest := sha256.Sum256([]byte(strings.Join([]string{"bootwright.infrastructureservices.mirror-fill-v1", ReleaseImplementation, ReleaseEvidenceVersion}, "\x00")))
	return hex.EncodeToString(digest[:])
}

func ReleaseBlockID(cluster string) string {
	return reconciliation.BlockIdentity("mirror-release-", cluster)
}
