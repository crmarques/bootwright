package prerequisites

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// ErrBootstrapIncompatible means an otherwise valid retained resolution needs
// a different current automation/provided-execution profile. It is distinct
// from corruption and never authorizes replacement of an unfinished receipt.
var ErrBootstrapIncompatible = errors.New("retained bootstrap is incompatible with the current executable")

// ErrAutomationSuperseded narrows ErrBootstrapIncompatible to the one cause a
// host can settle from what it already holds: the retained resolution names
// every release, byte and signer this executable wants, and only the embedded
// automation it carries has moved. Setup reprojects that resolution instead of
// resolving a new one, so no publisher is consulted and nothing is acquired
// again. It always accompanies ErrBootstrapIncompatible, never replaces it.
var ErrAutomationSuperseded = errors.New("retained bootstrap automation is superseded by the current executable")

// MinimumRecordedAnsibleVersion is the oldest ansible-core a retained
// resolution may name: the collection's templating trust and strict Boolean
// semantics begin there. It judges records only. What setup selects is the
// narrower qualified minor, so a record an earlier build wrote stays readable
// until setup supersedes it.
const MinimumRecordedAnsibleVersion = "2.19.0"

func validateRecordedAnsibleVersion(version string) error {
	if len(version) <= 80 && bootstrapVersion.MatchString(version) {
		parts := strings.Split(version, ".")
		major, majorErr := strconv.ParseUint(parts[0], 10, 64)
		minor, minorErr := strconv.ParseUint(parts[1], 10, 64)
		if majorErr == nil && minorErr == nil && (major > 2 || major == 2 && minor >= 19) {
			return nil
		}
	}
	return diagnostics.NewFailure("controller.unsupported", "the retained resolution names an ansible-core below the recorded minimum "+MinimumRecordedAnsibleVersion, "")
}

// BootstrapDefinition freezes one authenticated publisher resolution. Source
// bytes reconstruct the complete projection; its digest includes every path,
// file digest and executable bit without duplicating thousands of file entries.
type BootstrapDefinition struct {
	Format            string               `json:"format"`
	Platform          Platform             `json:"platform"`
	PythonIntent      string               `json:"pythonIntent"`
	AnsibleIntent     string               `json:"ansibleIntent"`
	PythonVersion     string               `json:"pythonVersion"`
	AnsibleVersion    string               `json:"ansibleVersion"`
	PythonExecutable  string               `json:"pythonExecutable"`
	SitePackages      string               `json:"sitePackages"`
	Sources           []DependencySource   `json:"sources"`
	Wheels            []BootstrapWheel     `json:"wheels"`
	Metadata          []DependencySource   `json:"metadata"`
	ProjectionSHA256  string               `json:"projectionSHA256"`
	FileCount         int                  `json:"fileCount"`
	ExpandedBytes     int64                `json:"expandedBytes"`
	Execution         ExecutionRequirement `json:"execution"`
	ExecutionPackages []string             `json:"executionPackages"`
	AutomationDigest  string               `json:"automationDigest"`
	Digest            string               `json:"digest"`
}

type BootstrapWheel struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	SourceID string `json:"sourceID"`
}

var bootstrapVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
var bootstrapName = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
var bootstrapSourceID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// CanonicalBootstrap validates and clones a resolution, deriving its digest.
// Callers restoring durable state must also use ValidateBootstrap to compare
// that digest with the retained identity before granting execution authority.
func CanonicalBootstrap(value BootstrapDefinition) (BootstrapDefinition, error) {
	if value.Execution.Files == nil {
		value.Execution.Files = []InstalledFile{}
	}
	if value.Execution.Links == nil {
		value.Execution.Links = []InstalledLink{}
	}
	if value.Execution.Preload == nil {
		value.Execution.Preload = []string{}
	}
	invalid := func() (BootstrapDefinition, error) {
		return BootstrapDefinition{}, diagnostics.NewFailure("controller.unsupported", "resolved Python and Ansible bootstrap is malformed or exceeds its bounds", "")
	}
	if value.Format != "bootwright.controller.bootstrap-v1" || value.Platform.Architecture != "amd64" ||
		(value.Platform.OS != "fedora" && value.Platform.OS != "rhel") || value.Platform.Release == "" ||
		!bootstrapVersion.MatchString(value.PythonVersion) || !bootstrapVersion.MatchString(value.AnsibleVersion) ||
		(value.PythonIntent != "latest" && value.PythonIntent != value.PythonVersion) ||
		(value.AnsibleIntent != "latest" && value.AnsibleIntent != value.AnsibleVersion) ||
		len(value.Sources) < 2 || len(value.Sources) > 128 || len(value.Wheels) != len(value.Sources)-1 ||
		len(value.Metadata) != 2 || value.FileCount < 1 || value.FileCount > 16000 ||
		value.ExpandedBytes < 1 || value.ExpandedBytes > 256<<20 || !bootstrapHash(value.ProjectionSHA256) || !bootstrapHash(value.AutomationDigest) {
		return invalid()
	}
	if err := validateRecordedAnsibleVersion(value.AnsibleVersion); err != nil {
		return BootstrapDefinition{}, err
	}
	minor := value.PythonVersion[:strings.LastIndex(value.PythonVersion, ".")]
	if !strings.HasPrefix(minor, "3.") || value.PythonExecutable != "python/bin/python"+minor || value.SitePackages != "python/lib/python"+minor+"/site-packages/" || value.Execution.PythonExecutable != value.PythonExecutable {
		return invalid()
	}
	if len(value.ExecutionPackages) == 0 || len(value.ExecutionPackages) > 32 {
		return invalid()
	}
	for index, name := range value.ExecutionPackages {
		if !bootstrapName.MatchString(name) || index > 0 && value.ExecutionPackages[index-1] >= name {
			return invalid()
		}
	}
	ids := map[string]DependencySource{}
	var sourceBytes int64
	for index, source := range value.Sources {
		endpoint, ok := bootstrapSource(source, 64<<20)
		if !ok || ids[source.ID].ID != "" {
			return invalid()
		}
		if sourceBytes > 256<<20-source.Bytes {
			return invalid()
		}
		sourceBytes += source.Bytes
		if index == 0 {
			if endpoint.Hostname() != "github.com" || !strings.HasPrefix(endpoint.Path, "/astral-sh/python-build-standalone/releases/download/") || !strings.HasPrefix(path.Base(endpoint.Path), "cpython-"+value.PythonVersion+"+") || !strings.HasSuffix(endpoint.Path, "-x86_64-unknown-linux-gnu-install_only_stripped.tar.gz") {
				return invalid()
			}
		} else if endpoint.Hostname() != "files.pythonhosted.org" || !strings.HasPrefix(endpoint.Path, "/packages/") || !strings.HasSuffix(endpoint.Path, ".whl") {
			return invalid()
		}
		ids[source.ID] = source
	}
	names := map[string]bool{}
	for index, wheel := range value.Wheels {
		if !bootstrapName.MatchString(wheel.Name) || len(wheel.Version) == 0 || len(wheel.Version) > 80 || strings.ContainsAny(wheel.Version, "\\/\x00\r\n\t ") || names[wheel.Name] || wheel.SourceID != value.Sources[index+1].ID {
			return invalid()
		}
		names[wheel.Name] = true
		if wheel.Name == "ansible-core" && wheel.Version != value.AnsibleVersion {
			return invalid()
		}
		endpoint, _ := url.Parse(value.Sources[index+1].URL)
		parts := strings.Split(strings.TrimSuffix(path.Base(endpoint.Path), ".whl"), "-")
		if len(parts) < 5 || strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(parts[0], "_", "-"), ".", "-")) != wheel.Name || parts[1] != wheel.Version {
			return invalid()
		}
	}
	if !names["ansible-core"] || !names["urllib3"] {
		return invalid()
	}
	for index, metadata := range value.Metadata {
		endpoint, ok := bootstrapSource(metadata, 8<<20)
		if !ok {
			return invalid()
		}
		switch endpoint.Hostname() {
		case "raw.githubusercontent.com":
			if index != 0 || endpoint.Path != "/astral-sh/uv/main/crates/uv-python/download-metadata.json" {
				return invalid()
			}
		case "pypi.org":
			// Setup selects from the Index API; the project JSON an earlier
			// build selected from stays readable in its records.
			expected := []string{"/simple/ansible-core/", "/pypi/ansible-core/json"}
			if value.AnsibleIntent != "latest" {
				expected = []string{"/simple/ansible-core/", "/pypi/ansible-core/" + value.AnsibleVersion + "/json"}
			}
			if index != 1 || !slices.Contains(expected, endpoint.Path) {
				return invalid()
			}
		default:
			return invalid()
		}
	}
	value.Digest = ""
	data, err := json.Marshal(value)
	if err != nil || len(data) > 256<<10 {
		return invalid()
	}
	var canonical BootstrapDefinition
	if json.Unmarshal(data, &canonical) != nil {
		return invalid()
	}
	digest := sha256.Sum256(append([]byte("bootwright.controller.bootstrap-v1\x00"), data...))
	canonical.Digest = hex.EncodeToString(digest[:])
	return canonical, nil
}

func ValidateBootstrap(value BootstrapDefinition) error {
	canonical, err := CanonicalBootstrap(value)
	if err != nil {
		return err
	}
	actualJSON, _ := json.Marshal(value)
	canonicalJSON, _ := json.Marshal(canonical)
	if canonical.Digest != value.Digest || !bytes.Equal(actualJSON, canonicalJSON) {
		return diagnostics.NewFailure("controller.state", "retained bootstrap resolution differs from its immutable digest", "")
	}
	return nil
}

// ClosureDigest identifies the Python and Ansible closure one resolution
// executes: its platform, its interpreter release and layout, the approved
// bytes of every source, the wheels they hold and the provided execution
// foundation the closure was qualified against. It leaves out how the closure
// was found and what is projected beside it: the release intents, the index
// metadata read, the embedded automation and the projection identity, file
// count and size that automation moves. A resolution carried onto other
// automation, or bound to another native transaction, keeps it.
func ClosureDigest(value BootstrapDefinition) (string, error) {
	return definitionHash("bootwright.controller.execution-closure-v1", struct {
		Platform          Platform
		PythonVersion     string
		AnsibleVersion    string
		PythonExecutable  string
		SitePackages      string
		Sources           []DependencySource
		Wheels            []BootstrapWheel
		Execution         ExecutionRequirement
		ExecutionPackages []string
	}{value.Platform, value.PythonVersion, value.AnsibleVersion, value.PythonExecutable, value.SitePackages,
		value.Sources, value.Wheels, value.Execution, value.ExecutionPackages})
}

func bootstrapHash(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && hex.EncodeToString(decoded) == value
}

func bootstrapSource(source DependencySource, maximum int64) (*url.URL, bool) {
	endpoint, err := url.Parse(source.URL)
	return endpoint, err == nil && len(source.URL) <= 4096 && endpoint.Scheme == "https" && endpoint.Hostname() != "" && endpoint.User == nil && endpoint.RawQuery == "" && endpoint.Fragment == "" && endpoint.Opaque == "" && (endpoint.Port() == "" || endpoint.Port() == "443") &&
		len(source.ID) <= 256 && bootstrapSourceID.MatchString(source.ID) && bootstrapHash(source.SHA256) && source.Bytes > 0 && source.Bytes <= maximum
}
