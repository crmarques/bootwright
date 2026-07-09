package inventory

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	secret "github.com/crmarques/bootwright/internal/secrets"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

const defaultVSphereISOFolder = "bootwright-vmedia"

func vsphereInventoryName(value string) string {
	if i := strings.LastIndex(value, "/"); i >= 0 {
		return value[i+1:]
	}
	return value
}

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
	if path := secret.ResolvePath(vc.CredentialsRef.Name, secret.NewIndex(state), secretsDir); path != "" {
		out["credentialsPath"] = path
	}
	return out
}

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
