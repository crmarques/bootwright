package main

import (
	"context"
	"crypto/rand"
	"strings"
	"time"

	"github.com/crmarques/bootwright/ansible"
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons/instance"
	"github.com/crmarques/bootwright/internal/addons/packages"
	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/containercluster/networking"
	"github.com/crmarques/bootwright/internal/controller/clients"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/dnsserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/infrastructureservices/ntpserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/proxy"
	"github.com/crmarques/bootwright/internal/machine/power"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/managedos/media"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/ansiblerunner"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/material"
	"github.com/crmarques/bootwright/internal/storage/ceph"
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
	"github.com/crmarques/bootwright/internal/trust"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// lifecycleDependencies names every capability an operation consumes. A nil
// field leaves the lifecycle unavailable rather than substituting one.
type lifecycleDependencies struct {
	Workspace  lifecycle.Workspace
	Inputs     lifecycle.Inputs
	Host       lifecycle.HostIdentity
	Guard      lifecycle.ExecutionGuard
	Setup      lifecycle.SetupReader
	Selection  contexts.SelectionStore
	Presenter  lifecycle.PlanPresenter
	Progress   lifecycle.ProgressReporter
	Confirmer  lifecycle.Confirmer
	Executable lifecycle.Executable
	// Capabilities substitutes the implementation set an operation may run.
	// Production leaves it empty and uses this build's own capabilities.
	Capabilities lifecycle.CapabilityResolver
	// Media is the host media store a plan freezes installation media from.
	Media    media.Store
	Capacity capacity.CapacityChecker
}

// storeMediaRecords reads the host media store's records for the installation
// capability. A plan reads it while it holds the shared root lock, which the
// store's own shared read is compatible with.
type storeMediaRecords struct{ store media.Store }

func (r storeMediaRecords) MediaRecords(ctx context.Context) (map[string]installation.MediaRecord, error) {
	records := map[string]installation.MediaRecord{}
	err := r.store.ReadMedia(ctx, func(view media.View) error {
		entries, err := view.Entries(ctx)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			records[entry.Name] = installation.MediaRecord{SHA256: entry.SHA256, Size: entry.Size, Observed: entry.Observed, Failure: entry.Failure}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return records, nil
}

// automationDigest is the content identity of the embedded collection this
// build would run. An operation records it; other builds prove the same blocks.
type automationDigest struct{}

func (automationDigest) CatalogDigest() string { return ansible.Digest() }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// capabilityResolver is the immutable, ordered, allowlisted registry of the
// lifecycle capabilities this build offers, with no discovery or plugin path.
type capabilityResolver []boundCapability

type boundCapability struct {
	kind           string
	implementation string
	capability     lifecycle.Capability
}

func (r capabilityResolver) Bindings() []lifecycle.CapabilityBinding {
	bindings := make([]lifecycle.CapabilityBinding, 0, len(r))
	for _, bound := range r {
		bindings = append(bindings, lifecycle.CapabilityBinding{Kind: bound.kind, Implementation: bound.implementation})
	}
	return bindings
}

// Resolve answers the capability bound to exactly this kind and
// implementation, so a block frozen against one implementation never continues
// against another this build happens to offer for the same kind.
func (r capabilityResolver) Resolve(kind, implementation string) (lifecycle.Capability, bool) {
	for _, bound := range r {
		if bound.kind == kind && bound.implementation == implementation && bound.capability != nil {
			return bound.capability, true
		}
	}
	return nil, false
}

// operationPlaybook binds one implementation identity and operation to its
// fixed entrypoint, relative to the collection's playbook root. Composition
// owns the binding, so a capability names no automation of its own and desired
// state never names a playbook.
func operationPlaybook() map[string]string {
	bindings := map[string]string{
		networking.Implementation + "/apply":            "containercluster/network_apply.yml",
		networking.Implementation + "/observe":          "containercluster/network_observe.yml",
		instance.Implementation + "/apply":              "addons/instance_apply.yml",
		instance.Implementation + "/observe":            "addons/instance_observe.yml",
		artifactserver.Implementation + "/apply":        "infrastructureservices/artifact_server_apply.yml",
		artifactserver.Implementation + "/observe":      "infrastructureservices/artifact_server_observe.yml",
		artifactserver.Implementation + "/destroy":      "infrastructureservices/artifact_server_destroy.yml",
		libvirt.HostImplementation + "/apply":           "substrate/libvirt_host_apply.yml",
		libvirt.HostImplementation + "/observe":         "substrate/libvirt_host_observe.yml",
		libvirt.HostImplementation + "/destroy":         "substrate/libvirt_host_destroy.yml",
		libvirt.MachineImplementation + "/apply":        "substrate/libvirt_machine_apply.yml",
		libvirt.MachineImplementation + "/observe":      "substrate/libvirt_machine_observe.yml",
		libvirt.MachineImplementation + "/destroy":      "substrate/libvirt_machine_destroy.yml",
		baremetal.Implementation + "/apply":             "substrate/baremetal_machine_apply.yml",
		baremetal.Implementation + "/observe":           "substrate/baremetal_machine_observe.yml",
		baremetal.Implementation + "/destroy":           "substrate/baremetal_machine_destroy.yml",
		power.Implementation + "/power":                 "machine/power.yml",
		power.ReadImplementation + "/read":              "machine/power_read.yml",
		agentinstall.InstallImplementation + "/apply":   "containercluster/install_apply.yml",
		agentinstall.InstallImplementation + "/observe": "containercluster/install_observe.yml",
		agentinstall.InstallImplementation + "/destroy": "containercluster/install_destroy.yml",
		agentinstall.MediaImplementation + "/apply":     "containercluster/media_apply.yml",
		agentinstall.MediaImplementation + "/observe":   "containercluster/media_observe.yml",
		agentinstall.MediaImplementation + "/destroy":   "containercluster/media_destroy.yml",
		installation.TreeImplementation + "/apply":      "managedos/tree_apply.yml",
		installation.TreeImplementation + "/observe":    "managedos/tree_observe.yml",
		installation.TreeImplementation + "/destroy":    "managedos/tree_destroy.yml",
		installation.Implementation + "/apply":          "managedos/install_apply.yml",
		installation.Implementation + "/observe":        "managedos/install_observe.yml",
		installation.Implementation + "/destroy":        "managedos/install_destroy.yml",
		installation.EraseImplementation + "/destroy":   "managedos/erase_destroy.yml",
		installation.EraseImplementation + "/observe":   "managedos/erase_observe.yml",
		ceph.Implementation + "/apply":                  "storage/core_apply.yml",
		ceph.Implementation + "/observe":                "storage/core_observe.yml",
		ceph.Implementation + "/destroy":                "storage/core_destroy.yml",
		ceph.ImageImplementation + "/apply":             "storage/image_apply.yml",
		ceph.ImageImplementation + "/observe":           "storage/image_observe.yml",
		ceph.ImageImplementation + "/destroy":           "storage/image_destroy.yml",
		ceph.HostImplementation + "/apply":              "storage/host_apply.yml",
		ceph.HostImplementation + "/observe":            "storage/host_observe.yml",
		ceph.HostImplementation + "/destroy":            "storage/host_destroy.yml",
		ceph.ReplicationImplementation + "/apply":       "storage/replication_apply.yml",
		ceph.ReplicationImplementation + "/observe":     "storage/replication_observe.yml",
		ceph.ReplicationImplementation + "/destroy":     "storage/replication_destroy.yml",
		ceph.SMBImplementation + "/apply":               "storage/smb_apply.yml",
		ceph.SMBImplementation + "/observe":             "storage/smb_observe.yml",
		ceph.SMBImplementation + "/destroy":             "storage/smb_destroy.yml",
	}
	bindings[ceph.ExportImplementation+"/apply"] = "storage/export_apply.yml"
	bindings[ceph.ExportImplementation+"/observe"] = "storage/export_observe.yml"
	for prefix, definition := range map[string]managedservice.Definition{
		"proxy": proxy.Definition(), "dns_server": dnsserver.Definition(), "ntp_server": ntpserver.Definition(),
	} {
		for _, operation := range []string{"apply", "observe", "destroy"} {
			bindings[definition.Implementation+"/"+operation] = "infrastructureservices/" + prefix + "_" + operation + ".yml"
		}
	}
	return bindings
}

// capabilityRunner is the port every runner-driven capability declares, so
// the one runner composition builds reaches each of them unchanged.
type capabilityRunner interface {
	Run(context.Context, lifecycle.RunRequest) (lifecycle.RunResult, error)
}

// buildCapabilitiesWith binds every capability over one runner. Production
// passes the Ansible runner; the capability contract suite passes its own.
// records is the reader the installation capability freezes its images from.
func buildCapabilitiesWith(clock systemClock, controller controllerDependencies, runner capabilityRunner, records installation.MediaRecords) capabilityResolver {
	resolver := capabilityResolver{{
		kind: networking.Kind, implementation: networking.Implementation, capability: networking.New(runner, packages.Builtin()),
	}, {
		kind: instance.Kind, implementation: instance.Implementation, capability: instance.New(runner, packages.Builtin(), addonFacts),
	}, {
		kind: clients.Kind, implementation: clients.Implementation,
		capability: clients.New(controller.Tools, controller.StagePackages, controller.Setup, controller.ClientInstaller),
	}, {
		kind: libvirt.MachineKind, implementation: libvirt.MachineImplementation,
		capability: libvirt.NewMachine(runner),
	}, {
		kind: baremetal.Kind, implementation: baremetal.Implementation,
		capability: baremetal.NewMachine(runner),
	}, {
		kind: installation.TreeKind, implementation: installation.TreeImplementation,
		capability: installation.NewTree(runner).WithMedia(records),
	}, {
		kind: installation.Kind, implementation: installation.EraseImplementation,
		capability: installation.NewErase(runner).WithClaims(baremetal.BlockID).WithIdentities(provedIdentities{}).WithMedia(records).WithRetirementPolicies(ceph.PhysicalRetirementPolicies),
	}, {
		kind: installation.Kind, implementation: installation.Implementation,
		capability: installation.New(runner).WithIdentities(provedIdentities{}).WithMedia(records).WithRetirementPolicies(ceph.PhysicalRetirementPolicies),
	}, {
		kind: libvirt.HostKind, implementation: libvirt.HostImplementation,
		capability: libvirt.NewHost(runner),
	}, {
		kind: agentinstall.Kind, implementation: agentinstall.MediaImplementation,
		capability: agentinstall.NewMedia(runner, material.AuthorizedKey),
	}, {
		kind: agentinstall.Kind, implementation: agentinstall.InstallImplementation,
		capability: agentinstall.NewInstall(runner).WithIdentities(provedIdentities{}),
	}}
	for _, kind := range []api.Kind{api.StorageCluster, api.StoragePlacementPolicy, api.StoragePool, api.StorageRBDImage, api.StorageFilesystem, api.StorageSubvolume, api.StorageClient, api.StorageObjectGateway, api.StorageObjectUser, api.StorageObjectBucket, api.StorageNFSExport, api.StorageNVMeoFSubsystem} {
		resolver = append(resolver, boundCapability{kind: string(kind), implementation: ceph.Implementation, capability: ceph.New(kind, runner).WithProofs(cephDependencyProofs{})})
	}
	resolver = append(resolver, boundCapability{kind: string(api.StorageCluster), implementation: ceph.ImageImplementation, capability: ceph.NewImage(runner)})
	resolver = append(resolver, boundCapability{kind: string(api.StorageExport), implementation: ceph.ExportImplementation, capability: ceph.NewExport(runner).WithProofs(cephDependencyProofs{})})
	resolver = append(resolver, boundCapability{kind: string(api.StorageCluster), implementation: ceph.HostImplementation, capability: ceph.NewHost(runner).WithProofs(cephDependencyProofs{})})
	resolver = append(resolver, boundCapability{kind: string(api.StorageCluster), implementation: ceph.PhysicalRetirementImplementation,
		capability: ceph.NewPhysicalRetirement().WithClaims(baremetal.BlockID).WithProofs(cephDependencyProofs{}).WithErasureProofs(physicalCephEraseProof).WithUninstalledProofs(installation.PhysicalEraseUnstarted)})
	resolver = append(resolver, boundCapability{kind: string(api.StorageSMBResource), implementation: ceph.SMBImplementation, capability: ceph.NewSMB(runner).WithProofs(cephDependencyProofs{})})
	resolver = append(resolver, boundCapability{kind: string(api.StorageReplication), implementation: ceph.ReplicationImplementation, capability: ceph.NewReplication(runner).WithProofs(cephDependencyProofs{})})
	for _, definition := range []managedservice.Definition{proxy.Definition(), dnsserver.Definition(), ntpserver.Definition()} {
		resolver = append(resolver, boundCapability{
			kind: string(definition.Kind), implementation: definition.Implementation,
			capability: managedservice.NewCapability(definition, runner),
		})
	}
	return append(resolver, boundCapability{
		kind: artifactserver.Kind, implementation: artifactserver.Implementation,
		capability: artifactserver.New(runner, clock),
	})
}

// lifecycleMedia is the media store reader the installation capability plans
// through, and none when no store is wired, so planning an installation
// refuses rather than freezing unproved media.
func lifecycleMedia(deps lifecycleDependencies) installation.MediaRecords {
	if deps.Media == nil {
		return nil
	}
	return storeMediaRecords{store: deps.Media}
}

func wireLifecycle(deps lifecycleDependencies, controller controllerDependencies, compiler compilation.Compiler, binder *custody.Service) lifecycle.Service {
	if deps.Workspace == nil || deps.Inputs == nil || deps.Host == nil || deps.Guard == nil {
		return lifecycle.Service{}
	}
	clock := systemClock{}
	runner := ansiblerunner.New(operationPlaybook(), controller.Stager)
	var capabilities lifecycle.CapabilityResolver = buildCapabilitiesWith(clock, controller, runner, lifecycleMedia(deps))
	if deps.Capabilities != nil {
		capabilities = deps.Capabilities
	}
	options := lifecycleOptions(deps, clock, runner)
	options.ApplyAdmission = fipsApplyAdmission(controller.Host, libvirtCapacityAdmission(deps.Capacity))
	options.ListenerAdmission = firewallApplyAdmission(controller.Firewall)
	options.PlanInspection = fipsPlanInspection(controller.Host, firewallPlanInspection(controller.Firewall))
	if controller.Lock != nil {
		options.Lock = lifecycle.RuntimeLock{Digest: controller.Lock.Digest, AnsibleCore: controller.Lock.AnsibleCore}
		if controller.Setup != nil && controller.Python != nil {
			options.Rebuild = prerequisites.NewRuntimeRebuilder(controller.Setup, controller.Python, *controller.Lock)
		}
	}
	return lifecycle.New(deps.Workspace, deps.Inputs, compiler, binder, deps.Host, automationDigest{}, deps.Guard, capabilities, options)
}

func runtimeGuard(python prerequisites.PythonRuntime) lifecycle.ExecutionGuard {
	if python == nil {
		return nil
	}
	return python
}

// lifecycleOptions opens each execution's invocation scope through the runner
// every capability shares, so its copies admit their siblings' live jobs.
func lifecycleOptions(deps lifecycleDependencies, clock systemClock, runner ansiblerunner.Runner) lifecycle.Options {
	return lifecycle.Options{
		Confirmer: deps.Confirmer, Presenter: deps.Presenter, Progress: deps.Progress,
		Clock: clock, Entropy: rand.Read, Selection: currentSelection(deps.Selection),
		BeginInvocation: runner.BeginInvocation,
		Executable:      deps.Executable,
		Operations:      operationStores(clock, deps.Executable),
		Setup:           deps.Setup,
	}
}

func operationStores(clock systemClock, executable lifecycle.Executable) lifecycle.OperationStoreFactory {
	return func(area operationstore.Area) lifecycle.OperationStore {
		store := operationstore.New(area, clock.Now)
		if !executable.Modified {
			store.Running(operationstore.Executable{Version: executable.Version, Commit: executable.Commit})
		}
		return store
	}
}

// currentSelection resolves an omitted context name through the invoking
// user's own marker. Every context-backed service binds this one reader, so a
// stale marker is caught in the same place for all of them.
func currentSelection(store contexts.SelectionStore) func(context.Context) (string, error) {
	if store == nil {
		return nil
	}
	return func(ctx context.Context) (string, error) {
		selected, err := store.Read(ctx)
		if err != nil {
			return "", err
		}
		return selected.Name, nil
	}
}

type cephDependencyProofs struct{}

func (cephDependencyProofs) FIPS(proofs []lifecycle.BlockEvidence, machine, address string) error {
	return installation.ProvedFIPS(proofs, machine, address)
}

func (cephDependencyProofs) DataDisk(block reconciliation.Block, evidence []byte, path, serial string, size int64, rotational bool) bool {
	return libvirt.ProvedDataDisk(block, evidence, path, serial, size, rotational)
}

func (cephDependencyProofs) HostKey(proofs []lifecycle.BlockEvidence, machine, address string) (trust.HostKey, error) {
	return installation.ProvedHostKey(proofs, machine, address)
}

func (cephDependencyProofs) PackageTree(proofs []lifecycle.BlockEvidence, profile string) (ceph.PackageTree, error) {
	request, err := installation.ProvedPackageTree(proofs, profile)
	if err != nil {
		return ceph.PackageTree{}, err
	}
	return ceph.PackageTree{Context: request.Identity.Context, Media: request.Media.Name, MediaSHA256: request.Media.SHA256, MediaSize: request.Media.Size, Path: request.Publication.Path}, nil
}

func (cephDependencyProofs) PhysicalMachine(block reconciliation.Block, evidence []byte) (ceph.PhysicalIdentity, bool, error) {
	value, recognized, err := baremetal.ProvedPresence(block, evidence)
	return ceph.PhysicalIdentity{UUID: strings.ToLower(value.UUID), MACs: value.Addresses}, recognized, err
}

func physicalCephEraseProof(applied reconciliation.Block, removed lifecycle.BlockEvidence, uuid string) (ceph.PhysicalErasure, bool, error) {
	receipt, recognized, err := installation.PhysicalEraseProof(applied, removed, uuid)
	proof := ceph.PhysicalErasure{MACs: receipt.MACs, Targets: []ceph.Device{}}
	for _, target := range receipt.Targets {
		proof.Targets = append(proof.Targets, ceph.Device{Path: target.DeviceName, WWN: target.WWN, Serial: target.SerialNumber, SizeBytes: target.SizeBytes})
	}
	return proof, recognized, err
}
