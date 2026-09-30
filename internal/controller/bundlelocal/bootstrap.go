package bundlelocal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

const pythonMetadataURL = "https://raw.githubusercontent.com/astral-sh/uv/main/crates/uv-python/download-metadata.json"

// ansibleIndexURL is ansible-core's project page on PyPI's Index API, which
// serves its PEP 691 JSON form only when asked for simpleIndexJSON.
const ansibleIndexURL = "https://pypi.org/simple/ansible-core/"
const simpleIndexJSON = "application/vnd.pypi.simple.v1+json"

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
	if err := qualifiedIntent(versions); err != nil {
		return prerequisites.BootstrapDefinition{}, err
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
	ansibleMetadata, err := c.metadata(ctx, http.MethodGet, ansibleIndexURL, egress)
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
		Metadata:  []prerequisites.DependencySource{bootstrapMetadataSource("python-metadata", pythonMetadataURL, pythonMetadata.data), bootstrapMetadataSource("ansible-metadata", ansibleIndexURL, ansibleMetadata.data)},
		Execution: cloneExecution(native.Execution), AutomationDigest: ansible.Digest(),
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

// qualifiedIntent refuses, before any publisher is contacted, an exact
// release this build does not qualify. No input sets one today; the rule
// holds the same set that latest selects from.
func qualifiedIntent(versions controller.DependencyVersions) error {
	if versions.Python != "latest" && !stableBootstrapVersion.MatchString(versions.Python) || versions.Ansible != "latest" && !stableBootstrapVersion.MatchString(versions.Ansible) {
		return bundleFailure("bootstrap version intent must be latest or an exact stable release")
	}
	if versions.Python != "latest" && !prerequisites.QualifiedControllerPython(versions.Python) {
		return unqualifiedPython()
	}
	if versions.Ansible != "latest" {
		return prerequisites.ValidateQualifiedAnsibleVersion(versions.Ansible)
	}
	return nil
}

func unqualifiedPython() error {
	return diagnostics.NewFailureWithRemediation("controller.unsupported", "controller automation is qualified for CPython "+strings.Join(prerequisites.QualifiedControllerPythons(), ", ")+" only", "", "use a Bootwright build that qualifies this Python minor")
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
		if candidate.Name != "cpython" || candidate.OS != "linux" || candidate.Libc != "gnu" || candidate.Arch.Family != "x86_64" || candidate.Arch.Variant != nil || candidate.Variant != nil || candidate.Prerelease != "" || candidate.Major != 3 || candidate.Patch < 0 || candidate.Patch > 9999 || !pythonBuildDate.MatchString(candidate.Build) {
			continue
		}
		version := strconv.Itoa(candidate.Major) + "." + strconv.Itoa(candidate.Minor) + "." + strconv.Itoa(candidate.Patch)
		if !prerequisites.QualifiedControllerPython(version) || requested != "latest" && requested != version {
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

type ansibleIndexFile struct {
	Filename string          `json:"filename"`
	Yanked   json.RawMessage `json:"yanked"`
}

var indexAPIVersion = regexp.MustCompile(`^1\.(0|[1-9][0-9]*)$`)

// indexAPIMinor is the newest Index API minor this selector was checked
// against: ansible-core's page, read on 2026-09-29, and
// https://docs.pypi.org/api/index-api/ serve 1.4. PEP 629 asks a client to
// warn of a newer minor; setup refuses one instead.
const indexAPIMinor = 4

// selectAnsibleRelease picks, from the Index API project page, the highest
// stable patch of the qualified minor that publishes a live pure wheel, or
// the requested exact release when it is such a candidate. Python
// compatibility is not judged here: pip's exact-root resolve enforces
// Requires-Python, so an incompatible release refuses rather than yielding to
// an older one.
func selectAnsibleRelease(data []byte, requested, python string) (string, error) {
	if !prerequisites.QualifiedControllerPython(python) {
		return "", unqualifiedPython()
	}
	if requested != "latest" {
		if err := prerequisites.ValidateQualifiedAnsibleVersion(requested); err != nil {
			return "", err
		}
	}
	invalid := bundleFailure("requested Ansible release has no valid publisher metadata")
	if len(data) > 8<<20 {
		return "", invalid
	}
	var page struct {
		Meta struct {
			APIVersion string `json:"api-version"`
		} `json:"meta"`
		Name  string             `json:"name"`
		Files []ansibleIndexFile `json:"files"`
	}
	if json.Unmarshal(data, &page) != nil || !indexAPIVersion.MatchString(page.Meta.APIVersion) || page.Name != "ansible-core" {
		return "", invalid
	}
	if minor, err := strconv.Atoi(strings.TrimPrefix(page.Meta.APIVersion, "1.")); err != nil || minor > indexAPIMinor {
		return "", diagnostics.NewFailureWithRemediation("controller.unsupported", "the publisher's Index API page is version "+page.Meta.APIVersion+", newer than the 1."+strconv.Itoa(indexAPIMinor)+" this build reads", "", "use a Bootwright build that reads this Index API version")
	}
	version, ok := qualifiedAnsible(page.Files, requested)
	switch {
	case !ok:
		return "", invalid
	case version == "" && requested == "latest":
		return "", bundleFailure("Ansible publisher metadata lists no stable ansible-core " + prerequisites.QualifiedAnsibleMinor + " release with a live wheel")
	case version == "":
		return "", bundleFailure("Ansible publisher metadata lists no live wheel of ansible-core " + requested)
	}
	return version, nil
}

// qualifiedAnsible reports false when a file's yank is none of the shapes
// PEP 691 allows: absent, a Boolean, or a non-empty reason.
func qualifiedAnsible(files []ansibleIndexFile, requested string) (string, bool) {
	selected, selectedPatch := "", -1
	for _, file := range files {
		var reason string
		yanked := string(file.Yanked) == "true" || json.Unmarshal(file.Yanked, &reason) == nil && reason != ""
		if !yanked && file.Yanked != nil && string(file.Yanked) != "false" {
			return "", false
		}
		version, wheel := strings.CutPrefix(file.Filename, "ansible_core-")
		version, pure := strings.CutSuffix(version, "-py3-none-any.whl")
		if yanked || !wheel || !pure || prerequisites.ValidateQualifiedAnsibleVersion(version) != nil || requested != "latest" && version != requested {
			continue
		}
		patch, err := strconv.Atoi(version[strings.LastIndex(version, ".")+1:])
		if err == nil && patch > selectedPatch {
			selected, selectedPatch = version, patch
		}
	}
	return selected, true
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
	return receiveBootstrapMetadata(ctx, method, endpoint, client.Do)
}

func receiveBootstrapMetadata(ctx context.Context, method, endpoint string, send func(*http.Request) (*http.Response, error)) (toolMetadata, error) {
	request, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return toolMetadata{}, err
	}
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("Cache-Control", "no-cache")
	if endpoint == ansibleIndexURL {
		request.Header.Set("Accept", simpleIndexJSON)
	}
	response, err := send(request)
	if err != nil {
		if ctx.Err() != nil {
			return toolMetadata{}, ctx.Err()
		}
		return toolMetadata{}, transportFailure("bootstrap publisher", endpoint, err)
	}
	defer response.Body.Close()
	mediaType, _, typeErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Encoding") != "" || endpoint == ansibleIndexURL && (typeErr != nil || mediaType != simpleIndexJSON) {
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
