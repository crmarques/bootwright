package cli

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/ownership"
)

func bmcEmulatorDestroyConsequence(state v1alpha1.State, sel clusteraccess.Selection, runScope converge.Scope, artifactServerOnly, evidenceDegraded bool, records []ownership.ResourceRecord) ([]converge.InfraComponentServiceRef, []ownership.ResourceRecord) {
	if artifactServerOnly || sel.MachineSelection || !converge.ScopeTearsMachineLayer(runScope) {
		return nil, nil
	}
	refs := selectedBMCEmulatorServiceRefs(sel.RenderState, nil)
	var selected []ownership.ResourceRecord
	for _, record := range records {
		if strings.TrimSpace(record.Kind) != string(ownership.KindBMCEmulator) {
			continue
		}
		if evidenceDegraded || sel.Active {
			matched := false
			for _, ref := range refs {
				if strings.TrimSpace(record.Name) == strings.TrimSpace(ref.Name) && strings.TrimSpace(record.Host) == strings.TrimSpace(ref.Host) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		selected = append(selected, record)
	}
	return refs, selected
}

func bmcScanRefs(refs []converge.InfraComponentServiceRef, records []ownership.ResourceRecord) []converge.InfraComponentServiceRef {
	out := append([]converge.InfraComponentServiceRef{}, refs...)
	for _, record := range records {
		out = append(out, converge.InfraComponentServiceRef{Kind: record.Kind, Name: record.Name, Host: record.Host})
	}
	return out
}

func infraComponentDestroyConsequence(state v1alpha1.State, sel clusteraccess.Selection, runScope converge.Scope, artifactServerOnly, evidenceDegraded bool, records []ownership.ResourceRecord) ([]converge.InfraComponentServiceRef, []ownership.ResourceRecord) {
	if artifactServerOnly {
		refs := selectedInfraComponentServiceRefs(state, true, false, nil)
		recordRefs := destroyRecordScopeRefs(refs, evidenceDegraded)
		return refs, filterInfraComponentRecords(records, nil, recordRefs, true)
	}
	if !converge.ScopeTearsMachineLayer(runScope) {
		return nil, nil
	}
	if sel.MachineSelection {
		return nil, nil
	}
	refs := selectedInfraComponentServiceRefs(sel.RenderState, false, false, nil)
	if !sel.Active {
		recordRefs := destroyRecordScopeRefs(refs, evidenceDegraded)
		selectedRecords := filterInfraComponentRecords(records, nil, recordRefs, false)
		selectedRecords = append(selectedRecords, filterControllerResolverRecords(records, nil, recordRefs)...)
		return refs, selectedRecords
	}
	selectedRecords := filterInfraComponentRecords(records, nil, refs, false)
	selectedRecords = append(selectedRecords, filterControllerResolverRecords(records, nil, refs)...)
	return refs, selectedRecords
}

func destroyRecordScopeRefs(refs []converge.InfraComponentServiceRef, evidenceDegraded bool) []converge.InfraComponentServiceRef {
	if !evidenceDegraded {
		return nil
	}
	return append([]converge.InfraComponentServiceRef{}, refs...)
}

func filterInfraComponentRecords(records []ownership.ResourceRecord, hosts map[string]bool, refs []converge.InfraComponentServiceRef, artifactServerOnly bool) []ownership.ResourceRecord {
	type identity struct {
		name string
		host string
	}
	wanted := map[identity]bool{}
	for _, ref := range refs {
		wanted[identity{name: strings.TrimSpace(ref.Name), host: strings.TrimSpace(ref.Host)}] = true
	}
	var out []ownership.ResourceRecord
	for _, record := range records {
		if strings.TrimSpace(record.Kind) != "infra-component" {
			continue
		}
		if artifactServerOnly && strings.TrimSpace(record.Labels["bootwright.kind"]) != "artifacts" {
			continue
		}
		if hosts != nil && !hosts[strings.TrimSpace(record.Host)] {
			continue
		}
		if refs != nil && !wanted[identity{name: strings.TrimSpace(record.Name), host: strings.TrimSpace(record.Host)}] {
			continue
		}
		out = append(out, record)
	}
	return out
}

func filterInfraComponentTransitionRecords(records []ownership.ResourceRecord, refs []converge.InfraComponentServiceRef, artifactServerOnly, selectAll bool) []ownership.ResourceRecord {
	wanted := map[string]bool{}
	for _, ref := range refs {
		wanted[strings.TrimSpace(ref.Name)] = true
	}
	var out []ownership.ResourceRecord
	for _, record := range records {
		if strings.TrimSpace(record.Kind) != string(ownership.KindInfraComponentTransition) {
			continue
		}
		if artifactServerOnly && strings.TrimSpace(record.Labels["bootwright.kind"]) != "artifacts" {
			continue
		}
		if !selectAll && !wanted[strings.TrimSpace(record.Name)] {
			continue
		}
		out = append(out, record)
	}
	return out
}

func filterControllerResolverRecords(records []ownership.ResourceRecord, hosts map[string]bool, refs []converge.InfraComponentServiceRef) []ownership.ResourceRecord {
	wanted := map[string]bool{}
	for _, ref := range refs {
		if strings.TrimSpace(ref.Kind) == v1alpha1.ComponentSlotNameResolution {
			wanted[strings.TrimSpace(ref.Name)] = true
		}
	}
	var out []ownership.ResourceRecord
	for _, record := range records {
		if strings.TrimSpace(record.Kind) != string(ownership.KindControllerNameResolver) {
			continue
		}
		if hosts != nil && !hosts[strings.TrimSpace(record.Attributes["machineRef"])] {
			continue
		}
		identity := strings.TrimSpace(record.Provider) + "-" + strings.TrimSpace(record.Attributes["component"])
		if refs != nil && !wanted[identity] {
			continue
		}
		out = append(out, record)
	}
	return out
}

func controllerResolverDestroySelected(refs []converge.InfraComponentServiceRef, records []ownership.ResourceRecord) bool {
	for _, ref := range refs {
		if strings.TrimSpace(ref.Kind) == v1alpha1.ComponentSlotNameResolution {
			return true
		}
	}
	for _, record := range records {
		if strings.TrimSpace(record.Kind) == string(ownership.KindControllerNameResolver) {
			return true
		}
	}
	return false
}
