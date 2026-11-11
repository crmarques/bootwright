package mirrorfill

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
)

const (
	ImagesKind            = string(api.Registry)
	ImagesImplementation  = "mirror-images-v1"
	ImagesEvidenceVersion = "mirror-images-evidence-v1"
	MaxImages             = 64
)

type Image struct {
	Digest     string `json:"digest"`
	Repository string `json:"repository"`
	Source     string `json:"source"`
}

type Images func(api.Catalog) ([]Image, error)

type ImagesIdentity struct {
	Context     string `json:"context"`
	Environment string `json:"environment"`
	Registry    string `json:"registry"`
}

type ImagesRequest struct {
	Egress    managedservice.Egress      `json:"egress"`
	Identity  ImagesIdentity             `json:"identity"`
	Images    []Image                    `json:"images"`
	Mirror    managedservice.ImageSource `json:"mirror"`
	Placement machine.Placement          `json:"placement"`
	Version   string                     `json:"version"`
}

var (
	imageDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	repositoryPattern  = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?(:[0-9]{1,5})?(/[a-z0-9]+([._-]+[a-z0-9]+)*)+$`)
	mirrorHostPattern  = regexp.MustCompile(`^([a-z0-9]([a-z0-9.-]*[a-z0-9])?|\[[0-9a-f:.]+\]):[0-9]{1,5}$`)
)

func (r ImagesRequest) Canonical() ([]byte, error) { return reconciliation.Freeze(r, "mirror images") }

func DecodeImagesRequest(data []byte) (ImagesRequest, error) {
	request, err := reconciliation.ThawVersion[ImagesRequest](data, "mirror images", ImagesImplementation)
	if err != nil {
		return ImagesRequest{}, err
	}
	if err := request.validate(); err != nil {
		return ImagesRequest{}, err
	}
	return request, nil
}

func (r ImagesRequest) validate() error {
	invalid := refusal("lifecycle.state", "the frozen mirror images request names an image it could not copy by digest into its mirror", "destroy it with the build that applied it")
	mirror := r.Mirror
	if len(r.Images) == 0 || len(r.Images) > MaxImages || r.Identity.Environment == "" || r.Identity.Registry == "" || !mirrorHostPattern.MatchString(mirror.Host) ||
		mirror.CredentialsRef == "" || mirror.CredentialsType != "usernamePassword" || mirror.TrustBundleRef == "" {
		return invalid
	}
	seen := map[string]bool{}
	for _, image := range r.Images {
		if seen[image.Repository+"@"+image.Digest] || !repositoryPattern.MatchString(image.Repository) || len(image.Repository) > 255 ||
			!imageDigestPattern.MatchString(image.Digest) || image.Source != image.Repository+"@"+image.Digest {
			return invalid
		}
		seen[image.Repository+"@"+image.Digest] = true
	}
	return nil
}

func (r ImagesRequest) SecretReferences() []string {
	return append(r.Placement.SecretReferences(), r.Mirror.SecretReferences()...)
}

func (r ImagesRequest) SecretClasses() secrets.Classes {
	return r.Placement.SecretClasses().Merge(r.Mirror.SecretClasses())
}

func ImagesContentDigest() string {
	digest := sha256.Sum256([]byte(strings.Join([]string{"bootwright.infrastructureservices.mirror-fill-v1", ImagesImplementation, ImagesEvidenceVersion}, "\x00")))
	return hex.EncodeToString(digest[:])
}

func ImagesBlockID(environment string) string {
	return reconciliation.BlockIdentity("mirror-images-", environment)
}
