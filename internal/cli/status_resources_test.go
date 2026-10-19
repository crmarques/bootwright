package cli

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestStatusRendersStorageResources(t *testing.T) {
	result := &lifecycle.StatusResult{
		Context:  lifecycle.ContextIdentity{Name: "lab", Mode: "ready"},
		Clusters: []lifecycle.ClusterSummary{{Name: "sno", Kind: "ContainerCluster", Status: lifecycle.RealizationPending}},
		StorageClusters: []lifecycle.ClusterSummary{
			{Name: "ceph", Kind: "StorageCluster", Status: lifecycle.RealizationPending, Resources: []lifecycle.ResourceSummary{
				{Kind: "StoragePool", Name: "rbd", Status: lifecycle.RealizationPending},
				{Kind: "StorageClient", Name: "app", Status: lifecycle.RealizationDone},
			}},
			{Name: "archive", Kind: "StorageCluster", Status: lifecycle.RealizationPending},
		},
	}
	var encoded bytes.Buffer
	if err := writeLifecycleStatus(&encoded, result, true); err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Result struct {
			Clusters        []map[string]any `json:"clusters"`
			StorageClusters []map[string]any `json:"storageClusters"`
		} `json:"result"`
	}
	if err := json.Unmarshal(encoded.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	want := []any{
		map[string]any{"kind": "StoragePool", "name": "rbd", "status": "pending"},
		map[string]any{"kind": "StorageClient", "name": "app", "status": "done"},
	}
	storage := envelope.Result.StorageClusters
	if len(storage) != 2 || !reflect.DeepEqual(storage[0]["resources"], want) {
		t.Fatalf("storage clusters = %+v", storage)
	}
	if empty, found := storage[1]["resources"]; !found || !reflect.DeepEqual(empty, []any{}) {
		t.Fatalf("a storage cluster without rows reads resources %#v (present %t)", empty, found)
	}
	if _, found := envelope.Result.Clusters[0]["resources"]; found || len(envelope.Result.Clusters) != 1 {
		t.Fatalf("a container cluster row = %+v", envelope.Result.Clusters)
	}
	var text bytes.Buffer
	if err := writeLifecycleStatus(&text, result, false); err != nil {
		t.Fatal(err)
	}
	shown := text.String()
	section := strings.Index(shown, "Storage resources\n")
	pool := strings.Index(shown, "  [PENDING]   StoragePool/rbd     ceph\n")
	client := strings.Index(shown, "  [OK]        StorageClient/app   ceph\n")
	if section < 0 || pool < section || client < pool || section < strings.Index(shown, "Storage clusters\n") {
		t.Fatalf("status text = %q", shown)
	}
}
