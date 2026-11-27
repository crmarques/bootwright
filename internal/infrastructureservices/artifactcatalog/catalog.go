package artifactcatalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

const (
	MaxBytes   = 512 << 20
	MaxEntries = 256
	ClaimClass = "artifact"
)

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Entry struct {
	Name      string `json:"name"`
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	OriginURL string `json:"originURL,omitempty"`
}

type Row struct {
	Entry
	Source  string   `json:"source"`
	Holders []string `json:"holders"`
}

func ValidName(name string) bool { return api.ValidLexical(api.LexicalName, name) }

func ValidDigest(digest string) bool { return digestPattern.MatchString(digest) }

func ValidURL(value string) bool {
	if len(value) > 4096 || strings.ContainsAny(value, "?#\\") ||
		strings.IndexFunc(value, func(r rune) bool { return r <= 32 || r >= 127 }) >= 0 {
		return false
	}
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Hostname() != "" &&
		parsed.User == nil && (parsed.Port() == "443" || parsed.Port() == "" && parsed.Host[len(parsed.Host)-1] != ':')
}

func (e Entry) Valid() bool {
	return ValidName(e.Name) && ValidDigest(e.SHA256) && e.Size > 0 && e.Size <= MaxBytes &&
		(e.OriginURL == "" || ValidURL(e.OriginURL))
}

// Identity pins the source as well as the bytes so registration cannot race a
// delete followed by an add with a different origin.
func (e Entry) Identity() string {
	data, _ := json.Marshal(e)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func Failure(message, remedy string) error {
	return diagnostics.NewFailureWithRemediation("artifact.catalog", message, "", remedy)
}

func Supply(name string) string {
	return "run bootwright catalog artifact add --name " + name + " --from-file <path>, or register an HTTPS origin"
}

func HelmReference(environment api.Object) string {
	return helmReference(environment.Spec())
}

func helmReference(spec api.Value) string {
	return spec.Get("mirror", "artifacts", "toolSources", "helm", "catalogArtifact").Text()
}

func References(catalog api.Catalog) []string {
	var names []string
	for _, environment := range catalog.OfKind(api.Environment) {
		for _, name := range []string{HelmReference(environment), helmReference(environment.Spec().Get("defaults", string(api.Environment)))} {
			if ValidName(name) {
				names = append(names, name)
			}
		}
	}
	return names
}
