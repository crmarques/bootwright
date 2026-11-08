package installation

import (
	"context"
	"math"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
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
	return func(ctx context.Context, catalog api.Catalog, contextName string, allocations []capacity.Allocation, pending capacity.Unfinished) ([]capacity.Write, error) {
		if len(allocations) == 0 || len(InstalledMachines(catalog)) == 0 {
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

func plannedWrites(catalog api.Catalog, controller, contextName string, requests []Request, allocations []capacity.Allocation, pending capacity.Unfinished) ([]plannedWrite, error) {
	hosted := make(map[string]capacity.Allocation, len(allocations))
	for _, allocation := range allocations {
		hosted[allocation.Machine] = allocation
	}
	var planned []plannedWrite
	trees := map[string]bool{}
	for _, request := range requests {
		profile, _ := catalog.Find(api.MachineInstallProfile, request.Identity.Profile)
		anaconda := profile.Spec().Get("installer", "anaconda")
		if request.Tree != nil && request.TreeMedia != nil {
			tree, err := publishedWrite(catalog, anaconda.Get("packageSource", "hostedTree", "artifactServerEndpoint"), profile.Identity(), controller, contextName, TreeWrite+" "+profile.Name(), request.TreeMedia.Name, TreeAllowance)
			if err != nil {
				return nil, err
			}
			key := tree.write.Host + "\x00" + request.Tree.Path
			if !trees[key] {
				trees[key] = true
				if waiting, err := pending(TreeKind, TreeImplementation, profile.Name()); err != nil {
					return nil, err
				} else if waiting {
					planned = append(planned, tree)
				}
			}
		}
		machine := request.Identity.Object
		waiting, err := pending(Kind, Implementation, machine)
		if err != nil {
			return nil, err
		}
		if !waiting {
			continue
		}
		if request.Image != nil || request.Private != nil {
			image, err := publishedWrite(catalog, anaconda.Get("redfishVirtualMedia", "artifactServerEndpoint"), profile.Identity(), controller, contextName, PublishedWrite, request.BootMedia.Name, InstallerAllowance)
			if err != nil {
				return nil, err
			}
			planned = append(planned, image)
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

func publishedWrite(catalog api.Catalog, selection api.Value, identity, controller, contextName, label, image string, allowance int64) (plannedWrite, error) {
	server, err := artifactserver.Selected(catalog, selection, identity)
	if err != nil {
		return plannedWrite{}, err
	}
	placement, err := artifactserver.PlacementFor(catalog, server, controller)
	if err != nil {
		return plannedWrite{}, err
	}
	return plannedWrite{
		write: capacity.Write{Label: label, Host: placement.Machine, Path: artifactserver.ContentRoot(contextName, server.Name()), Context: contextName, Local: placement.Local()},
		image: image, allowance: allowance,
	}, nil
}
