package libvirt

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

const (
	rootDisk     = "root"
	maxDiskNotes = 8
)

var (
	domainStates = []string{"running", "idle", "paused", "in shutdown", domainOff, "crashed", "pmsuspended"}
	powerStates  = []string{"On", "Off"}
)

func (MachineCapability) ProjectedPaths() []lifecycle.ProjectedPath {
	return []lifecycle.ProjectedPath{
		{Kind: api.Machine, Path: "spec.substrate.providerRef"},
		{Kind: api.InfraProvider, Path: "spec.libvirt.machineProfiles[].name"},
		{Kind: api.InfraProvider, Path: "spec.libvirt.machineProfiles[].diskGiB"},
		{Kind: api.InfraProvider, Path: "spec.libvirt.machineProfiles[].dataDisks[].name"},
		{Kind: api.InfraProvider, Path: "spec.libvirt.machineProfiles[].dataDisks[].sizeGiB"},
		{Kind: api.InfraProvider, Path: "spec.libvirt.machineProfiles[].dataDisks[].media"},
	}
}

func (MachineCapability) Project(applied api.Catalog, block reconciliation.Block, evidence json.RawMessage) ([]lifecycle.Projection, error) {
	request, err := DecodeMachineRequest(block.Request)
	if err != nil {
		return nil, err
	}
	observed, err := decodeMachineEvidence(evidence, block.RequestDigest)
	if err != nil {
		return nil, err
	}
	if !observed.Answered || !observed.Owned || observed.Domain != request.Domain {
		return nil, refusal("lifecycle.state", "the machine evidence does not prove this context's domain", "")
	}
	name := request.Identity.Object
	providerRef, profileRef := placement(applied, name)
	if providerRef == "" || profileRef == "" {
		return nil, refusal("lifecycle.state", "the applied revision does not place Machine/"+name+" on a libvirt profile", "")
	}
	machine := lifecycle.Projection{
		Kind: api.Machine, Name: name, Notes: machineNotes(name, observed),
		Observed: nestedMap([]string{"substrate"}, api.FieldValue{Name: "providerRef", Value: api.StringValue(providerRef)}),
	}
	profile := lifecycle.Projection{
		Kind: api.InfraProvider, Name: providerRef, Notes: boundedNotes(name, slices.Concat(missingDiskNotes(name, request, observed), unnamedDiskNotes(name, request, observed))),
		Observed: api.MapValue(api.FieldValue{Name: "libvirt", Value: api.MapValue(
			api.FieldValue{Name: "machineProfiles", Value: api.ListValue(profileItem(profileRef, request, observed))},
		)}),
	}
	return []lifecycle.Projection{machine, profile}, nil
}

func nestedMap(path []string, leaf api.FieldValue) api.Value {
	value := api.MapValue(leaf)
	for index := len(path) - 1; index >= 0; index-- {
		value = api.MapValue(api.FieldValue{Name: path[index], Value: value})
	}
	return value
}

func placement(applied api.Catalog, machine string) (string, string) {
	object, found := applied.Find(api.Machine, machine)
	if !found {
		return "", ""
	}
	return object.Spec().Get("substrate", "providerRef").Text(), substrate.MachineProfileRef(object)
}

func machineNotes(name string, observed MachineEvidence) []string {
	var notes []string
	switch {
	case slices.Contains(domainStates, observed.State):
		notes = append(notes, "machine-"+name+": domain "+observed.State)
	case observed.State != "":
		notes = append(notes, "machine-"+name+": its domain state is not one libvirt names")
	}
	switch {
	case slices.Contains(powerStates, observed.Power):
		notes = append(notes, "machine-"+name+": its BMC reports power "+observed.Power)
	case observed.Power != "":
		notes = append(notes, "machine-"+name+": its BMC power is not one it reports")
	}
	return notes
}

func boundedNotes(name string, notes []string) []string {
	if len(notes) <= maxDiskNotes {
		return notes
	}
	return append(notes[:maxDiskNotes-1:maxDiskNotes-1], "machine-"+name+": "+strconv.Itoa(len(notes)-maxDiskNotes+1)+" more disk notes")
}

func missingDiskNotes(name string, request MachineRequest, observed MachineEvidence) []string {
	var notes []string
	for _, disk := range request.Disks {
		if !slices.ContainsFunc(observed.Disks, func(seen DiskEvidence) bool { return seen.Name == disk.Name && seen.Present }) {
			notes = append(notes, "machine-"+name+": disk "+disk.Name+" is missing")
		}
	}
	return notes
}

func profileItem(profileRef string, request MachineRequest, observed MachineEvidence) api.Value {
	fields := []api.FieldValue{{Name: "name", Value: api.StringValue(profileRef)}}
	var data []DiskEvidence
	for _, disk := range observed.Disks {
		switch {
		case !disk.Present:
		case disk.Name == rootDisk:
			fields = append(fields, api.FieldValue{Name: "diskGiB", Value: api.IntegerValue(strconv.Itoa(disk.SizeGiB))})
		default:
			data = append(data, disk)
		}
	}
	data = slices.Concat(data, attachedDisks(request, observed))
	declares := slices.ContainsFunc(request.Disks, func(disk Disk) bool { return disk.Name != rootDisk })
	if declares || len(data) > 0 {
		slices.SortFunc(data, func(first, second DiskEvidence) int { return strings.Compare(first.Name, second.Name) })
		items := make([]api.Value, 0, len(data))
		for _, disk := range data {
			items = append(items, dataDiskItem(disk))
		}
		fields = append(fields, api.FieldValue{Name: "dataDisks", Value: api.ListValue(items...)})
	}
	return api.MapValue(fields...)
}

func attachedDisks(request MachineRequest, observed MachineEvidence) []DiskEvidence {
	taken := map[string]bool{rootDisk: true}
	for _, disk := range request.Disks {
		taken[disk.Name] = true
	}
	var named []DiskEvidence
	for _, disk := range observed.Attached {
		if disk.Present && substrate.SafeSegment(disk.Target) && !taken[disk.Target] && disk.SizeGiB > 0 {
			taken[disk.Target] = true
			disk.Name = disk.Target
			named = append(named, disk)
		}
	}
	return named
}

func unnamedDiskNotes(name string, request MachineRequest, observed MachineEvidence) []string {
	named := attachedDisks(request, observed)
	var notes []string
	for _, disk := range observed.Attached {
		switch {
		case !disk.Present || slices.ContainsFunc(named, func(seen DiskEvidence) bool { return seen.Target == disk.Target }):
		case disk.SizeGiB <= 0 && substrate.SafeSegment(disk.Target):
			notes = append(notes, "machine-"+name+": disk "+disk.Target+" attached outside the profile has no readable whole-GiB size")
		default:
			notes = append(notes, "machine-"+name+": disk "+disk.Target+" attached outside the profile cannot be named")
		}
	}
	return notes
}

func dataDiskItem(disk DiskEvidence) api.Value {
	fields := []api.FieldValue{
		{Name: "name", Value: api.StringValue(disk.Name)},
		{Name: "sizeGiB", Value: api.IntegerValue(strconv.Itoa(disk.SizeGiB))},
	}
	if disk.RotationRate == 1 {
		fields = append(fields, api.FieldValue{Name: "media", Value: api.StringValue("ssd")})
	} else if disk.RotationRate == 7200 {
		fields = append(fields, api.FieldValue{Name: "media", Value: api.StringValue("hdd")})
	}
	return api.MapValue(fields...)
}
