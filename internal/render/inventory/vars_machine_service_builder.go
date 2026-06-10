package inventory

import (
	"fmt"
	"sort"
)

type machineServiceKey struct {
	kind         string
	providerName string
	name         string
	machineRef   string
}

type machineServiceBuilder struct {
	groups map[machineServiceKey]map[string]any
	order  []machineServiceKey
}

func newMachineServiceBuilder() *machineServiceBuilder {
	return &machineServiceBuilder{groups: map[machineServiceKey]map[string]any{}}
}

func (b *machineServiceBuilder) Add(component map[string]any, clusterName string) {
	k, ok := machineServiceKeyFor(component)
	if !ok {
		return
	}
	dst, ok := b.groups[k]
	if !ok {
		dst = cloneComponentVars(component)
		delete(dst, "clusterName")
		addConsumingCluster(dst, clusterName)
		b.groups[k] = dst
		b.order = append(b.order, k)
		return
	}
	mergeMachineServiceVars(dst, component)
	addConsumingCluster(dst, clusterName)
}

func (b *machineServiceBuilder) Services() []any {
	sort.SliceStable(b.order, func(i, j int) bool {
		a, z := b.order[i], b.order[j]
		if a.machineRef != z.machineRef {
			return a.machineRef < z.machineRef
		}
		if a.kind != z.kind {
			return a.kind < z.kind
		}
		if a.providerName != z.providerName {
			return a.providerName < z.providerName
		}
		return a.name < z.name
	})
	out := make([]any, 0, len(b.order))
	for _, k := range b.order {
		out = append(out, b.groups[k])
	}
	return out
}

func machineServiceKeyFor(component map[string]any) (machineServiceKey, bool) {
	k := machineServiceKey{
		kind:         stringMapValue(component, "kind"),
		providerName: stringMapValue(component, "providerName"),
		name:         stringMapValue(component, "name"),
		machineRef:   stringMapValue(component, "machineRef"),
	}
	return k, k.kind != "" && k.providerName != "" && k.name != "" && k.machineRef != ""
}

func mergeMachineServiceVars(dst, src map[string]any) {
	for _, field := range []string{
		"machineAddress",
		"realisation",
		"bmcRole",
		"applyRole",
		"destroyRole",
		"image",
		"bindAddress",
		"port",
		"listeners",
		"endpoints",
		"url",
		"tls",
		"bmcEmulated",
		"configConsistent",
	} {
		if _, ok := dst[field]; !ok {
			if value, exists := src[field]; exists {
				dst[field] = value
			}
		}
	}
	appendUniqueMapList(dst, src, "frontends", frontendVarsKey)
	appendUniqueMapList(dst, src, "hostRecords", recordVarsKey)
	appendUniqueMapList(dst, src, "domainRecords", recordVarsKey)
	appendUniqueMapList(dst, src, "machines", machineVarsKey)
	mergeStringListField(dst, src, "additionalIngressHosts")
	mergeStringListField(dst, src, "upstreamSources")
	mergeStringListField(dst, src, "allowedNetworks")
}

func addConsumingCluster(service map[string]any, clusterName string) {
	if clusterName == "" {
		return
	}
	existing := map[string]bool{}
	var out []string
	if current, ok := service["consumingClusters"].([]string); ok {
		for _, name := range current {
			existing[name] = true
			out = append(out, name)
		}
	}
	if !existing[clusterName] {
		out = append(out, clusterName)
	}
	sort.Strings(out)
	service["consumingClusters"] = out
}

func appendUniqueMapList(dst, src map[string]any, field string, key func(map[string]any) string) {
	raw, ok := src[field].([]any)
	if !ok || len(raw) == 0 {
		return
	}
	seen := map[string]bool{}
	out := []any{}
	if current, ok := dst[field].([]any); ok {
		for _, item := range current {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}
			k := key(entry)
			seen[k] = true
			out = append(out, item)
		}
	}
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		k := key(entry)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, cloneComponentVars(entry))
	}
	if len(out) > 0 {
		dst[field] = out
	}
}

func mergeStringListField(dst, src map[string]any, field string) {
	values := append(stringListValue(dst[field]), stringListValue(src[field])...)
	if len(values) == 0 {
		return
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	dst[field] = out
}

func stringMapValue(m map[string]any, key string) string {
	value, _ := m[key].(string)
	return value
}

func stringListValue(raw any) []string {
	switch values := raw.(type) {
	case []string:
		return append([]string(nil), values...)
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if s, ok := value.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func frontendVarsKey(m map[string]any) string {
	return fmt.Sprintf("%v|%v|%v", m["clusterName"], m["name"], m["vip"])
}

func recordVarsKey(m map[string]any) string {
	return fmt.Sprintf("%v|%v", m["name"], m["address"])
}

func machineVarsKey(m map[string]any) string {
	return fmt.Sprintf("%v|%v|%v", m["clusterName"], m["providerName"], m["name"])
}
