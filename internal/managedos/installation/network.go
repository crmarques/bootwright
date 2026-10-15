package installation

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	machineref "github.com/crmarques/bootwright/internal/machine"
)

func networkFor(catalog api.Catalog, machine api.Object, install Installation) (Network, error) {
	composed, issues := machineref.ComposeNetwork(machine, catalog)
	document, mapping := nativeDocument(composed).(map[string]any)
	if len(issues) != 0 || !composed.Present() || !mapping {
		return Network{}, refusal("api.value", "the network configuration of "+machine.Identity()+" does not compose",
			"correct spec.network on "+machine.Identity())
	}
	_ = install
	return Network{
		CheckpointSeconds: networkCheckpointSeconds,
		Document:          document,
		DocumentPath:      NetworkDocumentPath,
		MarkerPath:        NetworkMarkerPath,
	}, nil
}

func nativeDocument(value api.Value) any {
	switch value.Type() {
	case api.Mapping:
		out := map[string]any{}
		for _, field := range value.Fields() {
			out[field.Name] = nativeDocument(field.Value)
		}
		return out
	case api.Sequence:
		items := value.Items()
		out := make([]any, 0, len(items))
		for _, item := range items {
			out = append(out, nativeDocument(item))
		}
		return out
	case api.Boolean:
		return value.Bool()
	case api.Integer:
		if number, ok := value.Int64(); ok {
			return number
		}
		return value.Text()
	case api.Number:
		if number, ok := value.Float64(); ok {
			return number
		}
		return value.Text()
	case api.Absent:
		return nil
	}
	return value.Text()
}
