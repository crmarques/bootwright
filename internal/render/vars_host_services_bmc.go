package render

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/graph"
)

func bmcHostServiceVarsFromGraph(state v1alpha1.State, service stategraph.HostService) (map[string]any, bool) {
	out := map[string]any{
		"kind":             v1alpha1.ProviderServiceKindBMC,
		"providerName":     service.Identity.ProviderName,
		"name":             service.Identity.Name,
		"hostRef":          service.HostRef,
		"hostAddress":      lookupHostAddress(state, service.HostRef),
		"realisation":      service.Fields["realisation"],
		"bmcRole":          service.Fields["bmcRole"],
		"applyRole":        service.Fields["applyRole"],
		"destroyRole":      service.Fields["destroyRole"],
		"machines":         []any{},
		"configConsistent": true,
	}
	configKey := ""
	for _, consumer := range service.Consumers {
		ci, ok := clusterInfraByName(state, consumer.ClusterInfra)
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
			"providerName": machine.From.Provider,
		})
	}
	if len(out["machines"].([]any)) == 0 {
		return nil, false
	}
	return out, true
}

func machineBMCServiceConfig(state v1alpha1.State, machine v1alpha1.ClusterMachineComponent) map[string]any {
	if machine.From.Profile == "" {
		return nil
	}
	provider, ok := findProvider(state, machine.From.Provider)
	if !ok {
		return nil
	}
	profile, ok := findProfile(provider, machine.From.Profile)
	if !ok {
		return nil
	}
	return machineEmulatedBMCVars(state, profile)
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
