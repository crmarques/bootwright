package desiredstate

import (
	"fmt"
	"net"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func validateLoadBalancerBindAddresses(prefix string, binds []v1alpha1.LoadBalancerBindAddress, referenced map[string]bool) []string {
	var errs []string
	if len(binds) == 0 {
		errs = append(errs, fmt.Sprintf("%s.bindAddresses is required", prefix))
		return errs
	}
	seen := map[string]bool{}
	for i, bind := range binds {
		owner := fmt.Sprintf("%s.bindAddresses[%d]", prefix, i)
		if bind.Address == "" {
			errs = append(errs, fmt.Sprintf("%s.address is required", owner))
		} else if net.ParseIP(bind.Address) == nil {
			errs = append(errs, fmt.Sprintf("%s.address %q is not a valid IP", owner, bind.Address))
		}
		if len(binds) > 1 && bind.Name == "" {
			errs = append(errs, fmt.Sprintf("%s.name is required when a load balancer has multiple bindAddresses", owner))
		}
		if bind.Name != "" {
			if seen[bind.Name] {
				errs = append(errs, fmt.Sprintf("%s.name %q is duplicated", owner, bind.Name))
			}
			seen[bind.Name] = true
		}
	}
	// A non-empty endpoint source.bindAddressRef is a name reference and must
	// resolve regardless of bind count; the single-bind shortcut applies only
	// when the endpoint leaves source.bindAddressRef empty.
	for ref := range referenced {
		if !seen[ref] {
			errs = append(errs, fmt.Sprintf("%s source.bindAddressRef %q does not match any bindAddresses[].name", prefix, ref))
		}
	}
	return errs
}

// validateLoadBalancerBindAddressUse flags named bindAddresses that no
// endpoint source.bindAddressRef selects. It applies only to multi-bind load
// balancers: a single bindAddress is implicitly selected by endpoints that
// omit source.bindAddressRef.
func validateLoadBalancerBindAddressUse(prefix string, binds []v1alpha1.LoadBalancerBindAddress, referenced map[string]bool) []string {
	if len(binds) < 2 {
		return nil
	}
	var errs []string
	for _, bind := range binds {
		if bind.Name != "" && !referenced[bind.Name] {
			errs = append(errs, fmt.Sprintf("%s.bindAddresses[%s] is not referenced by any endpoint source.bindAddressRef",
				prefix, bind.Name))
		}
	}
	return errs
}
