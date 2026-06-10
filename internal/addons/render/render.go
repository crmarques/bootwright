package render

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/crmarques/bootwright/api/v1alpha1"
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

func OLMResources(extension v1alpha1.ClusterAddon) ([]ManifestResource, error) {
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
	for _, custom := range olm.CustomResources {
		out = append(out, mustResource(cloneAnyMap(custom)))
	}
	return out, nil
}

func DesiredHash(extension v1alpha1.ClusterAddon, policy v1alpha1.ClusterAddonPolicy) (string, error) {
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
	}{
		APIVersion: v1alpha1.APIVersion,
		Extension:  extension,
		Policy: desiredHashPolicy{
			ServerSideApply: policy.UseServerSideApply(),
			FieldManager:    policy.FieldManager,
			Prune:           policy.Prune,
			ContinueOnError: policy.ContinueOnError,
		},
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
			path := filepath.Join(filepath.Dir(extension.SourcePath), filepath.Clean(manifest.Path))
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

func ManifestPath(extension v1alpha1.ClusterAddon, manifest v1alpha1.ClusterAddonManifestRef) string {
	return filepath.Join(filepath.Dir(extension.SourcePath), filepath.Clean(manifest.Path))
}

func ObservedResourceID(kind, namespace, name string) string {
	if namespace == "" {
		return kind + "/" + name
	}
	return kind + "/" + namespace + "/" + name
}
