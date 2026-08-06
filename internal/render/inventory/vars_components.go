package inventory

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/artifacts"
	"github.com/crmarques/bootwright/internal/infra/proxy"
	"github.com/crmarques/bootwright/internal/render/installer"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

func componentsVars(state v1alpha1.State, ci v1alpha1.ClusterInstall, ocp v1alpha1.ContainerCluster, paths PathOptions) []any {
	var out []any
	clusterName := ocp.Metadata.Name

	for _, m := range ci.Machines {
		out = append(out, machineComponentVars(state, ci, m, clusterName, paths))
	}
	for _, component := range loadBalancerComponentsForCluster(state, ci, ocp) {
		lb := loadBalancerComponentVars(state, component)
		lb["clusterName"] = clusterName
		lb["frontends"] = loadBalancerFrontends(state, ci, component.Metadata.Name, clusterName, ci.Machines, stateview.ClusterNodesForInstall(state, ci))
		out = append(out, lb)
	}
	for _, server := range artifactServersForContainerCluster(state, ci, ocp) {
		out = append(out, artifactServerComponentVars(state, ci, server))
	}
	for _, selected := range proxyComponentsForCluster(state) {
		out = append(out, proxyComponentVars(state, selected.entry, selected.component))
	}
	for _, selected := range nameResolutionComponentsForCluster(state, ci) {
		out = append(out, nameResolutionComponentVars(state, selected.entry, selected.component))
	}
	for _, selected := range ntpComponentsForCluster(state) {
		out = append(out, ntpComponentVars(state, selected.entry, selected.component))
	}
	if selected, ok := registryComponentForCluster(state, ocp); ok {
		out = append(out, registryComponentVars(state, selected.entry, selected.component))
	}
	return out
}

func artifactServersForContainerCluster(state v1alpha1.State, ci v1alpha1.ClusterInstall, ocp v1alpha1.ContainerCluster) []artifacts.Server {
	seen := map[string]bool{}
	var out []artifacts.Server
	add := func(ref v1alpha1.ArtifactServerEndpointRef) {
		server, _, ok := artifacts.ResolveEndpointRef(state, ref)
		if !ok || server.Config == nil || seen[server.Component.Metadata.Name] {
			return
		}
		seen[server.Component.Metadata.Name] = true
		out = append(out, server)
	}
	if artifacts.ClusterUsesBareMetalMachine(state, ci) {
		add(ci.Agent.RedfishVirtualMedia.ArtifactServerEndpoint)
	}
	if v1alpha1.InstallMode(ocp) == v1alpha1.InstallModeDisconnected {
		add(ci.Agent.BootArtifacts.ArtifactServerEndpoint)
	}
	return out
}

type selectedProxyComponent struct {
	entry     v1alpha1.EnvironmentProxyComponent
	component v1alpha1.InfraComponent
}

type selectedNameResolutionComponent struct {
	entry     v1alpha1.EnvironmentNameResolutionComponent
	component v1alpha1.InfraComponent
}

type selectedNTPComponent struct {
	entry     v1alpha1.EnvironmentNTPComponent
	component v1alpha1.InfraComponent
}

type selectedRegistryComponent struct {
	entry     v1alpha1.EnvironmentRegistryComponent
	component v1alpha1.InfraComponent
}

func ContainerClusterLoadBalancerHosts(state v1alpha1.State, clusterName string) []string {
	for _, cluster := range state.ContainerClusters {
		if cluster.Metadata.Name != clusterName {
			continue
		}
		install, ok := stateview.ClusterInstallForContainerCluster(state, cluster)
		if !ok {
			return nil
		}
		out := []string(nil)
		for _, component := range loadBalancerComponentsForCluster(state, install, cluster) {
			if host := component.Spec.LoadBalancer.MachineRef.Name; host != "" {
				out = append(out, host)
			}
		}
		return out
	}
	return nil
}

func loadBalancerComponentsForCluster(state v1alpha1.State, ci v1alpha1.ClusterInstall, ocp v1alpha1.ContainerCluster) []v1alpha1.InfraComponent {
	seen := map[string]bool{}
	out := []v1alpha1.InfraComponent{}
	for _, role := range installer.StandardEndpointNames {
		endpoint, ok := stateview.ContainerEndpoint(ci, ocp, role)
		if !ok || endpoint.Source.Type != v1alpha1.EndpointSourceInfraComponent || endpoint.Source.ComponentRef.Name == "" {
			continue
		}
		name := endpoint.Source.ComponentRef.Name
		if seen[name] {
			continue
		}
		seen[name] = true
		component, ok := stateview.InfraComponent(state, name)
		if ok && component.Spec.LoadBalancer != nil {
			out = append(out, component)
		}
	}
	return out
}

func proxyComponentsForCluster(state v1alpha1.State) []selectedProxyComponent {
	env := stateview.Environment(state)
	if env == nil {
		return nil
	}
	seen := map[string]bool{}
	out := []selectedProxyComponent{}
	for _, name := range []string{env.Spec.ProxyNameFor(v1alpha1.ProxyConsumerBootwright), env.Spec.ProxyNameFor(v1alpha1.ProxyConsumerContainerClusterInstall)} {
		entry, ok := proxy.SelectedProxy(*env, name)
		if !ok || entry.Management != v1alpha1.EnvironmentComponentManaged || entry.ComponentRef.Name == "" {
			continue
		}
		if seen[entry.ComponentRef.Name] {
			continue
		}
		seen[entry.ComponentRef.Name] = true
		component, ok := stateview.InfraComponent(state, entry.ComponentRef.Name)
		if ok && component.Spec.Proxy != nil {
			out = append(out, selectedProxyComponent{entry: entry, component: component})
		}
	}
	return out
}

func nameResolutionComponentsForCluster(state v1alpha1.State, ci v1alpha1.ClusterInstall) []selectedNameResolutionComponent {
	env := stateview.Environment(state)
	if env == nil {
		return nil
	}
	seen := map[string]bool{}
	out := []selectedNameResolutionComponent{}
	for _, network := range stateview.ClusterNetworkConfigs(state, ci) {
		for _, ref := range network.Spec.NameResolutionRefs {
			entry, ok := stateview.NameResolutionEntry(env, ref.Name)
			if !ok || entry.Management != v1alpha1.EnvironmentComponentManaged || entry.ComponentRef.Name == "" {
				continue
			}
			if seen[entry.ComponentRef.Name] {
				continue
			}
			seen[entry.ComponentRef.Name] = true
			component, ok := stateview.InfraComponent(state, entry.ComponentRef.Name)
			if ok && component.Spec.NameResolution != nil {
				out = append(out, selectedNameResolutionComponent{entry: entry, component: component})
			}
		}
	}
	return out
}

func ntpComponentsForCluster(state v1alpha1.State) []selectedNTPComponent {
	env := stateview.Environment(state)
	if env == nil {
		return nil
	}
	seen := map[string]bool{}
	out := []selectedNTPComponent{}
	for _, entry := range env.Spec.InfraComponents.NTP {
		if entry.Management != v1alpha1.EnvironmentComponentManaged || entry.ComponentRef.Name == "" {
			continue
		}
		if seen[entry.ComponentRef.Name] {
			continue
		}
		seen[entry.ComponentRef.Name] = true
		component, ok := stateview.InfraComponent(state, entry.ComponentRef.Name)
		if ok && component.Spec.NTP != nil {
			out = append(out, selectedNTPComponent{entry: entry, component: component})
		}
	}
	return out
}

func registryComponentForCluster(state v1alpha1.State, ocp v1alpha1.ContainerCluster) (selectedRegistryComponent, bool) {
	if v1alpha1.InstallMode(ocp) != v1alpha1.InstallModeDisconnected {
		return selectedRegistryComponent{}, false
	}
	entry, ok := stateview.SelectedRegistryEntry(stateview.Environment(state))
	if !ok || entry.Management != v1alpha1.EnvironmentComponentManaged || entry.ComponentRef.Name == "" {
		return selectedRegistryComponent{}, false
	}
	component, ok := stateview.InfraComponent(state, entry.ComponentRef.Name)
	if !ok || component.Spec.Registry == nil {
		return selectedRegistryComponent{}, false
	}
	return selectedRegistryComponent{entry: entry, component: component}, true
}
