package prerequisites

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/controller/evidence"
)

// ToolsDigest names a host tool area by its exact closure, so contexts with
// the same resolved tools share one area and other closures use separate areas.
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

// ToolFiles defines the exact managed executables one target release publishes.
func ToolFiles(request evidence.ToolRequest) []ToolFile {
	prefix := path.Join("tools", request.Kind, request.Compatibility, request.Version)
	members := []string{request.Kind}
	switch request.Kind {
	case "helm":
		members = []string{"linux-amd64/helm"}
	case "openshift-clients":
		members = []string{"oc", "kubectl"}
	}
	files := make([]ToolFile, 0, len(members))
	for _, member := range members {
		files = append(files, ToolFile{Member: member, Path: path.Join(prefix, path.Base(member))})
	}
	return files
}

// ToolFilePresence attributes each member to its retained archive and checks
// the whole area in one walk. It neither opens nor executes the member.
func ToolFilePresence(ctx context.Context, area BundleArea, tools []ToolDefinition) (map[string]string, error) {
	result := make(map[string]string)
	entries := map[string]BundleEntry{}
	if area != nil && len(tools) != 0 {
		listed, err := area.Entries(ctx)
		if err != nil {
			return nil, err
		}
		for _, entry := range listed {
			entries[entry.Path] = entry
		}
	}
	for _, tool := range tools {
		source, retained := entries[path.Join("sources", tool.Source.ID)]
		for _, file := range tool.Files {
			entry, present := entries[file.Path]
			switch {
			case !retained:
				result[file.Path] = "managed source missing"
			case source.Directory || source.Size != tool.Source.Bytes:
				result[file.Path] = "managed source does not match its retained identity"
			case !present:
				result[file.Path] = "managed executable missing"
			case entry.Directory:
				result[file.Path] = "managed executable is a directory"
			default:
				result[file.Path] = ""
			}
		}
	}
	return result, nil
}

// UnreleasedClient refuses an OpenShift client whose oc does not name its
// frozen release, as proved by the adapter before publication. Published
// clients are checked for presence only. The same release reuses the retained
// source, so restoring sources cannot help; select another release.
func UnreleasedClient(tool ToolDefinition) error {
	return failure("controller.install", "the oc of OpenShift client release "+tool.Version+" does not name its frozen release",
		"declare a release.version whose oc passes the release-stamp check; the same release reuses the retained source and refuses again")
}

// LocateInstalledTool returns an absolute executable path in the caller's
// exact controller-stage closure. It does not consult the search path.
func LocateInstalledTool(ctx context.Context, view StorageView, tools []ToolDefinition, tool evidence.InstalledTool) (string, error) {
	unavailable, missing := "run bootwright install, then apply --stage controller", "run bootwright apply --stage controller"
	if view.Context.Name != "" {
		unavailable, missing = "run bootwright install, then "+stageInvocation(view.Context.Name), stageCommand(view.Context.Name)
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

// publishedMember selects one named executable by kind, compatibility and release.
func publishedMember(tools []ToolDefinition, tool evidence.InstalledTool) (string, bool) {
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

func SelectContextTools(_ context.Context, catalog TargetToolCatalog, view StorageView, requests []evidence.ToolRequest) ([]ToolDefinition, bool, error) {
	return catalog.Select(requests, view.State.ToolSources)
}
