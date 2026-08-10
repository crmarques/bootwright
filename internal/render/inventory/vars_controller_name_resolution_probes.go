package inventory

import (
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	stategraph "github.com/crmarques/bootwright/internal/state/graph"
)

func nameResolutionControllerProbes(hostRecords, domainRecords, cnameRecords []any) []any {
	targets := map[string][]string{}
	addTarget := func(name, address string) {
		if name == "" || address == "" {
			return
		}
		targets[name] = append(targets[name], address)
	}
	addAddressRecords := func(records []any) {
		for _, raw := range records {
			record, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			name, _ := record["name"].(string)
			address, _ := record["address"].(string)
			addTarget(name, address)
		}
	}
	addAddressRecords(hostRecords)
	addAddressRecords(domainRecords)
	for _, raw := range cnameRecords {
		record, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := record["name"].(string)
		target, _ := record["address"].(string)
		if name == "" {
			continue
		}
		addresses := targets[target]
		if len(addresses) == 0 {
			targets[name] = nil
			continue
		}
		targets[name] = append(targets[name], addresses...)
	}
	for _, raw := range domainRecords {
		record, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := record["name"].(string)
		address, _ := record["address"].(string)
		if name == "" || address == "" {
			continue
		}
		probeLabel := "bootwright-probe"
		probeName := probeLabel + "." + name
		for suffix := 2; ; suffix++ {
			if _, exists := targets[probeName]; !exists {
				break
			}
			probeName = fmt.Sprintf("%s-%d.%s", probeLabel, suffix, name)
		}
		addTarget(probeName, address)
	}
	grouped := map[string]map[string]bool{}
	for name, addresses := range targets {
		if len(addresses) == 0 {
			grouped[name] = map[string]bool{}
			continue
		}
		for _, address := range addresses {
			ip := net.ParseIP(strings.TrimSpace(address))
			if ip != nil {
				address = ip.String()
			}
			if grouped[name] == nil {
				grouped[name] = map[string]bool{}
			}
			grouped[name][address] = true
		}
	}
	names := make([]string, 0, len(grouped))
	for name := range grouped {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]any, 0, len(names))
	for _, name := range names {
		addresses := make([]string, 0, len(grouped[name]))
		for address := range grouped[name] {
			addresses = append(addresses, address)
		}
		sort.Strings(addresses)
		out = append(out, map[string]any{
			"name":      name,
			"addresses": stringSliceAny(addresses),
		})
	}
	return out
}

func serviceEntryNames(service stategraph.MachineService) []string {
	seen := map[string]bool{}
	var out []string
	for _, consumer := range service.Consumers {
		entryName := consumer.Fields["entryName"]
		if entryName == "" || seen[entryName] {
			continue
		}
		seen[entryName] = true
		out = append(out, entryName)
	}
	sort.Strings(out)
	return out
}

func nameResolutionRecordsForGraphService(state v1alpha1.State, service stategraph.MachineService) ([]any, []any, []any) {
	hostRecords := map[string]map[string]any{}
	domainRecords := map[string]map[string]any{}
	cnameRecords := map[string]map[string]any{}
	for _, entryName := range serviceEntryNames(service) {
		hosts, domains, cnames := nameResolutionRecordsVars(state, entryName, serviceEntryStringField(service, entryName, "additionalIngressHosts"))
		mergeRecordVars(hostRecords, hosts)
		mergeRecordVars(domainRecords, domains)
		mergeRecordVars(cnameRecords, cnames)
	}
	return sortedRecordVars(hostRecords), sortedRecordVars(domainRecords), sortedRecordVars(cnameRecords)
}

func serviceEntryStringField(service stategraph.MachineService, entryName, field string) []string {
	seen := map[string]bool{}
	var out []string
	for _, consumer := range service.Consumers {
		if consumer.Fields["entryName"] != entryName {
			continue
		}
		for _, value := range consumer.MergeStringFields[field] {
			if value == "" || seen[value] {
				continue
			}
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func mergeRecordVars(dst map[string]map[string]any, records []any) {
	for _, raw := range records {
		record, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		key := fmt.Sprintf("%v|%v", record["name"], record["address"])
		dst[key] = cloneComponentVars(record)
	}
}

func sortedRecordVars(records map[string]map[string]any) []any {
	if len(records) == 0 {
		return nil
	}
	keys := make([]string, 0, len(records))
	for key := range records {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]any, 0, len(keys))
	for _, key := range keys {
		out = append(out, records[key])
	}
	return out
}
