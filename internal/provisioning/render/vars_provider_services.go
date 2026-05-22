package render

import (
	"fmt"
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

const providerServiceKindBMC = "bmc"

func providerServicesVars(state v1alpha1.State) []any {
	builder := newProviderServiceBuilder()
	for _, ocp := range state.ContainerClusters {
		ci, err := clusterInfraForOCP(state, ocp)
		if err != nil {
			continue
		}
		for _, raw := range componentsVars(state, ci, ocp) {
			component, ok := raw.(map[string]any)
			if !ok || component["kind"] == v1alpha1.ComponentSlotMachines {
				continue
			}
			if component["hostRef"] == nil || component["applyRole"] == nil {
				continue
			}
			builder.Add(component, ocp.Metadata.Name)
		}
	}
	for _, raw := range bmcProviderServiceVars(state) {
		if component, ok := raw.(map[string]any); ok {
			builder.Add(component, "")
		}
	}
	return builder.Services()
}

func providerHostSetupsVars(state v1alpha1.State) []any {
	type key struct {
		host string
		role string
	}
	seen := map[key]bool{}
	var keys []key
	for _, ci := range state.ClusterInfras {
		for _, machine := range ci.Spec.Components.Machines {
			hostRef := machineHostRef(state, machine)
			if hostRef == "" {
				continue
			}
			driver := ProviderDriver(state, machine)
			for _, role := range driver.Roles.HostSetupRoles {
				k := key{host: hostRef, role: role}
				if seen[k] {
					continue
				}
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].host != keys[j].host {
			return keys[i].host < keys[j].host
		}
		return keys[i].role < keys[j].role
	})
	out := make([]any, 0, len(keys))
	for _, k := range keys {
		out = append(out, map[string]any{
			"hostRef":     k.host,
			"hostAddress": lookupHostAddress(state, k.host),
			"applyRole":   k.role,
		})
	}
	return out
}

type bmcProviderServiceGroup struct {
	key              bmcProviderServiceKey
	component        map[string]any
	configKey        string
	configConsistent bool
}

type bmcProviderServiceKey struct {
	providerName string
	hostRef      string
	applyRole    string
}

func bmcProviderServiceVars(state v1alpha1.State) []any {
	groups := map[bmcProviderServiceKey]*bmcProviderServiceGroup{}
	var order []bmcProviderServiceKey
	for _, ocp := range state.ContainerClusters {
		ci, err := clusterInfraForOCP(state, ocp)
		if err != nil {
			continue
		}
		for _, machine := range ci.Spec.Components.Machines {
			driver := ProviderDriver(state, machine)
			if driver.Dispatch.BMCRole == "none" || driver.Roles.BMCApplyRole == "" {
				continue
			}
			hostRef := machineHostRef(state, machine)
			if hostRef == "" {
				continue
			}
			bmc := machineBMCServiceConfig(state, machine)
			if bmc == nil {
				continue
			}
			k := bmcProviderServiceKey{
				providerName: machine.From.Provider,
				hostRef:      hostRef,
				applyRole:    driver.Roles.BMCApplyRole,
			}
			g, ok := groups[k]
			if !ok {
				g = &bmcProviderServiceGroup{
					key:       k,
					configKey: bmcConfigKey(bmc),
					component: map[string]any{
						"kind":             providerServiceKindBMC,
						"providerName":     machine.From.Provider,
						"name":             driver.Dispatch.BMCRole,
						"hostRef":          hostRef,
						"hostAddress":      lookupHostAddress(state, hostRef),
						"realisation":      driver.Dispatch.BMCRole,
						"bmcRole":          driver.Dispatch.BMCRole,
						"applyRole":        driver.Roles.BMCApplyRole,
						"destroyRole":      driver.Roles.BMCDestroyRole,
						"bmcEmulated":      bmc,
						"machines":         []any{},
						"configConsistent": true,
					},
					configConsistent: true,
				}
				groups[k] = g
				order = append(order, k)
			}
			if g.configKey != bmcConfigKey(bmc) {
				g.configConsistent = false
				g.component["configConsistent"] = false
			}
			g.component["machines"] = append(g.component["machines"].([]any), map[string]any{
				"clusterName":  ocp.Metadata.Name,
				"name":         machine.Name,
				"bmcEmulated":  bmc,
				"providerName": machine.From.Provider,
			})
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		if order[i].hostRef != order[j].hostRef {
			return order[i].hostRef < order[j].hostRef
		}
		if order[i].providerName != order[j].providerName {
			return order[i].providerName < order[j].providerName
		}
		return order[i].applyRole < order[j].applyRole
	})
	out := make([]any, 0, len(order))
	for _, k := range order {
		out = append(out, groups[k].component)
	}
	return out
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

func cloneComponentVars(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
