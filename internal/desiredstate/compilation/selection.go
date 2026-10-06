package compilation

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/customplaybooks"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func selectResources(sources desiredstate.Sources, documents []desiredstate.Document, env *objectRecord, ds *diagnosticSink) (map[string]bool, []string) {
	selected := map[string]bool{env.path: true}
	base := filepath.Dir(env.path)
	resources := env.object.Spec().Get("resources")
	if !resources.Present() || resources.Len() == 0 {
		for _, file := range sources.Files {
			selected[file.Path()] = true
		}
		return selected, []string{}
	}
	selectDeclaredResources(sources, resources, env, base, selected, ds)
	selectStoredAddons(sources, documents, base, selected)
	return selected, excludedResources(sources, documents, base, selected, ds)
}

const resourceSelectionRemedy = "list a .yaml or .yml file, or a directory holding them, below the Environment's directory, and include it in the input you pass"

func selectDeclaredResources(sources desiredstate.Sources, resources api.Value, env *objectRecord, base string, selected map[string]bool, ds *diagnosticSink) {
	cleaned := map[string]int{}
	raw := map[string]bool{}
	for i, resource := range resources.Items() {
		field := "$.spec.resources[" + strconv.Itoa(i) + "]"
		value := resource.Text()
		clean := filepath.Clean(value)
		if raw[value] {
			continue
		}
		raw[value] = true
		if value == "" || strings.TrimSpace(value) != value || filepath.IsAbs(value) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || strings.ContainsRune(value, 0) {
			ds.issue(env, api.Issue{Code: "api.value", Field: field, Message: "resource path must stay inside the Environment directory", Remediation: "use a relative path below the Environment's directory"})
			continue
		}
		if earlier, seen := cleaned[clean]; seen {
			ds.issue(env, api.Issue{Code: "api.duplicate", Field: field, Message: "resource path repeats entry [" + strconv.Itoa(earlier) + "] after cleaning", Remediation: "remove the repeated entry"})
			continue
		}
		cleaned[clean] = i
		candidate := filepath.Join(base, clean)
		matches := 0
		for _, file := range sources.Files {
			if file.Path() == candidate || pathWithin(file.Path(), candidate) {
				selected[file.Path()] = true
				matches++
			}
		}
		if matches == 0 {
			ds.issue(env, api.Issue{Code: "api.reference", Field: field, Message: unmatchedResourceCause(value, clean), Remediation: resourceSelectionRemedy})
		}
	}
}

func unmatchedResourceCause(value, clean string) string {
	if strings.ContainsAny(value, "*?[") {
		return "resource paths are literal; glob patterns are not expanded"
	}
	segments := strings.Split(filepath.ToSlash(clean), "/")
	if last := segments[len(segments)-1]; strings.HasSuffix(last, ".yaml") || strings.HasSuffix(last, ".yml") {
		segments = segments[:len(segments)-1]
	}
	if slices.ContainsFunc(segments, func(segment string) bool { return segment != "." && desiredstate.SkippedDirectory(segment) }) {
		return "resource path is inside a directory discovery skips (dot-prefixed, vendor, node_modules, playbooks, roles, collections, manifests or secrets)"
	}
	return "resource path selects no discovered desired-state file"
}

func selectStoredAddons(sources desiredstate.Sources, documents []desiredstate.Document, base string, selected map[string]bool) {
	// A marker proves descriptor selection only. It never authenticates a package.
	markers := map[string][]desiredstate.SourceFile{}
	for _, marker := range sources.Markers {
		markers[marker.Path()] = append(markers[marker.Path()], marker)
	}
	for _, file := range sources.Files {
		rel, err := filepath.Rel(base, file.Path())
		if err != nil {
			continue
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) != 4 || parts[0] != "add-ons" || parts[1] != "_store" || parts[3] != "add-on.yaml" || !api.ValidLexical("name", parts[2]) {
			continue
		}
		matches := markers[filepath.Join(filepath.Dir(file.Path()), ".bootwright-addon")]
		if len(matches) != 1 || string(matches[0].Bytes()) != parts[2]+"\n" {
			continue
		}
		count := 0
		valid := false
		for _, document := range documents {
			if document.Path != file.Path() || document.IsEmpty() {
				continue
			}
			count++
			root := documentBody(document)
			valid = nodeText(mappingNode(root, "apiVersion")) == api.APIVersion && nodeText(mappingNode(root, "kind")) == string(api.ClusterAddon) && nodeText(mappingNode(mappingNode(root, "metadata"), "name")) == parts[2]
		}
		if count == 1 && valid {
			selected[file.Path()] = true
		}
	}
}

func excludedResources(sources desiredstate.Sources, documents []desiredstate.Document, base string, selected map[string]bool, ds *diagnosticSink) []string {
	excluded := []string{}
	for _, file := range sources.Files {
		if selected[file.Path()] {
			continue
		}
		identities := []string{}
		for _, document := range documents {
			if document.Path != file.Path() {
				continue
			}
			root := documentBody(document)
			kind := api.Kind(nodeText(mappingNode(root, "kind")))
			name := nodeText(mappingNode(mappingNode(root, "metadata"), "name"))
			if nodeText(mappingNode(root, "apiVersion")) == api.APIVersion && api.KindIndex(kind) >= 0 && api.ValidLexical("name", name) {
				identities = append(identities, string(kind)+"/"+name)
			}
		}
		if len(identities) == 0 {
			continue
		}
		rel, _ := filepath.Rel(base, file.Path())
		rel = filepath.ToSlash(rel)
		excluded = append(excluded, rel)
		remediation := "add the relative declaration path to Environment.spec.resources"
		if !pathWithin(file.Path(), base) {
			remediation = "relocate the declaration inside the Environment directory before adding its relative path to resources"
		}
		location := diagnostics.SourceLocation{Path: file.Path()}
		ds.add(diagnostics.Diagnostic{Severity: "warning", Code: "api.selection", Message: "resource file is excluded: " + strings.Join(sortedNames(identities), ", "), Source: &location, Remediation: remediation})
	}
	slices.Sort(excluded)
	return excluded
}

func pathWithin(path, base string) bool {
	rel, err := filepath.Rel(base, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func validateIdentities(records []*objectRecord, ds *diagnosticSink) {
	identities := map[string][]*objectRecord{}
	clusters := map[string][]*objectRecord{}
	for _, record := range records {
		identities[record.object.Identity()] = append(identities[record.object.Identity()], record)
		if record.object.Kind() == api.ContainerCluster || record.object.Kind() == api.StorageCluster {
			clusters[record.object.Name()] = append(clusters[record.object.Name()], record)
		}
	}
	for _, record := range records {
		if len(identities[record.object.Identity()]) > 1 {
			ds.issue(record, api.Issue{Code: "api.duplicate", Field: "$.metadata.name", Message: "object identity is ambiguous; every declaration must be unique within its kind"})
		}
		if record.object.Kind() == api.ContainerCluster || record.object.Kind() == api.StorageCluster {
			kinds := map[api.Kind]bool{}
			for _, collision := range clusters[record.object.Name()] {
				kinds[collision.object.Kind()] = true
			}
			if len(kinds) > 1 {
				ds.issue(record, api.Issue{Code: "api.duplicate", Field: "$.metadata.name", Message: "container and storage clusters share one name namespace"})
			}
		}
	}
	bindings := map[string]int{}
	for _, record := range records {
		if record.object.Kind() != api.ContainerCluster && record.object.Kind() != api.StorageCluster {
			continue
		}
		for _, node := range clusterNodes(record.object).Items() {
			name := node.Get("machineRef").Text()
			if name != "" {
				bindings[name]++
			}
		}
	}
	for _, record := range records {
		if record.object.Kind() != api.ContainerCluster && record.object.Kind() != api.StorageCluster {
			continue
		}
		for _, node := range clusterNodes(record.object).Items() {
			if bindings[node.Get("machineRef").Text()] > 1 {
				field := "$.spec.nodes"
				if record.object.Kind() == api.StorageCluster {
					field = "$.spec.ceph.topology.nodes"
				}
				ds.issue(record, api.Issue{Code: "api.invariant", Field: field, Message: "a Machine may be bound by only one selected cluster node"})
			}
		}
	}
}

func clusterNodes(object api.Object) api.Value {
	if object.Kind() == api.StorageCluster {
		return object.Spec().Get("ceph", "topology", "nodes")
	}
	return object.Spec().Get("nodes")
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
