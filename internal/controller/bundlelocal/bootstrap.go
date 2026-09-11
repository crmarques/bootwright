package bundlelocal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	automation "github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

const pythonMetadataURL = "https://raw.githubusercontent.com/astral-sh/uv/main/crates/uv-python/download-metadata.json"

type bootstrapMetadataFetcher func(context.Context, string, string, prerequisites.SetupEgress) (toolMetadata, error)
type bootstrapWheelResolver func(context.Context, *projection, prerequisites.BootstrapDefinition, prerequisites.SetupEgress) ([]byte, error)

// BootstrapCatalog resolves one publisher snapshot. Its private function ports
// allow parser/isolation tests without network access or installed-host effects.
type BootstrapCatalog struct {
	metadata bootstrapMetadataFetcher
	fetch    sourceFetcher
	resolve  bootstrapWheelResolver
}

func NewBootstrapResolver() *BootstrapCatalog {
	return &BootstrapCatalog{metadata: fetchBootstrapMetadata, fetch: fetchSource, resolve: resolveBootstrapWheels}
}

func (c *BootstrapCatalog) Resolve(ctx context.Context, platform prerequisites.Platform, versions controller.DependencyVersions, egress prerequisites.SetupEgress) (prerequisites.BootstrapDefinition, error) {
	if c == nil || c.metadata == nil || c.fetch == nil || c.resolve == nil {
		return prerequisites.BootstrapDefinition{}, bundleFailure("bootstrap resolver adapters are unavailable")
	}
	if versions.Python != "latest" && !stableBootstrapVersion.MatchString(versions.Python) || versions.Ansible != "latest" && !stableBootstrapVersion.MatchString(versions.Ansible) {
		return prerequisites.BootstrapDefinition{}, bundleFailure("bootstrap version intent must be latest or an exact stable release")
	}
	if versions.Ansible != "latest" {
		if err := prerequisites.ValidateBootstrapAnsibleVersion(versions.Ansible); err != nil {
			return prerequisites.BootstrapDefinition{}, err
		}
	}
	if _, err := explicitProxy(egress); err != nil {
		return prerequisites.BootstrapDefinition{}, err
	}
	record, _, err := compiledCatalog()
	if err != nil {
		return prerequisites.BootstrapDefinition{}, err
	}
	native, ok := selectNative(record, platform)
	if !ok || platform.Architecture != "amd64" {
		return prerequisites.BootstrapDefinition{}, unsupportedPlatform()
	}
	pythonMetadata, err := c.metadata(ctx, http.MethodGet, pythonMetadataURL, egress)
	if err != nil {
		return prerequisites.BootstrapDefinition{}, err
	}
	pythonVersion, source, err := selectPythonArtifact(pythonMetadata.data, versions.Python)
	if err != nil {
		return prerequisites.BootstrapDefinition{}, err
	}
	asset, err := c.metadata(ctx, http.MethodHead, source.URL, egress)
	if err != nil {
		return prerequisites.BootstrapDefinition{}, err
	}
	source.Bytes = asset.size
	ansibleURL := "https://pypi.org/pypi/ansible-core/json"
	if versions.Ansible != "latest" {
		ansibleURL = "https://pypi.org/pypi/ansible-core/" + versions.Ansible + "/json"
	}
	ansibleMetadata, err := c.metadata(ctx, http.MethodGet, ansibleURL, egress)
	if err != nil {
		return prerequisites.BootstrapDefinition{}, err
	}
	ansibleVersion, err := selectAnsibleRelease(ansibleMetadata.data, versions.Ansible, pythonVersion)
	if err != nil {
		return prerequisites.BootstrapDefinition{}, err
	}
	minor := pythonVersion[:strings.LastIndex(pythonVersion, ".")]
	value := prerequisites.BootstrapDefinition{
		Format: "bootwright.controller.bootstrap-v1", Platform: platform, PythonIntent: versions.Python, AnsibleIntent: versions.Ansible,
		PythonVersion: pythonVersion, AnsibleVersion: ansibleVersion, PythonExecutable: "python/bin/python" + minor,
		SitePackages: "python/lib/python" + minor + "/site-packages/", Sources: []prerequisites.DependencySource{source},
		Metadata:  []prerequisites.DependencySource{bootstrapMetadataSource("python-metadata", pythonMetadataURL, pythonMetadata.data), bootstrapMetadataSource("ansible-metadata", ansibleURL, ansibleMetadata.data)},
		Execution: cloneExecution(native.Execution), AutomationDigest: automation.Digest(),
	}
	value.Execution.PythonExecutable = value.PythonExecutable
	value.ExecutionPackages = executionPackageOwners()
	archive, err := c.fetch(ctx, source, egress)
	if err != nil {
		return prerequisites.BootstrapDefinition{}, err
	}
	projected := newProjection()
	projected.site = value.SitePackages
	if err := projected.archive(ctx, archive); err != nil {
		return prerequisites.BootstrapDefinition{}, err
	}
	if _, ok := projected.files[value.PythonExecutable]; !ok {
		return prerequisites.BootstrapDefinition{}, bundleFailure("selected Python archive lacks its declared executable")
	}
	report, err := c.resolve(ctx, projected, value, egress)
	if err != nil {
		return prerequisites.BootstrapDefinition{}, err
	}
	wheels, sources, err := parsePipReport(report, value)
	if err != nil {
		return prerequisites.BootstrapDefinition{}, err
	}
	value.Wheels = wheels
	sourceBytes := value.Sources[0].Bytes
	for _, source := range sources {
		metadata, err := c.metadata(ctx, http.MethodHead, source.URL, egress)
		if err != nil {
			return prerequisites.BootstrapDefinition{}, err
		}
		source.Bytes = metadata.size
		if source.Bytes <= 0 || sourceBytes > 256<<20-source.Bytes {
			return prerequisites.BootstrapDefinition{}, bundleFailure("resolved bootstrap source closure exceeds its acquisition bound")
		}
		sourceBytes += source.Bytes
		data, err := c.fetch(ctx, source, egress)
		if err != nil {
			return prerequisites.BootstrapDefinition{}, err
		}
		if err := projected.wheel(ctx, data); err != nil {
			return prerequisites.BootstrapDefinition{}, err
		}
		value.Sources = append(value.Sources, source)
	}
	if err := projected.automation(ctx); err != nil {
		return prerequisites.BootstrapDefinition{}, err
	}
	if err := qualifyResolvedProjection(projected, value.Execution); err != nil {
		return prerequisites.BootstrapDefinition{}, err
	}
	value.ProjectionSHA256, value.FileCount, value.ExpandedBytes = projected.identity(), len(projected.files), projected.bytes
	return prerequisites.CanonicalBootstrap(value)
}

// These package owners accompany the exact signed loader/glibc/libgcc ELF
// profiles. Native solving may not replace their files beneath the retained
// bootstrap; establishing a different provided profile needs new qualification.
func executionPackageOwners() []string { return []string{"glibc", "libgcc"} }

type pythonArtifact struct {
	Name string `json:"name"`
	OS   string `json:"os"`
	Libc string `json:"libc"`
	Arch struct {
		Family  string  `json:"family"`
		Variant *string `json:"variant"`
	} `json:"arch"`
	Major      int     `json:"major"`
	Minor      int     `json:"minor"`
	Patch      int     `json:"patch"`
	Prerelease string  `json:"prerelease"`
	Variant    *string `json:"variant"`
	URL        string  `json:"url"`
	SHA256     string  `json:"sha256"`
	Build      string  `json:"build"`
}

var stableBootstrapVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
var pythonBuildDate = regexp.MustCompile(`^[0-9]{8}$`)

func selectPythonArtifact(data []byte, requested string) (string, prerequisites.DependencySource, error) {
	var records map[string]pythonArtifact
	if len(data) > 8<<20 || json.Unmarshal(data, &records) != nil || len(records) > 16000 || requested != "latest" && !stableBootstrapVersion.MatchString(requested) {
		return "", prerequisites.DependencySource{}, bundleFailure("Python publisher metadata or requested version is invalid")
	}
	var selected pythonArtifact
	for _, candidate := range records {
		if candidate.Name != "cpython" || candidate.OS != "linux" || candidate.Libc != "gnu" || candidate.Arch.Family != "x86_64" || candidate.Arch.Variant != nil || candidate.Variant != nil || candidate.Prerelease != "" || candidate.Major != 3 || candidate.Minor < 10 || candidate.Minor > 99 || candidate.Patch < 0 || candidate.Patch > 9999 || !pythonBuildDate.MatchString(candidate.Build) {
			continue
		}
		version := strconv.Itoa(candidate.Major) + "." + strconv.Itoa(candidate.Minor) + "." + strconv.Itoa(candidate.Patch)
		if requested != "latest" && requested != version {
			continue
		}
		if selected.URL != "" && candidate.Minor == selected.Minor && candidate.Patch == selected.Patch && candidate.Build == selected.Build && (candidate.URL != selected.URL || candidate.SHA256 != selected.SHA256) {
			return "", prerequisites.DependencySource{}, bundleFailure("Python publisher metadata contains conflicting identities for one release build")
		}
		if selected.URL == "" || candidate.Minor > selected.Minor || candidate.Minor == selected.Minor && candidate.Patch > selected.Patch || candidate.Minor == selected.Minor && candidate.Patch == selected.Patch && candidate.Build > selected.Build {
			selected = candidate
		}
	}
	version := strconv.Itoa(selected.Major) + "." + strconv.Itoa(selected.Minor) + "." + strconv.Itoa(selected.Patch)
	endpoint, err := url.Parse(selected.URL)
	digest, decodeErr := hex.DecodeString(selected.SHA256)
	if err != nil || len(selected.URL) > 4096 || endpoint.Scheme != "https" || endpoint.Host != "github.com" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" ||
		!strings.HasPrefix(endpoint.Path, "/astral-sh/python-build-standalone/releases/download/"+selected.Build+"/cpython-"+version+"+"+selected.Build+"-") ||
		!strings.HasSuffix(endpoint.Path, "-x86_64-unknown-linux-gnu-install_only_stripped.tar.gz") || decodeErr != nil || len(digest) != sha256.Size || hex.EncodeToString(digest) != selected.SHA256 {
		return "", prerequisites.DependencySource{}, bundleFailure("requested stable Python release has no approved standalone artifact")
	}
	return version, prerequisites.DependencySource{ID: "python-" + version + "-" + selected.Build, URL: selected.URL, SHA256: selected.SHA256}, nil
}

func selectAnsibleRelease(data []byte, requested, python string) (string, error) {
	var record struct {
		Info struct {
			Version     string   `json:"version"`
			Classifiers []string `json:"classifiers"`
		} `json:"info"`
	}
	if len(data) > 8<<20 || json.Unmarshal(data, &record) != nil || !stableBootstrapVersion.MatchString(record.Info.Version) || requested != "latest" && requested != record.Info.Version {
		return "", bundleFailure("requested Ansible release has no valid publisher metadata")
	}
	if err := prerequisites.ValidateBootstrapAnsibleVersion(record.Info.Version); err != nil {
		return "", err
	}
	minor := python[:strings.LastIndex(python, ".")]
	if !slices.Contains(record.Info.Classifiers, "Programming Language :: Python :: "+minor) {
		return "", bundleFailure("selected Ansible release does not declare support for the selected Python version; set compatible Environment dependencyVersions")
	}
	return record.Info.Version, nil
}

func bootstrapMetadataSource(prefix, endpoint string, data []byte) prerequisites.DependencySource {
	digest := sha256.Sum256(data)
	sum := hex.EncodeToString(digest[:])
	return prerequisites.DependencySource{ID: prefix + "-" + sum[:16], URL: endpoint, SHA256: sum, Bytes: int64(len(data))}
}

func bootstrapOrigin(value *url.URL) bool {
	if value == nil || value.Scheme != "https" || value.User != nil || value.Fragment != "" || value.Opaque != "" || value.Port() != "" && value.Port() != "443" {
		return false
	}
	switch value.Hostname() {
	case "raw.githubusercontent.com", "pypi.org", "files.pythonhosted.org", "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
		return true
	}
	return false
}

func fetchBootstrapMetadata(ctx context.Context, method, endpoint string, egress prerequisites.SetupEgress) (toolMetadata, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || !bootstrapOrigin(parsed) || method != http.MethodGet && method != http.MethodHead {
		return toolMetadata{}, bundleFailure("bootstrap publisher metadata request is invalid")
	}
	client, closeIdle, err := acquisitionClient(ctx, egress, bootstrapOrigin, time.Minute)
	if err != nil {
		return toolMetadata{}, err
	}
	defer closeIdle()
	request, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return toolMetadata{}, err
	}
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("Cache-Control", "no-cache")
	response, err := client.Do(request)
	if err != nil {
		return toolMetadata{}, bundleFailure("bootstrap publisher metadata could not be acquired")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Encoding") != "" {
		return toolMetadata{}, bundleFailure("bootstrap publisher returned an unapproved response")
	}
	if method == http.MethodHead {
		if response.ContentLength <= 0 || response.ContentLength > maxMemberBytes {
			return toolMetadata{}, bundleFailure("bootstrap publisher artifact has no bounded positive size")
		}
		return toolMetadata{size: response.ContentLength}, nil
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	if err != nil || len(data) == 0 || len(data) > 8<<20 {
		return toolMetadata{}, bundleFailure("bootstrap publisher metadata exceeds its bound or is incomplete")
	}
	return toolMetadata{data: data, size: int64(len(data))}, nil
}

type pipReportItem struct {
	IsDirect bool `json:"is_direct"`
	IsYanked bool `json:"is_yanked"`
	Metadata struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"metadata"`
	Download struct {
		URL     string `json:"url"`
		Archive struct {
			Hashes map[string]string `json:"hashes"`
		} `json:"archive_info"`
	} `json:"download_info"`
}

func parsePipReport(data []byte, value prerequisites.BootstrapDefinition) ([]prerequisites.BootstrapWheel, []prerequisites.DependencySource, error) {
	var report struct {
		Version     string `json:"version"`
		Environment struct {
			Python         string `json:"python_full_version"`
			Implementation string `json:"implementation_name"`
			Machine        string `json:"platform_machine"`
			System         string `json:"sys_platform"`
		} `json:"environment"`
		Install []pipReportItem `json:"install"`
	}
	if len(data) > 8<<20 || json.Unmarshal(data, &report) != nil || report.Version != "1" || len(report.Install) < 2 || len(report.Install) > 127 || report.Environment.Python != value.PythonVersion || report.Environment.Implementation != "cpython" || report.Environment.Machine != "x86_64" || report.Environment.System != "linux" {
		return nil, nil, bundleFailure("maintained wheel resolver returned an invalid, incompatible or unsupported report")
	}
	slices.SortFunc(report.Install, func(a, b pipReportItem) int {
		return strings.Compare(normalizeWheelName(a.Metadata.Name), normalizeWheelName(b.Metadata.Name))
	})
	wheels := make([]prerequisites.BootstrapWheel, 0, len(report.Install))
	sources := make([]prerequisites.DependencySource, 0, len(report.Install))
	names := map[string]bool{}
	for _, item := range report.Install {
		name := normalizeWheelName(item.Metadata.Name)
		endpoint, err := url.Parse(item.Download.URL)
		sum := item.Download.Archive.Hashes["sha256"]
		decoded, de := hex.DecodeString(sum)
		if len(name) > 128 || !wheelPackageName.MatchString(name) || len(item.Metadata.Version) == 0 || len(item.Metadata.Version) > 80 || strings.ContainsAny(item.Metadata.Version, "\\/\x00\r\n\t ") || len(item.Download.URL) > 4096 || names[name] || item.IsDirect || item.IsYanked || err != nil || endpoint.Scheme != "https" || endpoint.Host != "files.pythonhosted.org" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || !strings.HasPrefix(endpoint.Path, "/packages/") || !strings.HasSuffix(endpoint.Path, ".whl") || de != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != sum || name == "ansible-core" && item.Metadata.Version != value.AnsibleVersion {
			return nil, nil, bundleFailure("wheel resolver selected an unapproved source, release, or package identity")
		}
		names[name] = true
		id := "wheel-" + name + "-" + sum[:16]
		wheels = append(wheels, prerequisites.BootstrapWheel{Name: name, Version: item.Metadata.Version, SourceID: id})
		sources = append(sources, prerequisites.DependencySource{ID: id, URL: item.Download.URL, SHA256: sum})
	}
	if !names["ansible-core"] || !names["urllib3"] {
		return nil, nil, bundleFailure("wheel resolver omitted a required bootstrap dependency")
	}
	return wheels, sources, nil
}

var wheelNameSeparators = regexp.MustCompile(`[-_.]+`)
var wheelPackageName = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func normalizeWheelName(value string) string {
	return wheelNameSeparators.ReplaceAllString(strings.ToLower(value), "-")
}
