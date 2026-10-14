package libvirt

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const rootDisk = "root"

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
		Kind: api.InfraProvider, Name: providerRef, Notes: missingDiskNotes(name, request, observed),
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
	return object.Spec().Get("substrate", "providerRef").Text(), object.Spec().Get("substrate", "profileRef").Text()
}

func machineNotes(name string, observed MachineEvidence) []string {
	var notes []string
	if observed.State != "" {
		notes = append(notes, "machine-"+name+": domain "+observed.State)
	}
	if observed.Power != "" {
		notes = append(notes, "machine-"+name+": its controller reports power "+observed.Power)
	}
	return notes
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
	if slices.ContainsFunc(request.Disks, func(disk Disk) bool { return disk.Name != rootDisk }) {
		slices.SortFunc(data, func(first, second DiskEvidence) int { return strings.Compare(first.Name, second.Name) })
		items := make([]api.Value, 0, len(data))
		for _, disk := range data {
			items = append(items, dataDiskItem(disk))
		}
		fields = append(fields, api.FieldValue{Name: "dataDisks", Value: api.ListValue(items...)})
	}
	return api.MapValue(fields...)
}

func dataDiskItem(disk DiskEvidence) api.Value {
	fields := []api.FieldValue{
		{Name: "name", Value: api.StringValue(disk.Name)},
		{Name: "sizeGiB", Value: api.IntegerValue(strconv.Itoa(disk.SizeGiB))},
	}
	if disk.RotationRate == 1 {
		fields = append(fields, api.FieldValue{Name: "media", Value: api.StringValue("ssd")})
	}
	return api.MapValue(fields...)
}
