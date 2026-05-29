package render

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/artifacts"
)

const emulatedVmediaOffset = 1
const agentISOPublishTokenExpr = "__BOOTWRIGHT_AGENT_ISO_PUBLISH_TOKEN__"

func emulatedBMCListenPorts(l *v1alpha1.MachineProfileLibvirtProvisioner) (port, vmediaPort int) {
	port = v1alpha1.DefaultBMCEmulationStartPort
	vmediaPort = port + emulatedVmediaOffset
	if l == nil || l.BMCEmulationDefaults == nil {
		return port, vmediaPort
	}
	d := l.BMCEmulationDefaults
	if d.Port != 0 {
		port = d.Port
	}
	if d.VMediaPort != 0 {
		vmediaPort = d.VMediaPort
	} else {
		vmediaPort = port + emulatedVmediaOffset
	}
	return port, vmediaPort
}

func machineBootVars(state v1alpha1.State, ci v1alpha1.ClusterInfra, m v1alpha1.ClusterMachineComponent, clusterName string) map[string]any {
	provider, ok := findProvider(state, m.From.Provider)
	if !ok {
		return nil
	}
	isoBasename := fmt.Sprintf("agent-%s.iso", clusterName)

	if m.From.Profile != "" {
		profile, ok := findProfile(provider, m.From.Profile)
		if !ok {
			return nil
		}
		if profile.Libvirt == nil {
			return nil
		}
		return emulatedBootVars(state, ci, m, profile, clusterName, isoBasename)
	}
	if m.From.Name != "" {
		server, ok := findProviderMachine(provider, m.From.Name)
		if !ok {
			return nil
		}
		if server.BareMetal == nil {
			return nil
		}
		return baremetalBootVars(state, server, isoBasename)
	}
	return nil
}

func machineEmulatedBMCVars(state v1alpha1.State, profile v1alpha1.MachineProfileCapability) map[string]any {
	l := profile.Libvirt
	if l == nil {
		return nil
	}
	port, vmediaPort := emulatedBMCListenPorts(l)
	out := map[string]any{
		"protocol":          "redfish",
		"libvirtURI":        l.URI,
		"bindAddress":       "0.0.0.0",
		"port":              port,
		"vmediaPort":        vmediaPort,
		"credentialRef":     "",
		"sushyToolsVersion": componentPinVersion(state, "sushy-tools", defaultSushyToolsVersion),
	}
	if d := l.BMCEmulationDefaults; d != nil {
		if d.Protocol != "" {
			out["protocol"] = d.Protocol
		}
		if d.BindAddress != "" {
			out["bindAddress"] = d.BindAddress
		}
		if d.Auth != nil {
			out["credentialRef"] = d.Auth.CredentialRef.Name
		}
	}
	return out
}

func emulatedBootVars(state v1alpha1.State, _ v1alpha1.ClusterInfra, m v1alpha1.ClusterMachineComponent, profile v1alpha1.MachineProfileCapability, clusterName, isoBasename string) map[string]any {
	libvirt := profile.Libvirt
	hostRef := libvirt.HostRef.Name
	hostAddr := lookupHostAddress(state, hostRef)
	port, vmediaPort := emulatedBMCListenPorts(libvirt)
	credRef := ""
	if d := libvirt.BMCEmulationDefaults; d != nil && d.Auth != nil {
		credRef = d.Auth.CredentialRef.Name
	}
	systemID := ansibleUUIDv5(clusterName + "-" + m.Name)

	stageDir := fmt.Sprintf("{{ bootwright_managed_dir }}/bmc/%s/vmedia", m.From.Provider)
	return map[string]any{
		"redfish": map[string]any{
			"baseUrl":       fmt.Sprintf("http://%s:%d", hostAddr, port),
			"systemId":      systemID,
			"credentialRef": credRef,
			"validateCerts": false,
			"setBootSource": false,
		},
		"agentIso": map[string]any{
			"stageHost": hostRef,
			"stagePath": fmt.Sprintf("%s/%s/%s", stageDir, agentISOPublishTokenExpr, isoBasename),
			"fetchUrl":  fmt.Sprintf("http://127.0.0.1:%d/%s/%s", vmediaPort, agentISOPublishTokenExpr, isoBasename),
		},
		"media": map[string]any{
			"libvirt": map[string]any{
				"hostRef": hostRef,
				"uri":     libvirt.URI,
				"domain":  fmt.Sprintf("%s-%s", clusterName, m.Name),
			},
		},
	}
}

func baremetalBootVars(state v1alpha1.State, server v1alpha1.MachineCapability, isoBasename string) map[string]any {
	bmc := server.BareMetal.BMC
	baseURL, systemID := normalizeRedfishURL(bmc.Address)
	stageHost, stagePath, fetchURL := baremetalAgentISOTarget(state, isoBasename)

	return map[string]any{
		"redfish": map[string]any{
			"baseUrl":       baseURL,
			"systemId":      systemID,
			"credentialRef": bmc.CredentialsRef.Name,
			"validateCerts": !bmc.DisableCertificateVerification,
			"setBootSource": true,
		},
		"agentIso": map[string]any{
			"stageHost": stageHost,
			"stagePath": stagePath,
			"fetchUrl":  fetchURL,
		},
	}
}

func baremetalAgentISOTarget(state v1alpha1.State, isoBasename string) (stageHost, stagePath, fetchURL string) {
	server, ok := artifacts.Select(state)
	if !ok || server.Config == nil {
		return "", "", ""
	}
	hostRef := server.Config.HostRef.Name
	stagePath = fmt.Sprintf("{{ bootwright_managed_dir }}/services/artifact-server/%s-%s/public/%s/%s", v1alpha1.KindInfraComponent, server.Component.Metadata.Name, agentISOPublishTokenExpr, isoBasename)
	fetchURL = artifactEndpointFetchURL(state, server, server.Entry.Routes.RedfishVirtualMedia.Endpoint, agentISOPublishTokenExpr, isoBasename)
	if fetchURL == "" {
		return "", "", ""
	}
	return hostRef, stagePath, fetchURL
}

func agentISOPublishTargets(state v1alpha1.State, ci v1alpha1.ClusterInfra, ocp v1alpha1.ContainerCluster) []any {
	clusterName := ocp.Metadata.Name
	targets := map[string]map[string]any{}
	var keys []string
	for _, m := range ci.Spec.Components.Machines {
		boot := machineBootVars(state, ci, m, clusterName)
		if boot == nil {
			continue
		}
		iso, ok := boot["agentIso"].(map[string]any)
		if !ok {
			continue
		}
		stageHost, _ := iso["stageHost"].(string)
		stagePath, _ := iso["stagePath"].(string)
		fetchURL, _ := iso["fetchUrl"].(string)
		if stageHost == "" || stagePath == "" || fetchURL == "" {
			continue
		}
		redfish, _ := boot["redfish"].(map[string]any)
		requiresBMCFetchChecks, _ := redfish["setBootSource"].(bool)
		key := stageHost + "\x00" + stagePath + "\x00" + fetchURL
		target, ok := targets[key]
		if !ok {
			keys = append(keys, key)
			target = map[string]any{
				"stageHost":         stageHost,
				"stagePath":         stagePath,
				"fetchUrl":          fetchURL,
				"requiresHTTPS":     requiresBMCFetchChecks,
				"requiresByteRange": requiresBMCFetchChecks,
			}
			targets[key] = target
			continue
		}
		if requiresBMCFetchChecks {
			target["requiresHTTPS"] = true
			target["requiresByteRange"] = true
		}
	}
	sort.Strings(keys)
	out := make([]any, 0, len(keys))
	for _, key := range keys {
		out = append(out, targets[key])
	}
	return out
}

func normalizeRedfishURL(addr string) (baseURL, systemID string) {
	s := strings.TrimSpace(addr)
	s = normalizeRedfishTransport(s)
	const systemsMarker = "/redfish/v1/Systems/"
	if i := strings.Index(s, systemsMarker); i >= 0 {
		base := s[:i]
		rest := strings.TrimSuffix(s[i+len(systemsMarker):], "/")
		if rest != "" && !strings.Contains(rest, "/") {
			return base, rest
		}
		return base, ""
	}
	return strings.TrimRight(s, "/"), ""
}

func normalizeRedfishTransport(addr string) string {
	i := strings.Index(addr, "://")
	if i <= 0 {
		return addr
	}
	scheme := strings.ToLower(addr[:i])
	suffix := addr[i:]
	if j := strings.LastIndex(scheme, "+"); j >= 0 {
		switch transport := scheme[j+1:]; transport {
		case "http", "https":
			return transport + suffix
		}
	}
	switch scheme {
	case "redfish", "redfish-virtualmedia":
		return "https" + suffix
	default:
		return addr
	}
}
