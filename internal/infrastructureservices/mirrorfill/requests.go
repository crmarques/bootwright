package mirrorfill

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

const (
	Kind                 = string(api.ArtifactServer)
	FilesImplementation  = "mirror-files-v1"
	FilesEvidenceVersion = "mirror-files-evidence-v1"
	Family               = "mirror-fill"
	MaxFiles             = 128
)

type Artifact struct {
	Bytes    int64
	Location string
	SHA256   string
	Source   string
}

type Artifacts func(api.Catalog) ([]Artifact, error)

type Identity struct {
	Context     string `json:"context"`
	Environment string `json:"environment"`
	Server      string `json:"server"`
}

type File struct {
	Bytes  int64  `json:"bytes"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Source string `json:"source"`
}

type FilesRequest struct {
	Egress     managedservice.Egress `json:"egress"`
	Files      []File                `json:"files"`
	Identity   Identity              `json:"identity"`
	Placement  machine.Placement     `json:"placement"`
	Root       string                `json:"root"`
	ServedRoot string                `json:"servedRoot"`
	Version    string                `json:"version"`
}

var (
	segmentPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)
	digestPattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

func (r FilesRequest) Canonical() ([]byte, error) { return reconciliation.Freeze(r, "mirror files") }

func DecodeFilesRequest(data []byte) (FilesRequest, error) {
	request, err := reconciliation.ThawVersion[FilesRequest](data, "mirror files", FilesImplementation)
	if err != nil {
		return FilesRequest{}, err
	}
	if err := request.validate(); err != nil {
		return FilesRequest{}, err
	}
	return request, nil
}

func (r FilesRequest) validate() error {
	invalid := refusal("lifecycle.state", "the frozen mirror files request names a file it could not publish beneath its root", "destroy it with the build that applied it")
	if len(r.Files) == 0 || len(r.Files) > MaxFiles || !strings.HasPrefix(r.Root, r.ServedRoot+"/") || r.Identity.Environment == "" || r.Identity.Server == "" {
		return invalid
	}
	seen := map[string]bool{}
	for _, file := range r.Files {
		if seen[file.Path] || !safePath(file.Path) || !digestPattern.MatchString(file.SHA256) || file.Bytes < 1 || !strings.HasPrefix(file.Source, "https://") {
			return invalid
		}
		seen[file.Path] = true
	}
	return nil
}

func safePath(path string) bool {
	segments := strings.Split(path, "/")
	for _, segment := range segments {
		if !segmentPattern.MatchString(segment) || segment == "." || segment == ".." {
			return false
		}
	}
	return len(segments) > 1
}

func (r FilesRequest) SecretReferences() []string { return r.Placement.SecretReferences() }

func FilesContentDigest() string {
	digest := sha256.Sum256([]byte(strings.Join([]string{"bootwright.infrastructureservices.mirror-fill-v1", FilesImplementation, FilesEvidenceVersion}, "\x00")))
	return hex.EncodeToString(digest[:])
}

func FilesBlockID(environment string) string {
	return reconciliation.BlockIdentity("mirror-files-", environment)
}

func refusal(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}
