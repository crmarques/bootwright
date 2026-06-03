package desiredstate

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

var validHostCapabilities = map[string]bool{
	v1alpha1.HostCapabilityLibvirt:          true,
	v1alpha1.HostCapabilityContainerRuntime: true,
	v1alpha1.HostCapabilityCephAdmin:        true,
	v1alpha1.HostCapabilityCephNode:         true,
}

func validateHosts(state v1alpha1.State) []string {
	var errs []string
	seen := map[string]bool{}
	for _, h := range state.Hosts {
		if e := validateName(v1alpha1.KindHost, h.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		if seen[h.Metadata.Name] {
			errs = append(errs, fmt.Sprintf("duplicate Host %q", h.Metadata.Name))
		}
		seen[h.Metadata.Name] = true
		addresses := map[string]bool{}
		if len(h.Spec.Addresses) == 0 {
			errs = append(errs, fmt.Sprintf("Host/%s spec.addresses is required", h.Metadata.Name))
		}
		for i, address := range h.Spec.Addresses {
			prefix := fmt.Sprintf("Host/%s spec.addresses[%d]", h.Metadata.Name, i)
			if address.Name == "" {
				errs = append(errs, fmt.Sprintf("%s.name is required", prefix))
			} else if addresses[address.Name] {
				errs = append(errs, fmt.Sprintf("%s.name %q is duplicated", prefix, address.Name))
			}
			addresses[address.Name] = true
			if address.Address == "" {
				errs = append(errs, fmt.Sprintf("%s.address is required", prefix))
			}
		}
		if h.Spec.SSH == nil {
			errs = append(errs, fmt.Sprintf("Host/%s spec.ssh is required", h.Metadata.Name))
		} else {
			if h.Spec.SSH.AddressName == "" {
				errs = append(errs, fmt.Sprintf("Host/%s spec.ssh.addressName is required", h.Metadata.Name))
			} else if !addresses[h.Spec.SSH.AddressName] {
				errs = append(errs, fmt.Sprintf("Host/%s spec.ssh.addressName %q does not resolve to spec.addresses[].name", h.Metadata.Name, h.Spec.SSH.AddressName))
			}
			if h.Spec.SSH.KeyRef.Name == "" {
				errs = append(errs, fmt.Sprintf("Host/%s spec.ssh.keyRef.name is required", h.Metadata.Name))
			}
		}
		if len(h.Spec.Capabilities) == 0 {
			errs = append(errs, fmt.Sprintf("Host/%s spec.capabilities is required (canonical: libvirt, container-runtime, ceph-admin, ceph-node)", h.Metadata.Name))
		}
		for _, cap := range h.Spec.Capabilities {
			if !validHostCapabilities[cap] {
				errs = append(errs, fmt.Sprintf("Host/%s spec.capabilities %q is not in the canonical set {libvirt, container-runtime, ceph-admin, ceph-node}", h.Metadata.Name, cap))
			}
		}
	}
	return errs
}
