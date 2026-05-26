package artifactpub

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/stateview"
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

func Select(state v1alpha1.State) (Server, bool) {
	env := stateview.Environment(state)
	if env == nil {
		return Server{}, false
	}
	entry, ok := selectedArtifactServer(env.Spec.InfraComponents.ArtifactServers)
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

func selectedArtifactServer(entries []v1alpha1.EnvironmentArtifactServerComponent) (v1alpha1.EnvironmentArtifactServerComponent, bool) {
	if len(entries) == 0 {
		return v1alpha1.EnvironmentArtifactServerComponent{}, false
	}
	for _, entry := range entries {
		if entry.Default {
			return entry, true
		}
	}
	if len(entries) == 1 {
		return entries[0], true
	}
	return v1alpha1.EnvironmentArtifactServerComponent{}, false
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

func RouteAvailable(server Server, endpointName string) bool {
	if server.Entry.Type == v1alpha1.EnvironmentComponentExternal {
		switch endpointName {
		case server.Entry.Routes.RedfishVirtualMedia.Endpoint:
			return server.Entry.Spec != nil && server.Entry.Spec.RedfishVirtualMediaURL != ""
		case server.Entry.Routes.ClusterInstall.Endpoint:
			return server.Entry.Spec != nil && server.Entry.Spec.ClusterInstallURL != ""
		}
	}
	_, ok := Endpoint(server, endpointName)
	return ok
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
