package compilation

import (
	"path/filepath"
	"slices"
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
	if !resources.Present() {
		for _, file := range sources.Files {
			selected[file.Path()] = true
		}
		return selected, []string{}
	}
	if resources.Len() == 0 {
		ds.issue(env, api.Issue{Code: "api.value", Field: "$.spec.resources", Message: "resource selection must not be empty"})
	}
	seen := map[string]bool{}
	for _, resource := range resources.Items() {
		value := resource.Text()
		clean := filepath.Clean(value)
		if value == "" || strings.TrimSpace(value) != value || filepath.IsAbs(value) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || strings.ContainsRune(value, 0) {
			ds.issue(env, api.Issue{Code: "api.value", Field: "$.spec.resources", Message: "resource path must remain inside the Environment directory"})
			continue
		}
		if seen[clean] {
			ds.issue(env, api.Issue{Code: "api.duplicate", Field: "$.spec.resources", Message: "resource paths must be unique after cleaning"})
			continue
		}
		seen[clean] = true
		candidate := filepath.Join(base, clean)
		matches := 0
		for _, file := range sources.Files {
			if file.Path() == candidate || pathWithin(file.Path(), candidate) {
				selected[file.Path()] = true
				matches++
			}
		}
		if matches == 0 {
			ds.issue(env, api.Issue{Code: "api.reference", Field: "$.spec.resources", Message: "resource path does not select an acquired desired-state file"})
		}
	}
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
		ds.add(diagnostics.Diagnostic{Severity: "warning", Code: "api.deferred", Message: "resource file is excluded: " + strings.Join(sortedNames(identities), ", "), Source: &location, Remediation: remediation})
	}
	slices.Sort(excluded)
	return selected, excluded
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
	if record.object.Kind() != api.Secret {
		return
	}
	for _, field := range record.object.Spec().Get("source", "file").Fields() {
		value := field.Value.Text()
		if value == "" {
			continue
		}
		if strings.ContainsAny(value, "\x00\r\n") || strings.TrimSpace(value) != value {
			ds.issue(record, api.Issue{Code: "api.value", Field: "$.spec.source.file." + field.Name, Message: "Secret path has invalid whitespace or characters"})
			continue
		}
		if value == "~" || strings.HasPrefix(value, "~/") {
			continue
		}
		resolved := value
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(filepath.Dir(record.path), resolved)
		}
		resolved = filepath.Clean(resolved)
		for _, root := range roots {
			if resolved != root && !pathWithin(resolved, root) {
				continue
			}
			rel, _ := filepath.Rel(root, resolved)
			parts := strings.Split(filepath.ToSlash(rel), "/")
			if !slices.Contains(parts[:max(0, len(parts)-1)], "secrets") {
				ds.issue(record, api.Issue{Code: "api.invariant", Field: "$.spec.source.file." + field.Name, Message: "a Secret payload inside an input tree must be below a secrets directory", Remediation: "use a secrets directory relative to the recipient declaration or an external payload path"})
			}
		}
	}
}
