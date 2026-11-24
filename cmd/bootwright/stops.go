package main

import (
	"slices"

	"github.com/crmarques/bootwright/internal/addons/clusterobjects/driver"
	"github.com/crmarques/bootwright/internal/addons/instance"
	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/controller/clients"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/infrastructureservices/mirrorfill"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/machine/hardware"
	"github.com/crmarques/bootwright/internal/machine/power"
	"github.com/crmarques/bootwright/internal/managedos/diskdiscovery"
	"github.com/crmarques/bootwright/internal/managedos/hostfirewall"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/managedos/media"
	"github.com/crmarques/bootwright/internal/managedos/physicalerase"
	"github.com/crmarques/bootwright/internal/managedos/providedhostname"
	"github.com/crmarques/bootwright/internal/managedos/providedregistration"
	"github.com/crmarques/bootwright/internal/managedos/providedtime"
	"github.com/crmarques/bootwright/internal/managedos/providedupdate"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/ansiblerunner"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/logview"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/storage/ceph"
	"github.com/crmarques/bootwright/internal/substrate"
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
	"github.com/crmarques/bootwright/internal/substrate/kubevirt"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
	"github.com/crmarques/bootwright/internal/workspace/contextfs"
	"github.com/crmarques/bootwright/internal/workspace/contextfs/kernel"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func stopCatalog() []diagnostics.Stop {
	return slices.Concat(ansiblerunner.Stops(), lifecycle.Stops(), hostfirewall.Stops(), providedupdate.Stops(), providedhostname.Stops(), providedregistration.Stops(), providedtime.Stops(), power.Stops(),
		diskdiscovery.Stops(), physicalerase.Stops(), mirrorfill.Stops(), baremetal.Stops(), kubevirt.Stops(), libvirt.Stops(),
		agentinstall.Stops(), managedservice.Stops(), installation.Stops(), ceph.Stops(),
		kernel.Stops(), media.Stops(),
		artifactserver.Stops(), machine.Stops(), hardware.Stops(), clients.Stops(), reconciliation.Stops(), substrate.Stops(), driver.Stops(), instance.Stops(), cli.Stops(), contexts.Stops(), contextfs.Stops(), operationstore.Stops(), logview.Stops())
}
