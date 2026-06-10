package desiredstate

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/graph"
)

func validateSharedMachineServices(state v1alpha1.State) []string {
	return stategraph.ResolveMachineServices(state).ValidateSharedServices()
}

type libvirtBMCServiceConfig struct {
	libvirtURI     string
	bindAddress    string
	port           int
	vmediaPort     int
	credentialsRef string
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
		if p.Spec.Type != v1alpha1.ProvisionerLibvirt || p.Spec.Libvirt == nil || p.Spec.Libvirt.BMCEmulationDefaults == nil {
			continue
		}
		l := p.Spec.Libvirt
		d := l.BMCEmulationDefaults
		if d.Enabled != nil && !*d.Enabled {
			continue
		}
		owner := fmt.Sprintf("InfraProvider/%s spec.libvirt.bmcEmulationDefaults", p.Metadata.Name)
		serviceID := p.Metadata.Name + "|" + l.MachineRef.Name
		config := libvirtBMCServiceConfig{
			libvirtURI:     l.URI,
			bindAddress:    effectiveBMCEmulationBindAddress(d),
			port:           effectiveBMCEmulationPort(d),
			vmediaPort:     effectiveBMCEmulationVMediaPort(d),
			credentialsRef: effectiveBMCEmulationCredentialsRef(d),
		}
		if existing, ok := serviceConfigs[serviceID]; ok && existing != config {
			errs = append(errs, fmt.Sprintf("%s is incompatible with another libvirt BMC emulation provider using Machine/%s; shared BMC services must use matching URI, bind address, ports, and auth",
				owner, l.MachineRef.Name))
		}
		serviceConfigs[serviceID] = config
		errs = append(errs, checkBMCEmulationPort(ports, l.MachineRef.Name, config.port, libvirtBMCPortOwner{serviceID: serviceID, owner: owner, field: "port"})...)
		errs = append(errs, checkBMCEmulationPort(ports, l.MachineRef.Name, config.vmediaPort, libvirtBMCPortOwner{serviceID: serviceID, owner: owner, field: "vmediaPort"})...)
	}
	return errs
}

func checkBMCEmulationPort(ports map[string]libvirtBMCPortOwner, machineRef string, port int, owner libvirtBMCPortOwner) []string {
	key := fmt.Sprintf("%s|%d", machineRef, port)
	existing, ok := ports[key]
	if !ok {
		ports[key] = owner
		return nil
	}
	if existing.serviceID == owner.serviceID {
		return nil
	}
	return []string{fmt.Sprintf("%s.%s %d conflicts with %s.%s on Machine/%s",
		owner.owner, owner.field, port, existing.owner, existing.field, machineRef)}
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

func effectiveBMCEmulationCredentialsRef(d *v1alpha1.BMCEmulationDefaults) string {
	if d == nil || d.Auth == nil {
		return ""
	}
	return d.Auth.CredentialsRef.Name
}
