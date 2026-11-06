package cephcluster

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
)

func (a *admission) enrollmentAddresses(spec api.Value) {
	for _, node := range spec.Get("topology", "nodes").Items() {
		host, found := a.catalog.Find(api.Machine, node.Get("machineRef").Text())
		if !found {
			continue
		}
		reference, field := spec.Get("cephadm", "addressRef").Text(), "$.spec.cephadm.addressRef"
		if node.Get("name").Text() == spec.Get("cephadm", "bootstrap", "nodeRef").Text() && spec.Get("cephadm", "bootstrap", "addressRef").Text() != "" {
			reference, field = spec.Get("cephadm", "bootstrap", "addressRef").Text(), "$.spec.cephadm.bootstrap.addressRef"
		}
		if reference == "" {
			continue
		}
		address, err := machine.ResolveAddress(host, reference)
		ssh, sshErr := machine.ResolveAddress(host, host.Spec().Get("access", "ssh", "addressRef").Text())
		if err == nil && sshErr == nil && address != ssh {
			a.invalid(field, "Machine "+host.Name()+" enrollment address differs from its fleet SSH address", "select an addressRef resolving to the Machine access.ssh.addressRef address; separate enrollment addresses are not implemented")
		}
	}
}
