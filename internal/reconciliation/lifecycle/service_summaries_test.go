package lifecycle

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// Status summarizes the managed infrastructure services alone, by the kinds
// infrastructure services names: an external service and an object of any
// other kind are left out whatever they author.
func TestStatusSummarizesOnlyManagedServices(t *testing.T) {
	object := func(kind api.Kind, name string, fields ...string) api.Object {
		spec := []api.FieldValue{}
		for index := 0; index+1 < len(fields); index += 2 {
			spec = append(spec, api.FieldValue{Name: fields[index], Value: api.StringValue(fields[index+1])})
		}
		return api.NewObject(kind, name, api.Value{}, api.MapValue(spec...))
	}
	catalog := api.NewCatalog([]api.Object{
		object(api.Proxy, "egress", "management", "managed", "machineRef", "services"),
		object(api.DNSServer, "names", "management", "external"),
		object(api.ArtifactServer, "media", "management", "managed", "machineRef", "controller", "retention", "install-only"),
		object(api.NTPServer, "time", "management", "managed", "machineRef", "services"),
		object(api.Machine, "services", "management", "managed"),
	})
	got := serviceSummaries(catalog, []string{string(api.Proxy), string(api.ArtifactServer)})
	want := []ServiceSummary{
		{Kind: "ArtifactServer", Name: "media", Machine: "controller", Status: "unsupported"},
		{Kind: "NTPServer", Name: "time", Machine: "services", Status: "unsupported"},
		{Kind: "Proxy", Name: "egress", Machine: "services", Status: "pending"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("service summaries = %+v, want %+v", got, want)
	}
}
