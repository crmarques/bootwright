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

func emulatedBMCListenPorts(l *v1alpha1.InfraProviderLibvirt) (port, vmediaPort int) {
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

func machineBootVars(state v1alpha1.State, ci v1alpha1.ClusterInstall, m v1alpha1.InstallMachine, clusterName string) map[string]any {
	return machineBootVarsWithISO(state, ci, m, clusterName, fmt.Sprintf("agent-%s.iso", clusterName))
}

func machineBootVarsWithISO(state v1alpha1.State, ci v1alpha1.ClusterInstall, m v1alpha1.InstallMachine, clusterName, isoBasename string) map[string]any {
	provider, ok := findProvider(state, m.Source.ProviderRef.Name)
	if !ok {
		return nil
	}

	if m.Source.ProfileRef.Name != "" {
		if _, ok := findProfile(provider, m.Source.ProfileRef.Name); !ok {
			return nil
		}
		if provider.Spec.Type != v1alpha1.ProvisionerLibvirt || provider.Spec.Libvirt == nil {
			return nil
		}
		return emulatedBootVars(state, ci, m, provider.Spec.Libvirt, clusterName, isoBasename)
	}
	if m.Source.MachineRef.Name != "" {
		server, ok := findProviderMachine(state, m.Source.MachineRef.Name)
		if !ok {
			return nil
		}
		if server.Spec.Substrate.BareMetal == nil {
			return nil
		}
		return baremetalBootVars(state, ci, server, isoBasename)
	}
	return nil
}

func machineEmulatedBMCVars(state v1alpha1.State, l *v1alpha1.InfraProviderLibvirt) map[string]any {
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

func emulatedBootVars(state v1alpha1.State, _ v1alpha1.ClusterInstall, m v1alpha1.InstallMachine, libvirt *v1alpha1.InfraProviderLibvirt, clusterName, isoBasename string) map[string]any {
	machineRef := libvirt.MachineRef.Name
	hostAddr := lookupMachineAddress(state, machineRef)
	port, vmediaPort := emulatedBMCListenPorts(libvirt)
	credRef := ""
	if d := libvirt.BMCEmulationDefaults; d != nil && d.Auth != nil {
		credRef = d.Auth.CredentialRef.Name
	}
	systemID := ansibleUUIDv5(clusterName + "-" + m.Name)

	stageDir := fmt.Sprintf("{{ bootwright_provider_state_dir }}/bmc/%s/vmedia", m.Source.ProviderRef.Name)
	return map[string]any{
		"redfish": map[string]any{
			"baseUrl":       fmt.Sprintf("http://%s:%d", hostAddr, port),
			"systemId":      systemID,
			"credentialRef": credRef,
			"validateCerts": false,
			"setBootSource": false,
		},
		"agentIso": map[string]any{
			"stageHost": machineRef,
			"stagePath": fmt.Sprintf("%s/%s/%s", stageDir, agentISOPublishTokenExpr, isoBasename),
			"fetchUrl":  fmt.Sprintf("http://127.0.0.1:%d/%s/%s", vmediaPort, agentISOPublishTokenExpr, isoBasename),
		},
		"media": map[string]any{
			"libvirt": map[string]any{
				"machineRef": machineRef,
				"uri":        libvirt.URI,
				"domain":     fmt.Sprintf("%s-%s", clusterName, m.Name),
			},
		},
	}
}

func baremetalBootVars(state v1alpha1.State, ci v1alpha1.ClusterInstall, server v1alpha1.Machine, isoBasename string) map[string]any {
	bmc := server.Spec.Substrate.BareMetal.BMC
	baseURL, systemID := normalizeRedfishURL(bmc.Address)
	stageHost, stagePath, fetchURL := baremetalAgentISOTarget(state, ci, isoBasename)

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

func baremetalAgentISOTarget(state v1alpha1.State, ci v1alpha1.ClusterInstall, isoBasename string) (stageHost, stagePath, fetchURL string) {
	server, endpoint, ok := artifacts.ResolveConsumerEndpoint(state, ci, v1alpha1.ArtifactConsumerRedfishVirtualMedia)
	if !ok {
		return "", "", ""
	}
	fetchURL = artifactEndpointFetchURL(state, server, endpoint, agentISOPublishTokenExpr, isoBasename)
	if fetchURL == "" || server.Config == nil {
		return "", "", fetchURL
	}
	machineRef := server.Config.MachineRef.Name
	stagePath = fmt.Sprintf("{{ bootwright_managed_services_dir }}/%s/public/%s/%s", server.Component.Metadata.Name, agentISOPublishTokenExpr, isoBasename)
	return machineRef, stagePath, fetchURL
}

func agentISOPublishTargets(state v1alpha1.State, ci v1alpha1.ClusterInstall, ocp v1alpha1.ContainerCluster) []any {
	clusterName := ocp.Metadata.Name
	targets := map[string]map[string]any{}
	var keys []string
	for _, m := range ci.Machines {
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
