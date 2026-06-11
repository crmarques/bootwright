package inventory

import (
	"time"

	"github.com/crmarques/bootwright/internal/ownership"
)

type ownershipInventoryFacts struct {
	Hosts               map[string]map[string]any
	ProviderHosts       map[string]bool
	InfraHosts          map[string]bool
	InfraComponentHosts map[string]bool
	StorageHosts        map[string]bool
}

func ownershipInventory(records []ownership.ResourceRecord) ownershipInventoryFacts {
	out := ownershipInventoryFacts{
		Hosts:               map[string]map[string]any{},
		ProviderHosts:       map[string]bool{},
		InfraHosts:          map[string]bool{},
		InfraComponentHosts: map[string]bool{},
		StorageHosts:        map[string]bool{},
	}
	for _, record := range records {
		if record.Host == "" {
			continue
		}
		out.Hosts[record.Host] = ownershipHostEntry(record)
		switch record.Kind {
		case "bmc-emulator":
			out.ProviderHosts[record.Host] = true
		case "infra-component":
			out.InfraComponentHosts[record.Host] = true
		case "storage-cluster":
			out.StorageHosts[record.Host] = true
		case "libvirt-domain", "libvirt-network", "kubevirt-machine", "vsphere-machine", "vsphere-vmedia", "managed-os-install":
			out.InfraHosts[record.Host] = true
		}
	}
	return out
}

func ownershipHostEntry(record ownership.ResourceRecord) map[string]any {
	entry := map[string]any{
		"bootwright_host_name": record.Host,
	}
	for key, value := range record.HostFacts {
		if value != "" {
			entry[key] = value
		}
	}
	if _, ok := entry["bootwright_host_name"]; !ok {
		entry["bootwright_host_name"] = record.Host
	}
	return entry
}

func ownershipRecordsVars(records []ownership.ResourceRecord) []any {
	out := make([]any, 0, len(records))
	for _, record := range records {
		out = append(out, ownershipRecordVars(record))
	}
	return out
}

func ownershipRecordsByKindVars(records []ownership.ResourceRecord) map[string]any {
	out := map[string]any{}
	for _, record := range records {
		current, _ := out[record.Kind].([]any)
		out[record.Kind] = append(current, ownershipRecordVars(record))
	}
	return out
}

func ownershipRecordVars(record ownership.ResourceRecord) map[string]any {
	out := map[string]any{
		"apiVersion": record.APIVersion,
		"kind":       record.Kind,
		"name":       record.Name,
		"owner":      record.Owner,
	}
	add := func(key, value string) {
		if value != "" {
			out[key] = value
		}
	}
	add("context", record.Context)
	add("host", record.Host)
	add("provider", record.Provider)
	add("cluster", record.Cluster)
	add("machine", record.Machine)
	if len(record.Paths) > 0 {
		out["paths"] = append([]string(nil), record.Paths...)
	}
	if len(record.HostFacts) > 0 {
		out["hostFacts"] = stringMapAny(record.HostFacts)
	}
	if len(record.Labels) > 0 {
		out["labels"] = stringMapAny(record.Labels)
	}
	if len(record.Attributes) > 0 {
		out["attributes"] = stringMapAny(record.Attributes)
	}
	if !record.UpdatedAt.IsZero() {
		out["updatedAt"] = record.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return out
}

func stringMapAny(in map[string]string) map[string]any {
	out := map[string]any{}
	for key, value := range in {
		out[key] = value
	}
	return out
}
