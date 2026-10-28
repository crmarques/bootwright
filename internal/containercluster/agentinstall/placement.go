package agentinstall

import (
	"encoding/json"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const maxPlacementNotes = 8

func (InstallCapability) ProjectedPaths() []lifecycle.ProjectedPath {
	return []lifecycle.ProjectedPath{
		{Kind: api.OpenShiftCluster, Path: "spec.nodes[].name"},
		{Kind: api.OpenShiftCluster, Path: "spec.nodes[].labels"},
		{Kind: api.OpenShiftCluster, Path: "spec.nodes[].taints"},
	}
}

// Project renders the authored labels and taints each node was read carrying,
// so drift shows as a difference; a missing infra role label is a note.
func (InstallCapability) Project(applied api.Catalog, block reconciliation.Block, evidence json.RawMessage) ([]lifecycle.Projection, error) {
	request, err := DecodeInstallRequest(block.Request)
	if err != nil {
		return nil, err
	}
	observed, err := decodeInstallEvidence(evidence, block.RequestDigest)
	if err != nil {
		return nil, err
	}
	if observed.Identity == "" || observed.Cluster != observed.Identity {
		return nil, refusal("lifecycle.state", "the installation evidence does not prove this operation's cluster", "")
	}
	cluster, found := applied.Find(api.OpenShiftCluster, request.Identity.Cluster)
	if !found {
		return nil, refusal("lifecycle.state", "the applied revision does not declare OpenShiftCluster/"+request.Identity.Cluster, "")
	}
	var items []api.Value
	var notes []string
	for _, node := range cluster.Spec().Get("nodes").Items() {
		name := node.Get("name").Text()
		index := slices.IndexFunc(observed.Placement, func(seen NodePlacement) bool { return seen.Name == name })
		seen := NodePlacement{Labels: map[string]string{}}
		if index >= 0 {
			seen = observed.Placement[index]
		}
		items = append(items, projectedNode(node, seen))
		if declared := slices.IndexFunc(request.Nodes, func(frozen Node) bool { return frozen.Name == name }); declared >= 0 && placed(request.Nodes[declared]) {
			notes = append(notes, placementNotes(request.Nodes[declared], seen, index >= 0)...)
		}
	}
	if len(notes) > maxPlacementNotes {
		notes = append(notes[:maxPlacementNotes-1], "further nodes lack part of their placement")
	}
	return []lifecycle.Projection{{
		Kind: api.OpenShiftCluster, Name: request.Identity.Cluster, Notes: notes,
		Observed: api.MapValue(api.FieldValue{Name: "nodes", Value: api.ListValue(items...)}),
	}}, nil
}

func projectedNode(node api.Value, seen NodePlacement) api.Value {
	fields := []api.FieldValue{{Name: "name", Value: node.Get("name")}}
	if authored := node.Get("labels").Fields(); len(authored) != 0 {
		var labels []api.FieldValue
		for _, label := range authored {
			if value, found := seen.Labels[label.Name]; found {
				labels = append(labels, api.FieldValue{Name: label.Name, Value: api.StringValue(value)})
			}
		}
		fields = append(fields, api.FieldValue{Name: "labels", Value: api.MapValue(labels...)})
	}
	if authored := node.Get("taints").Items(); len(authored) != 0 {
		keys := make([]string, 0, len(authored))
		for _, taint := range authored {
			keys = append(keys, taint.Get("key").Text())
		}
		var taints []api.Value
		for _, taint := range seen.Taints {
			if !slices.Contains(keys, taint.Key) {
				continue
			}
			entry := []api.FieldValue{{Name: "key", Value: api.StringValue(taint.Key)}}
			if taint.Value != "" {
				entry = append(entry, api.FieldValue{Name: "value", Value: api.StringValue(taint.Value)})
			}
			taints = append(taints, api.MapValue(append(entry, api.FieldValue{Name: "effect", Value: api.StringValue(taint.Effect)})...))
		}
		fields = append(fields, api.FieldValue{Name: "taints", Value: api.ListValue(taints...)})
	}
	return api.MapValue(fields...)
}

func placementNotes(node Node, seen NodePlacement, read bool) []string {
	if !read {
		return []string{"node " + node.Name + ": its placement was not read"}
	}
	var lacking []string
	for key, value := range node.Labels {
		if observed, found := seen.Labels[key]; !found || observed != value {
			lacking = append(lacking, "label "+key)
		}
	}
	slices.Sort(lacking)
	for _, taint := range node.Taints {
		if !slices.Contains(seen.Taints, taint) {
			lacking = append(lacking, "taint "+taint.Key+":"+taint.Effect)
		}
	}
	if len(lacking) == 0 {
		return nil
	}
	note := "node " + node.Name + " lacks " + strings.Join(lacking, ", ")
	if len(note) > 200 {
		note = note[:197] + "..."
	}
	return []string{note}
}
