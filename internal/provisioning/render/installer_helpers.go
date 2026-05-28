package render

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/artifactpub"
	"github.com/crmarques/bootwright/internal/proxy"
)

// installerProxyConfig builds the install-config.yaml proxy block from
// the (effective env proxy, managed proxy URL, credentialed URLs)
// inputs. Returns nil when no proxy is in play.
func installerProxyConfig(eff *proxy.Effective, secrets InstallerSecrets, managedURL string) map[string]any {
	if eff == nil && managedURL == "" {
		return nil
	}
	if eff == nil {
		eff = &proxy.Effective{}
	}
	httpURL := pickURL(eff.HTTP, managedURL, secrets.ProxyHTTP)
	httpsURL := pickURL(eff.HTTPS, managedURL, secrets.ProxyHTTPS)
	if httpURL == "" && httpsURL == "" && len(eff.NoProxy) == 0 {
		return nil
	}
	out := map[string]any{}
	if httpURL != "" {
		out["httpProxy"] = httpURL
	}
	if httpsURL != "" {
		out["httpsProxy"] = httpsURL
	}
	if len(eff.NoProxy) > 0 {
		out["noProxy"] = strings.Join(eff.NoProxy, ",")
	}
	return out
}

// pickURL prefers a credentialed URL (already has user:pass inlined),
// falls back to the operator-declared URL, then the derived managed URL.
func pickURL(declared, derived, withCreds string) string {
	if withCreds != "" {
		return withCreds
	}
	if declared != "" {
		return declared
	}
	return derived
}

func clusterInstallProxyInputs(state v1alpha1.State, env *v1alpha1.Environment, ci v1alpha1.ClusterInfra) (*proxy.Effective, string, error) {
	if env == nil {
		return nil, "", nil
	}
	managedURL, err := proxy.ManagedProxyURL(state, ci)
	if err != nil {
		return nil, "", err
	}
	eff := proxy.ResolveFor(state, env, env.Spec.ProxyFor.ClusterInstall)
	if eff == nil && managedURL != "" {
		eff = &proxy.Effective{NoProxy: proxy.ResolveNoProxy(state, env, nil)}
	}
	return eff, managedURL, nil
}

func effectiveMirrorRegistryURL(state v1alpha1.State, ci v1alpha1.ClusterInfra, env *v1alpha1.Environment) string {
	if env != nil && env.Spec.Registries != nil && env.Spec.Registries.Mirror != nil {
		if u := strings.TrimRight(env.Spec.Registries.Mirror.URL, "/"); u != "" {
			return u
		}
	}
	entry, ok := selectedRegistryEntry(env)
	if !ok {
		return ""
	}
	if entry.Type == v1alpha1.EnvironmentComponentExternal {
		return strings.TrimRight(entry.URL, "/")
	}
	if entry.Type != v1alpha1.EnvironmentComponentManaged {
		return ""
	}
	component, ok := findInfraComponent(state, entry.ComponentRef.Name)
	if !ok || component.Spec.Registry == nil {
		return ""
	}
	registry := component.Spec.Registry
	host := proxy.ClusterFacingHostAddress(state, registry.HostRef.Name, ci)
	if host == "" {
		return ""
	}
	port := registry.Port
	if port == 0 {
		port = v1alpha1.DefaultMirrorRegistryPort
	}
	return fmt.Sprintf("%s:%d", host, port)
}

func installerImageDigestSources(state v1alpha1.State, ci v1alpha1.ClusterInfra, ocp v1alpha1.ContainerCluster, env *v1alpha1.Environment) []v1alpha1.ImageDigestSource {
	var out []v1alpha1.ImageDigestSource
	if env != nil && env.Spec.Registries != nil {
		out = append(out, env.Spec.Registries.ImageDigestSources...)
	}
	if v1alpha1.InstallMode(ocp) != v1alpha1.InstallModeDisconnected {
		return out
	}
	mirrorURL := effectiveMirrorRegistryURL(state, ci, env)
	if mirrorURL != "" {
		known := map[string]bool{}
		for _, src := range out {
			known[src.Source] = true
		}
		for _, src := range v1alpha1.DefaultReleaseImageDigestSources(ocp, mirrorURL) {
			if known[src.Source] {
				continue
			}
			out = append(out, src)
			known[src.Source] = true
		}
	}
	for i := range out {
		if out[i].SourcePolicy == "" {
			out[i].SourcePolicy = v1alpha1.ImageSourcePolicyNever
		}
	}
	return out
}

func imageDigestSourcesConfig(sources []v1alpha1.ImageDigestSource) []any {
	if len(sources) == 0 {
		return nil
	}
	out := make([]any, 0, len(sources))
	for _, source := range sources {
		mirrors := make([]any, 0, len(source.Mirrors))
		for _, m := range source.Mirrors {
			mirrors = append(mirrors, m)
		}
		entry := map[string]any{"source": source.Source, "mirrors": mirrors}
		if source.SourcePolicy != "" {
			entry["sourcePolicy"] = source.SourcePolicy
		}
		out = append(out, entry)
	}
	return out
}

func disconnectedBootArtifactsConfig(state v1alpha1.State, ocp v1alpha1.ContainerCluster) map[string]any {
	if v1alpha1.InstallMode(ocp) != v1alpha1.InstallModeDisconnected {
		return nil
	}
	server, ok := artifactpub.Select(state)
	if !ok {
		return nil
	}
	url := artifactServerEndpointURL(state, server, server.Entry.Routes.ClusterInstall.Endpoint)
	if url == "" {
		return nil
	}
	return map[string]any{
		"minimalISO":           true,
		"bootArtifactsBaseURL": url,
	}
}

func clusterInfraForOCP(state v1alpha1.State, ocp v1alpha1.ContainerCluster) (v1alpha1.ClusterInfra, error) {
	name := ""
	for _, node := range ocp.Spec.Nodes {
		if node.MachineRef.ClusterInfra == "" {
			continue
		}
		if name == "" {
			name = node.MachineRef.ClusterInfra
			continue
		}
		if node.MachineRef.ClusterInfra != name {
			return v1alpha1.ClusterInfra{}, fmt.Errorf("%s: nodes reference multiple ClusterInfra objects (%q and %q)", ocp.Metadata.Name, name, node.MachineRef.ClusterInfra)
		}
	}
	for _, infra := range state.ClusterInfras {
		if infra.Metadata.Name == name {
			return infra, nil
		}
	}
	return v1alpha1.ClusterInfra{}, fmt.Errorf("%s: machineRef.clusterInfra %q not found", ocp.Metadata.Name, name)
}

func installerNodeRole(role string) string {
	if role == v1alpha1.NodeRoleWorker {
		return "worker"
	}
	return "master"
}

func nodeRoleCount(ocp v1alpha1.ContainerCluster, role string) int {
	count := 0
	for _, node := range ocp.Spec.Nodes {
		if node.Role == role {
			count++
		}
	}
	return count
}

// pullSecretPlaceholder encodes the SecretRef name as the auths-key of
// a JSON pull-secret so the placeholder file is still parseable JSON.
func pullSecretPlaceholder(ref string) string {
	data, err := json.Marshal(map[string]any{
		"auths": map[string]any{
			"bootwright-secret-ref:" + ref: map[string]any{},
		},
	})
	if err != nil {
		return "{}"
	}
	return string(data)
}

func secretRefPlaceholder(kind, ref string) string {
	return fmt.Sprintf("<bootwright-%s-ref:%s>", kind, ref)
}

func cloneYAMLMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = cloneYAMLValue(v)
	}
	return out
}

func cloneYAMLValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return cloneYAMLMap(t)
	case []any:
		out := make([]any, 0, len(t))
		for _, c := range t {
			out = append(out, cloneYAMLValue(c))
		}
		return out
	default:
		return t
	}
}
