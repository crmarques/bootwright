package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"testing"
)

func TestStructuredClusterInfoYAMLEqualsJSON(t *testing.T) {
	documents := map[string]map[string]any{}
	for _, format := range []string{"json", "yaml"} {
		var out bytes.Buffer
		if err := writeClusterInfo(WithPresentation(&out, PresentationOptions{Format: format}), io.Discard, "cluster info", storageOnly(readyCeph()), true); err != nil {
			t.Fatal(err)
		}
		if format == "yaml" {
			documents[format] = structuredDocument(t, out.String())
			continue
		}
		var document map[string]any
		if err := json.Unmarshal(out.Bytes(), &document); err != nil {
			t.Fatalf("%v: %s", err, out.String())
		}
		documents[format] = document
	}
	fromJSON, fromYAML := documents["json"]["result"], documents["yaml"]["result"]
	if !reflect.DeepEqual(fromJSON, fromYAML) {
		t.Fatalf("JSON result %#v differs from YAML result %#v", fromJSON, fromYAML)
	}
	storage, _ := fromYAML.(map[string]any)["storage"].([]any)
	if len(storage) != 1 {
		t.Fatalf("storage = %#v", fromYAML)
	}
	resources, _ := storage[0].(map[string]any)["resources"].([]any)
	want := []string{"replicated size 2/min 2, application rbd", "client.rbd-canary read-write on pool rbd-data", "zone lab/lab/lab-a"}
	if len(resources) != len(want) {
		t.Fatalf("resources = %#v", resources)
	}
	for index, row := range resources {
		if scope := row.(map[string]any)["scope"]; scope != want[index] {
			t.Fatalf("resource %d scope = %#v, want %q", index, scope, want[index])
		}
	}
	endpoints, _ := storage[0].(map[string]any)["endpoints"].([]any)
	services := []string{}
	for _, row := range endpoints {
		services = append(services, row.(map[string]any)["service"].(string))
	}
	if !reflect.DeepEqual(services, []string{"mgmt-gateway", "nfs.files", "rgw.objects"}) {
		t.Fatalf("endpoint services = %v", services)
	}
	for _, format := range []string{"json", "yaml"} {
		var out bytes.Buffer
		if err := writeClusterInfo(WithPresentation(&out, PresentationOptions{Format: format}), io.Discard, "cluster info", storageOnly(unavailableCeph()), true); err != nil {
			t.Fatal(err)
		}
		var document map[string]any
		if format == "yaml" {
			document = structuredDocument(t, out.String())
		} else if err := json.Unmarshal(out.Bytes(), &document); err != nil {
			t.Fatal(err)
		}
		entry := document["result"].(map[string]any)["storage"].([]any)[0].(map[string]any)
		if rows, present := entry["endpoints"].([]any); !present || len(rows) != 0 {
			t.Fatalf("%s: an unavailable entry's endpoints = %#v", format, entry["endpoints"])
		}
	}
}
