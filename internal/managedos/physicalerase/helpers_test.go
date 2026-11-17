package physicalerase

import (
	"context"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/managedos/installationtest"
	"github.com/crmarques/bootwright/internal/managedos/installfixtures"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

var (
	field             = installfixtures.Field
	text              = installfixtures.Text
	number            = installfixtures.Number
	metalProvider     = installfixtures.MetalProvider
	server            = installfixtures.Server
	labCatalog        = installfixtures.LabCatalog
	eraseMachine      = installfixtures.EraseMachine
	sshKeySecret      = installfixtures.SSHKeySecret
	expectRefusal     = installfixtures.ExpectRefusal
	fleetMaterial     = installfixtures.FleetMaterial
	planInput         = installfixtures.PlanInput
	realizing         = installfixtures.Realizing
	metalOn           = installfixtures.MetalOn
	metalNetwork      = installfixtures.MetalNetwork
	metalCatalog      = installfixtures.MetalCatalog
	hardwareNIC       = installfixtures.HardwareNIC
	declaredInterface = installfixtures.DeclaredInterface
	labMedia          = installationtest.LabMedia
	requestOf         = installationtest.RequestOf
)

const updateNone = "none"

type fakeRunner struct {
	requests []lifecycle.RunRequest
	result   lifecycle.RunResult
	err      error
}

func (r *fakeRunner) Run(_ context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.requests = append(r.requests, request)
	return r.result, r.err
}

type pinReader struct {
	identity machineref.HardwareIdentity
	pinned   bool
	err      error
	asked    []string
	handed   [][]lifecycle.BlockEvidence
}

func (r *pinReader) PinnedIdentity(name string, proved []lifecycle.BlockEvidence) (machineref.HardwareIdentity, bool, error) {
	r.asked = append(r.asked, name)
	r.handed = append(r.handed, proved)
	return r.identity, r.pinned, r.err
}

func dataDisk(serial, size, wwn, onRemoval string) api.Value {
	disk := api.MapValue(text("serialNumber", serial), number("sizeBytes", size))
	if wwn != "" {
		disk = disk.With("wwn", api.StringValue(wwn))
	}
	if onRemoval != "" {
		disk = disk.With("onRemoval", api.StringValue(onRemoval))
	}
	return disk
}

func erasing(machine api.Object, root string, data ...api.Value) api.Object {
	spec := machine.Spec()
	if root != "" {
		spec = spec.WithPath(api.StringValue(root), "hardware", "rootDisk", "onRemoval")
	}
	if len(data) != 0 {
		spec = spec.WithPath(api.ListValue(data...), "hardware", "dataDisks")
	}
	return machine.WithSpec(spec)
}
