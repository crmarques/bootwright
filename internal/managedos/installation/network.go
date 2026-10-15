package installation

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/canonicaljson"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/substrate"
)

const composedNetworkBytes = 32 << 10

func networkFor(catalog api.Catalog, machine api.Object, install Installation) (Network, error) {
	composed, issues := machineref.ComposeNetwork(machine, catalog)
	if len(issues) != 0 || !composed.Present() || composed.Type() != api.Mapping {
		return Network{}, refusal("api.value", "the network configuration of "+machine.Identity()+" does not compose",
			"correct spec.network on "+machine.Identity())
	}
	template, err := substrate.NetworkTemplate(catalog, machine)
	if err != nil {
		return Network{}, err
	}
	composed = withRoutePurge(withResolvers(withAuthoredMACs(composed, template), install.Nameservers))
	document, err := boundedDocument(machine, composed)
	if err != nil {
		return Network{}, err
	}
	return Network{
		CheckpointSeconds: networkCheckpointSeconds,
		Document:          document,
		DocumentPath:      NetworkDocumentPath,
		MarkerPath:        NetworkMarkerPath,
	}, nil
}

func withAuthoredMACs(composed, template api.Value) api.Value {
	if !composed.Has("interfaces") {
		return composed
	}
	authored := map[string]api.Value{}
	for _, iface := range template.Get("interfaces").Items() {
		authored[iface.Get("name").Text()] = iface.Get("mac-address")
	}
	interfaces := composed.Get("interfaces").Items()
	for index, iface := range interfaces {
		if !iface.Has("mac-address") {
			continue
		}
		if mac := authored[iface.Get("name").Text()]; mac.Present() {
			interfaces[index] = iface.With("mac-address", mac)
		} else {
			interfaces[index] = iface.Without("mac-address")
		}
	}
	return composed.With("interfaces", api.ListValue(interfaces...))
}

func withResolvers(composed api.Value, nameservers []string) api.Value {
	if len(nameservers) == 0 {
		return composed
	}
	return composed.WithPath(api.StringList(nameservers...), "dns-resolver", "config", "server")
}

func withRoutePurge(composed api.Value) api.Value {
	var entries []api.Value
	for _, iface := range composed.Get("interfaces").Items() {
		if state := iface.Get("state").Text(); state == "absent" || state == "ignore" {
			continue
		}
		entries = append(entries, api.MapValue(
			api.FieldValue{Name: "next-hop-interface", Value: iface.Get("name")},
			api.FieldValue{Name: "state", Value: api.StringValue("absent")},
		))
	}
	entries = append(entries, composed.Get("routes", "config").Items()...)
	if len(entries) == 0 {
		return composed
	}
	return composed.WithPath(api.ListValue(entries...), "routes", "config")
}

func boundedDocument(machine api.Object, composed api.Value) (map[string]any, error) {
	document, _ := nativeDocument(composed).(map[string]any)
	encoded, err := canonicaljson.Encode(document, canonicaljson.Bare)
	if err == nil && len(encoded) > composedNetworkBytes {
		return nil, refusal("api.value", "the composed network of "+machine.Identity()+" exceeds 32 KiB, which its installation does not carry",
			"reduce the network configuration of "+machine.Identity())
	}
	var thawed map[string]any
	if err == nil {
		err = canonicaljson.Decode(encoded, &thawed, canonicaljson.Bare)
	}
	if err != nil {
		return nil, refusal("api.value", "the composed network of "+machine.Identity()+" holds a number its frozen installation cannot carry exactly",
			"write each number in the network of "+machine.Identity()+" as an integer of at most 15 digits, or as text")
	}
	return document, nil
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
