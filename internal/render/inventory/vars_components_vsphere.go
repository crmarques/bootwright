package inventory

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	secret "github.com/crmarques/bootwright/internal/secrets"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

// defaultVSphereISOFolder is the stock datastore folder uploaded boot and
// install ISOs land in when spec.vsphere.isoStaging.folder is not authored.
const defaultVSphereISOFolder = "bootwright-vmedia"

// vsphereInventoryName reduces a value that may be authored in
// install-config inventory-path form (/dc1/host/cluster1) to the object
// NAME the community.vmware modules resolve by. vSphere object names
// cannot contain '/', so the last path segment is the name; plain names
// pass through unchanged. Folders are excluded — they resolve by path.
func vsphereInventoryName(value string) string {
	if i := strings.LastIndex(value, "/"); i >= 0 {
		return value[i+1:]
	}
	return value
}

// vSphereMachineVars projects the placement, vCenter connection, and
// ISO-staging inputs machine_substrate_vsphere and the vsphere boot/media
// roles consume for one profile-based machine. Topology values that the
// roles pass to name-resolving module parameters (computeCluster,
// datastore, resourcePool) are reduced to object names here; the authored
// inventory-path form stays on the provider vars for install-config.
func vSphereMachineVars(state v1alpha1.State, provider v1alpha1.InfraProvider, profile v1alpha1.MachineProfile, secretsDir string) map[string]any {
	spec := provider.Spec.VSphere
	fd, ok := stateview.VSphereProfileFailureDomain(spec, profile)
	if !ok {
		return nil
	}
	vc, ok := stateview.VSphereVCenterForServer(spec, fd.Server)
	if !ok {
		return nil
	}
	out := map[string]any{
		"server":                         vc.Server,
		"credentialsRef":                 vc.CredentialsRef.Name,
		"disableCertificateVerification": vc.DisableCertificateVerification,
		"failureDomain":                  fd.Name,
		"topology": map[string]any{
			"datacenter":     fd.Topology.Datacenter,
			"computeCluster": vsphereInventoryName(fd.Topology.ComputeCluster),
			"datastore":      vsphereInventoryName(fd.Topology.Datastore),
			"folder":         fd.Topology.Folder,
			"resourcePool":   vsphereInventoryName(fd.Topology.ResourcePool),
			"networks":       stringSliceAny(fd.Topology.Networks),
		},
		"isoStaging": vSphereISOStagingVars(spec, fd),
	}
	if vc.Port != 0 {
		out["port"] = vc.Port
	}
	if profile.Template != "" {
		out["template"] = profile.Template
	}
	if path := secret.ResolvePath(vc.CredentialsRef.Name, stateview.Environment(state), secretsDir); path != "" {
		out["credentialsPath"] = path
	}
	return out
}

// vSphereISOStagingVars applies the isoStaging defaults: the machine's
// failure-domain datastore and the stock vmedia folder, each independently
// overridable by spec.vsphere.isoStaging. The datastore is reduced to its
// object name — the roles pass it to name-resolving module parameters and
// build "[<name>] path" attach targets from it.
func vSphereISOStagingVars(spec *v1alpha1.InfraProviderVSphere, fd v1alpha1.VSphereFailureDomain) map[string]any {
	datastore := fd.Topology.Datastore
	folder := defaultVSphereISOFolder
	if staging := spec.ISOStaging; staging != nil {
		if staging.Datastore != "" {
			datastore = staging.Datastore
		}
		if staging.Folder != "" {
			folder = staging.Folder
		}
	}
	return map[string]any{"datastore": vsphereInventoryName(datastore), "folder": folder}
}
