package artifacts

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/view"
)

type Server struct {
	Component v1alpha1.InfraComponent
	Config    *v1alpha1.ArtifactServerComponent
	Entry     v1alpha1.EnvironmentArtifactServerComponent
}

type ResolvedEndpoint struct {
	Endpoint v1alpha1.ArtifactServerEndpoint
	Listener v1alpha1.ArtifactServerListener
	Host     string
}

func Select(state v1alpha1.State, ci v1alpha1.ClusterInfra) (Server, bool) {
	return SelectByName(state, ci.Spec.ArtifactAccess.ServerRef.Name)
}

func SelectByName(state v1alpha1.State, name string) (Server, bool) {
	env := stateview.Environment(state)
	if env == nil || name == "" {
		return Server{}, false
	}
	entry, ok := artifactServerEntry(env.Spec.InfraComponents.ArtifactServers, name)
	if !ok {
		return Server{}, false
	}
	if entry.Type == v1alpha1.EnvironmentComponentExternal {
		return Server{Entry: entry}, true
	}
	component, ok := stateview.InfraComponent(state, entry.ComponentRef.Name)
	if !ok || component.Spec.ArtifactServer == nil {
		return Server{}, false
	}
	return Server{Component: component, Config: component.Spec.ArtifactServer, Entry: entry}, true
}

func artifactServerEntry(entries []v1alpha1.EnvironmentArtifactServerComponent, name string) (v1alpha1.EnvironmentArtifactServerComponent, bool) {
	if name == "" {
		return v1alpha1.EnvironmentArtifactServerComponent{}, false
	}
	for _, entry := range entries {
		if entry.Name == name {
			return entry, true
		}
	}
	return v1alpha1.EnvironmentArtifactServerComponent{}, false
}

func ConsumerEndpointName(ci v1alpha1.ClusterInfra, consumer string) string {
	switch consumer {
	case v1alpha1.ArtifactConsumerRedfishVirtualMedia:
		return ci.Spec.ArtifactAccess.RedfishVirtualMedia.EndpointRef.Name
	case v1alpha1.ArtifactConsumerContainerClusterInstall:
		return ci.Spec.ArtifactAccess.ContainerClusterInstall.EndpointRef.Name
	default:
		return ""
	}
}

func ResolveConsumerEndpoint(state v1alpha1.State, ci v1alpha1.ClusterInfra, consumer string) (Server, string, bool) {
	endpointName := ConsumerEndpointName(ci, consumer)
	if endpointName == "" {
		return Server{}, "", false
	}
	server, ok := Select(state, ci)
	if !ok || !EndpointAvailable(server, endpointName) {
		return Server{}, "", false
	}
	return server, endpointName, true
}

func ResolveEndpoint(state v1alpha1.State, server Server, name string) (ResolvedEndpoint, bool) {
	if name == "" || server.Config == nil {
		return ResolvedEndpoint{}, false
	}
	endpoint, ok := Endpoint(server, name)
	if !ok {
		return ResolvedEndpoint{}, false
	}
	listener, ok := Listener(server, endpoint.Listener)
	if !ok {
		return ResolvedEndpoint{}, false
	}
	host, ok := stateview.NamedHostAddress(state, server.Config.HostRef.Name, endpoint.HostAddress)
	if !ok || host == "" {
		return ResolvedEndpoint{}, false
	}
	return ResolvedEndpoint{Endpoint: endpoint, Listener: listener, Host: host}, true
}

func EndpointAvailable(server Server, endpointName string) bool {
	if server.Entry.Type == v1alpha1.EnvironmentComponentExternal {
		_, ok := ExternalEndpoint(server, endpointName)
		return ok
	}
	_, ok := Endpoint(server, endpointName)
	return ok
}

func ExternalEndpoint(server Server, name string) (v1alpha1.EnvironmentArtifactServerEndpoint, bool) {
	if name == "" {
		return v1alpha1.EnvironmentArtifactServerEndpoint{}, false
	}
	for _, endpoint := range server.Entry.Endpoints {
		if endpoint.Name == name && endpoint.URL != "" {
			return endpoint, true
		}
	}
	return v1alpha1.EnvironmentArtifactServerEndpoint{}, false
}

func Endpoint(server Server, name string) (v1alpha1.ArtifactServerEndpoint, bool) {
	if server.Config == nil {
		return v1alpha1.ArtifactServerEndpoint{}, false
	}
	for _, endpoint := range server.Config.Endpoints {
		if endpoint.Name == name {
			return endpoint, true
		}
	}
	return v1alpha1.ArtifactServerEndpoint{}, false
}

func Listener(server Server, name string) (v1alpha1.ArtifactServerListener, bool) {
	if server.Config == nil {
		return v1alpha1.ArtifactServerListener{}, false
	}
	for _, listener := range server.Config.Listeners {
		if listener.Name == name {
			return listener, true
		}
	}
	return v1alpha1.ArtifactServerListener{}, false
}

func ClusterNeedsPublication(state v1alpha1.State, ci v1alpha1.ClusterInfra, ocp v1alpha1.ContainerCluster) bool {
	if v1alpha1.InstallMode(ocp) == v1alpha1.InstallModeDisconnected {
		return true
	}
	return ClusterUsesBareMetalMachine(state, ci)
}

func ClusterUsesBareMetalMachine(state v1alpha1.State, ci v1alpha1.ClusterInfra) bool {
	for _, machine := range ci.Spec.Components.Machines {
		if machine.From.Name == "" {
			continue
		}
		provider, ok := stateview.Provider(state, machine.From.Provider)
		if !ok {
			continue
		}
		server, ok := stateview.Machine(provider, machine.From.Name)
		if !ok || v1alpha1.MachineProvisionerKind(server) != v1alpha1.ProvisionerBareMetal {
			continue
		}
		return true
	}
	return false
}

func EndpointHosts(state v1alpha1.State, server Server) []string {
	if server.Config == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(host string) {
		if host == "" || seen[host] {
			return
		}
		seen[host] = true
		out = append(out, host)
	}
	for _, endpoint := range server.Config.Endpoints {
		if host, ok := stateview.NamedHostAddress(state, server.Config.HostRef.Name, endpoint.HostAddress); ok {
			add(host)
		}
	}
	sort.Strings(out)
	return out
}
