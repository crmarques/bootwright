package desiredstate

import (
	"fmt"
	"net"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func validateClusterServices(v1alpha1.ClusterInstall, map[string]v1alpha1.InfraProvider) []string {
	return nil
}

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
			errs = append(errs, fmt.Sprintf("%s.ip is required", owner))
		} else if net.ParseIP(bind.Address) == nil {
			errs = append(errs, fmt.Sprintf("%s.ip %q is not a valid IP", owner, bind.Address))
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
	if len(binds) == 1 || referenced == nil {
		return errs
	}
	for ref := range referenced {
		if !seen[ref] {
			errs = append(errs, fmt.Sprintf("%s source.bindAddress %q does not match any bindAddresses[].name", prefix, ref))
		}
	}
	for _, bind := range binds {
		if bind.Name != "" && !referenced[bind.Name] {
			errs = append(errs, fmt.Sprintf("%s.bindAddresses[%s] is not referenced by any endpoint source.bindAddress",
				prefix, bind.Name))
		}
	}
	return errs
}
