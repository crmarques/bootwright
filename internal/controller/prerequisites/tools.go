package prerequisites

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// ToolsDigest names the shared host area that holds one exact target tool
// closure. It is content-addressed by the resolved tools alone, so every
// context selecting the same tools proves the same files and a different
// closure never disturbs them.
func ToolsDigest(tools []ToolDefinition) string {
	ordered := slices.Clone(tools)
	slices.SortFunc(ordered, func(a, b ToolDefinition) int { return strings.Compare(a.Source.ID, b.Source.ID) })
	encoded, err := json.Marshal(ordered)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(append([]byte("bootwright.controller.tool-area-v1\x00"), encoded...))
	return hex.EncodeToString(digest[:])
}

// ToolDefinition freezes publisher metadata for the Ansible dependency role.
// Only fixed regular archive members may become the named private executables.
type ToolDefinition struct {
	Kind          string           `json:"kind"`
	Version       string           `json:"version"`
	Compatibility string           `json:"compatibility"`
	Archive       string           `json:"archive"`
	Source        DependencySource `json:"source"`
	Files         []ToolFile       `json:"files"`
}

type ToolFile struct {
	Member string `json:"member"`
	Path   string `json:"path"`
}

// UnreleasedClient refuses an OpenShift client whose oc does not name its
// frozen release, whether the adapter's release-stamp check or Go's read-only
// projection found it. The same release and mirror reuse the retained source,
// so restoring sources cannot help; only another release or mirror can.
func UnreleasedClient(tool ToolDefinition) error {
	return failure("controller.setup", "the oc of OpenShift client release "+tool.Version+" does not name its frozen release",
		"declare a release.version or downloads.openshiftClientsMirror whose oc passes the release-stamp check; the same release and mirror reuse the retained source and refuse again")
}

// WithTools creates a fresh immutable dependency closure. The base catalog
// stays identifiable while exact resolved tool metadata names its own bundle.
func WithTools(base Definition, tools []ToolDefinition) (Definition, error) {
	invalid := func() (Definition, error) {
		return Definition{}, diagnostics.NewFailure("controller.unsupported", "resolved target tool closure is malformed or exceeds its bound", "")
	}
	if len(tools) > 128 || len(base.Tools) != 0 {
		return invalid()
	}
	baseDigest := base.BaseCatalogDigest
	if baseDigest == "" {
		baseDigest = base.CatalogDigest
	}
	digest, err := hex.DecodeString(baseDigest)
	if err != nil || len(digest) != sha256.Size || hex.EncodeToString(digest) != baseDigest {
		return invalid()
	}
	base.BaseCatalogDigest = baseDigest
	base.Sources = slices.Clone(base.Sources)
	base.Runtime.Files = slices.Clone(base.Runtime.Files)
	base.Runtime.Links = slices.Clone(base.Runtime.Links)
	base.Execution.Files = slices.Clone(base.Execution.Files)
	base.Execution.Links = slices.Clone(base.Execution.Links)
	base.Execution.Preload = slices.Clone(base.Execution.Preload)
	base.Tools = slices.Clone(tools)
	sources := map[string]DependencySource{}
	for _, source := range base.Sources {
		if prior, exists := sources[source.ID]; exists && prior != source {
			return invalid()
		}
		sources[source.ID] = source
	}
	files := map[string]bool{}
	for index, tool := range base.Tools {
		if tool.Kind == "" || tool.Version == "" || len(tool.Files) == 0 || len(tool.Files) > 8 || tool.Archive != "tar.gz" && tool.Archive != "binary" {
			return invalid()
		}
		digest, err := hex.DecodeString(tool.Source.SHA256)
		endpoint, urlErr := url.Parse(tool.Source.URL)
		if err != nil || len(digest) != sha256.Size || hex.EncodeToString(digest) != tool.Source.SHA256 || urlErr != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || tool.Source.ID == "" || len(tool.Source.ID) > 256 || tool.Source.Bytes <= 0 || tool.Source.Bytes > 1<<30 {
			return invalid()
		}
		if prior, found := sources[tool.Source.ID]; found && prior != tool.Source {
			return invalid()
		}
		sources[tool.Source.ID] = tool.Source
		base.Tools[index].Files = slices.Clone(tool.Files)
		for _, file := range tool.Files {
			if file.Member == "" || file.Member == "." || path.Clean(file.Member) != file.Member || strings.HasPrefix(file.Member, "/") || strings.HasPrefix(file.Member, "../") || strings.ContainsAny(file.Member, "\\\x00\r\n\t") || !strings.HasPrefix(file.Path, "tools/") || path.Clean(file.Path) != file.Path || strings.ContainsAny(file.Path, "\\\x00\r\n\t") || files[file.Path] {
				return invalid()
			}
			files[file.Path] = true
		}
	}
	base.Sources = base.Sources[:0]
	if len(sources) > 512 {
		return invalid()
	}
	for _, source := range sources {
		base.Sources = append(base.Sources, source)
	}
	slices.SortFunc(base.Sources, func(a, b DependencySource) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(base.Tools, func(a, b ToolDefinition) int { return strings.Compare(a.Source.ID, b.Source.ID) })
	base.CatalogDigest = baseDigest
	if len(base.Tools) != 0 {
		encoded, err := json.Marshal(struct {
			Base  string           `json:"base"`
			Tools []ToolDefinition `json:"tools"`
		}{baseDigest, base.Tools})
		if err != nil {
			return invalid()
		}
		digest := sha256.Sum256(append([]byte("bootwright.controller.tools-v1\x00"), encoded...))
		base.CatalogDigest = hex.EncodeToString(digest[:])
	}
	return base, nil
}

// LocateInstalledTool answers the absolute path of one executable a retained
// controller closure published on this host. A search path is never authority:
// a consumer runs the exact file the controller stage installed for the
// release its own graph selected.
func LocateInstalledTool(ctx context.Context, view StorageView, tool controller.InstalledTool) (string, error) {
	if view.OpenBundle == nil {
		return "", failure("controller.state", "the retained controller areas are unavailable",
			"run bootwright setup, then apply --stage controller")
	}
	for _, definition := range view.State.RetainedDefinitions {
		member, found := publishedMember(definition.Tools, tool)
		if !found {
			continue
		}
		area, err := view.OpenBundle(ctx, ToolsDigest(definition.Tools))
		if err != nil || area == nil {
			continue
		}
		entries, err := area.Entries(ctx)
		if err != nil {
			continue
		}
		if !slices.ContainsFunc(entries, func(entry BundleEntry) bool {
			return entry.Path == member && !entry.Directory
		}) {
			continue
		}
		location, err := area.Location(ctx)
		if err != nil {
			continue
		}
		return location.Path + "/" + member, nil
	}
	return "", failure("controller.state",
		"the "+tool.Executable+" of release "+tool.Version+" is not installed on this controller",
		"run bootwright apply --stage controller")
}

// publishedMember is the file one retained closure publishes for exactly this
// client kind, compatibility and version. A closure publishes more than one
// executable, so the member is named rather than inferred from the kind.
func publishedMember(tools []ToolDefinition, tool controller.InstalledTool) (string, bool) {
	for _, definition := range tools {
		if definition.Kind != tool.Kind || definition.Compatibility != tool.Compatibility || definition.Version != tool.Version {
			continue
		}
		for _, file := range definition.Files {
			if strings.HasSuffix(file.Path, "/"+tool.Executable) {
				return file.Path, true
			}
		}
	}
	return "", false
}
