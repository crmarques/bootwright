package render

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	"github.com/crmarques/bootwright/internal/addons/hooks"
	"go.yaml.in/yaml/v3"
)

type ManifestResource struct {
	APIVersion string
	Kind       string
	Namespace  string
	Name       string
	Object     map[string]any
	Content    []byte
}

// OLMResources is the full ordered resource list for an OLM add-on: the
// shipped CatalogSource (if any), the operator-install set (Namespace,
// OperatorGroup, Subscription), then the declared custom resources. It is the
// canonical ordering that DesiredHash folds into the add-on hash, so callers
// that need the groups separately (the apply path gates on the catalog's
// READY connection before the Subscription and on the operator's CSV before
// the custom resources) use CatalogResources + OperatorResources +
// CustomResources, whose concatenation is byte-identical to this.
func OLMResources(extension v1alpha1.ClusterAddon) ([]ManifestResource, error) {
	catalog, err := CatalogResources(extension)
	if err != nil {
		return nil, err
	}
	operator, err := OperatorResources(extension)
	if err != nil {
		return nil, err
	}
	custom, err := CustomResources(extension)
	if err != nil {
		return nil, err
	}
	return append(append(catalog, operator...), custom...), nil
}

// CatalogResources returns the add-on's shipped CatalogSource (empty when the
// add-on subscribes to a catalog the cluster already serves). It lands in the
// subscription's sourceNamespace, ahead of everything else, so the catalog
// registry can start while the operator-install set applies.
func CatalogResources(extension v1alpha1.ClusterAddon) ([]ManifestResource, error) {
	if extension.Spec.OLM == nil || extension.Spec.OLM.CatalogSource == nil {
		return nil, nil
	}
	catalog := extension.Spec.OLM.CatalogSource
	spec := map[string]any{
		"sourceType": "grpc",
		"image":      catalog.Image,
	}
	if catalog.DisplayName != "" {
		spec["displayName"] = catalog.DisplayName
	}
	if catalog.Publisher != "" {
		spec["publisher"] = catalog.Publisher
	}
	if catalog.PollInterval != "" {
		spec["updateStrategy"] = map[string]any{
			"registryPoll": map[string]any{
				"interval": catalog.PollInterval,
			},
		}
	}
	return []ManifestResource{mustResource(map[string]any{
		"apiVersion": "operators.coreos.com/v1alpha1",
		"kind":       "CatalogSource",
		"metadata": map[string]any{
			"name":      catalog.Name,
			"namespace": extension.Spec.OLM.Subscription.SourceNamespace,
		},
		"spec": spec,
	})}, nil
}

// OperatorResources returns the operator-install set (Namespace if create,
// OperatorGroup if set, then the Subscription) in OLMResources order. The
// Subscription is always present.
func OperatorResources(extension v1alpha1.ClusterAddon) ([]ManifestResource, error) {
	if extension.Spec.OLM == nil {
		return nil, nil
	}
	olm := extension.Spec.OLM
	namespace := olm.Namespace.Name
	var out []ManifestResource
	if olm.Namespace.Create {
		object := map[string]any{
			"apiVersion": "v1",
			"kind":       "Namespace",
			"metadata": map[string]any{
				"name": namespace,
			},
		}
		if len(olm.Namespace.Labels) > 0 {
			object["metadata"].(map[string]any)["labels"] = stringMapAny(olm.Namespace.Labels)
		}
		out = append(out, mustResource(object))
	}
	if group := olm.OperatorGroup; group != nil {
		object := map[string]any{
			"apiVersion": "operators.coreos.com/v1",
			"kind":       "OperatorGroup",
			"metadata": map[string]any{
				"name":      group.Name,
				"namespace": namespace,
			},
			"spec": map[string]any{},
		}
		if len(group.TargetNamespaces) > 0 {
			object["spec"].(map[string]any)["targetNamespaces"] = append([]string(nil), group.TargetNamespaces...)
		}
		out = append(out, mustResource(object))
	}
	sub := olm.Subscription
	subSpec := map[string]any{
		"name":                sub.Package,
		"channel":             sub.Channel,
		"source":              sub.Source,
		"sourceNamespace":     sub.SourceNamespace,
		"installPlanApproval": sub.InstallPlanApproval,
	}
	if sub.StartingCSV != "" {
		subSpec["startingCSV"] = sub.StartingCSV
	}
	out = append(out, mustResource(map[string]any{
		"apiVersion": "operators.coreos.com/v1alpha1",
		"kind":       "Subscription",
		"metadata": map[string]any{
			"name":      sub.Name,
			"namespace": namespace,
		},
		"spec": subSpec,
	}))
	return out, nil
}

// CustomResources returns only the spec.olm.customResources, in declared order.
func CustomResources(extension v1alpha1.ClusterAddon) ([]ManifestResource, error) {
	if extension.Spec.OLM == nil {
		return nil, nil
	}
	out := make([]ManifestResource, 0, len(extension.Spec.OLM.CustomResources))
	for _, custom := range extension.Spec.OLM.CustomResources {
		out = append(out, mustResource(cloneAnyMap(custom)))
	}
	return out, nil
}

func DesiredHash(extension v1alpha1.ClusterAddon, policy addons.ClusterAddonPolicy) (string, error) {
	type manifestFile struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	payload := struct {
		APIVersion string                `json:"apiVersion"`
		Extension  v1alpha1.ClusterAddon `json:"extension"`
		Policy     desiredHashPolicy     `json:"policy"`
		Resources  []map[string]any      `json:"resources,omitempty"`
		Manifests  []manifestFile        `json:"manifests,omitempty"`
		HookDigest string                `json:"hookDigest,omitempty"`
	}{
		APIVersion: v1alpha1.APIVersion,
		Extension:  extension,
		Policy: desiredHashPolicy{
			ServerSideApply: policy.UseServerSideApply(),
			FieldManager:    policy.FieldManager,
			Prune:           policy.Prune,
			ContinueOnError: policy.ContinueOnError,
		},
		// The Extension field already serializes the hook specs; HookDigest folds
		// in the shipped content (playbook, vendored trees, manifest templates) so
		// editing a shipped playbook without touching the add-on YAML still moves
		// the desired hash and re-runs the add-on.
		HookDigest: hookContentDigest(extension),
	}
	switch extension.Spec.Type {
	case v1alpha1.ClusterAddonTypeOLM:
		resources, err := OLMResources(extension)
		if err != nil {
			return "", err
		}
		for _, resource := range resources {
			payload.Resources = append(payload.Resources, resource.Object)
		}
	case v1alpha1.ClusterAddonTypeManifestSet:
		for _, manifest := range extension.Spec.ManifestSet.Manifests {
			path := ManifestPath(extension, manifest)
			data, err := os.ReadFile(path)
			if err != nil {
				return "", fmt.Errorf("read ClusterAddon/%s manifest %s: %w", extension.Metadata.Name, manifest.Path, err)
			}
			payload.Manifests = append(payload.Manifests, manifestFile{Path: manifest.Path, Content: string(data)})
		}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode ClusterAddon/%s desired hash input: %w", extension.Metadata.Name, err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

type desiredHashPolicy struct {
	ServerSideApply bool   `json:"serverSideApply"`
	FieldManager    string `json:"fieldManager,omitempty"`
	Prune           bool   `json:"prune,omitempty"`
	ContinueOnError bool   `json:"continueOnError,omitempty"`
}

func mustResource(object map[string]any) ManifestResource {
	data, err := yaml.Marshal(object)
	if err != nil {
		panic(err)
	}
	metadata, _ := object["metadata"].(map[string]any)
	return ManifestResource{
		APIVersion: stringValue(object["apiVersion"]),
		Kind:       stringValue(object["kind"]),
		Namespace:  stringValue(metadata["namespace"]),
		Name:       stringValue(metadata["name"]),
		Object:     object,
		Content:    data,
	}
}

func stringMapAny(in map[string]string) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneAnyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		switch typed := v.(type) {
		case map[string]any:
			out[k] = cloneAnyMap(typed)
		case []any:
			values := make([]any, len(typed))
			copy(values, typed)
			out[k] = values
		default:
			out[k] = v
		}
	}
	return out
}

func stringValue(value any) string {
	s, _ := value.(string)
	return s
}

// hookContentDigest folds every hook's shipped-content digest into one value for
// the add-on desired hash, so a change to any hook's playbook, vendored tree, or
// manifest template re-runs the add-on. Order follows spec.hooks.
func hookContentDigest(extension v1alpha1.ClusterAddon) string {
	if len(extension.Spec.Hooks) == 0 {
		return ""
	}
	sum := sha256.New()
	for _, hook := range extension.Spec.Hooks {
		sum.Write([]byte(hook.Name))
		sum.Write([]byte{0})
		sum.Write([]byte(hooks.ContentDigest(extension.SourcePath, hook)))
		sum.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(sum.Sum(nil))
}

func ManifestPath(extension v1alpha1.ClusterAddon, manifest v1alpha1.ClusterAddonManifestRef) string {
	return filepath.Join(filepath.Dir(extension.SourcePath), filepath.Clean(manifest.Path))
}

func ObservedResourceID(kind, namespace, name string) string {
	if namespace == "" {
		return kind + "/" + name
	}
	return kind + "/" + namespace + "/" + name
}
