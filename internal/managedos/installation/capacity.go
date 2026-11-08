package installation

import (
	"context"
	"math"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
)

const (
	InstallerAllowance = 64 << 20
	TreeAllowance      = 256 << 20
	TreeWrite          = "hosted package tree"
	PublishedWrite     = "published installer media"
	StagedWrite        = "staged installer media"
)

type MediaSizes func(context.Context) (map[string]int64, error)

func StoreSizes(records MediaRecords) MediaSizes {
	return func(ctx context.Context) (map[string]int64, error) {
		if records == nil {
			return map[string]int64{}, nil
		}
		held, err := records.MediaRecords(ctx)
		if err != nil {
			return nil, err
		}
		sizes := make(map[string]int64, len(held))
		for name, record := range held {
			if record.Failure == "" && record.Size > 0 {
				sizes[name] = record.Size
			}
		}
		return sizes, nil
	}
}

func FrozenSizes(blocks []reconciliation.Block) MediaSizes {
	return func(context.Context) (map[string]int64, error) {
		sizes := map[string]int64{}
		for _, block := range blocks {
			switch {
			case block.Kind == Kind && block.Implementation == Implementation:
				request, err := DecodeRequest(block.Request)
				if err != nil {
					return nil, err
				}
				sizes[request.BootMedia.Name] = request.BootMedia.Size
			case block.Kind == TreeKind && block.Implementation == TreeImplementation:
				request, err := DecodeTreeRequest(block.Request)
				if err != nil {
					return nil, err
				}
				sizes[request.Media.Name] = request.Media.Size
			}
		}
		return sizes, nil
	}
}

type plannedWrite struct {
	write     capacity.Write
	image     string
	allowance int64
}

func CapacityWrites(sizes MediaSizes) capacity.WriteProjector {
	return func(ctx context.Context, catalog api.Catalog, contextName string, allocations []capacity.Allocation, pending func(kind, object string) bool) ([]capacity.Write, error) {
		if len(InstalledMachines(catalog)) == 0 {
			return nil, nil
		}
		controller, err := lifecycle.ControllerMachine(catalog)
		if err != nil {
			return nil, err
		}
		requests, _, err := Requests(catalog, controller, contextName)
		if err != nil {
			return nil, err
		}
		planned, err := plannedWrites(catalog, controller, contextName, requests, allocations, pending)
		if err != nil || len(planned) == 0 {
			return nil, err
		}
		known, err := sizes(ctx)
		if err != nil {
			return nil, err
		}
		writes := make([]capacity.Write, 0, len(planned))
		for _, item := range planned {
			size, found := known[item.image]
			switch {
			case !found || size <= 0:
				item.write.Unknown = "the media store holds no size for image " + item.image + ", so the bytes the " + item.write.Label + " writes are unknown"
			case size > math.MaxInt64-item.allowance:
				return nil, refusal("lifecycle.state", "the recorded size of image "+item.image+" overflows the capacity budget", "")
			default:
				item.write.Bytes = size + item.allowance
			}
			writes = append(writes, item.write)
		}
		return writes, nil
	}
}

func plannedWrites(catalog api.Catalog, controller, contextName string, requests []Request, allocations []capacity.Allocation, pending func(kind, object string) bool) ([]plannedWrite, error) {
	hosted := make(map[string]capacity.Allocation, len(allocations))
	for _, allocation := range allocations {
		hosted[allocation.Machine] = allocation
	}
	var planned []plannedWrite
	trees := map[string]bool{}
	for _, request := range requests {
		profile := request.Identity.Profile
		if request.Tree != nil && request.TreeMedia != nil && !trees[profile] && pending(TreeKind, profile) {
			trees[profile] = true
			placement, err := treePlacement(catalog, profile, controller)
			if err != nil {
				return nil, err
			}
			planned = append(planned, placedWrite(TreeWrite+" "+profile, placement, contextName, request.Tree.Path, request.TreeMedia.Name, TreeAllowance))
		}
		machine := request.Identity.Object
		if !pending(Kind, machine) {
			continue
		}
		published := request.Image
		if published == nil {
			published = request.Private
		}
		if published != nil {
			planned = append(planned, placedWrite(PublishedWrite, request.Placement, contextName, published.Path, request.BootMedia.Name, InstallerAllowance))
		}
		if allocation, found := hosted[machine]; found && allocation.Staging != "" {
			planned = append(planned, plannedWrite{
				write: capacity.Write{Label: StagedWrite, Host: allocation.Host, Path: allocation.Staging, Context: allocation.Context, Local: allocation.Local},
				image: request.BootMedia.Name, allowance: InstallerAllowance,
			})
		}
	}
	return planned, nil
}

func placedWrite(label string, placement machineref.Placement, contextName, path, image string, allowance int64) plannedWrite {
	return plannedWrite{
		write: capacity.Write{Label: label, Host: placement.Machine, Path: path, Context: contextName, Local: placement.Local()},
		image: image, allowance: allowance,
	}
}

func treePlacement(catalog api.Catalog, profileName, controller string) (machineref.Placement, error) {
	profile, _ := catalog.Find(api.MachineInstallProfile, profileName)
	source := profile.Spec().Get("installer", "anaconda", "packageSource", "hostedTree")
	server, err := artifactserver.Selected(catalog, source.Get("artifactServerEndpoint"), profile.Identity())
	if err != nil {
		return machineref.Placement{}, err
	}
	return artifactserver.PlacementFor(catalog, server, controller)
}
