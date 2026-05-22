package desiredstate

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/artifactpub"
	"github.com/crmarques/bootwright/internal/stateview"
	"github.com/crmarques/bootwright/internal/support"
)

type sharedProviderServiceKey struct {
	kind         string
	providerName string
	name         string
	hostRef      string
}

type sharedProviderServiceEntry struct {
	key    sharedProviderServiceKey
	owner  string
	fields map[string]string
}

func validateSharedProviderServices(state v1alpha1.State) []string {
	groups := map[sharedProviderServiceKey]sharedProviderServiceEntry{}
	var errs []string
	for _, entry := range sharedProviderServiceEntries(state) {
		existing, ok := groups[entry.key]
		if !ok {
			groups[entry.key] = entry
			continue
		}
		for field, want := range existing.fields {
			if got := entry.fields[field]; got != want {
				errs = append(errs, fmt.Sprintf(
					"shared provider service %s %s/%s on Host/%s has conflicting %s: %s uses %q, %s uses %q",
					entry.key.kind, entry.key.providerName, entry.key.name, entry.key.hostRef,
					field, existing.owner, want, entry.owner, got))
			}
		}
	}
	return errs
}

func sharedProviderServiceEntries(state v1alpha1.State) []sharedProviderServiceEntry {
	var out []sharedProviderServiceEntry
	for _, ci := range state.ClusterInfras {
		c := ci.Spec.Components
		for _, lb := range c.LoadBalancers {
			if cap, ok := loadBalancerCapability(state, lb.From); ok && cap.HAProxy != nil {
				out = append(out, sharedProviderServiceEntry{
					key: sharedProviderServiceKey{
						kind:         v1alpha1.ComponentSlotLoadBalancer,
						providerName: lb.From.Provider,
						name:         lb.Name,
						hostRef:      cap.HAProxy.HostRef.Name,
					},
					owner: fmt.Sprintf("ClusterInfra/%s spec.components.loadBalancers[%s]", ci.Metadata.Name, lb.Name),
					fields: serviceFields(v1alpha1.ComponentSlotLoadBalancer, "haProxy", map[string]string{
						"capabilityName": lb.From.Name,
					}),
				})
			}
		}
		if c.Proxy != nil {
			if cap, ok := proxyCapability(state, c.Proxy.From); ok && cap.Squid != nil {
				out = append(out, sharedProviderServiceEntry{
					key: sharedProviderServiceKey{
						kind:         v1alpha1.ComponentSlotProxy,
						providerName: c.Proxy.From.Provider,
						name:         c.Proxy.From.Name,
						hostRef:      cap.Squid.HostRef.Name,
					},
					owner: fmt.Sprintf("ClusterInfra/%s spec.components.proxy", ci.Metadata.Name),
					fields: serviceFields(v1alpha1.ComponentSlotProxy, "squid", map[string]string{
						"bindAddress": c.Proxy.BindAddress,
						"port":        fmt.Sprint(c.Proxy.Port),
					}),
				})
			}
		}
		if c.NameResolution != nil {
			if cap, ok := dnsCapability(state, c.NameResolution.From); ok && cap.Dnsmasq != nil {
				out = append(out, sharedProviderServiceEntry{
					key: sharedProviderServiceKey{
						kind:         v1alpha1.ComponentSlotNameResolution,
						providerName: c.NameResolution.From.Provider,
						name:         c.NameResolution.From.Name,
						hostRef:      cap.Dnsmasq.HostRef.Name,
					},
					owner: fmt.Sprintf("ClusterInfra/%s spec.components.nameResolution", ci.Metadata.Name),
					fields: serviceFields(v1alpha1.ComponentSlotNameResolution, "dnsmasq", map[string]string{
						"bindAddress": c.NameResolution.BindAddress,
						"port":        fmt.Sprint(c.NameResolution.Port),
					}),
				})
			}
		}
		if c.Registry != nil {
			if cap, ok := registryCapability(state, c.Registry.From); ok && cap.MirrorRegistry != nil {
				out = append(out, sharedProviderServiceEntry{
					key: sharedProviderServiceKey{
						kind:         v1alpha1.ComponentSlotRegistry,
						providerName: c.Registry.From.Provider,
						name:         c.Registry.From.Name,
						hostRef:      cap.MirrorRegistry.HostRef.Name,
					},
					owner: fmt.Sprintf("ClusterInfra/%s spec.components.registry", ci.Metadata.Name),
					fields: serviceFields(v1alpha1.ComponentSlotRegistry, "mirrorRegistry", map[string]string{
						"bindAddress": c.Registry.BindAddress,
						"port":        fmt.Sprint(c.Registry.Port),
					}),
				})
			}
		}
		if ocp, ok := stateview.ClusterForInfra(state, ci); ok && artifactpub.ClusterNeedsPublication(state, ci, ocp) {
			if publisher, ok := artifactpub.Select(state); ok && publisher.Capability.HTTP != nil {
				out = append(out, sharedProviderServiceEntry{
					key: sharedProviderServiceKey{
						kind:         v1alpha1.ComponentSlotArtifacts,
						providerName: publisher.ProviderName,
						name:         publisher.Capability.Name,
						hostRef:      publisher.Capability.HTTP.HostRef.Name,
					},
					owner: fmt.Sprintf("ClusterInfra/%s generated artifact publisher", ci.Metadata.Name),
					fields: serviceFields(v1alpha1.ComponentSlotArtifacts, "http", map[string]string{
						"bindAddress": v1alpha1.DefaultServiceBindAddress,
						"port":        fmt.Sprint(publisher.Capability.HTTP.Port),
					}),
				})
			}
		}
	}
	return out
}

func serviceFields(kind, realisation string, fields map[string]string) map[string]string {
	s := support.LookupService(kind, realisation)
	fields["realisation"] = realisation
	fields["applyRole"] = s.ApplyRole
	fields["destroyRole"] = s.DestroyRole
	return fields
}

func loadBalancerCapability(state v1alpha1.State, from v1alpha1.From) (v1alpha1.LoadBalancerCapability, bool) {
	provider, ok := stateview.Provider(state, from.Provider)
	if !ok {
		return v1alpha1.LoadBalancerCapability{}, false
	}
	return stateview.LoadBalancer(provider, from.Name)
}

func proxyCapability(state v1alpha1.State, from v1alpha1.From) (v1alpha1.ProxyCapability, bool) {
	provider, ok := stateview.Provider(state, from.Provider)
	if !ok {
		return v1alpha1.ProxyCapability{}, false
	}
	return stateview.Proxy(provider, from.Name)
}

func dnsCapability(state v1alpha1.State, from v1alpha1.From) (v1alpha1.DNSCapability, bool) {
	provider, ok := stateview.Provider(state, from.Provider)
	if !ok {
		return v1alpha1.DNSCapability{}, false
	}
	return stateview.DNS(provider, from.Name)
}

func registryCapability(state v1alpha1.State, from v1alpha1.From) (v1alpha1.RegistryCapability, bool) {
	provider, ok := stateview.Provider(state, from.Provider)
	if !ok {
		return v1alpha1.RegistryCapability{}, false
	}
	return stateview.Registry(provider, from.Name)
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
