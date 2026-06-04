package render

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/graph"
)

func bmcMachineServiceVarsFromGraph(state v1alpha1.State, service stategraph.MachineService) (map[string]any, bool) {
	out := map[string]any{
		"kind":             v1alpha1.ProviderServiceKindBMC,
		"providerName":     service.Identity.ProviderName,
		"name":             service.Identity.Name,
		"machineRef":       service.MachineRef,
		"machineAddress":   lookupMachineAddress(state, service.MachineRef),
		"realisation":      service.Fields["realisation"],
		"bmcRole":          service.Fields["bmcRole"],
		"applyRole":        service.Fields["applyRole"],
		"destroyRole":      service.Fields["destroyRole"],
		"machines":         []any{},
		"configConsistent": true,
	}
	configKey := ""
	for _, consumer := range service.Consumers {
		ci, ok := clusterInstallByName(state, consumer.ClusterInstall)
		if !ok {
			continue
		}
		machine, ok := findClusterMachine(ci, consumer.Fields["machineName"])
		if !ok {
			continue
		}
		bmc := machineBMCServiceConfig(state, machine)
		if bmc == nil {
			continue
		}
		if configKey == "" {
			configKey = bmcConfigKey(bmc)
			out["bmcEmulated"] = bmc
		} else if configKey != bmcConfigKey(bmc) {
			out["configConsistent"] = false
		}
		out["machines"] = append(out["machines"].([]any), map[string]any{
			"clusterName":  consumer.Cluster,
			"name":         machine.Name,
			"bmcEmulated":  bmc,
			"providerName": machine.Source.ProviderRef.Name,
		})
	}
	if len(out["machines"].([]any)) == 0 {
		return nil, false
	}
	return out, true
}

func machineBMCServiceConfig(state v1alpha1.State, machine v1alpha1.InstallMachine) map[string]any {
	if machine.Source.ProfileRef.Name == "" {
		return nil
	}
	provider, ok := findProvider(state, machine.Source.ProviderRef.Name)
	if !ok {
		return nil
	}
	if _, ok := findProfile(provider, machine.Source.ProfileRef.Name); !ok {
		return nil
	}
	if provider.Spec.Type != v1alpha1.ProvisionerLibvirt || provider.Spec.Libvirt == nil {
		return nil
	}
	return machineEmulatedBMCVars(state, provider.Spec.Libvirt)
}

func bmcConfigKey(m map[string]any) string {
	return fmt.Sprintf("%v|%v|%v|%v|%v|%v|%v",
		m["protocol"],
		m["libvirtURI"],
		m["bindAddress"],
		m["port"],
		m["vmediaPort"],
		m["credentialRef"],
		m["sushyToolsVersion"],
	)
}
