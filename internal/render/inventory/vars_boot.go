package inventory

import (
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/artifacts"
	"github.com/crmarques/bootwright/internal/render/installer"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

const emulatedVmediaOffset = 1
const agentISOPublishTokenExpr = "__BOOTWRIGHT_AGENT_ISO_PUBLISH_TOKEN__"

func emulatedBMCListenPorts(l *v1alpha1.InfraProviderLibvirt) (port, vMediaPort int) {
	port = v1alpha1.DefaultBMCEmulationStartPort
	vMediaPort = port + emulatedVmediaOffset
	if l == nil || l.BMCEmulationDefaults == nil {
		return port, vMediaPort
	}
	d := l.BMCEmulationDefaults
	if d.Port != 0 {
		port = d.Port
	}
	if d.VMediaPort != 0 {
		vMediaPort = d.VMediaPort
	} else {
		vMediaPort = port + emulatedVmediaOffset
	}
	return port, vMediaPort
}

func machineBootVars(state v1alpha1.State, ci v1alpha1.ClusterInstall, m v1alpha1.InstallMachine, clusterName string) map[string]any {
	return machineBootVarsWithISO(state, ci, m, clusterName, fmt.Sprintf("agent-%s.iso", clusterName), ci.Agent.RedfishVirtualMedia.ArtifactServerEndpoint)
}

func sshReadinessVars() map[string]any {
	return map[string]any{
		"type": "ssh",
		"ssh": map[string]any{
			"user": "core",
			"port": 22,
		},
	}
}

func machineBootVarsWithISO(state v1alpha1.State, ci v1alpha1.ClusterInstall, m v1alpha1.InstallMachine, clusterName, isoBasename string, redfishVirtualMedia v1alpha1.ArtifactServerEndpointRef) map[string]any {
	provider, ok := stateview.Provider(state, m.Source.ProviderRef.Name)
	if !ok {
		return nil
	}

	if m.Source.ProfileRef.Name != "" {
		profile, ok := stateview.MachineProfile(provider, m.Source.ProfileRef.Name)
		if !ok {
			return nil
		}
		switch {
		case provider.Spec.Type == v1alpha1.ProvisionerLibvirt && provider.Spec.Libvirt != nil:
			return emulatedBootVars(state, ci, m, provider.Spec.Libvirt, clusterName, isoBasename)
		case provider.Spec.Type == v1alpha1.ProvisionerVSphere && provider.Spec.VSphere != nil:
			return vsphereBootVars(provider.Spec.VSphere, profile, m, isoBasename)
		}
		return nil
	}
	if provider.Spec.Type == v1alpha1.ProvisionerBareMetal && m.Source.MachineRef.Name != "" {
		server, ok := stateview.Machine(state, m.Source.MachineRef.Name)
		if !ok {
			return nil
		}
		if server.Spec.Hardware.Management.BMC.Address == "" {
			return nil
		}
		return baremetalBootVars(state, redfishVirtualMedia, server, isoBasename)
	}
	return nil
}

func machineEmulatedBMCVars(state v1alpha1.State, l *v1alpha1.InfraProviderLibvirt) map[string]any {
	if l == nil {
		return nil
	}
	port, vMediaPort := emulatedBMCListenPorts(l)
	out := map[string]any{
		"protocol":          "redfish",
		"libvirtURI":        l.URI,
		"bindAddress":       "0.0.0.0",
		"port":              port,
		"vMediaPort":        vMediaPort,
		"credentialsRef":    "",
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
			out["credentialsRef"] = d.Auth.CredentialsRef.Name
		}
	}
	return out
}

func emulatedBootVars(state v1alpha1.State, _ v1alpha1.ClusterInstall, m v1alpha1.InstallMachine, libvirt *v1alpha1.InfraProviderLibvirt, clusterName, isoBasename string) map[string]any {
	machineRef := libvirt.MachineRef.Name
	hostAddr := stateview.MachineSSHAddressByName(state, machineRef)
	port, vMediaPort := emulatedBMCListenPorts(libvirt)
	credRef := ""
	if d := libvirt.BMCEmulationDefaults; d != nil && d.Auth != nil {
		credRef = d.Auth.CredentialsRef.Name
	}
	systemID := ansibleUUIDv5(clusterName + "-" + m.Name)

	stageDir := fmt.Sprintf("/var/lib/libvirt/images/bootwright/{{ bootwright_provider_state_dir | dirname | basename }}/bmc/%s/vmedia", m.Source.ProviderRef.Name)
	return map[string]any{
		"redfish": map[string]any{
			"baseUrl":             fmt.Sprintf("http://%s:%d", hostAddr, port),
			"systemId":            systemID,
			"credentialsRef":      credRef,
			"validateCerts":       false,
			"setBootSource":       false,
			"vmediaColdInitRetry": true,
		},
		"readiness": sshReadinessVars(),
		"agentIso": map[string]any{
			"stageHost":        machineRef,
			"stagePath":        fmt.Sprintf("%s/%s/%s", stageDir, agentISOPublishTokenExpr, isoBasename),
			"fetchUrl":         fmt.Sprintf("http://127.0.0.1:%d/%s/%s", vMediaPort, agentISOPublishTokenExpr, isoBasename),
			"transferProtocol": "HTTP",
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

func vsphereBootVars(spec *v1alpha1.InfraProviderVSphere, profile v1alpha1.MachineProfile, m v1alpha1.InstallMachine, isoBasename string) map[string]any {
	fd, ok := stateview.VSphereProfileFailureDomain(spec, profile)
	if !ok {
		return nil
	}
	staging := vSphereISOStagingVars(spec, fd)
	stageDir := fmt.Sprintf("{{ bootwright_provider_state_dir }}/vsphere/%s/vmedia", m.Source.ProviderRef.Name)
	return map[string]any{
		"readiness": sshReadinessVars(),
		"agentIso": map[string]any{
			"stageHost": "localhost",
			"stagePath": fmt.Sprintf("%s/%s/%s", stageDir, agentISOPublishTokenExpr, isoBasename),
			"fetchUrl":  fmt.Sprintf("[%s] %s/%s/%s", staging["datastore"], staging["folder"], agentISOPublishTokenExpr, isoBasename),
		},
	}
}

func baremetalBootVars(state v1alpha1.State, redfishVirtualMedia v1alpha1.ArtifactServerEndpointRef, server v1alpha1.Machine, isoBasename string) map[string]any {
	bmc := server.Spec.Hardware.Management.BMC
	baseURL, systemID := normalizeRedfishURL(bmc.Address)
	stageHost, stagePath, fetchURL, fetchBase := baremetalAgentISOTarget(state, redfishVirtualMedia, isoBasename)
	origin, _ := url.Parse(fetchBase)

	redfish := map[string]any{
		"baseUrl":        baseURL,
		"systemId":       systemID,
		"credentialsRef": bmc.CredentialsRef.Name,
		"validateCerts":  bmc.TLS.VerifyEnabled(),
		"setBootSource":  true,
	}
	if vm := bmc.VirtualMedia; vm != nil && vm.TLS != nil {
		certificate := map[string]any{
			"trust":            vm.TLS.TrustMode(),
			"restoreAfterBoot": vm.TLS.RestoreVerificationEnabled(),
			"removeAfterBoot":  vm.TLS.RemoveCertificateAfterBoot,
		}
		if origin != nil && origin.Hostname() != "" {
			certificate["host"] = origin.Hostname()
			certificate["port"] = artifactCertificatePort(origin)
		}
		redfish["artifactCertificate"] = certificate
	}

	agentISO := map[string]any{
		"stageHost": stageHost,
		"stagePath": stagePath,
		"fetchUrl":  fetchURL,
	}
	if origin != nil && origin.Scheme != "" {
		agentISO["transferProtocol"] = strings.ToUpper(origin.Scheme)
	}
	return map[string]any{
		"redfish":   redfish,
		"readiness": sshReadinessVars(),
		"agentIso":  agentISO,
	}
}

func artifactCertificatePort(origin *url.URL) string {
	if port := origin.Port(); port != "" {
		return port
	}
	if origin.Scheme == "http" {
		return "80"
	}
	return "443"
}

func baremetalAgentISOTarget(state v1alpha1.State, redfishVirtualMedia v1alpha1.ArtifactServerEndpointRef, isoBasename string) (stageHost, stagePath, fetchURL, fetchBase string) {
	server, endpoint, ok := artifacts.ResolveEndpointRef(state, redfishVirtualMedia)
	if !ok {
		return "", "", "", ""
	}
	fetchBase = installer.ArtifactServerEndpointURL(state, server, endpoint)
	fetchURL = artifactServerEndpointFetchURL(state, server, endpoint, agentISOPublishTokenExpr, isoBasename)
	if fetchURL == "" || server.Config == nil {
		return "", "", fetchURL, fetchBase
	}
	machineRef := server.Config.MachineRef.Name
	stagePath = fmt.Sprintf("{{ bootwright_managed_services_dir }}/%s/public/%s/%s", server.Component.Metadata.Name, agentISOPublishTokenExpr, isoBasename)
	return machineRef, stagePath, fetchURL, fetchBase
}

func agentISOPublishTargets(state v1alpha1.State, ci v1alpha1.ClusterInstall, ocp v1alpha1.ContainerCluster) []any {
	clusterName := ocp.Metadata.Name
	targets := map[string]map[string]any{}
	var keys []string
	for _, m := range ci.Machines {
		if provider, ok := stateview.Provider(state, m.Source.ProviderRef.Name); ok && provider.Spec.Type == v1alpha1.ProvisionerVSphere {
			continue
		}
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
			return bracketRedfishHost(base), rest
		}
		return bracketRedfishHost(base), ""
	}
	return bracketRedfishHost(strings.TrimRight(s, "/")), ""
}

func bracketRedfishHost(base string) string {
	i := strings.Index(base, "://")
	if i < 0 {
		return base
	}
	scheme, host := base[:i+3], base[i+3:]
	if host == "" || strings.HasPrefix(host, "[") {
		return base
	}
	if strings.Contains(host, ":") && net.ParseIP(host) != nil {
		return scheme + "[" + host + "]"
	}
	return base
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
