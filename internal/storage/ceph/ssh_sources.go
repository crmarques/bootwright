package ceph

import (
	"net/netip"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func validateSSHSourceAdmission(catalog api.Catalog, request Request) error {
	managers := []string{request.Bootstrap}
	for _, raw := range request.Services {
		service, err := reconciliation.DecodeEvidence[nativeService](raw, 60*1024, "Ceph native service")
		if err != nil {
			return err
		}
		if service.ServiceType == "mgr" {
			managers = append(managers, service.Placement.Hosts...)
		}
	}
	for _, target := range request.Hosts {
		object, _ := catalog.Find(api.Machine, target.Machine)
		sources, issue := machine.SSHSourcePrefixes(object, catalog)
		if issue != nil {
			return cephRequestInvalid.Fail("Machine/"+target.Machine+" has invalid SSH sources", "")
		}
		if len(sources) == 0 {
			continue
		}
		for _, source := range request.Hosts {
			if !slices.Contains(managers, source.Name) {
				continue
			}
			address, err := netip.ParseAddr(source.Address)
			if err != nil || !slices.ContainsFunc(sources, func(value string) bool {
				prefix, _ := netip.ParsePrefix(value)
				return prefix.Contains(address.Unmap())
			}) {
				return cephPrerequisite.Fail("Machine/"+target.Machine+" excludes the declared SSH IP of eligible Ceph manager Machine/"+source.Machine,
					"include that manager's numeric Ceph SSH address in spec.firewall.ssh.allowedSources on Machine/"+target.Machine+"; include its actual egress IP too if routing translates or selects a different source")
			}
		}
	}
	return nil
}
