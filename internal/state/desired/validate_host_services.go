package desiredstate

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/graph"
)

func validateSharedHostServices(state v1alpha1.State) []string {
	return stategraph.ResolveHostServices(state).ValidateSharedServices()
}

type libvirtBMCServiceConfig struct {
	libvirtURI    string
	bindAddress   string
	port          int
	vmediaPort    int
	credentialRef string
}

type libvirtBMCPortOwner struct {
	serviceID string
	owner     string
	field     string
}

func validateLibvirtBMCEmulationHostPorts(state v1alpha1.State) []string {
	serviceConfigs := map[string]libvirtBMCServiceConfig{}
	ports := map[string]libvirtBMCPortOwner{}
	var errs []string
	for _, p := range state.InfraProviders {
		for _, mp := range p.Spec.MachineProfiles {
			l := mp.Libvirt
			if l == nil || l.BMCEmulationDefaults == nil {
				continue
			}
			d := l.BMCEmulationDefaults
			if d.Enabled != nil && !*d.Enabled {
				continue
			}
			owner := fmt.Sprintf("InfraProvider/%s spec.machineProfiles[%s].libvirt.bmcEmulationDefaults", p.Metadata.Name, mp.Name)
			serviceID := p.Metadata.Name + "|" + l.HostRef.Name
			config := libvirtBMCServiceConfig{
				libvirtURI:    l.URI,
				bindAddress:   effectiveBMCEmulationBindAddress(d),
				port:          effectiveBMCEmulationPort(d),
				vmediaPort:    effectiveBMCEmulationVMediaPort(d),
				credentialRef: effectiveBMCEmulationCredentialRef(d),
			}
			if existing, ok := serviceConfigs[serviceID]; ok && existing != config {
				errs = append(errs, fmt.Sprintf("%s is incompatible with another libvirt BMC emulation profile on provider %s host %s; profiles sharing one provider-host BMC service must use matching URI, bind address, ports, and auth",
					owner, p.Metadata.Name, l.HostRef.Name))
			}
			serviceConfigs[serviceID] = config
			errs = append(errs, checkBMCEmulationPort(ports, l.HostRef.Name, config.port, libvirtBMCPortOwner{serviceID: serviceID, owner: owner, field: "port"})...)
			errs = append(errs, checkBMCEmulationPort(ports, l.HostRef.Name, config.vmediaPort, libvirtBMCPortOwner{serviceID: serviceID, owner: owner, field: "vmediaPort"})...)
		}
	}
	return errs
}

func checkBMCEmulationPort(ports map[string]libvirtBMCPortOwner, hostRef string, port int, owner libvirtBMCPortOwner) []string {
	key := fmt.Sprintf("%s|%d", hostRef, port)
	existing, ok := ports[key]
	if !ok {
		ports[key] = owner
		return nil
	}
	if existing.serviceID == owner.serviceID {
		return nil
	}
	return []string{fmt.Sprintf("%s.%s %d conflicts with %s.%s on Host/%s",
		owner.owner, owner.field, port, existing.owner, existing.field, hostRef)}
}

func effectiveBMCEmulationBindAddress(d *v1alpha1.BMCEmulationDefaults) string {
	if d != nil && d.BindAddress != "" {
		return d.BindAddress
	}
	return v1alpha1.DefaultBMCBindAddress
}

func effectiveBMCEmulationPort(d *v1alpha1.BMCEmulationDefaults) int {
	if d != nil && d.Port != 0 {
		return d.Port
	}
	return v1alpha1.DefaultBMCEmulationStartPort
}

func effectiveBMCEmulationVMediaPort(d *v1alpha1.BMCEmulationDefaults) int {
	if d != nil && d.VMediaPort != 0 {
		return d.VMediaPort
	}
	return effectiveBMCEmulationPort(d) + 1
}

func effectiveBMCEmulationCredentialRef(d *v1alpha1.BMCEmulationDefaults) string {
	if d == nil || d.Auth == nil {
		return ""
	}
	return d.Auth.CredentialRef.Name
}
