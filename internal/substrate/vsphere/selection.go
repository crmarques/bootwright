package vsphere

import (
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/network"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/substrate"
)

func invalid(message, remedy string) error {
	return diagnostics.NewFailureWithRemediation("api.invariant", message, "", remedy)
}

func Requests(catalog api.Catalog, controllerMachine, contextName string) ([]Request, error) {
	if !api.ValidLexical(api.LexicalName, contextName) {
		return nil, invalid("invalid context identity", "select a valid context")
	}
	var requests []Request
	macs := map[string]string{}
	for _, provider := range substrate.ProvidersOn(catalog, substrate.ArmVSphere) {
		for _, m := range substrate.HostedMachines(catalog, provider.Name()) {
			r, err := requestFor(catalog, provider, m, controllerMachine, contextName)
			if err != nil {
				return nil, err
			}
			for _, nic := range r.Interfaces {
				qualified := r.Server + "/" + nic.MACAddress
				if peer, ok := macs[qualified]; ok {
					return nil, invalid("duplicate vSphere MAC on "+peer+" and "+m.Identity(), "set a distinct manual macAddress on the conflicting NIC")
				}
				macs[qualified] = m.Identity()
			}
			requests = append(requests, r)
		}
	}
	slices.SortFunc(requests, func(a, b Request) int { return strings.Compare(a.Identity.Object, b.Identity.Object) })
	return requests, nil
}

func requestFor(c api.Catalog, provider, m api.Object, controller, contextName string) (Request, error) {
	target, err := substrate.TargetFor(c, m, contextName, controller)
	if err != nil {
		return Request{}, err
	}
	placement, err := machine.PlacementFor(target.PlacementMachine, controller)
	if err != nil {
		return Request{}, err
	}
	inv, _, ok := substrate.VSpherePlacement(provider, m, c)
	_, profile, selected := substrate.SelectedProfile(c, m)
	if !ok || !selected {
		return Request{}, invalid("the Machine has no vSphere placement", "correct the selected profile on "+m.Identity())
	}
	install, selected := c.Find(api.MachineInstallProfile, api.MachineInstallProfileRef(m))
	if !selected || (!install.Spec().Has("installer", "anaconda") && !install.Spec().Has("installer", "templateClone")) {
		return Request{}, invalid("vSphere Machines require an Anaconda ISO or template clone installer", "select a MachineInstallProfile on "+m.Identity())
	}
	media := substrate.VSphereMediaFor(provider, inv, contextName, m.Name())
	r := Request{Identity: Identity{Context: contextName, Object: m.Name(), Block: reconciliation.BlockIdentity("machine-", m.Name())}, Version: requestVersion, Name: media.Name, Owner: media.Owner, Server: target.Controller.Endpoint, CredentialsRef: target.Controller.CredentialsRef, TrustBundleRef: target.Controller.TrustBundleRef, TLSVerify: true,
		Datacenter: inv.Datacenter, ComputeCluster: inv.ComputeCluster, ResourcePool: inv.ResourcePool, Datastore: inv.Datastore, Folder: inv.Folder, TPM: profile.Has("tpm"),
		StagingDatastore: media.Datastore, StagingFolder: media.Folder, Placement: placement, Provider: provider.Name(), DataDisks: []DataDisk{}, Interfaces: []Interface{}}
	r.CPU, _ = profile.Get("cpu").Int64()
	r.MemoryMiB, _ = profile.Get("memoryMiB").Int64()
	r.DiskGiB, _ = profile.Get("diskGiB").Int64()
	if install.Spec().Has("installer", "templateClone") {
		r.Template = profile.Get("template").Text()
		if r.Template == "" {
			return Request{}, invalid("template cloning requires an inventory template", "set template on the selected machine profile")
		}
	}
	for _, disk := range profile.Get("dataDisks").Items() {
		size, _ := disk.Get("sizeGiB").Int64()
		r.DataDisks = append(r.DataDisks, DataDisk{Name: disk.Get("name").Text(), SizeGiB: size})
	}
	for _, nic := range target.Interfaces {
		ref := substrate.NICSegment(c, provider, m, nic.Name)
		seg, found := substrate.Segments(c, provider).Named("name", ref)
		if !found || !seg.Has("vsphere", "portgroup") {
			return Request{}, invalid("vSphere NIC has no portgroup", "select a segment on each NIC of "+m.Identity())
		}
		entry := Interface{MTU: substrate.SegmentMTU(seg), Name: nic.Name, MACAddress: nic.MACAddress, Portgroup: seg.Get("vsphere", "portgroup").Text()}
		for _, tag := range substrate.SegmentTagged(seg) {
			net, found := c.Find(api.Network, tag)
			vlan, ok := network.VLAN(net)
			if !found || !ok {
				return Request{}, invalid("vSphere trunk requires a VLAN on each tagged Network", "set spec.vlan on Network/"+tag)
			}
			entry.TaggedVLANs = append(entry.TaggedVLANs, vlan)
		}
		slices.Sort(entry.TaggedVLANs)
		r.Interfaces = append(r.Interfaces, entry)
	}
	return r, nil
}

func (MachineCapability) Claimed(c api.Catalog) []string {
	claimed := substrate.HostedOn(c, substrate.ArmVSphere)
	for _, p := range substrate.ProvidersOn(c, substrate.ArmVSphere) {
		claimed = append(claimed, p.Identity())
	}
	return claimed
}
