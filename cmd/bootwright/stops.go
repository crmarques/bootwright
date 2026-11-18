package main

import (
	"slices"

	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/infrastructureservices/mirrorfill"
	"github.com/crmarques/bootwright/internal/machine/power"
	"github.com/crmarques/bootwright/internal/managedos/diskdiscovery"
	"github.com/crmarques/bootwright/internal/managedos/hostfirewall"
	"github.com/crmarques/bootwright/internal/managedos/physicalerase"
	"github.com/crmarques/bootwright/internal/reconciliation/ansiblerunner"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
	"github.com/crmarques/bootwright/internal/substrate/kubevirt"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
)

func stopCatalog() []diagnostics.Stop {
	return slices.Concat(ansiblerunner.Stops(), lifecycle.Stops(), hostfirewall.Stops(), power.Stops(),
		diskdiscovery.Stops(), physicalerase.Stops(), mirrorfill.Stops(), baremetal.Stops(), kubevirt.Stops(), libvirt.Stops(),
		agentinstall.Stops(), managedservice.Stops())
}
