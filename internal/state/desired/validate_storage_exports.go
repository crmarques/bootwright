package desiredstate

import (
	"fmt"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	addoninputs "github.com/crmarques/bootwright/internal/addons/inputs"
)

func validateStorageExports(state v1alpha1.State, clusters map[string]v1alpha1.StorageCluster, pools map[string]v1alpha1.StoragePool, filesystems map[string]v1alpha1.StorageFilesystem, gateways map[string]v1alpha1.StorageObjectGateway, machines map[string]v1alpha1.Machine) []string {
	var errs []string
	seen := map[string]bool{}
	for _, export := range state.StorageExports {
		if e := validateName(v1alpha1.KindStorageExport, export.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		if seen[export.Metadata.Name] {
			errs = append(errs, fmt.Sprintf("duplicate StorageExport %q", export.Metadata.Name))
		}
		seen[export.Metadata.Name] = true
		prefix := fmt.Sprintf("StorageExport/%s spec", export.Metadata.Name)
		cluster, clusterOK := clusters[export.Spec.StorageClusterRef.Name]
		if export.Spec.StorageClusterRef.Name == "" {
			errs = append(errs, prefix+".storageClusterRef.name is required")
		} else if !clusterOK {
			errs = append(errs, fmt.Sprintf("%s.storageClusterRef.name %q does not match any StorageCluster", prefix, export.Spec.StorageClusterRef.Name))
		}
		switch export.Spec.Type {
		case v1alpha1.StorageExportTypeDataFoundation:
		case "":
			errs = append(errs, prefix+".type is required")
			continue
		default:
			errs = append(errs, fmt.Sprintf("%s.type %q must be %q", prefix, export.Spec.Type, v1alpha1.StorageExportTypeDataFoundation))
			continue
		}
		df := export.Spec.DataFoundation
		if clusterOK {
			errs = append(errs, validateStorageExportExternalDetails(state, export, cluster, machines)...)
		}
		if clusterOK && storageClusterExternal(cluster) {
			if df != nil {
				errs = append(errs, fmt.Sprintf("%s.dataFoundation must be empty when storageClusterRef points to StorageCluster/%s with spec.management=external", prefix, cluster.Metadata.Name))
			}
			continue
		}
		if df == nil {
			errs = append(errs, prefix+".dataFoundation is required when storageClusterRef points to managed Ceph")
			continue
		}
		if df.RBDPoolRef.Name == "" {
			errs = append(errs, prefix+".dataFoundation.rbdPoolRef.name is required")
		} else if pool, ok := pools[df.RBDPoolRef.Name]; !ok {
			errs = append(errs, fmt.Sprintf("%s.dataFoundation.rbdPoolRef.name %q does not match any StoragePool", prefix, df.RBDPoolRef.Name))
		} else if pool.Spec.StorageClusterRef.Name != export.Spec.StorageClusterRef.Name {
			errs = append(errs, fmt.Sprintf("%s.dataFoundation.rbdPoolRef.name %q belongs to StorageCluster/%s, want StorageCluster/%s", prefix, pool.Metadata.Name, pool.Spec.StorageClusterRef.Name, export.Spec.StorageClusterRef.Name))
		}
		if df.CephFSRef.Name == "" {
			errs = append(errs, prefix+".dataFoundation.cephFSRef.name is required")
		} else if fs, ok := filesystems[df.CephFSRef.Name]; !ok {
			errs = append(errs, fmt.Sprintf("%s.dataFoundation.cephFSRef.name %q does not match any StorageFilesystem", prefix, df.CephFSRef.Name))
		} else if fs.Spec.StorageClusterRef.Name != export.Spec.StorageClusterRef.Name {
			errs = append(errs, fmt.Sprintf("%s.dataFoundation.cephFSRef.name %q belongs to StorageCluster/%s, want StorageCluster/%s", prefix, fs.Metadata.Name, fs.Spec.StorageClusterRef.Name, export.Spec.StorageClusterRef.Name))
		}
		if df.ObjectGatewayRef.Name != "" {
			if gw, ok := gateways[df.ObjectGatewayRef.Name]; !ok {
				errs = append(errs, fmt.Sprintf("%s.dataFoundation.objectGatewayRef.name %q does not match any StorageObjectGateway", prefix, df.ObjectGatewayRef.Name))
			} else if gw.Spec.StorageClusterRef.Name != export.Spec.StorageClusterRef.Name {
				errs = append(errs, fmt.Sprintf("%s.dataFoundation.objectGatewayRef.name %q belongs to StorageCluster/%s, want StorageCluster/%s", prefix, gw.Metadata.Name, gw.Spec.StorageClusterRef.Name, export.Spec.StorageClusterRef.Name))
			}
		}
	}
	return errs
}

func validateStorageExportExternalDetails(state v1alpha1.State, export v1alpha1.StorageExport, cluster v1alpha1.StorageCluster, machines map[string]v1alpha1.Machine) []string {
	var errs []string
	prefix := fmt.Sprintf("StorageExport/%s spec.externalDetails", export.Metadata.Name)
	details := export.Spec.ExternalDetails
	if details == nil {
		if storageClusterExternal(cluster) {
			return []string{prefix + " is required when storageClusterRef points to external Ceph"}
		}
		return nil
	}
	sourceCount := 0
	if strings.TrimSpace(details.FromSecret) != "" {
		sourceCount++
	}
	if details.Generated != nil {
		sourceCount++
	}
	if details.SSHExecution != nil {
		sourceCount++
	}
	if sourceCount != 1 {
		errs = append(errs, prefix+" must set exactly one of fromSecret, generated, or sshExecution")
	}
	if storageClusterExternal(cluster) && details.Generated != nil {
		errs = append(errs, prefix+".generated must be empty when storageClusterRef points to external Ceph")
	}
	if strings.TrimSpace(details.FromSecret) != "" && !environmentDeclaresSecret(state, details.FromSecret) {
		errs = append(errs, fmt.Sprintf("%s.fromSecret %q is not declared in Environment spec.secrets", prefix, details.FromSecret))
	}
	if details.SSHExecution != nil {
		errs = append(errs, validateStorageExportSSHExecution(prefix+".sshExecution", cluster, details.SSHExecution, machines)...)
	}
	return errs
}

func validateStorageExportSSHExecution(prefix string, cluster v1alpha1.StorageCluster, spec *v1alpha1.StorageExportExternalDetailsSSHExecution, machines map[string]v1alpha1.Machine) []string {
	var errs []string
	if storageClusterExternal(cluster) && len(spec.MachineRefs) == 0 {
		errs = append(errs, prefix+".machineRefs is required when storageClusterRef points to external Ceph")
	}
	for i, ref := range spec.MachineRefs {
		owner := fmt.Sprintf("%s.machineRefs[%d].name", prefix, i)
		if ref.Name == "" {
			errs = append(errs, owner+" is required")
			continue
		}
		machine, ok := machines[ref.Name]
		if !ok {
			errs = append(errs, fmt.Sprintf("%s %q does not match any Machine", owner, ref.Name))
			continue
		}
		if !machineHasCapability(machine, v1alpha1.MachineCapabilityCephAdmin) {
			errs = append(errs, fmt.Sprintf("%s %q must reference a Machine with capability %q", owner, ref.Name, v1alpha1.MachineCapabilityCephAdmin))
		}
		if machine.Spec.Access.SSH == nil {
			errs = append(errs, fmt.Sprintf("Machine/%s spec.access.ssh is required for %s", machine.Metadata.Name, owner))
		}
	}
	if spec.Timeout != "" {
		if _, err := time.ParseDuration(spec.Timeout); err != nil {
			errs = append(errs, fmt.Sprintf("%s.timeout %q must be a Go duration such as 10m, 30m, or 1h", prefix, spec.Timeout))
		}
	}
	if spec.Exporter.Source == "" {
		errs = append(errs, prefix+".exporter.source is required")
	} else if spec.Exporter.Source != v1alpha1.StorageExportExternalDetailsExporterBoundDataFoundationAddon {
		errs = append(errs, fmt.Sprintf("%s.exporter.source %q must be %q", prefix, spec.Exporter.Source, v1alpha1.StorageExportExternalDetailsExporterBoundDataFoundationAddon))
	}
	if spec.Config.RBDDataPoolName == "" {
		errs = append(errs, prefix+".config.rbdDataPoolName is required")
	}
	if spec.Config.Format != "" && spec.Config.Format != "json" {
		errs = append(errs, fmt.Sprintf("%s.config.format %q must be %q when set", prefix, spec.Config.Format, "json"))
	}
	if spec.Config.RestrictedAuthPermission && spec.Config.ClusterName == "" {
		errs = append(errs, prefix+".config.clusterName is required when restrictedAuthPermission is true")
	}
	for i, endpoint := range spec.Config.MonitoringEndpoint {
		if strings.TrimSpace(endpoint) == "" {
			errs = append(errs, fmt.Sprintf("%s.config.monitoringEndpoint[%d] must not be empty", prefix, i))
		}
	}
	if spec.Config.MonitoringEndpointPort < 0 || spec.Config.MonitoringEndpointPort > 65535 {
		errs = append(errs, fmt.Sprintf("%s.config.monitoringEndpointPort must be between 0 and 65535", prefix))
	}
	return errs
}

func environmentDeclaresSecret(state v1alpha1.State, name string) bool {
	for _, env := range state.Environments {
		if _, ok := env.Spec.Secrets[name]; ok {
			return true
		}
	}
	return false
}

func validateStorageExportAttachmentEffects(state v1alpha1.State, exports map[string]v1alpha1.StorageExport) []string {
	var errs []string
	for _, effect := range addoninputs.EffectBindings(state, v1alpha1.ClusterAddonInputEffectStorageExportAttachment, v1alpha1.ClusterAddonProvidesDataFoundation) {
		prefix := fmt.Sprintf("ClusterAddonBinding/%s ClusterAddon/%s input[%s]", effect.Binding.Metadata.Name, effect.Addon.Name, effect.Input.Name)
		if !addonProvides(effect.Extension, v1alpha1.ClusterAddonProvidesDataFoundation) {
			errs = append(errs, fmt.Sprintf("%s effect %q with provider %q requires ClusterAddon/%s to provide %q", prefix, effect.Effect.Type, effect.Effect.Provider, effect.Addon.Name, v1alpha1.ClusterAddonProvidesDataFoundation))
		}
		exportRef := addoninputs.LocalObjectReferenceValue(effect.Input.Values, "exportRef")
		if exportRef.Name == "" {
			continue
		}
		export, exportOK := exports[exportRef.Name]
		if !exportOK {
			errs = append(errs, fmt.Sprintf("%s.values.exportRef.name %q does not match any StorageExport", prefix, exportRef.Name))
			continue
		}
		if export.Spec.Type != v1alpha1.StorageExportTypeDataFoundation {
			errs = append(errs, fmt.Sprintf("%s.values.exportRef.name %q must reference a data-foundation StorageExport", prefix, exportRef.Name))
			continue
		}
	}
	return errs
}

func addonProvides(extension v1alpha1.ClusterAddon, capability string) bool {
	for _, item := range extension.Spec.Provides {
		if item == capability {
			return true
		}
	}
	return false
}
