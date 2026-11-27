package main

import (
	"context"
	"crypto/rand"
	"slices"
	"strings"
	"time"

	"github.com/crmarques/bootwright/ansible"
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/acquisition/listing"
	"github.com/crmarques/bootwright/internal/addons"
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
	"github.com/crmarques/bootwright/internal/infrastructureservices/mirrorfill"
	"github.com/crmarques/bootwright/internal/infrastructureservices/ntpserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/proxy"
	"github.com/crmarques/bootwright/internal/infrastructureservices/registry"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/machine/account"
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
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/material"
	"github.com/crmarques/bootwright/internal/storage/ceph"
	"github.com/crmarques/bootwright/internal/storage/cephcluster"
	"github.com/crmarques/bootwright/internal/substrate"
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
	"github.com/crmarques/bootwright/internal/substrate/kubevirt"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
	"github.com/crmarques/bootwright/internal/trust"
	"github.com/crmarques/bootwright/internal/workspace/contextfs"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// lifecycleDependencies names every capability an operation consumes. A nil
// field leaves the lifecycle unavailable rather than substituting one.
type lifecycleDependencies struct {
	Workspace  lifecycle.Workspace
	Host       lifecycle.HostIdentity
	HostKeys   lifecycle.ControllerHostKeys
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
	Media      media.Store
	Artifacts  artifactStore
	Capacity   capacity.CapacityChecker
	Enrollment account.Remote
	Paths      lifecycle.ContextPaths
	Logs       logview.Workspace
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
			records[entry.Name] = installation.MediaRecord{SHA256: entry.SHA256, Size: entry.Size, Observed: entry.Observed, Failure: entry.Failure, Release: entry.Release}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return records, nil
}

// automationDigest is the content identity of the embedded collection this
// build would run. An operation records it; current execution contracts prove recovery.
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
	family         string
	capability     lifecycle.Capability
}

func (r capabilityResolver) Bindings() []lifecycle.CapabilityBinding {
	bindings := make([]lifecycle.CapabilityBinding, 0, len(r))
	for _, bound := range r {
		bindings = append(bindings, lifecycle.CapabilityBinding{Kind: bound.kind, Implementation: bound.implementation, Family: bound.family})
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
func readOnlyOperations() []string {
	return []string{power.ReadImplementation + "/" + power.CheckOperation, agentinstall.InstallImplementation + "/prepare_credentials", ceph.Implementation + "/prepare_credentials", kubevirt.HostImplementation + "/" + lifecycle.RevokeOperation}
}

func operationPlaybook() map[string]string {
	bindings := map[string]string{
		networking.Implementation + "/apply":                               "containercluster/network_apply.yml",
		networking.Implementation + "/observe":                             "containercluster/network_observe.yml",
		instance.Implementation + "/apply":                                 "addons/instance_apply.yml",
		instance.Implementation + "/observe":                               "addons/instance_observe.yml",
		artifactserver.Implementation + "/apply":                           "infrastructureservices/artifact_server_apply.yml",
		artifactserver.Implementation + "/observe":                         "infrastructureservices/artifact_server_observe.yml",
		artifactserver.Implementation + "/destroy":                         "infrastructureservices/artifact_server_destroy.yml",
		libvirt.HostImplementation + "/apply":                              "substrate/libvirt_host_apply.yml",
		libvirt.HostImplementation + "/observe":                            "substrate/libvirt_host_observe.yml",
		libvirt.HostImplementation + "/destroy":                            "substrate/libvirt_host_destroy.yml",
		libvirt.MachineImplementation + "/apply":                           "substrate/libvirt_machine_apply.yml",
		libvirt.MachineImplementation + "/observe":                         "substrate/libvirt_machine_observe.yml",
		libvirt.MachineImplementation + "/destroy":                         "substrate/libvirt_machine_destroy.yml",
		libvirt.MachineImplementation + "/" + libvirt.RootResetOperation:   "substrate/libvirt_machine_root_reset.yml",
		baremetal.Implementation + "/apply":                                "substrate/baremetal_machine_apply.yml",
		baremetal.Implementation + "/observe":                              "substrate/baremetal_machine_observe.yml",
		baremetal.Implementation + "/destroy":                              "substrate/baremetal_machine_destroy.yml",
		kubevirt.HostImplementation + "/apply":                             "substrate/kubevirt_host_apply.yml",
		kubevirt.HostImplementation + "/observe":                           "substrate/kubevirt_host_observe.yml",
		kubevirt.HostImplementation + "/destroy":                           "substrate/kubevirt_host_destroy.yml",
		kubevirt.HostImplementation + "/lend":                              "substrate/kubevirt_host_lend.yml",
		kubevirt.HostImplementation + "/revoke":                            "substrate/kubevirt_host_revoke.yml",
		kubevirt.MachineImplementation + "/" + kubevirt.RootResetOperation: "substrate/kubevirt_machine_root_reset.yml",
		power.Implementation + "/power":                                    "machine/power.yml",
		power.ReadImplementation + "/read":                                 "machine/power_read.yml",
		power.ReadImplementation + "/" + power.CheckOperation:              "machine/power_hardware_check.yml",
		agentinstall.InstallImplementation + "/prepare_credentials":        "containercluster/admin_prepare_credentials.yml",
		agentinstall.InstallImplementation + "/update_credentials":         "containercluster/admin_update_credentials.yml",
		agentinstall.InstallImplementation + "/apply":                      "containercluster/install_apply.yml",
		agentinstall.InstallImplementation + "/observe":                    "containercluster/install_observe.yml",
		agentinstall.InstallImplementation + "/destroy":                    "containercluster/install_destroy.yml",
		agentinstall.MediaImplementation + "/apply":                        "containercluster/media_apply.yml",
		agentinstall.MediaImplementation + "/observe":                      "containercluster/media_observe.yml",
		agentinstall.MediaImplementation + "/destroy":                      "containercluster/media_destroy.yml",
		installation.TreeImplementation + "/apply":                         "managedos/tree_apply.yml",
		installation.TreeImplementation + "/observe":                       "managedos/tree_observe.yml",
		installation.TreeImplementation + "/destroy":                       "managedos/tree_destroy.yml",
		installation.Implementation + "/apply":                             "managedos/install_apply.yml",
		installation.Implementation + "/observe":                           "managedos/install_observe.yml",
		installation.Implementation + "/destroy":                           "managedos/install_destroy.yml",
		diskdiscovery.DiscoveryImplementation + "/apply":                   "managedos/discovery_apply.yml",
		diskdiscovery.DiscoveryImplementation + "/observe":                 "managedos/discovery_observe.yml",
		diskdiscovery.DiscoveryImplementation + "/destroy":                 "managedos/discovery_destroy.yml",
		physicalerase.EraseImplementation + "/destroy":                     "managedos/erase_destroy.yml",
		physicalerase.EraseImplementation + "/observe":                     "managedos/erase_observe.yml",
		providedupdate.Implementation + "/apply":                           "managedos/provided_update_apply.yml",
		providedupdate.Implementation + "/observe":                         "managedos/provided_update_observe.yml",
		providedhostname.Implementation + "/apply":                         "managedos/provided_hostname_apply.yml",
		providedhostname.Implementation + "/observe":                       "managedos/provided_hostname_observe.yml",
		providedregistration.Implementation + "/apply":                     "managedos/provided_registration_apply.yml",
		providedregistration.Implementation + "/observe":                   "managedos/provided_registration_observe.yml",
		providedtime.Implementation + "/apply":                             "managedos/provided_time_apply.yml",
		providedtime.Implementation + "/observe":                           "managedos/provided_time_observe.yml",
		hostfirewall.FirewallImplementation + "/apply":                     "managedos/firewall_apply.yml",
		hostfirewall.FirewallImplementation + "/observe":                   "managedos/firewall_observe.yml",
		hostfirewall.FirewallImplementation + "/destroy":                   "managedos/firewall_destroy.yml",
		ceph.Implementation + "/prepare_credentials":                       "storage/admin_prepare_credentials.yml",
		ceph.Implementation + "/update_credentials":                        "storage/admin_update_credentials.yml",
		ceph.Implementation + "/apply":                                     "storage/core_apply.yml",
		ceph.Implementation + "/observe":                                   "storage/core_observe.yml",
		ceph.Implementation + "/destroy":                                   "storage/core_destroy.yml",
		ceph.ImageImplementation + "/apply":                                "storage/image_apply.yml",
		ceph.ImageImplementation + "/observe":                              "storage/image_observe.yml",
		ceph.ImageImplementation + "/destroy":                              "storage/image_destroy.yml",
		ceph.HostImplementation + "/apply":                                 "storage/host_apply.yml",
		ceph.HostImplementation + "/observe":                               "storage/host_observe.yml",
		ceph.HostImplementation + "/destroy":                               "storage/host_destroy.yml",
		ceph.ReplicationImplementation + "/apply":                          "storage/replication_apply.yml",
		ceph.ReplicationImplementation + "/observe":                        "storage/replication_observe.yml",
		ceph.ReplicationImplementation + "/destroy":                        "storage/replication_destroy.yml",
		ceph.SMBImplementation + "/apply":                                  "storage/smb_apply.yml",
		ceph.SMBImplementation + "/observe":                                "storage/smb_observe.yml",
		ceph.SMBImplementation + "/destroy":                                "storage/smb_destroy.yml",
		ceph.SignOnImplementation + "/apply":                               "storage/core_apply.yml",
		ceph.SignOnImplementation + "/observe":                             "storage/core_observe.yml",
		ceph.SignOnImplementation + "/destroy":                             "storage/core_destroy.yml",
		ceph.SignOnImplementation + "/suspend_prepare":                     "storage/signon_suspend_prepare.yml",
		ceph.SignOnImplementation + "/suspend":                             "storage/signon_suspend.yml",
		ceph.SignOnImplementation + "/resume":                              "storage/signon_resume.yml",
	}
	for _, operation := range []string{"apply", "observe", "destroy"} {
		bindings[mirrorfill.FilesImplementation+"/"+operation] = "infrastructureservices/mirror_files_" + operation + ".yml"
		bindings[hardware.Implementation+"/"+operation] = "machine/hardware_" + operation + ".yml"
		bindings[kubevirt.MachineImplementation+"/"+operation] = "substrate/kubevirt_machine_" + operation + ".yml"
		bindings[mirrorfill.ImagesImplementation+"/"+operation] = "infrastructureservices/mirror_images_" + operation + ".yml"
		bindings[mirrorfill.ReleaseImplementation+"/"+operation] = "infrastructureservices/mirror_release_" + operation + ".yml"
	}
	bindings[ceph.ConsumerImplementation+"/apply"] = "storage/export_apply.yml"
	bindings[ceph.ConsumerImplementation+"/observe"] = "storage/export_observe.yml"
	for prefix, definition := range map[string]managedservice.Definition{
		"proxy": proxy.Definition(), "dns_server": dnsServerDefinition(), "ntp_server": ntpserver.Definition(), "registry": registryDefinition(),
	} {
		for _, operation := range []string{"apply", "observe", "destroy"} {
			bindings[definition.Implementation+"/"+operation] = "infrastructureservices/" + prefix + "_" + operation + ".yml"
		}
	}
	return bindings
}

func kubevirtHostRequirements(registry addons.Registry) kubevirt.HostRequirements {
	return func(catalog api.Catalog, cluster string) ([]reconciliation.ObjectRef, bool) {
		selected, _ := addons.Instances(catalog, registry)
		var requires []reconciliation.ObjectRef
		provided := false
		for _, member := range selected {
			if member.Instance.Cluster != cluster {
				continue
			}
			provided = provided || (slices.Contains(member.Package.Provides, "kubevirt") && member.Package.Descriptor == "")
			if !member.Package.Last {
				requires = append(requires, reconciliation.ObjectRef{Kind: instance.Kind, Object: cluster + "." + member.Package.Name})
			}
		}
		if host, found := catalog.Find(api.OpenShiftCluster, cluster); found && host.Spec().Get("networking", "secondaryNetworks").Len() != 0 {
			requires = append(requires, reconciliation.ObjectRef{Kind: networking.Kind, Object: cluster})
		}
		return requires, provided
	}
}

func kubevirtAuthoredProviders(registry addons.Registry) kubevirt.AuthoredProviders {
	return func(catalog api.Catalog, cluster string) []string {
		selected, _ := addons.Instances(catalog, registry)
		var authored []string
		for _, member := range selected {
			if member.Instance.Cluster == cluster && member.Package.Descriptor != "" && slices.Contains(member.Package.Provides, "kubevirt") {
				authored = append(authored, member.Package.Name)
			}
		}
		slices.Sort(authored)
		return authored
	}
}

// capabilityRunner is the port every runner-driven capability declares, so
// the one runner composition builds reaches each of them unchanged.
type capabilityRunner interface {
	Run(context.Context, lifecycle.RunRequest) (lifecycle.RunResult, error)
}

// buildCapabilitiesWith binds every capability over one runner. Production
// passes the Ansible runner; the capability contract suite passes its own.
// records is the reader the installation capability freezes its images from.
func buildCapabilitiesWith(clock systemClock, controller controllerDependencies, runner capabilityRunner, records installation.MediaRecords, enrollment ...account.Remote) capabilityResolver {
	var remote account.Remote
	if len(enrollment) != 0 {
		remote = enrollment[0]
	}
	resolver := capabilityResolver{{
		kind: networking.Kind, implementation: networking.Implementation, family: "cluster-network", capability: networking.New(runner, packages.Builtin()),
	}, {
		kind: instance.Kind, implementation: instance.Implementation, family: "addon-instance", capability: instance.New(runner, packages.Builtin(), addonFacts),
	}, {
		kind: clients.Kind, implementation: clients.Implementation, family: "controller-prerequisites",
		capability: clients.New(controller.Tools, controller.StagePackages, controller.Setup, controller.ClientInstaller).WithHostPrerequisites(controllerHostFirewalls),
	}, {
		kind: libvirt.MachineKind, implementation: libvirt.MachineImplementation, family: substrate.MachineFamily,
		capability: libvirt.NewMachine(runner),
	}, {
		kind: baremetal.Kind, implementation: baremetal.Implementation, family: substrate.MachineFamily,
		capability: baremetal.NewMachine(runner),
	}, {
		kind: kubevirt.MachineKind, implementation: kubevirt.MachineImplementation, family: substrate.MachineFamily,
		capability: kubevirt.NewMachine(runner),
	}, {
		kind: kubevirt.HostKind, implementation: kubevirt.HostImplementation, family: "substrate-host",
		capability: kubevirt.NewHost(runner, kubevirtHostRequirements(packages.Builtin())).WithAuthoredProviders(kubevirtAuthoredProviders(packages.Builtin())),
	}, {
		kind: installation.TreeKind, implementation: installation.TreeImplementation, family: "os-package-tree",
		capability: installation.NewTree(runner).WithMedia(records),
	}, {
		kind: installation.Kind, implementation: physicalerase.EraseImplementation, family: installation.EraseFamily,
	}, {
		kind: installation.Kind, implementation: installation.Implementation, family: installation.InstallFamily,
	}, {
		kind: installation.Kind, implementation: providedupdate.Implementation, family: machine.ProvidedUpdateFamily,
		capability: providedupdate.New(runner),
	}, {
		kind: installation.Kind, implementation: providedhostname.Implementation, family: machine.ProvidedHostnameFamily,
		capability: providedhostname.New(runner),
	}, {
		kind: installation.Kind, implementation: providedregistration.Implementation, family: machine.ProvidedRegistrationFamily,
		capability: providedregistration.New(runner),
	}, {
		kind: installation.Kind, implementation: providedtime.Implementation, family: machine.ProvidedTimeFamily,
		capability: providedtime.New(runner),
	}, boundCapability{
		kind: installation.Kind, implementation: hostfirewall.FirewallImplementation, family: hostfirewall.FirewallFamily,
		capability: hostfirewall.New(runner).WithHostPorts(hostPortRequirements),
	}, {
		kind: libvirt.HostKind, implementation: libvirt.HostImplementation, family: "substrate-host",
		capability: libvirt.NewHost(runner).WithHostPrerequisites(providerHostFirewall),
	}, {
		kind: agentinstall.Kind, implementation: agentinstall.MediaImplementation, family: "cluster-media",
		capability: agentinstall.NewMedia(runner, material.AuthorizedKey).WithIdentities(provedIdentities{}),
	}, {
		kind: agentinstall.Kind, implementation: agentinstall.InstallImplementation, family: "cluster-install",
		capability: agentinstall.NewInstall(runner).WithPasswordHash(material.BcryptPassword).WithIdentities(provedIdentities{}).WithHostAccess(kubevirt.NewLender(runner)),
	}}
	for _, kind := range []api.Kind{api.CephCluster, api.CephPlacementPolicy, api.CephPool, api.CephRBDImage, api.CephFilesystem, api.CephSubvolume, api.CephClient, api.CephObjectGateway, api.CephObjectUser, api.CephObjectBucket, api.CephNFSExport, api.CephNVMeoFSubsystem} {
		resolver = append(resolver, boundCapability{kind: string(kind), implementation: ceph.Implementation, family: "storage-core", capability: ceph.New(kind, runner).WithProofs(cephDependencyProofs{})})
	}
	resolver = append(resolver, boundCapability{kind: string(api.CephCluster), implementation: ceph.ImageImplementation, family: "storage-image", capability: ceph.NewImage(runner).WithRoute(clients.ProvedEgress)})
	resolver = append(resolver, boundCapability{kind: string(api.CephConsumer), implementation: ceph.ConsumerImplementation, family: "storage-consumer", capability: ceph.NewConsumer(runner).WithProofs(cephDependencyProofs{})})
	resolver = append(resolver, boundCapability{kind: string(api.CephCluster), implementation: ceph.HostImplementation, family: "storage-host", capability: ceph.NewHost(runner).WithProofs(cephDependencyProofs{})})
	resolver = append(resolver, boundCapability{kind: string(api.CephCluster), implementation: ceph.PhysicalRetirementImplementation, family: ceph.PhysicalRetirementFamily,
		capability: ceph.NewPhysicalRetirement().WithProofs(cephDependencyProofs{}).WithErasureProofs(physicalCephEraseProof).WithUninstalledProofs(physicalerase.PhysicalEraseUnstarted)})
	resolver = append(resolver, boundCapability{kind: string(api.CephSMBResource), implementation: ceph.SMBImplementation, family: "storage-smb", capability: ceph.NewSMB(runner).WithProofs(cephDependencyProofs{})})
	resolver = append(resolver, boundCapability{kind: string(api.CephSignOn), implementation: ceph.SignOnImplementation, family: "storage-signon", capability: ceph.NewSignOn(runner).WithProofs(cephDependencyProofs{})})
	resolver = append(resolver, boundCapability{kind: string(api.CephReplication), implementation: ceph.ReplicationImplementation, family: "storage-replication", capability: ceph.NewReplication(runner).WithProofs(cephDependencyProofs{})})
	for _, definition := range managedServiceDefinitions() {
		resolver = append(resolver, boundCapability{
			kind: string(definition.Kind), implementation: definition.Implementation, family: managedservice.Family,
			capability: managedservice.NewCapability(definition, runner).WithClock(clock).WithInstalledHostKeys(installation.ProvedHostKey),
		})
	}
	resolver = append(resolver, boundCapability{
		kind: artifactserver.Kind, implementation: artifactserver.Implementation, family: managedservice.Family,
		capability: artifactserver.New(runner, clock).WithInstalledHostKeys(installation.ProvedHostKey),
	})
	resolver = append(resolver, boundCapability{
		kind: mirrorfill.Kind, implementation: mirrorfill.FilesImplementation, family: mirrorfill.Family,
		capability: mirrorfill.NewFiles(runner, acquisitionTools().MirrorFiles).WithInstalledHostKeys(installation.ProvedHostKey),
	})
	resolver = append(resolver, boundCapability{
		kind: hardware.Kind, implementation: hardware.Implementation, family: hardware.Family, capability: hardware.New(runner).WithBootstrap(remote.HardwareAddresses, remote.PromptSSH),
	})
	resolver = append(resolver, boundCapability{
		kind: mirrorfill.ImagesKind, implementation: mirrorfill.ImagesImplementation, family: mirrorfill.Family,
		capability: mirrorfill.NewImages(runner, listing.MirrorImages).WithInstalledHostKeys(installation.ProvedHostKey),
	})
	resolver = append(resolver, boundCapability{
		kind: mirrorfill.ReleaseKind, implementation: mirrorfill.ReleaseImplementation, family: mirrorfill.Family,
		capability: mirrorfill.NewReleases(runner, acquisitionTools().MirrorReleases, material.RegistryAuth).WithInstalledHostKeys(installation.ProvedHostKey),
	})
	resolver = append(resolver, boundCapability{kind: account.Kind, implementation: account.Implementation,
		family: machine.EnrollmentFamily, capability: account.Capability{Remote: remote, ClusterKey: ceph.EnrollmentClusterKey}})
	return bindInstallations(resolver, runner, records)
}

func bindInstallations(resolver capabilityResolver, runner capabilityRunner, records installation.MediaRecords) capabilityResolver {
	discovery := diskdiscovery.New(runner).WithIdentities(provedIdentities{}).WithMedia(records).WithProtections(cephcluster.RootDiskProtections).WithRefinements(cephcluster.RootDiskRefinement).WithNonces(discoveryNonce)
	resolver = append(resolver, boundCapability{kind: installation.Kind, implementation: diskdiscovery.DiscoveryImplementation, family: installation.DiscoveryFamily, capability: discovery})
	var policies []installation.RetirementPolicy
	for _, bound := range resolver {
		if policy, retires := bound.capability.(installation.RetirementPolicy); retires {
			policies = append(policies, policy)
		}
	}
	for index, bound := range resolver {
		switch {
		case bound.kind == installation.Kind && bound.implementation == physicalerase.EraseImplementation:
			resolver[index].capability = physicalerase.New(runner).WithIdentities(provedIdentities{}).WithMedia(records).WithRetirementPolicies(policies...).WithDiscoveredRoots(discovery.Roots)
		case bound.kind == installation.Kind && bound.implementation == installation.Implementation:
			resolver[index].capability = installation.New(runner).WithIdentities(provedIdentities{}).WithMedia(records).WithRetirementPolicies(policies...).WithDiscoveredRoots(discovery.Roots).WithDiscoveredRootProof(discovery.ProveRoot)
		}
	}
	return resolver
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

var _ lifecycle.SecretBinder = (*custody.Service)(nil)

func wireLifecycle(deps lifecycleDependencies, controller controllerDependencies, compiler compilation.Compiler, binder *custody.Service) lifecycle.Service {
	if deps.Workspace == nil || deps.Host == nil || deps.Guard == nil {
		return lifecycle.Service{}
	}
	clock := systemClock{}
	runner := ansiblerunner.New(operationPlaybook(), controller.Stager).AdmitReadOnly(readOnlyOperations()...)
	bindings := buildCapabilitiesWith(clock, controller, runner, lifecycleMedia(deps), deps.Enrollment)
	for index, binding := range bindings {
		if binding.implementation == mirrorfill.FilesImplementation {
			bindings[index].capability = mirrorfill.NewFiles(runner, acquisitionTools().MirrorFiles, deps.Artifacts).WithInstalledHostKeys(installation.ProvedHostKey)
		}
	}
	var capabilities lifecycle.CapabilityResolver = bindings
	if deps.Capabilities != nil {
		capabilities = deps.Capabilities
	}
	options := lifecycleOptions(deps, clock, runner)
	options.Preparations = append(discoveryPreparations(capabilities), enrollmentPreparations(capabilities)...)
	options.ApplyAdmission = fipsApplyAdmission(controller.Host, libvirtCapacityAdmission(deps.Capacity))
	options.ListenerAdmission = firewallApplyAdmission(controller.Firewall)
	options.BlockAdmission = firewallBlockAdmission(controller.Firewall)
	options.HostKeys = deps.HostKeys
	options.PlanInspection = fipsPlanInspection(controller.Host, hostPortPlanInspection(hostPortRequirements, firewallPlanInspection(controller.Firewall)))
	if binder != nil {
		options.Readiness = statusReadiness(binder, lifecycleMedia(deps))
	}
	if controller.Lock != nil {
		options.Lock = lifecycle.RuntimeLock{Digest: controller.Lock.Digest, AnsibleCore: controller.Lock.AnsibleCore}
		if controller.Setup != nil && controller.Python != nil {
			options.Rebuild = prerequisites.NewRuntimeRebuilder(controller.Setup, controller.Python, *controller.Lock)
		}
	}
	return lifecycle.New(deps.Workspace, compiler, binder, deps.Host, automationDigest{}, deps.Guard, capabilities, options)
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
		Contracts:       currentExecutionContract,
		PasswordMatches: material.BcryptPasswordMatches,
		DriveMachine:    registeredMachineDriver(runner),
		Confirmer:       deps.Confirmer, Presenter: deps.Presenter, Progress: deps.Progress,
		Clock: clock, Entropy: rand.Read, Selection: currentSelection(deps.Selection),
		BeginInvocation: withEnrollmentInvocation(runner.BeginInvocation), BeginPrompts: account.BeginInvocation, EffectBearing: effectBearing,
		Executable: deps.Executable,
		Operations: operationStores(clock, deps.Executable),
		Setup:      deps.Setup,
		Paths:      deps.Paths,
	}
}

func wireLogs(deps lifecycleDependencies) logview.Service {
	return logview.New(deps.Logs, logview.Options{
		Operations: func(records, logs operationstore.Area) logview.OperationStore {
			return operationStore(systemClock{}, deps.Executable, records, logs)
		},
		Selection: currentSelection(deps.Selection),
		ToolLog: func(name string, block reconciliation.Block) (logview.ToolLog, bool) {
			log, ok := agentinstall.InstallerLogForBlock(name, block)
			if !ok {
				return logview.ToolLog{}, false
			}
			return logview.ToolLog{
				Name: "openshift-install", Active: log.Active, Project: log.Project,
				Open: func() (logview.LogFile, string, error) { return contextfs.OpenNativeToolLog(log.Path()) },
			}, true
		},
	})
}

func operationStores(clock systemClock, executable lifecycle.Executable) lifecycle.OperationStoreFactory {
	return func(records, logs operationstore.Area) lifecycle.OperationStore {
		return operationStore(clock, executable, records, logs)
	}
}

func operationStore(clock systemClock, executable lifecycle.Executable, records, logs operationstore.Area) *operationstore.Store {
	store := operationstore.New(records, logs, clock.Now).
		KeptContracts(operationstore.KeepsHosts, ceph.KeptImplementation, ceph.KeptHostImplementation).
		KeptContracts(operationstore.BlanksRoots, libvirt.RootResetImplementation, kubevirt.RootResetImplementation)
	if !executable.Modified {
		store.Running(operationstore.Executable{Version: executable.Version, Commit: executable.Commit})
	}
	return store
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
	proof := ceph.PhysicalIdentity{UUID: strings.ToLower(value.UUID), MACs: value.Addresses}
	if value.RootVolume != nil {
		proof.RootWWN = value.RootVolume.WWN
	}
	return proof, recognized, err
}

func physicalCephEraseProof(applied reconciliation.Block, removed lifecycle.BlockEvidence, uuid string) (ceph.PhysicalErasure, bool, error) {
	receipt, recognized, err := physicalerase.PhysicalEraseProof(applied, removed, uuid)
	proof := ceph.PhysicalErasure{MACs: receipt.MACs, Targets: []ceph.Device{}}
	for _, target := range receipt.Targets {
		proof.Targets = append(proof.Targets, ceph.Device{Path: target.DeviceName, WWN: target.WWN, Serial: target.SerialNumber, SizeBytes: target.SizeBytes})
	}
	return proof, recognized, err
}

func dnsServerDefinition() managedservice.Definition {
	return dnsserver.Definition(attachedGatewayRecords)
}

func registryDefinition() managedservice.Definition {
	return registry.Definition(material.BcryptPasswordFile)
}

func managedServiceDefinitions() []managedservice.Definition {
	return []managedservice.Definition{proxy.Definition(), dnsServerDefinition(), ntpserver.Definition(), registryDefinition()}
}

func attachedGatewayRecords(catalog api.Catalog) ([]managedservice.Record, error) {
	derived, err := cephcluster.AttachedGatewayRecords(catalog)
	if err != nil {
		return nil, err
	}
	records := make([]managedservice.Record, 0, len(derived))
	for _, record := range derived {
		records = append(records, managedservice.Record{Name: record.Name, Addresses: record.Addresses})
	}
	return records, nil
}
