package bundlelocal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

const maxToolSourceBytes int64 = 1 << 30
const maxToolMetadataBytes int64 = 1 << 20

type toolMetadata struct {
	data []byte
	size int64
}
type toolMetadataFetcher func(context.Context, string, string, int64, prerequisites.SetupEgress) (toolMetadata, error)

// ToolCatalog resolves only the finite native client vocabulary. New performs
// no network or filesystem effects; publisher metadata is read only by Resolve.
type ToolCatalog struct{ metadata toolMetadataFetcher }

func NewToolCatalog() *ToolCatalog { return &ToolCatalog{metadata: fetchToolMetadata} }

var _ prerequisites.TargetToolCatalog = (*ToolCatalog)(nil)

func (c *ToolCatalog) Select(requests []controller.ToolRequest, retained []prerequisites.DependencySource) ([]prerequisites.ToolDefinition, bool, error) {
	requests, err := toolRequests(requests)
	if err != nil {
		return nil, false, err
	}
	if len(retained) > 4096 {
		return nil, false, bundleFailure("retained tool sources exceed their inspection bound")
	}
	result, complete := []prerequisites.ToolDefinition{}, true
	for _, request := range requests {
		prefix := toolSourcePrefix(request)
		var selected *prerequisites.ToolDefinition
		identities := map[string]prerequisites.DependencySource{}
		for _, source := range retained {
			if !strings.HasPrefix(source.ID, prefix) {
				continue
			}
			version := strings.TrimPrefix(source.ID, prefix)
			definition, err := toolDefinition(request, version, source)
			if err != nil {
				return nil, false, err
			}
			if previous, found := identities[source.ID]; found && previous != source {
				return nil, false, bundleFailure("retained tool release has conflicting publisher identities")
			}
			identities[source.ID] = source
			if selected == nil || compareStableVersions(definition.Version, selected.Version) > 0 {
				value := definition
				selected = &value
			}
		}
		if selected == nil {
			complete = false
			continue
		}
		result = append(result, *selected)
	}
	return result, complete, nil
}

// Present lists the area once and confirms each tool's retained source by size
// and each published member by existence. It opens no file and runs no tool,
// so proving a prepared host costs one directory walk.
func (c *ToolCatalog) Present(ctx context.Context, area prerequisites.BundleArea, tools []prerequisites.ToolDefinition) (bool, error) {
	if len(tools) == 0 {
		return true, nil
	}
	if area == nil {
		return false, nil
	}
	listed, err := area.Entries(ctx)
	if err != nil {
		return false, err
	}
	entries := make(map[string]prerequisites.BundleEntry, len(listed))
	for _, entry := range listed {
		entries[entry.Path] = entry
	}
	present := func(name string, size int64) bool {
		entry, found := entries[name]
		return found && !entry.Directory && (size < 0 || entry.Size == size)
	}
	for _, tool := range tools {
		if !present(sourcePath(tool.Source), tool.Source.Bytes) {
			return false, nil
		}
		for _, file := range tool.Files {
			if !present(file.Path, -1) {
				return false, nil
			}
		}
	}
	return true, nil
}

func (c *ToolCatalog) Resolve(ctx context.Context, requests []controller.ToolRequest, egress prerequisites.SetupEgress) ([]prerequisites.ToolDefinition, error) {
	requests, err := toolRequests(requests)
	if err != nil {
		return nil, err
	}
	if _, err := explicitProxy(egress); err != nil {
		return nil, err
	}
	if c == nil || c.metadata == nil {
		return nil, bundleFailure("target tool metadata resolver is unavailable")
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	result := make([]prerequisites.ToolDefinition, 0, len(requests))
	for _, request := range requests {
		if err := bounded.Err(); err != nil {
			return nil, err
		}
		definition, err := c.resolve(bounded, request, egress)
		if err != nil {
			return nil, err
		}
		result = append(result, definition)
	}
	return result, nil
}

func toolRequests(requests []controller.ToolRequest) ([]controller.ToolRequest, error) {
	if len(requests) > 128 {
		return nil, bundleFailure("target tool requests exceed the supported closure bound")
	}
	requests = slices.Clone(requests)
	for _, request := range requests {
		if request.Mirror != "" && !safeToolURL(request.Mirror) {
			return nil, bundleFailure("tool mirrors require credential-free HTTPS base URLs")
		}
		if request.Version != "latest" && !toolVersion(request.Version) {
			return nil, bundleFailure("target tool requires an exact safe release version")
		}
		switch request.Kind {
		case "helm", "govc", "kubectl":
			if request.Compatibility != "" {
				return nil, bundleFailure("generic tool request has unqualified compatibility metadata")
			}
			if request.Version != "latest" && !stableVersion(request.Version) {
				return nil, bundleFailure("generic tool version must identify a stable release")
			}
		case "openshift-clients", "openshift-install":
			if request.Version == "latest" || request.Compatibility != "openshift" && request.Compatibility != "okd" {
				return nil, bundleFailure("installer and client tools require the exact target distribution release")
			}
			if request.Compatibility == "openshift" && !stableVersion(request.Version) {
				return nil, bundleFailure("OpenShift client tools require an exact stable target release")
			}
		case "virtctl":
			if request.Compatibility != "kubevirt" || request.Version != "latest" && !stableVersion(request.Version) {
				return nil, bundleFailure("virtctl requires the upstream KubeVirt publisher and latest or an exact stable release")
			}
		default:
			return nil, bundleFailure("target native tool is outside the supported dependency vocabulary")
		}
	}
	slices.SortFunc(requests, controller.CompareToolRequests)
	return slices.Compact(requests), nil
}

func toolVersion(version string) bool {
	if version == "" || len(version) > 96 || version == "latest" {
		return false
	}
	for _, c := range version {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune(".-_+", c)) {
			return false
		}
	}
	return version[0] >= '0' && version[0] <= '9' || version[0] == 'v'
}

func stableVersion(version string) bool {
	parts := strings.Split(strings.TrimPrefix(version, "v"), ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" || len(part) > 9 || len(part) > 1 && part[0] == '0' {
			return false
		}
		for _, c := range part {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

func compareStableVersions(a, b string) int {
	if stableVersion(a) && stableVersion(b) {
		left, right := strings.Split(strings.TrimPrefix(a, "v"), "."), strings.Split(strings.TrimPrefix(b, "v"), ".")
		for i := range left {
			av, _ := strconv.Atoi(left[i])
			bv, _ := strconv.Atoi(right[i])
			if av < bv {
				return -1
			}
			if av > bv {
				return 1
			}
		}
		// Both spellings are accepted at the boundary. Keep selection stable even
		// when retained sources contain both 1.2.3 and v1.2.3 identities.
		return strings.Compare(a, b)
	}
	return strings.Compare(a, b)
}

func toolSourcePrefix(request controller.ToolRequest) string {
	data, _ := json.Marshal(request)
	digest := sha256.Sum256(data)
	return "tool-" + request.Kind + "-" + hex.EncodeToString(digest[:16]) + "-"
}

func sourceURL(request controller.ToolRequest, version string) (string, string, error) {
	base, filename := "", ""
	switch request.Kind {
	case "helm":
		base, filename = "https://get.helm.sh", "helm-"+withV(version)+"-linux-amd64.tar.gz"
	case "govc":
		base, filename = "https://github.com/vmware/govmomi/releases/download/"+withV(version), "govc_Linux_x86_64.tar.gz"
	case "kubectl":
		base, filename = "https://dl.k8s.io/release/"+withV(version)+"/bin/linux/amd64", "kubectl"
	case "virtctl":
		if request.Compatibility != "kubevirt" {
			return "", "", bundleFailure("virtctl source must identify the upstream KubeVirt publisher")
		}
		base, filename = "https://github.com/kubevirt/kubevirt/releases/download/"+withV(version), "virtctl-"+withV(version)+"-linux-amd64"
	case "openshift-clients", "openshift-install":
		name := "openshift-install"
		if request.Kind == "openshift-clients" {
			name = "openshift-client"
		}
		filename = name + "-linux-" + version + ".tar.gz"
		base = "https://mirror.openshift.com/pub/openshift-v4/clients/ocp/" + version
		if request.Compatibility == "okd" {
			base = "https://github.com/okd-project/okd/releases/download/" + version
		}
	default:
		return "", "", bundleFailure("tool source has no qualified publisher")
	}
	if request.Mirror != "" {
		base = strings.TrimSuffix(request.Mirror, "/")
		if request.Kind != "helm" {
			base += "/" + version
		}
	}
	return base + "/" + filename, filename, nil
}

func withV(version string) string { return "v" + strings.TrimPrefix(version, "v") }

const okdPrimaryRepository = "okd-project/okd"
const okdSCOSRepository = "okd-project/okd-scos"

func okdSourceURL(endpoint, repository string) string {
	return strings.Replace(endpoint, "https://github.com/"+okdPrimaryRepository+"/", "https://github.com/"+repository+"/", 1)
}

func toolDefinition(request controller.ToolRequest, version string, source prerequisites.DependencySource) (prerequisites.ToolDefinition, error) {
	if !toolVersion(version) || request.Version != "latest" && version != request.Version || request.Version == "latest" && !stableVersion(version) {
		return prerequisites.ToolDefinition{}, bundleFailure("retained tool version does not satisfy its original request")
	}
	expected, _, err := sourceURL(request, version)
	if err != nil {
		return prerequisites.ToolDefinition{}, err
	}
	digest, decodeErr := hex.DecodeString(source.SHA256)
	approvedURL := source.URL == expected
	if request.Compatibility == "okd" && request.Mirror == "" {
		approvedURL = approvedURL || source.URL == okdSourceURL(expected, okdSCOSRepository)
	}
	if source.ID != toolSourcePrefix(request)+version || !approvedURL || decodeErr != nil || len(digest) != sha256.Size || hex.EncodeToString(digest) != source.SHA256 || source.Bytes <= 0 || source.Bytes > maxToolSourceBytes {
		return prerequisites.ToolDefinition{}, bundleFailure("tool source does not match its frozen publisher identity and bounds")
	}
	definition := prerequisites.ToolDefinition{Kind: request.Kind, Version: version, Compatibility: request.Compatibility, Source: source, Archive: "tar.gz", Files: []prerequisites.ToolFile{}}
	prefix := path.Join("tools", request.Kind, request.Compatibility, version)
	members := []string{request.Kind}
	switch request.Kind {
	case "helm":
		members = []string{"linux-amd64/helm"}
	case "openshift-clients":
		members = []string{"oc", "kubectl"}
	case "virtctl", "kubectl":
		definition.Archive = "binary"
	}
	for _, member := range members {
		definition.Files = append(definition.Files, prerequisites.ToolFile{Member: member, Path: path.Join(prefix, path.Base(member))})
	}
	return definition, nil
}

type githubToolRelease struct {
	Tag        string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Size   int64  `json:"size"`
		Digest string `json:"digest"`
	} `json:"assets"`
}

func (c *ToolCatalog) release(ctx context.Context, repository, version string, egress prerequisites.SetupEgress) (githubToolRelease, error) {
	endpoint := "https://api.github.com/repos/" + repository + "/releases/"
	if version == "latest" {
		endpoint += "latest"
	} else {
		endpoint += "tags/" + url.PathEscape(version)
	}
	data, err := c.metadata(ctx, http.MethodGet, endpoint, maxToolMetadataBytes, egress)
	var release githubToolRelease
	if err != nil {
		return release, err
	}
	if json.Unmarshal(data.data, &release) != nil || release.Draft || release.Prerelease || !toolVersion(release.Tag) || len(release.Assets) > 256 || version != "latest" && release.Tag != version {
		return release, bundleFailure("publisher release metadata is missing, unstable, or inconsistent")
	}
	return release, nil
}

func (c *ToolCatalog) resolve(ctx context.Context, request controller.ToolRequest, egress prerequisites.SetupEgress) (prerequisites.ToolDefinition, error) {
	version := request.Version
	var release githubToolRelease
	var err error
	repository := ""
	switch request.Kind {
	case "helm":
		repository = "helm/helm"
	case "govc":
		repository = "vmware/govmomi"
	case "virtctl":
		repository = "kubevirt/kubevirt"
	case "openshift-clients", "openshift-install":
		if request.Compatibility == "okd" {
			repository = okdPrimaryRepository
		}
	}
	if repository != "" {
		tag := version
		if version != "latest" && request.Compatibility != "okd" {
			tag = withV(version)
		}
		release, err = c.release(ctx, repository, tag, egress)
		// Both repositories are owned by the OKD project. The older SCOS stream
		// lives in its own repository; discover only an exact published tag,
		// without guessing a repository from the numeric OpenShift version.
		if request.Compatibility == "okd" && errors.Is(err, errToolMetadataNotFound) {
			repository = okdSCOSRepository
			release, err = c.release(ctx, repository, tag, egress)
		}
		if err != nil {
			return prerequisites.ToolDefinition{}, err
		}
		if version == "latest" {
			version = release.Tag
		}
	}
	if request.Kind == "kubectl" && version == "latest" {
		metadata, err := c.metadata(ctx, http.MethodGet, "https://dl.k8s.io/release/stable.txt", 128, egress)
		if err != nil {
			return prerequisites.ToolDefinition{}, err
		}
		version = strings.TrimSpace(string(metadata.data))
	}
	if request.Version == "latest" && !stableVersion(version) {
		return prerequisites.ToolDefinition{}, bundleFailure("latest tool metadata does not identify a stable release")
	}
	publisherRequest := request
	publisherRequest.Mirror = ""
	publisher, filename, err := sourceURL(publisherRequest, version)
	if err != nil {
		return prerequisites.ToolDefinition{}, err
	}
	if request.Compatibility == "okd" {
		publisher = okdSourceURL(publisher, repository)
	}
	source := prerequisites.DependencySource{ID: toolSourcePrefix(request) + version}
	source.URL, _, err = sourceURL(request, version)
	if err != nil {
		return prerequisites.ToolDefinition{}, err
	}
	if request.Compatibility == "okd" && request.Mirror == "" {
		source.URL = publisher
	}
	for _, asset := range release.Assets {
		if asset.Name != filename {
			continue
		}
		if asset.URL != publisher || asset.Size <= 0 || asset.Size > maxToolSourceBytes || source.Bytes != 0 {
			return prerequisites.ToolDefinition{}, bundleFailure("publisher asset identity is ambiguous or outside its bound")
		}
		source.Bytes = asset.Size
		if strings.HasPrefix(asset.Digest, "sha256:") {
			source.SHA256 = strings.TrimPrefix(asset.Digest, "sha256:")
		}
	}
	checksumURL := ""
	if source.SHA256 == "" {
		switch request.Kind {
		case "helm":
			checksumURL = publisher + ".sha256sum"
		case "kubectl":
			checksumURL = publisher + ".sha256"
		case "openshift-clients", "openshift-install":
			checksumURL = publisher[:strings.LastIndex(publisher, "/")+1] + "sha256sum.txt"
		case "govc":
			checksumURL = "https://github.com/vmware/govmomi/releases/download/" + withV(version) + "/checksums.txt"
		default:
			return prerequisites.ToolDefinition{}, bundleFailure("virtctl release lacks authenticated publisher SHA-256 metadata")
		}
		metadata, err := c.metadata(ctx, http.MethodGet, checksumURL, maxToolMetadataBytes, egress)
		if err != nil {
			return prerequisites.ToolDefinition{}, err
		}
		source.SHA256, err = toolChecksum(metadata.data, filename)
		if err != nil {
			return prerequisites.ToolDefinition{}, err
		}
	}
	if source.Bytes == 0 {
		metadata, err := c.metadata(ctx, http.MethodHead, publisher, 0, egress)
		if err != nil {
			return prerequisites.ToolDefinition{}, err
		}
		source.Bytes = metadata.size
	}
	return toolDefinition(request, version, source)
}

func toolChecksum(data []byte, filename string) (string, error) {
	result := ""
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 1 && len(fields) != 2 || len(fields) == 1 && len(lines) != 1 {
			continue
		}
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") != filename {
			continue
		}
		digest, err := hex.DecodeString(fields[0])
		if err != nil || len(digest) != sha256.Size || hex.EncodeToString(digest) != fields[0] || result != "" {
			return "", bundleFailure("publisher checksum metadata is malformed or ambiguous")
		}
		result = fields[0]
	}
	if result == "" {
		return "", bundleFailure("publisher checksum metadata does not name the required asset")
	}
	return result, nil
}
