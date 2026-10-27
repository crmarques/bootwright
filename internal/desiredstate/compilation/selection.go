package compilation

import (
	"path/filepath"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/customplaybooks"
	"github.com/crmarques/bootwright/internal/environment"
)

func selectResources(sources desiredstate.Sources, env *objectRecord) map[string]bool {
	selected := map[string]bool{env.path: true}
	for _, file := range sources.Files {
		selected[file.Path()] = true
	}
	return selected
}

func pathWithin(path, base string) bool {
	rel, err := filepath.Rel(base, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func validateIdentities(records []*objectRecord, ds *diagnosticSink, members func(api.Object) (environment.Membership, bool)) {
	identities := map[string][]*objectRecord{}
	clusters := map[string][]*objectRecord{}
	for _, record := range records {
		identities[record.object.Identity()] = append(identities[record.object.Identity()], record)
		if api.IsClusterKind(record.object.Kind()) {
			clusters[record.object.Name()] = append(clusters[record.object.Name()], record)
		}
	}
	for _, record := range records {
		if len(identities[record.object.Identity()]) > 1 {
			ds.issue(record, api.Issue{Code: "api.duplicate", Field: "$.metadata.name", Message: "object identity is ambiguous; every declaration must be unique within its kind"})
		}
		if api.IsClusterKind(record.object.Kind()) {
			if others := otherClusterKinds(record, clusters[record.object.Name()]); len(others) > 0 {
				ds.issue(record, api.Issue{Code: "api.duplicate", Field: "$.metadata.name", Message: "every cluster kind shares one name namespace, and " + strings.Join(others, ", ") + " also takes this name",
					Remediation: "rename one of " + strings.Join(append([]string{record.object.Identity()}, others...), ", ") + " and every reference to it"})
			}
		}
	}
	bindings := map[string]int{}
	for _, record := range records {
		membership, _ := members(record.object)
		for _, node := range membership.Nodes {
			if node.Machine != "" {
				bindings[node.Machine]++
			}
		}
	}
	for _, record := range records {
		membership, _ := members(record.object)
		for _, node := range membership.Nodes {
			if bindings[node.Machine] > 1 {
				ds.issue(record, api.Issue{Code: "api.invariant", Field: membership.Field, Message: "a Machine may be bound by only one selected cluster node"})
			}
		}
	}
}

func otherClusterKinds(record *objectRecord, collisions []*objectRecord) []string {
	var others []string
	for _, collision := range collisions {
		if collision.object.Kind() != record.object.Kind() && !slices.Contains(others, collision.object.Identity()) {
			others = append(others, collision.object.Identity())
		}
	}
	slices.Sort(others)
	return others
}

func validateSourcePaths(record *objectRecord, sources desiredstate.Sources, ds *diagnosticSink) {
	roots := append([]string{}, sources.Roots...)
	for i, root := range roots {
		for _, file := range sources.Files {
			if root == file.Path() {
				roots[i] = filepath.Dir(root)
				break
			}
		}
	}
	for _, source := range customplaybooks.ExternalSourcePaths(record.object) {
		resolved := filepath.Clean(source.Path)
		for _, root := range roots {
			if resolved == root || pathWithin(resolved, root) {
				ds.issue(record, api.Issue{Code: "api.invariant", Field: source.Field, Message: "external content must be outside every desired-state input tree", Remediation: "use a co-located content source or an absolute external content location"})
				break
			}
		}
	}
}
