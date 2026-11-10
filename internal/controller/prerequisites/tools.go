package prerequisites

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/controller"
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
// frozen release, as proved by the adapter before publication. Published
// clients are checked for presence only. The same release and mirror reuse the retained source,
// so restoring sources cannot help; only another release or mirror can.
func UnreleasedClient(tool ToolDefinition) error {
	return failure("controller.install", "the oc of OpenShift client release "+tool.Version+" does not name its frozen release",
		"declare a release.version or downloads.openshiftClientsMirror whose oc passes the release-stamp check; the same release and mirror reuse the retained source and refuse again")
}

// LocateInstalledTool answers the absolute path of one executable the exact
// closure tools names published on this host. A search path is never
// authority: the caller recovers the closure its own context's controller
// stage selects, so a consumer runs the exact file that stage installed for
// the release its own graph selected, and never one another closure holds.
func LocateInstalledTool(ctx context.Context, view StorageView, tools []ToolDefinition, tool controller.InstalledTool) (string, error) {
	unavailable, missing := "run bootwright controller install, then apply --stage controller", "run bootwright apply --stage controller"
	if view.Context.Name != "" {
		unavailable, missing = "run bootwright controller install, then "+stageInvocation(view.Context.Name), stageCommand(view.Context.Name)
	}
	if view.OpenBundle == nil {
		return "", failure("controller.state", "the retained controller areas are unavailable", unavailable)
	}
	absent := failure("controller.state",
		"the "+tool.Executable+" of release "+tool.Version+" is not installed on this controller",
		missing)
	member, found := publishedMember(tools, tool)
	if !found {
		return "", absent
	}
	area, err := view.OpenBundle(ctx, ToolsDigest(tools))
	if err != nil {
		return "", err
	}
	if area == nil {
		return "", absent
	}
	entries, err := area.Entries(ctx)
	if err != nil {
		return "", err
	}
	if !slices.ContainsFunc(entries, func(entry BundleEntry) bool {
		return entry.Path == member && !entry.Directory
	}) {
		return "", absent
	}
	location, err := area.Location(ctx)
	if err != nil {
		return "", err
	}
	return location.Path + "/" + member, nil
}

// publishedMember is the file one retained closure publishes for exactly this
// client kind, compatibility and version. A closure publishes several
// executables, so the member is named.
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

func SelectContextTools(_ context.Context, catalog TargetToolCatalog, view StorageView, requests []controller.ToolRequest) ([]ToolDefinition, bool, error) {
	return catalog.Select(requests, view.State.ToolSources)
}
